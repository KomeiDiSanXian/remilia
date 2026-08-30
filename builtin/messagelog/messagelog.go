// Package messagelog 提供消息历史记录（事实层）插件：内存热缓存 + SQLite 持久化。
//
// 架构设计：
//   - 每条消息通过 [MessageLogger] 中间件异步记录，不阻塞主流程
//   - 有界热缓存（per-chat ring + 全局预算 + 近似 LRU 淘汰）保留近期消息，
//     内存与群数/用户数解耦；查询优先走内存，DB 补齐后统一去重排序
//   - durable 队列 + spool 兜底，批量 flush 写入独立 SQLite（data/db/messagelog.db），
//     提交成功后才推进游标，重启重放 + event_id 幂等（0 丢失 0 重复）
//   - 出站记录：包装平台 Sender（[RecordingSender]）记录 pending → sent/failed/unknown
//     状态机；附件元数据始终落库、二进制按内容哈希存于独立目录
//   - 派生数据（词频 / 群 / 用户 / 会话统计）经 [Logger.ScanAfter] 或事件订阅
//     由独立插件消费，本包只负责「事实是什么」
//
// 部署形态：本插件为单机部署设计。热缓存 / durable 队列 / spool 均为进程内状态，
// SQLite 为单写者模型，不支持多实例共享同一数据库文件；横向扩展请按实例
// 独立部署（各自独立数据库）。
//
// 数据模型基于 platform.Event，记录 RequestID / 平台 / 群组 / 用户 / 内容 /
// 回复链 / 编辑与撤回墓碑等信息，可用于历史查询、排查（通过 RequestID 关联
// 审计日志）与派生数据重建。
//
// 使用示例（在 cmd/bot/plugins.go 中初始化）：
//
//	mlDB, _ := messagelog.OpenDB("data/db/messagelog.db")
//	messagelog.Default().UseDB(mlDB)
//	messagelog.Default().Start()
//	eng.Use(messagelog.MessageLogger())
//
// 查询接口：
//
//	// 内存热缓存 + DB 补齐（方向 / 撤回 / 附件过滤选项）
//	msgs := logger.QueryChat("groupID", 10, messagelog.QueryOptions{})
//	msgs, _ := logger.QueryRange("groupID", since, until, 1000, messagelog.QueryOptions{})
//	msg, ok := logger.QueryByEventID("groupID", "eventID")
//
//	// 全量重建（统计插件等派生数据消费者使用）
//	_ = logger.ScanAfter(time.Time{}, 0, func(e messagelog.RecordEntry) error { return nil })
//
// 注意：Clear 只清理内存缓存。数据库消息默认永久保留，如需清理请调用
// logger.Clear(before) 或配置 retention（天数 / 条数上限）定期执行。
package messagelog

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"
	"uuid"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware/ctxkeys"
	"github.com/KomeiDiSanXian/remilia/platform"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog/attachments"
)

// Logger 消息日志记录器。包含有界热缓存 + durable 队列 + 异步 SQLite 写入。
type Logger struct {
	db    *gorm.DB
	cap   int
	stop  context.CancelFunc
	wg    sync.WaitGroup
	queue *durableQueue

	flushInterval time.Duration
	batchSize     int
	queueSize     int
	spool         *spool // 引用，Stop 时关闭

	cache *cache // 有界热缓存（per-chat ring + 全局预算）

	// publisher 插件间事件发布器（统计等派生数据消费者订阅 TopicMessageRecorded）。
	publisher EventPublisher
	// stopDrainTimeout Stop 时排空剩余记录的等待上限（默认 10s；超时记录留在
	// spool，重启重放，不丢）。
	stopDrainTimeout time.Duration
	// recordedCh 已落库记录的广播队列；best-effort，满则丢弃（可经 ScanAfter 重建）。
	recordedCh chan RecordEntry
	// flushDone flushLoop 退出后关闭，广播循环据此排空收尾事件。
	flushDone chan struct{}

	outboundMu        sync.Mutex
	outboundPending   map[string]RecordEntry // event_id → pending 出站（结果阶段合并字段）
	outboundDataMu    sync.Mutex
	outboundData      map[string][]platform.Attachment // event_id → Data 附件（flush 时写入 Store）
	outboundDataBytes int64                            // outboundData 总字节数（上限保护）

	att *attachments.Manager

	retention RetentionOptions
	// attScope / attHotWindow / attMaxSize 附件下载范围与单文件上限
	// （与 attachments.Manager 解耦：元数据落库始终发生，即使未启用下载）。
	attScope     string
	attHotWindow time.Duration
	attMaxSize   int64

	// recordFailedOutbound 是否记录发送失败的出站（默认 true，见 Start）。
	recordFailedOutbound bool
	// recordSystemEvents 是否记录非消息类事件（默认 false）。
	recordSystemEvents bool
}

// New 创建一个新的 Logger，cap 为每个会话的热缓存容量。
// cap <= 0 时使用 DefaultCapacity。Start 时可经 Options 覆盖缓存预算。
func New(cap int) *Logger {
	if cap <= 0 {
		cap = DefaultCapacity
	}
	return &Logger{
		cap:                  cap,
		cache:                newCache(cap, 0, true), // 全局不限：测试/基准场景的默认
		outboundPending:      make(map[string]RecordEntry),
		outboundData:         make(map[string][]platform.Attachment),
		recordFailedOutbound: true, // 默认记录失败出站
	}
}

// OpenDB 打开或创建独立的 SQLite 数据库文件，自动建表。
// 返回的 *gorm.DB 不经过 infra/storage 插件体系，与其他插件数据完全隔离。
func OpenDB(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}
	// WAL 模式：读写不互斥，大幅降低写阻塞
	db.Exec("PRAGMA journal_mode = WAL")
	// synchronous = NORMAL：仅页面边界 fsync，消除 FlushFileBuffers 瓶颈
	db.Exec("PRAGMA synchronous = NORMAL")
	// auto_vacuum = INCREMENTAL：跟踪空闲页，Clear 时回收磁盘空间
	db.Exec("PRAGMA auto_vacuum = INCREMENTAL")
	// busy_timeout：多连接（MaxOpenConns>1）下写锁竞争时等待而非立即 SQLITE_BUSY
	db.Exec("PRAGMA busy_timeout = 5000")

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// WAL 多读者：放开单连接限制供查询并发读；SQLite 同一时刻仅一个写者，
	// 写仍由 flushLoop 单写者串行化（写路径天然不会并行）。
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)
	if err := db.AutoMigrate(&MessageRecord{}, &MessageMention{}, &AttachmentRecord{}); err != nil {
		return nil, err
	}
	// 迁移：遗留库一步升级 + 全新库幂等（列回填 + raw 索引 + user_version，见 migrate.go）
	if err := migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

// UseDB 绑定外部数据库实例到 Logger。
func (l *Logger) UseDB(db *gorm.DB) {
	l.db = db
}

// DB 返回底层 SQLite 句柄（只读/高级查询用）。
// 热缓存与持久化写入仍应走 Record / Query* API，此方法供需要直接
// 访问事实表的消费者（如测试、迁移工具）使用。
func (l *Logger) DB() *gorm.DB { return l.db }

// SetEventPublisher 注入插件间事件发布器（cmd/bot 使用 plugin.Manager 的 EventBus）。
// 必须在 Start 之前调用；Start 后设置不生效（广播循环已按当时状态启动）。
func (l *Logger) SetEventPublisher(pub EventPublisher) {
	l.publisher = pub
}

// Options 控制 Logger 的资源边界（队列 / spool / flush 批参数）。
// 零值使用默认值。
type Options struct {
	CacheIdleEvict *bool // nil = 默认 true（空闲会话优先淘汰）
	// RecordFailedOutbound 是否记录发送失败的出站；nil = 默认 true。
	RecordFailedOutbound *bool
	SpoolDir             string
	FlushInterval        time.Duration // 默认 500ms
	BatchSize            int           // 默认 1000
	QueueSize            int           // 默认 50000
	SpoolMaxSize         int64         // 默认 1GB
	CachePerChat         int           // 默认 200
	CacheGlobal          int           // 默认 50000；< 0 = 全局不限
	SpoolEnabled         bool
	// RecordSystemEvents 是否记录非消息类事件（进群退群等）；默认 false。
	RecordSystemEvents bool
	// Attachments 附件生命周期配置；nil = 不启用附件管理（仅测试/无附件场景）。
	Attachments *AttachmentOptions
	// Retention 历史保留策略；nil = 不启用后台清理。
	Retention *RetentionOptions
	// StopDrainTimeout Stop 排空剩余记录的等待上限；<=0 使用默认 10s。
	StopDrainTimeout time.Duration
}

// AttachmentOptions 附件生命周期（由 config.MessagelogAttachmentsConfig 翻译）。
type AttachmentOptions struct {
	Dir                 string
	Scope               string // hot_window / none / all
	DownloadBackoff     []time.Duration
	HotWindowAge        time.Duration // scope=hot_window 时的时间窗口
	MaxDiskUsage        int64         // 附件磁盘预算（soft limit）
	MaxPending          int           // 待下载任务上限
	DownloadConcurrency int
	DownloadRetries     int
	MaxSize             int64
	RatePerHost         float64 // 每秒请求数
	GracePeriod         time.Duration
	BackfillBatch       int
	IdleThreshold       float64
	BackfillAttempts    int
	BackfillInterval    time.Duration
	GCInterval          time.Duration
	LazyFallback        bool
	GCEnabled           bool
	BackfillEnabled     bool
}

// RetentionOptions 历史保留策略。
// Days / MaxEntries 同时配置时按更严格者删除最旧记录。
type RetentionOptions struct {
	Days            int
	MaxEntries      int
	CleanupInterval time.Duration
}

// Start 启动后台异步写入 goroutine。
// 在注册 MessageLogger 中间件前必须调用。
func (l *Logger) Start(opts ...Options) {
	if l.queue != nil {
		return
	}
	o := Options{}
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = 500 * time.Millisecond
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 1000
	}
	if o.QueueSize <= 0 {
		o.QueueSize = 50000
	}
	if o.SpoolMaxSize <= 0 {
		o.SpoolMaxSize = 1 << 30 // 1GB
	}

	l.flushInterval = o.FlushInterval
	l.batchSize = o.BatchSize
	l.queueSize = o.QueueSize
	l.stopDrainTimeout = o.StopDrainTimeout
	if l.stopDrainTimeout <= 0 {
		l.stopDrainTimeout = 10 * time.Second
	}
	l.recordFailedOutbound = true
	if o.RecordFailedOutbound != nil {
		l.recordFailedOutbound = *o.RecordFailedOutbound
	}
	l.recordSystemEvents = o.RecordSystemEvents

	// 重启 reconcile：上次运行遗留的 pending 出站无法确定结果，标记 unknown
	// （不伪造 sent/failed；重启时不存在合法在途 pending）。
	if l.db != nil {
		if err := l.db.Model(&MessageRecord{}).
			Where("is_outbound = ? AND send_status = ?", true, string(SendStatusPending)).
			Update("send_status", string(SendStatusUnknown)).Error; err != nil {
			logger.WithError(err).Warn("[MessageLog] Failed to reconcile pending outbound on start")
		}
	}

	// 有界热缓存：默认 per-chat 200 / 全局 50000，内存与群数解耦。
	perChat := o.CachePerChat
	if perChat <= 0 {
		perChat = 200
	}
	global := o.CacheGlobal
	if global == 0 {
		global = 50000
	}
	idleEvict := true
	if o.CacheIdleEvict != nil {
		idleEvict = *o.CacheIdleEvict
	}
	l.cache = newCache(perChat, global, idleEvict)

	var sp *spool
	if o.SpoolEnabled {
		sp, _ = openSpool(o.SpoolDir, o.SpoolMaxSize)
		if sp == nil {
			logger.Warn("[MessageLog] spool init failed, falling back to bounded queue with drop-on-full")
		} else {
			l.spool = sp
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	l.stop = cancel
	l.queue = newDurableQueue(o.QueueSize, sp)

	// 附件下载范围/上限默认值（元数据落库不依赖 manager）
	l.attScope = "hot_window"
	l.attHotWindow = 24 * time.Hour
	l.attMaxSize = 20 << 20
	if o.Attachments != nil {
		if o.Attachments.Scope != "" {
			l.attScope = o.Attachments.Scope
		}
		if o.Attachments.HotWindowAge > 0 {
			l.attHotWindow = o.Attachments.HotWindowAge
		}
		if o.Attachments.MaxSize > 0 {
			l.attMaxSize = o.Attachments.MaxSize
		}
	}

	// 附件生命周期（best-effort 层）：仅在有目录配置时启用
	if o.Attachments != nil && o.Attachments.Dir != "" && l.db != nil {
		cfg := attachments.Config{
			Dir:              o.Attachments.Dir,
			Scope:            o.Attachments.Scope,
			HotWindowAge:     o.Attachments.HotWindowAge,
			MaxDiskUsage:     o.Attachments.MaxDiskUsage,
			MaxPending:       o.Attachments.MaxPending,
			Concurrency:      o.Attachments.DownloadConcurrency,
			Retries:          o.Attachments.DownloadRetries,
			Backoff:          o.Attachments.DownloadBackoff,
			MaxSize:          o.Attachments.MaxSize,
			RatePerHost:      o.Attachments.RatePerHost,
			LazyFallback:     o.Attachments.LazyFallback,
			GCEnabled:        o.Attachments.GCEnabled,
			GracePeriod:      o.Attachments.GracePeriod,
			BackfillEnabled:  o.Attachments.BackfillEnabled,
			BackfillBatch:    o.Attachments.BackfillBatch,
			IdleThreshold:    o.Attachments.IdleThreshold,
			BackfillAttempts: o.Attachments.BackfillAttempts,
			BackfillInterval: o.Attachments.BackfillInterval,
			GCInterval:       o.Attachments.GCInterval,
		}
		if mgr, err := attachments.NewManager(l.db, cfg); err == nil {
			l.att = mgr
			mgr.Start(ctx)
		} else {
			logger.WithError(err).Warn("[MessageLog] attachments manager init failed, attachment download disabled")
		}
	}

	// 历史保留：配置了 days/max_entries 时启动后台清理
	if o.Retention != nil {
		l.retention = *o.Retention
		if l.retention.CleanupInterval <= 0 {
			l.retention.CleanupInterval = time.Hour
		}
		if l.retention.Days > 0 || l.retention.MaxEntries > 0 {
			l.wg.Add(1)
			go l.retentionLoop(ctx)
		}
	}

	// 事件广播：仅在注入了发布器时启动（统计插件等派生数据消费者）。
	if l.publisher != nil {
		l.recordedCh = make(chan RecordEntry, 10000)
		l.flushDone = make(chan struct{})
		l.wg.Add(1)
		go l.eventBroadcastLoop(ctx)
	}

	l.wg.Add(1)
	go l.flushLoop(ctx)
}

// Stop 停止后台写入，等待已提交数据全部落盘。
func (l *Logger) Stop() {
	if l.stop != nil {
		l.stop()
	}
	l.wg.Wait()
	if l.queue != nil {
		l.queue.close()
	}
	if l.att != nil {
		l.att.Stop()
	}
}

// flushLoop 后台批量写入循环：从 durable queue 取批 → SQLite 事务 →
// 成功后推进 spool cursor；失败则回写 spool（不丢，下轮重试）。
func (l *Logger) flushLoop(ctx context.Context) {
	defer l.wg.Done()
	defer func() {
		if l.flushDone != nil {
			close(l.flushDone)
		}
	}()
	ticker := time.NewTicker(l.flushInterval)
	defer ticker.Stop()

	flush := func() bool {
		recs := l.queue.drain(l.batchSize)
		if len(recs) == 0 {
			return true
		}
		if l.db == nil {
			// 无 DB（测试场景）：仅消费队列，不落盘
			return true
		}

		msgBatch := make([]MessageRecord, 0, len(recs))
		var mentionBatch []MessageMention
		var attBatch []AttachmentRecord
		var attTmp []string
		for _, r := range recs {
			msgBatch = append(msgBatch, recordToModel(r.job))
			if ms := entryToMentions(r.job); len(ms) > 0 {
				mentionBatch = append(mentionBatch, ms...)
			}
			if rows, tmps := l.buildAttachmentRows(r.job); len(rows) > 0 {
				attBatch = append(attBatch, rows...)
				attTmp = append(attTmp, tmps...)
			}
		}
		mlMetricsInst.recordsTotal.Add(float64(len(recs)))

		start := time.Now()
		tx := l.db.Begin()
		if tx.Error != nil {
			l.queue.requeue(recs)
			mlMetricsInst.flushFailedTotal.Inc()
			logger.WithError(tx.Error).Warn("[MessageLog] Failed to begin transaction")
			return false
		}
		// UPSERT on event_id：普通写入幂等（重放恰好一条）；出站 pending →
		// sent/failed 的完成记录覆盖状态，最终状态以完成记录为准。
		ok := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_id"}},
			DoUpdates: clause.AssignmentColumns(recordUpsertColumns),
		}).CreateInBatches(msgBatch, 500).Error == nil
		if ok && len(mentionBatch) > 0 {
			// mentions 由 (event_id, mention_id) 唯一索引保证幂等（见 migrate.go）
			ok = tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(mentionBatch, 500).Error == nil
		}
		if ok && len(attBatch) > 0 && l.att != nil {
			// 出站 Data 附件两阶段提交：rename 与引用行插入同在 flush 写事务内，
			// 与附件 GC 删除（BEGIN IMMEDIATE）串行化，杜绝悬空引用。
			for i, tmp := range attTmp {
				if tmp == "" {
					continue
				}
				if err := l.att.Store().CommitTemp(attBatch[i].StorageKey, tmp); err != nil {
					_ = l.att.Store().DiscardTemp(tmp)
					ok = false
					logger.WithError(err).Warn("[MessageLog] failed to commit attachment binary")
					break
				}
			}
		}
		if ok && len(attBatch) > 0 {
			// 附件引用行幂等：唯一索引 (event_id, url, name)（见 migrate.go）
			ok = tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(attBatch, 200).Error == nil
		}
		if ok {
			ok = tx.Commit().Error == nil
		} else {
			tx.Rollback()
		}
		if !ok {
			// 事务失败：清理未提交的临时文件（已 CommitTemp 的要么已 rename、
			// 要么去重丢弃，DiscardTemp 对缺失文件是 no-op）
			if l.att != nil {
				for _, tmp := range attTmp {
					if tmp != "" {
						_ = l.att.Store().DiscardTemp(tmp)
					}
				}
			}
			l.queue.requeue(recs)
			mlMetricsInst.flushFailedTotal.Inc()
			logger.Warn("[MessageLog] Failed to flush message batch, records kept in spool")
			return false
		}
		mlMetricsInst.flushTotal.Inc()
		mlMetricsInst.dbWriteLatency.Observe(time.Since(start).Seconds())
		// 提交成功后：入队待下载附件（pending），并释放出站 Data 缓冲
		if l.att != nil {
			var ids []int64
			for _, ar := range attBatch {
				if ar.Status == string(attachments.StatusPending) && ar.ID > 0 {
					ids = append(ids, ar.ID)
				}
			}
			if len(ids) > 0 {
				l.att.Enqueue(ids)
			}
		}
		eventIDs := make([]string, 0, len(recs))
		for _, r := range recs {
			eventIDs = append(eventIDs, r.job.EventID)
		}
		l.enqueueRecorded(recs)
		l.clearOutboundData(eventIDs)
		l.queue.ack(recs)
		return true
	}

	for {
		select {
		case <-ctx.Done():
			// 排空剩余任务再落盘；提交失败则留在 spool，重启重放
			deadline := time.Now().Add(l.stopDrainTimeout)
			for time.Now().Before(deadline) {
				if !flush() {
					return
				}
				if l.queue.depth() == 0 && (l.queue.spool == nil || l.queue.spool.unacked() == 0) {
					return
				}
			}
			logger.Warn("[MessageLog] Stop drain timeout, records remain in spool for replay")
			return
		case <-l.queue.notify:
			flush()
		case <-ticker.C:
			flush()
		}
	}
}

// enqueueRecorded 将本批已提交记录放入广播队列（非阻塞）。
// 队列满则丢弃并告警：事件仅用于派生数据增量，丢失可经 ScanAfter 全量重建。
func (l *Logger) enqueueRecorded(recs []queuedRecord) {
	if l.recordedCh == nil {
		return
	}
	for _, r := range recs {
		select {
		case l.recordedCh <- r.job:
		default:
			mlMetricsInst.recordedDroppedTotal.Inc()
			logger.Warn("[MessageLog] recorded event channel full, event dropped (rebuild via ScanAfter)")
		}
	}
}

// eventBroadcastLoop 将已落库记录广播给派生数据消费者（统计插件等）。
// 停止时等待 flushLoop 退出后排空剩余事件，保证已提交记录全部广播。
func (l *Logger) eventBroadcastLoop(ctx context.Context) {
	defer l.wg.Done()
	for {
		select {
		case <-ctx.Done():
			// 等 flushLoop 排空完成（其内部有 10s 截止），再消费剩余事件
			if l.flushDone != nil {
				<-l.flushDone
			}
			for {
				select {
				case e := <-l.recordedCh:
					l.publishRecorded(e)
				default:
					return
				}
			}
		case e := <-l.recordedCh:
			l.publishRecorded(e)
		}
	}
}

// publishRecorded 发布单条消息记录事件；发布失败仅告警（可经 ScanAfter 重建）。
func (l *Logger) publishRecorded(e RecordEntry) {
	if l.publisher == nil {
		return
	}
	if err := l.publisher.PublishContext(context.Background(), TopicMessageRecorded, MessageRecorded{Record: e}); err != nil {
		mlMetricsInst.recordedDroppedTotal.Inc()
		logger.WithError(err).Warn("[MessageLog] failed to broadcast message recorded event")
	}
}

// recordToModel 将 RecordEntry 转换为 GORM 模型。
func recordToModel(e RecordEntry) MessageRecord {
	return MessageRecord{
		RequestID:         e.RequestID,
		Platform:          e.Platform,
		Kind:              e.Kind,
		EventID:           e.EventID,
		PlatformMessageID: e.PlatformMessageID,
		ChatID:            e.ChatID,
		ChatName:          e.ChatName,
		ParentID:          e.ParentID,
		IsGroup:           e.IsGroup,
		UserID:            e.UserID,
		UserName:          e.UserName,
		UserRole:          e.UserRole,
		Content:           e.Content,
		ReplyToID:         e.ReplyToID,
		ReplyToEventID:    e.ReplyToEventID,
		ReplyToMessageID:  e.ReplyToMessageID,
		RawType:           e.RawType,
		Timestamp:         e.Timestamp.UnixNano(),
		CreatedAt:         e.CreatedAt.UnixNano(),
		IsOutbound:        e.IsOutbound,
		SendStatus:        string(e.SendStatus),
		LastError:         e.LastError,
		TriggeredBy:       e.TriggeredBy,
		IsEdited:          e.IsEdited,
		EditedAt:          e.EditedAt.UnixNano(),
		IsRecalled:        e.IsRecalled,
		RecalledAt:        e.RecalledAt.UnixNano(),
	}
}

// recordUpsertColumns 冲突（同 event_id）时更新的列：完整覆盖可变字段，
// 使 pending → sent/failed 的完成记录能覆盖状态（幂等重放同结果）。
var recordUpsertColumns = []string{
	"request_id", "platform", "kind", "platform_message_id",
	"chat_id", "chat_name", "parent_id", "user_id", "user_name", "user_role",
	"content", "reply_to_id", "reply_to_event_id", "reply_to_message_id",
	"raw_type", "send_status", "last_error", "triggered_by",
	"timestamp", "created_at", "edited_at", "recalled_at",
	"is_group", "is_outbound", "is_edited", "is_recalled",
}

// entryToMentions 将 RecordEntry 中的 Mentions 转换为 MessageMention 切片。
func entryToMentions(e RecordEntry) []MessageMention {
	if len(e.Mentions) == 0 {
		return nil
	}
	out := make([]MessageMention, 0, len(e.Mentions))
	for _, m := range e.Mentions {
		out = append(out, MessageMention{
			EventID:     e.EventID,
			MentionID:   m.ID,
			DisplayName: m.DisplayName,
			IsBot:       m.IsBot,
			IsSelf:      m.IsSelf,
		})
	}
	return out
}

// buildAttachmentRows 为一条记录构建附件引用行（元数据始终落库）。
// 返回 (行, 与行对齐的临时文件路径)：Data 附件走两阶段写入，tmp 由
// flush 事务内 CommitTemp 提交，保证「二进制可见」与「引用行就绪」在
// SQLite 写锁内原子（与附件 GC 删除串行化，杜绝悬空引用）。
//
//   - URL 附件：按下载范围策略取初始状态（pending → 入队下载；pending_lazy → 回填/懒加载）；
//   - Data 附件（出站二进制直传）：字节经 outboundData 缓冲带到这里，写入
//     AttachmentStore 临时文件并在 flush 事务内提交后标记 ready；写入失败
//     降级 pending_lazy（元数据保留）。
//
// 幂等：唯一索引 (event_id, url, name) + ON CONFLICT DO NOTHING，重放恰好一行。
func (l *Logger) buildAttachmentRows(e RecordEntry) ([]AttachmentRecord, []string) {
	if len(e.Attachments) == 0 && !l.hasOutboundData(e.EventID) {
		return nil, nil
	}
	now := time.Now().Unix()
	data := l.peekOutboundData(e.EventID)
	dataUsed := make(map[int]bool)

	rows := make([]AttachmentRecord, 0, len(e.Attachments)+len(data))
	tmps := make([]string, 0, len(e.Attachments)+len(data))
	for _, am := range e.Attachments {
		if am.URL != "" {
			rows = append(rows, AttachmentRecord{
				EventID:      e.EventID,
				Type:         am.Type,
				URL:          am.URL,
				Name:         am.Name,
				MimeType:     am.MimeType,
				Size:         am.Size,
				Status:       attachments.InitialStatus(l.attScope, l.attHotWindow, e.Timestamp),
				URLExpiresAt: attachments.URLExpiryUnix(am.URL),
				CreatedAt:    now,
			})
			tmps = append(tmps, "")
			continue
		}
		// URL 为空：优先匹配出站 Data 附件（同 Type+Name），否则 pending_lazy 兜底
		status := attachments.StatusPendingLazy
		lastErr := ""
		key, size, tmp := "", int64(0), ""
		for j, att := range data {
			if dataUsed[j] || string(att.Kind) != am.Type || att.Name != am.Name {
				continue
			}
			dataUsed[j] = true
			var err error
			key, size, tmp, err = l.storeAttachmentTemp(att)
			if err != nil {
				lastErr = "store data attachment: " + err.Error()
				if len(lastErr) > 256 {
					lastErr = lastErr[:256]
				}
			} else {
				status = attachments.StatusReady
			}
			break
		}
		rows = append(rows, AttachmentRecord{
			EventID:    e.EventID,
			Type:       am.Type,
			URL:        am.URL,
			Name:       am.Name,
			MimeType:   am.MimeType,
			Size:       size,
			SHA256:     key,
			StorageKey: key,
			Status:     status,
			LastError:  lastErr,
			CreatedAt:  now,
		})
		tmps = append(tmps, tmp)
	}
	return rows, tmps
}

// storeAttachmentTemp 把出站 Data 附件写入 AttachmentStore 临时文件，
// 返回 storage key、大小与临时路径（由调用方在写锁事务内 CommitTemp 提交）。
func (l *Logger) storeAttachmentTemp(att platform.Attachment) (string, int64, string, error) {
	if l.att == nil || len(att.Data) == 0 {
		return "", 0, "", errors.New("attachment store unavailable")
	}
	return l.att.Store().PutTemp(bytes.NewReader(att.Data), l.attMaxSize)
}

// attachmentMetas 把 platform.Attachment 列表转为元数据视图（不含二进制）。
func attachmentMetas(atts []platform.Attachment) []AttachmentMeta {
	if len(atts) == 0 {
		return nil
	}
	out := make([]AttachmentMeta, 0, len(atts))
	for _, a := range atts {
		out = append(out, AttachmentMeta{
			Type:     string(a.Kind),
			Name:     a.Name,
			MimeType: a.MimeType,
			Size:     int64(a.Size),
			URL:      a.URL,
		})
	}
	return out
}

// setOutboundData 暂存出站 Data 附件字节（flush 时写入 Store）。
// 带总量上限（异常路径保护：DB 持续失败时不会无限占内存，元数据不受影响）。
func (l *Logger) setOutboundData(eventID string, atts []platform.Attachment) {
	if eventID == "" || len(atts) == 0 {
		return
	}
	var data []platform.Attachment
	var total int64
	for _, a := range atts {
		if len(a.Data) == 0 {
			continue
		}
		data = append(data, a)
		total += int64(len(a.Data))
	}
	if len(data) == 0 {
		return
	}
	l.outboundDataMu.Lock()
	defer l.outboundDataMu.Unlock()
	const maxOutboundDataBytes = 64 << 20 // 64MB
	if l.outboundDataBytes+total > maxOutboundDataBytes {
		logger.Warn("[MessageLog] outbound data buffer over limit, attachment binary dropped (metadata kept)")
		return
	}
	l.outboundData[eventID] = data
	l.outboundDataBytes += total
}

func (l *Logger) hasOutboundData(eventID string) bool {
	l.outboundDataMu.Lock()
	defer l.outboundDataMu.Unlock()
	_, ok := l.outboundData[eventID]
	return ok
}

// peekOutboundData 读取（不消费）eventID 的 Data 附件。
func (l *Logger) peekOutboundData(eventID string) []platform.Attachment {
	l.outboundDataMu.Lock()
	defer l.outboundDataMu.Unlock()
	return l.outboundData[eventID]
}

// clearOutboundData 在 flush 提交成功后释放 Data 缓冲。
func (l *Logger) clearOutboundData(eventIDs []string) {
	if len(eventIDs) == 0 {
		return
	}
	l.outboundDataMu.Lock()
	for _, id := range eventIDs {
		if atts, ok := l.outboundData[id]; ok {
			for _, a := range atts {
				l.outboundDataBytes -= int64(len(a.Data))
			}
			delete(l.outboundData, id)
		}
	}
	l.outboundDataMu.Unlock()
}

// modelToEntry 将 GORM 模型转换为 RecordEntry（mentions 需外部注入）。
func modelToEntry(m MessageRecord, mentions []platform.UserInfo) RecordEntry {
	// 兼容旧行：迁移前的行只有 reply_to_id（= 平台消息 ID）；迁移已回填到
	// reply_to_message_id，此处兜底防御。
	replyToMessageID := m.ReplyToMessageID
	if replyToMessageID == "" {
		replyToMessageID = m.ReplyToID
	}
	return RecordEntry{
		RequestID:         m.RequestID,
		Platform:          m.Platform,
		Kind:              m.Kind,
		EventID:           m.EventID,
		PlatformMessageID: m.PlatformMessageID,
		ChatID:            m.ChatID,
		ChatName:          m.ChatName,
		ParentID:          m.ParentID,
		IsGroup:           m.IsGroup,
		UserID:            m.UserID,
		UserName:          m.UserName,
		UserRole:          m.UserRole,
		Content:           m.Content,
		ReplyToID:         m.ReplyToID,
		ReplyToEventID:    m.ReplyToEventID,
		ReplyToMessageID:  replyToMessageID,
		RawType:           m.RawType,
		Mentions:          mentions,
		Timestamp:         time.Unix(0, m.Timestamp),
		CreatedAt:         time.Unix(0, m.CreatedAt),
		IsOutbound:        m.IsOutbound,
		SendStatus:        SendStatus(m.SendStatus),
		LastError:         m.LastError,
		TriggeredBy:       m.TriggeredBy,
		IsEdited:          m.IsEdited,
		EditedAt:          time.Unix(0, m.EditedAt),
		IsRecalled:        m.IsRecalled,
		RecalledAt:        time.Unix(0, m.RecalledAt),
	}
}

// RecordAsync 从一个 platform.Event + Context 中提取全量信息，
// 异步写入 SQLite，同时写入内存 ring buffer 热缓存。
//
// 提取的信息包括：RequestID、平台、事件类型、群/用户、内容、回复链、原始类型等。
func (l *Logger) RecordAsync(ev platform.Event, ctx *eventctx.Context) {
	if !l.recordSystemEvents && !isMessageKind(ev.Kind()) {
		return
	}
	e := eventToEntry(ev, ctx)
	l.Record(e)
}

// isMessageKind 判断事件是否属于消息类（默认只记录消息类事件，避免系统事件噪音）。
func isMessageKind(k platform.EventKind) bool {
	switch k {
	case platform.EventKindPrivateMessage, platform.EventKindGroupMessage,
		platform.EventKindGuildMessage:
		return true
	default:
		return false
	}
}

// OnOutbound 实现 eventctx.OutboundObserver，记录经 ctx.Reply* 发送的出站消息。
//
// 发送结果以 send_status 记录：成功 = sent；失败 = failed（默认记录，可配置关闭）。
// 无文本内容（纯附件/卡片消息）同样记录「发送过一条消息」的事实。
// 注意：仅覆盖经框架 ctx.Reply* 的出站；插件直接调用 platform.Sender.Send
// （绕过 ctx.Reply）不会被观察到，此类路径应使用 [RecordingSender]。
func (l *Logger) OnOutbound(chatID string, req platform.SendRequest, res platform.SendResult, err error) {
	if chatID == "" {
		return
	}
	if err != nil && !l.recordFailedOutbound {
		return
	}
	e := outboundEntry(chatID, req)
	e.EventID = uuid.NewV7().String()
	l.setOutboundData(e.EventID, req.Message.Attachments)
	e.Platform = res.Platform
	if err != nil {
		e.SendStatus = SendStatusFailed
		e.LastError = truncateError(err)
	} else {
		e.SendStatus = SendStatusSent
		e.PlatformMessageID = res.MessageID
	}
	l.Record(e)
}

// eventToEntry 从 platform.Event 和 Context 提取完整的消息记录。
func eventToEntry(ev platform.Event, ctx *eventctx.Context) RecordEntry {
	chat := ev.Chat()
	sender := ev.Sender()

	requestID, _ := ctx.Get(ctxkeys.CtxKeyRequestID)
	rid, _ := requestID.(string)

	// Reply 单一真相源是段（GetReplyToID 段优先、接口兜底）；
	// 不直接断言 ReplyEvent，避免绕过段模型（与 AI 回复上下文同类问题）。
	replyToID := platform.GetReplyToID(ev)

	mentions := platform.GetMentions(ev)

	return RecordEntry{
		RequestID: rid,
		Platform:  ev.Platform(),
		Kind:      string(ev.Kind()),
		// 逻辑身份（EventID）由统一 Record 入口分配 UUIDv7
		PlatformMessageID: ev.ID(),
		ChatID:            chat.ID,
		ChatName:          chat.Name,
		ParentID:          chat.ParentID,
		IsGroup:           chat.IsGroup,
		UserID:            sender.ID,
		UserName:          sender.DisplayName,
		UserRole:          groupRoleString(sender.GroupRole),
		Content:           platform.Content(ev),
		ReplyToID:         replyToID,
		ReplyToMessageID:  replyToID, // 回复目标按平台消息 ID 记录，查询时解析
		RawType:           platform.RawType(ev),
		Mentions:          mentions,
		Attachments:       attachmentMetas(platform.Attachments(ev)),
		Timestamp:         ev.Timestamp(),
		CreatedAt:         time.Now(),
	}
}

func groupRoleString(r platform.GroupRole) string {
	switch r {
	case platform.GroupRoleOwner:
		return "owner"
	case platform.GroupRoleAdmin:
		return "admin"
	case platform.GroupRoleMember:
		return "member"
	default:
		return ""
	}
}

// loadMentions 批量加载多条消息的 @ 列表，按 EventID 分组。
func (l *Logger) loadMentions(eventIDs []string) map[string][]platform.UserInfo {
	if l.db == nil || len(eventIDs) == 0 {
		return nil
	}
	var models []MessageMention
	if err := l.db.Where("event_id IN ?", eventIDs).Find(&models).Error; err != nil {
		logger.WithError(err).Warn("[MessageLog] Failed to load mentions")
		return nil
	}
	m := make(map[string][]platform.UserInfo, len(eventIDs))
	for _, mm := range models {
		m[mm.EventID] = append(m[mm.EventID], platform.UserInfo{
			ID:          mm.MentionID,
			DisplayName: mm.DisplayName,
			IsBot:       mm.IsBot,
			IsSelf:      mm.IsSelf,
		})
	}
	return m
}

// loadAttachments 批量加载多条消息的附件元数据，按 EventID 分组。
func (l *Logger) loadAttachments(models []MessageRecord) map[string][]AttachmentMeta {
	if l.db == nil || len(models) == 0 {
		return nil
	}
	eventIDs := make([]string, 0, len(models))
	for _, m := range models {
		eventIDs = append(eventIDs, m.EventID)
	}
	var rows []AttachmentRecord
	if err := l.db.Where("event_id IN ?", eventIDs).Find(&rows).Error; err != nil {
		logger.WithError(err).Warn("[MessageLog] Failed to load attachments")
		return nil
	}
	out := make(map[string][]AttachmentMeta, len(models))
	for _, r := range rows {
		out[r.EventID] = append(out[r.EventID], attachmentMetaFromRecord(r))
	}
	return out
}

func attachmentMetaFromRecord(r AttachmentRecord) AttachmentMeta {
	return AttachmentMeta{
		ID:         r.ID,
		Type:       r.Type,
		Name:       r.Name,
		MimeType:   r.MimeType,
		Size:       r.Size,
		URL:        r.URL,
		Status:     r.Status,
		StorageKey: r.StorageKey,
	}
}

// --- 清理 ---

// Clear 删除时间戳早于 before 的消息：内存热缓存裁剪 + SQLite 删除。
func (l *Logger) Clear(before time.Time) {
	if l.cache != nil {
		l.cache.pruneBefore(before)
	}

	if l.db != nil {
		l.deleteMessagesBefore(before)
	}
}

// retentionLoop 后台历史保留清理：按 retention_days / max_entries 删除最旧记录。
func (l *Logger) retentionLoop(ctx context.Context) {
	defer l.wg.Done()
	ticker := time.NewTicker(l.retention.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.applyRetention()
		}
	}
}

// applyRetention 执行一轮保留清理（days 与 max_entries 同时配置时按更严格者）。
func (l *Logger) applyRetention() {
	if l.db == nil {
		return
	}
	cleaned := false
	if l.retention.Days > 0 {
		cutoff := time.Now().Add(-time.Duration(l.retention.Days) * 24 * time.Hour)
		l.deleteMessagesBefore(cutoff)
		cleaned = true
	}
	if l.retention.MaxEntries > 0 {
		var total int64
		if err := l.db.Model(&MessageRecord{}).Count(&total).Error; err == nil && total > int64(l.retention.MaxEntries) {
			var ids []int64
			if err := l.db.Model(&MessageRecord{}).Order("id ASC").
				Limit(int(total-int64(l.retention.MaxEntries))).Pluck("id", &ids).Error; err == nil && len(ids) > 0 {
				l.deleteMessageIDs(ids)
				cleaned = true
			}
		}
	}
	if cleaned {
		// 附件引用已删：触发二进制 GC（grace_period 过后回收）
		if l.att != nil {
			l.att.TriggerGC()
		}
		// DELETE 后回收空闲页，控制 DB 文件膨胀
		l.db.Exec("PRAGMA incremental_vacuum(500)")
	}
}

// deleteMessagesBefore 删除时间早于 before 的消息及其 mentions / 附件引用。
func (l *Logger) deleteMessagesBefore(before time.Time) {
	if l.db == nil {
		return
	}
	cutoff := before.UnixNano()
	var ids []int64
	l.db.Model(&MessageRecord{}).Where("timestamp < ?", cutoff).Pluck("id", &ids)
	if len(ids) == 0 {
		return
	}
	l.deleteMessageIDs(ids)
}

// deleteMessageIDs 按 message_records.id 删除消息及其关联行。
// mentions / 附件引用 / 消息在同一事务内删除：失败整体回滚，避免残留
// 孤儿关联行（附件引用残留会导致二进制 GC 永远无法回收）。
func (l *Logger) deleteMessageIDs(ids []int64) {
	if l.db == nil || len(ids) == 0 {
		return
	}
	// 分批避免 SQLite 变量上限（999）
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		chunk := ids[start:end]
		var eventIDs []string
		l.db.Model(&MessageRecord{}).Where("id IN ?", chunk).Pluck("event_id", &eventIDs)
		if err := l.db.Transaction(func(tx *gorm.DB) error {
			if len(eventIDs) > 0 {
				if err := tx.Where("event_id IN ?", eventIDs).Delete(&MessageMention{}).Error; err != nil {
					return err
				}
				if err := tx.Where("event_id IN ?", eventIDs).Delete(&AttachmentRecord{}).Error; err != nil {
					return err
				}
			}
			return tx.Where("id IN ?", chunk).Delete(&MessageRecord{}).Error
		}); err != nil {
			logger.WithError(err).Warn("[MessageLog] Failed to clear old messages from DB")
		}
	}
}

// --- 统计 ---

// GroupCount 返回热缓存中有条目的会话数量。
func (l *Logger) GroupCount() int {
	if l.cache == nil {
		return 0
	}
	return l.cache.chatCount()
}

// UserCount 兼容占位：热缓存按会话维度存储，不再维护独立的用户维度。
func (l *Logger) UserCount() int {
	return 0
}

// GroupMessageCount 返回群 groupID 在热缓存中的消息数量。
func (l *Logger) GroupMessageCount(groupID string) int {
	if l.cache == nil {
		return 0
	}
	return l.cache.entryCount(groupID)
}

// --- 全局默认实例 ---

var defaultLogger = New(DefaultCapacity)

// Default 返回全局默认 Logger 实例。
func Default() *Logger { return defaultLogger }

// MessageLogger 返回一个中间件，自动异步记录每一条平台事件。
//
// 必须在使用前调用 Default().UseDB(db) 和 Default().Start()。
// 依赖于 RequestID 中间件先执行（应在 MessageLogger 之前注册）。
//
// 该中间件同时注入出站观察者（OutboundObserverExt），使所有经 ctx.Reply*
// 发送的出站消息（任何插件）在发送完成后被记录（见 Logger.OnOutbound）。
func MessageLogger() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			if defaultLogger.queue != nil {
				ctx.Ext().SetTyped(eventctx.OutboundObserverExt{Observer: defaultLogger})
			}
			err := next(ctx)
			pe := ctx.GetPlatformEvent()
			if pe != nil && defaultLogger.queue != nil {
				defaultLogger.RecordAsync(pe, ctx)
			}
			return err
		}
	}
}
