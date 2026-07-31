// Package messagelog 提供群消息历史记录功能，包含内存热缓存 + SQLite 持久化。
//
// 架构设计：
//   - 每条消息通过 [MessageLogger] 中间件异步记录，不阻塞主流程
//   - 内存 ring buffer（热缓存）保留近期消息，查询优先走内存
//   - 异步批量写入独立的 SQLite 数据库（data/db/messagelog.db）
//   - 写入采用 channel + 批量 flush（每100ms/累积1000条），10k msg/s 下亦可胜任
//
// 数据模型基于 platform.Event，记录 RequestID / 平台 / 群组 / 用户 / 内容 / 回复链等信息，
// 可用于历史查询、词频统计、词云、排查（通过 RequestID 关联审计日志）。
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
//	// 内存热缓存（最近 N 条）
//	msgs := messagelog.QueryGroup("groupID", 10)
//	msgs := messagelog.QueryUser("userID", 10)
//
//	// SQLite 时间区间查询（词云插件使用）
//	entries, _ := logger.QueryGroupFromDB("groupID", since, until, 1000)
//	freq, _ := logger.WordFreqFromDB("groupID", since, until, 1000)
//
// 注意：Clear 只清理内存缓存。数据库消息默认永久保留（无TTL），
// 如需清理请调用 logger.Clear(before) 或通过管理命令定期执行。
package messagelog

import (
	"context"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware/ctxkeys"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// DefaultCapacity 每个群/用户的内存环形缓冲区默认大小。
const DefaultCapacity = 1000

// RecordEntry 一条消息的完整记录（内存缓存 + DB 查询的统一结构）。
type RecordEntry struct {
	RequestID string              // RequestID 中间件分配的追踪 ID
	Platform  string              // 平台标识符（"qq", "discord", "telegram"）
	Kind      string              // 事件类别（"GROUP_MESSAGE", "PRIVATE_MESSAGE"）
	EventID   string              // 平台级唯一事件 ID
	ChatID    string              // 会话 ID（群 ID / 用户 ID）
	ChatName  string              // 会话名称（平台提供时有效）
	ParentID  string              // 父容器 ID（频道场景的 guild_id / server_id）
	IsGroup   bool                // 是否为群组/频道消息
	UserID    string              // 发送者 ID
	UserName  string              // 发送者显示名
	UserRole  string              // 发送者在群中的角色（owner / admin / member）
	Content   string              // 消息文本内容
	ReplyToID string              // 被回复的消息 ID（回复链追踪）
	RawType   string              // 平台原始事件类型字符串
	Mentions  []platform.UserInfo // @ 用户列表（平台提供时有效）
	Timestamp time.Time           // 事件发生时间
	CreatedAt time.Time           // 记录入库时间

	// IsOutbound 是否为机器人发出的出站消息（如 AI 回复）。
	// 出站消息记录在独立的 botBuf ring 中，QueryGroup/QueryUser 不含出站消息。
	IsOutbound bool
}

// MessageRecord 对应 SQLite 表的 GORM 模型。
type MessageRecord struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	RequestID string `gorm:"index;not null"`
	Platform  string `gorm:"index;not null"`
	Kind      string `gorm:"index;not null"`
	EventID   string `gorm:"index"`
	ChatID    string `gorm:"index:idx_chat_time;not null"`
	ChatName  string
	ParentID  string
	IsGroup   bool
	UserID    string `gorm:"index;not null"`
	UserName  string
	UserRole  string
	Content   string `gorm:"type:text"`
	ReplyToID string
	RawType   string
	Timestamp int64 `gorm:"index:idx_chat_time"`
	CreatedAt int64

	// IsOutbound 是否为机器人发出的出站消息（AI 回复等）。
	IsOutbound bool `gorm:"index"`
}

// MessageMention 对应 SQLite message_mentions 表的 GORM 模型。
// EventID 关联到 MessageRecord.EventID，支持通过平台事件 ID 追溯 @ 信息。
type MessageMention struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	EventID     string `gorm:"index;not null"`
	MentionID   string `gorm:"index;not null"`
	DisplayName string
	IsBot       bool
	IsSelf      bool
}

// ring 单个维度（群或用户）的环形缓冲区（内存热缓存）。
type ring struct {
	buf  []RecordEntry
	head int
	size int
	cap_ int
}

func newRing(cap int) *ring {
	if cap <= 0 {
		cap = DefaultCapacity
	}
	return &ring{buf: make([]RecordEntry, cap), cap_: cap}
}

func (r *ring) add(e RecordEntry) {
	r.buf[r.head] = e
	r.head = (r.head + 1) % r.cap_
	if r.size < r.cap_ {
		r.size++
	}
}

func (r *ring) snapshot(n int) []RecordEntry {
	if n <= 0 || r.size == 0 {
		return nil
	}
	if n > r.size {
		n = r.size
	}
	out := make([]RecordEntry, n)
	start := (r.head - n + r.cap_) % r.cap_
	for i := range out {
		out[i] = r.buf[(start+i)%r.cap_]
	}
	return out
}

// recordJob 异步写入队列的任务。
type recordJob struct {
	entry RecordEntry
}

// Logger 消息日志记录器。包含内存 ring buffer + 异步 SQLite 写入。
type Logger struct {
	db     *gorm.DB
	cap    int
	stop   context.CancelFunc
	wg     sync.WaitGroup
	record chan recordJob

	groupMu  sync.RWMutex
	groupBuf map[string]*ring // groupID → ring
	userMu   sync.RWMutex
	userBuf  map[string]*ring // userID → ring

	botMu  sync.RWMutex
	botBuf map[string]*ring // chatID → ring（机器人出站消息，用于回复上下文查询）
}

// New 创建一个新的 Logger，cap 为每个群/用户的环形缓冲区容量。
// cap <= 0 时使用 DefaultCapacity。
func New(cap int) *Logger {
	if cap <= 0 {
		cap = DefaultCapacity
	}
	return &Logger{
		cap:      cap,
		groupBuf: make(map[string]*ring),
		userBuf:  make(map[string]*ring),
		botBuf:   make(map[string]*ring),
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

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&MessageRecord{}, &MessageMention{}); err != nil {
		return nil, err
	}
	return db, nil
}

// UseDB 绑定外部数据库实例到 Logger。
func (l *Logger) UseDB(db *gorm.DB) {
	l.db = db
}

// Start 启动后台异步写入 goroutine。
// 在注册 MessageLogger 中间件前必须调用。
func (l *Logger) Start() {
	if l.record != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.stop = cancel
	l.record = make(chan recordJob, 10000)
	l.wg.Add(1)
	go l.flushLoop(ctx)
}

// Stop 停止后台写入，等待已提交数据全部落盘。
func (l *Logger) Stop() {
	if l.stop != nil {
		l.stop()
	}
	l.wg.Wait()
}

// flushLoop 后台批量写入循环。
// 每 100ms 或积攒 1000 条执行一次批量 INSERT，减少 SQLite 事务开销。
func (l *Logger) flushLoop(ctx context.Context) {
	defer l.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	msgBatch := make([]MessageRecord, 0, 1000)
	mentionBatch := make([]MessageMention, 0, 1000)

	flush := func() {
		if l.db == nil {
			msgBatch = msgBatch[:0]
			mentionBatch = mentionBatch[:0]
			return
		}
		tx := l.db.Begin()
		if tx.Error != nil {
			logger.WithError(tx.Error).Warn("[MessageLog] Failed to begin transaction")
			return
		}
		if len(msgBatch) > 0 {
			if err := tx.CreateInBatches(msgBatch, 500).Error; err != nil {
				tx.Rollback()
				logger.WithError(err).Warn("[MessageLog] Failed to flush message batch")
				msgBatch = msgBatch[:0]
				mentionBatch = mentionBatch[:0]
				return
			}
		}
		if len(mentionBatch) > 0 {
			if err := tx.CreateInBatches(mentionBatch, 500).Error; err != nil {
				tx.Rollback()
				logger.WithError(err).Warn("[MessageLog] Failed to flush mention batch")
				msgBatch = msgBatch[:0]
				mentionBatch = mentionBatch[:0]
				return
			}
		}
		tx.Commit()
		msgBatch = msgBatch[:0]
		mentionBatch = mentionBatch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case job := <-l.record:
			msgBatch = append(msgBatch, recordToModel(job.entry))
			if ms := entryToMentions(job.entry); len(ms) > 0 {
				mentionBatch = append(mentionBatch, ms...)
			}
			if len(msgBatch) >= 1000 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// recordToModel 将 RecordEntry 转换为 GORM 模型。
func recordToModel(e RecordEntry) MessageRecord {
	return MessageRecord{
		RequestID: e.RequestID,
		Platform:  e.Platform,
		Kind:      e.Kind,
		EventID:   e.EventID,
		ChatID:    e.ChatID,
		ChatName:  e.ChatName,
		ParentID:  e.ParentID,
		IsGroup:   e.IsGroup,
		UserID:    e.UserID,
		UserName:  e.UserName,
		UserRole:  e.UserRole,
		Content:   e.Content,
		ReplyToID: e.ReplyToID,
		RawType:   e.RawType,
		Timestamp: e.Timestamp.UnixNano(),
		CreatedAt: e.CreatedAt.UnixNano(),
		IsOutbound: e.IsOutbound,
	}
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

// modelToEntry 将 GORM 模型转换为 RecordEntry（mentions 需外部注入）。
func modelToEntry(m MessageRecord, mentions []platform.UserInfo) RecordEntry {
	return RecordEntry{
		RequestID: m.RequestID,
		Platform:  m.Platform,
		Kind:      m.Kind,
		EventID:   m.EventID,
		ChatID:    m.ChatID,
		ChatName:  m.ChatName,
		ParentID:  m.ParentID,
		IsGroup:   m.IsGroup,
		UserID:    m.UserID,
		UserName:  m.UserName,
		UserRole:  m.UserRole,
		Content:   m.Content,
		ReplyToID: m.ReplyToID,
		RawType:   m.RawType,
		Mentions:  mentions,
		Timestamp: time.Unix(0, m.Timestamp),
		CreatedAt: time.Unix(0, m.CreatedAt),
		IsOutbound: m.IsOutbound,
	}
}

// Record 直接记录一条消息到内存缓存（不经过异步写入）。
// 适用于测试或需要同步记录的场景。生产环境推荐使用 RecordAsync。
func (l *Logger) Record(e RecordEntry) {
	if e.Content == "" {
		return
	}
	if e.ChatID != "" {
		l.groupMu.Lock()
		r, ok := l.groupBuf[e.ChatID]
		if !ok {
			r = newRing(l.cap)
			l.groupBuf[e.ChatID] = r
		}
		r.add(e)
		l.groupMu.Unlock()
	}
	if e.UserID != "" {
		l.userMu.Lock()
		r, ok := l.userBuf[e.UserID]
		if !ok {
			r = newRing(l.cap)
			l.userBuf[e.UserID] = r
		}
		r.add(e)
		l.userMu.Unlock()
	}
}

// RecordAsync 从一个 platform.Event + Context 中提取全量信息，
// 异步写入 SQLite，同时写入内存 ring buffer 热缓存。
//
// 提取的信息包括：RequestID、平台、事件类型、群/用户、内容、回复链、原始类型等。
func (l *Logger) RecordAsync(ev platform.Event, ctx *eventctx.Context) {
	e := eventToEntry(ev, ctx)

	// 异步写入 DB channel（buffer 满时静默丢弃，保护进程）
	select {
	case l.record <- recordJob{entry: e}:
	default:
	}

	// 同步写入内存 ring buffer
	if e.ChatID != "" {
		l.groupMu.Lock()
		r, ok := l.groupBuf[e.ChatID]
		if !ok {
			r = newRing(l.cap)
			l.groupBuf[e.ChatID] = r
		}
		r.add(e)
		l.groupMu.Unlock()
	}
	if e.UserID != "" {
		l.userMu.Lock()
		r, ok := l.userBuf[e.UserID]
		if !ok {
			r = newRing(l.cap)
			l.userBuf[e.UserID] = r
		}
		r.add(e)
		l.userMu.Unlock()
	}
}

// RecordOutbound 记录一条机器人发出的出站消息（如 AI 回复）。
//
// 写入独立的 botBuf ring（供回复上下文查询，QueryGroup/QueryUser 不含出站消息），
// 并在已 Start() 时异步持久化到 SQLite（IsOutbound=true）。
// 调用方需持有平台发送返回的 MessageID（eventID）。
func (l *Logger) RecordOutbound(chatID, eventID, content string, t time.Time) {
	if chatID == "" || eventID == "" || content == "" {
		return
	}
	e := RecordEntry{
		Platform:   "bot",
		Kind:       "OUTBOUND",
		EventID:    eventID,
		ChatID:     chatID,
		Content:    content,
		Timestamp:  t,
		CreatedAt:  time.Now(),
		IsOutbound: true,
	}

	// 异步写入 DB channel（未 Start 时 channel 为 nil，select 走 default 安全跳过）
	select {
	case l.record <- recordJob{entry: e}:
	default:
	}

	// 同步写入 botBuf ring
	l.botMu.Lock()
	r, ok := l.botBuf[chatID]
	if !ok {
		r = newRing(l.cap)
		l.botBuf[chatID] = r
	}
	r.add(e)
	l.botMu.Unlock()
}

// OnOutbound 实现 eventctx.OutboundObserver，记录经 ctx.Reply* 发送的出站消息。
//
// 发送失败、平台未返回 MessageID、或消息无文本内容时跳过（无法建立可靠的回复键）。
// 注意：仅覆盖经框架 ctx.Reply* 的出站；插件直接调用 platform.Sender.Send
// （绕过 ctx.Reply，如 sendqueue）不会被观察到。
func (l *Logger) OnOutbound(chatID string, req platform.SendRequest, res platform.SendResult, err error) {
	if err != nil || chatID == "" || res.MessageID == "" {
		return
	}
	text := req.Message.Text
	if text == "" {
		text = req.Message.Markdown
	}
	if text == "" {
		return
	}
	l.RecordOutbound(chatID, res.MessageID, text, time.Now())
}

// eventToEntry 从 platform.Event 和 Context 提取完整的消息记录。
func eventToEntry(ev platform.Event, ctx *eventctx.Context) RecordEntry {	chat := ev.Chat()
	sender := ev.Sender()

	requestID, _ := ctx.Get(ctxkeys.CtxKeyRequestID)
	rid, _ := requestID.(string)

	var replyToID string
	if re, ok := ev.(platform.ReplyEvent); ok {
		replyToID = re.ReplyToID()
	}

	mentions := platform.GetMentions(ev)

	return RecordEntry{
		RequestID: rid,
		Platform:  ev.Platform(),
		Kind:      string(ev.Kind()),
		EventID:   ev.ID(),
		ChatID:    chat.ID,
		ChatName:  chat.Name,
		ParentID:  chat.ParentID,
		IsGroup:   chat.IsGroup,
		UserID:    sender.ID,
		UserName:  sender.DisplayName,
		UserRole:  groupRoleString(sender.GroupRole),
		Content:   ev.Content(),
		ReplyToID: replyToID,
		RawType:   platform.RawType(ev),
		Mentions:  mentions,
		Timestamp: ev.Timestamp(),
		CreatedAt: time.Now(),
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

// --- 内存热缓存查询（优先走 ring buffer） ---

// QueryGroup 返回群 groupID 最近 n 条消息（从旧到新）。
// 只查询内存 ring buffer，n 超出缓冲区容量时只返回缓冲区内的条数。
func (l *Logger) QueryGroup(groupID string, n int) []RecordEntry {
	if groupID == "" || n <= 0 {
		return nil
	}
	l.groupMu.RLock()
	r := l.groupBuf[groupID]
	l.groupMu.RUnlock()
	if r != nil {
		return r.snapshot(n)
	}
	return nil
}

// QueryUser 返回用户 userID 最近 n 条消息（从旧到新）。
func (l *Logger) QueryUser(userID string, n int) []RecordEntry {
	if userID == "" || n <= 0 {
		return nil
	}
	l.userMu.RLock()
	r := l.userBuf[userID]
	l.userMu.RUnlock()
	if r != nil {
		return r.snapshot(n)
	}
	return nil
}

// QueryByEventID 按事件 ID 在指定会话中查找消息内容（回复上下文查询）。
//
// 查找顺序：
//  1. 入站消息 ring（groupBuf）
//  2. 机器人出站消息 ring（botBuf）
//  3. SQLite 兜底（入站与出站均持久化，按 chat_id + event_id 索引查询）
//
// 未找到返回零值和 false。
func (l *Logger) QueryByEventID(chatID, eventID string) (RecordEntry, bool) {
	if chatID == "" || eventID == "" {
		return RecordEntry{}, false
	}

	l.groupMu.RLock()
	r := l.groupBuf[chatID]
	l.groupMu.RUnlock()
	if r != nil {
		if e, ok := findInRing(r, eventID); ok {
			return e, true
		}
	}

	l.botMu.RLock()
	br := l.botBuf[chatID]
	l.botMu.RUnlock()
	if br != nil {
		if e, ok := findInRing(br, eventID); ok {
			return e, true
		}
	}

	if l.db != nil {
		var m MessageRecord
		if err := l.db.Where("chat_id = ? AND event_id = ?", chatID, eventID).First(&m).Error; err == nil {
			return modelToEntry(m, nil), true
		}
	}

	return RecordEntry{}, false
}

// findInRing 在 ring 快照中按事件 ID 线性查找（容量上限为 DefaultCapacity）。
func findInRing(r *ring, eventID string) (RecordEntry, bool) {
	for _, e := range r.snapshot(r.size) {
		if e.EventID == eventID {
			return e, true
		}
	}
	return RecordEntry{}, false
}

// WordFreq 统计群 groupID 最近 n 条消息中的词频（基于内存 ring buffer）。
// 返回 map[词]出现次数，可用于简单词云展示。
func (l *Logger) WordFreq(groupID string, n int) map[string]int {
	msgs := l.QueryGroup(groupID, n)
	freq := make(map[string]int)
	for _, m := range msgs {
		for _, word := range tokenize(m.Content) {
			freq[word]++
		}
	}
	return freq
}

// --- SQLite 查询（时间区间 + 大数据量） ---

// WordFreqEntry 词频统计结果条目。
type WordFreqEntry struct {
	Word  string
	Count int
}

// WordFreqFromDB 从 SQLite 查询指定时间区间内的词频（Go 侧分词）。
// 用于词云等需要分析大量历史消息的场景。
func (l *Logger) WordFreqFromDB(chatID string, since, until time.Time, limit int) ([]WordFreqEntry, error) {
	if l.db == nil {
		return nil, nil
	}
	var models []MessageRecord
	err := l.db.Where("chat_id = ? AND is_outbound = false AND timestamp BETWEEN ? AND ?", chatID, since.UnixNano(), until.UnixNano()).
		Order("id DESC").Limit(limit).Find(&models).Error
	if err != nil {
		return nil, err
	}
	freq := make(map[string]int)
	for _, m := range models {
		for _, word := range tokenize(m.Content) {
			freq[word]++
		}
	}
	out := make([]WordFreqEntry, 0, len(freq))
	for word, count := range freq {
		out = append(out, WordFreqEntry{Word: word, Count: count})
	}
	return out, nil
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

// QueryGroupFromDB 从 SQLite 查询群指定时间区间的消息记录。
func (l *Logger) QueryGroupFromDB(chatID string, since, until time.Time, limit int) ([]RecordEntry, error) {
	if l.db == nil {
		return nil, nil
	}
	var models []MessageRecord
	err := l.db.Where("chat_id = ? AND is_outbound = false AND timestamp BETWEEN ? AND ?", chatID, since.UnixNano(), until.UnixNano()).
		Order("id DESC").Limit(limit).Find(&models).Error
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, nil
	}
	eventIDs := make([]string, len(models))
	for i, m := range models {
		eventIDs[i] = m.EventID
	}
	mentionsMap := l.loadMentions(eventIDs)
	out := make([]RecordEntry, len(models))
	for i, m := range models {
		out[i] = modelToEntry(m, mentionsMap[m.EventID])
	}
	return out, nil
}

// QueryUserFromDB 从 SQLite 查询用户指定时间区间的消息记录。
func (l *Logger) QueryUserFromDB(userID string, since, until time.Time, limit int) ([]RecordEntry, error) {
	if l.db == nil {
		return nil, nil
	}
	var models []MessageRecord
	err := l.db.Where("user_id = ? AND is_outbound = false AND timestamp BETWEEN ? AND ?", userID, since.UnixNano(), until.UnixNano()).
		Order("id DESC").Limit(limit).Find(&models).Error
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, nil
	}
	eventIDs := make([]string, len(models))
	for i, m := range models {
		eventIDs[i] = m.EventID
	}
	mentionsMap := l.loadMentions(eventIDs)
	out := make([]RecordEntry, len(models))
	for i, m := range models {
		out[i] = modelToEntry(m, mentionsMap[m.EventID])
	}
	return out, nil
}

// --- 清理 ---

// Clear 从内存 ring buffer 中删除时间戳早于 before 的消息。
// 同时从 SQLite 中删除对应记录（DB 清理可选）。
func (l *Logger) Clear(before time.Time) {
	l.groupMu.Lock()
	for gid, r := range l.groupBuf {
		pruned := pruneRing(r, before, l.cap)
		if pruned.size == 0 {
			delete(l.groupBuf, gid)
		} else {
			l.groupBuf[gid] = pruned
		}
	}
	l.groupMu.Unlock()

	l.userMu.Lock()
	for uid, r := range l.userBuf {
		pruned := pruneRing(r, before, l.cap)
		if pruned.size == 0 {
			delete(l.userBuf, uid)
		} else {
			l.userBuf[uid] = pruned
		}
	}
	l.userMu.Unlock()

	l.botMu.Lock()
	for cid, r := range l.botBuf {
		pruned := pruneRing(r, before, l.cap)
		if pruned.size == 0 {
			delete(l.botBuf, cid)
		} else {
			l.botBuf[cid] = pruned
		}
	}
	l.botMu.Unlock()

	if l.db != nil {
		cutoff := before.UnixNano()
		var ids []string
		l.db.Model(&MessageRecord{}).Where("timestamp < ?", cutoff).Pluck("event_id", &ids)
		if len(ids) > 0 {
			if tx := l.db.Where("event_id IN ?", ids).Delete(&MessageMention{}); tx.Error != nil {
				logger.WithError(tx.Error).Warn("[MessageLog] Failed to clear mentions from DB")
			}
		}
		if tx := l.db.Where("timestamp < ?", cutoff).Delete(&MessageRecord{}); tx.Error != nil {
			logger.WithError(tx.Error).Warn("[MessageLog] Failed to clear old messages from DB")
		}
		// DELETE 后回收空闲页，控制 DB 文件膨胀
		l.db.Exec("PRAGMA incremental_vacuum(500)")
	}
}

// --- 统计 ---

// GroupCount 返回已记录消息的群数量。
func (l *Logger) GroupCount() int {
	l.groupMu.RLock()
	defer l.groupMu.RUnlock()
	return len(l.groupBuf)
}

// UserCount 返回已记录消息的用户数量。
func (l *Logger) UserCount() int {
	l.userMu.RLock()
	defer l.userMu.RUnlock()
	return len(l.userBuf)
}

// GroupMessageCount 返回群 groupID 在内存缓存中的消息数量。
func (l *Logger) GroupMessageCount(groupID string) int {
	l.groupMu.RLock()
	r := l.groupBuf[groupID]
	l.groupMu.RUnlock()
	if r == nil {
		return 0
	}
	return r.size
}

// --- 内部工具 ---

func pruneRing(r *ring, before time.Time, cap int) *ring {
	all := r.snapshot(r.size)
	out := newRing(cap)
	for _, m := range all {
		if !m.Timestamp.Before(before) {
			out.add(m)
		}
	}
	return out
}

// tokenize 简单分词：按空白切割，过滤长度 < 2 的词及纯标点词。
func tokenize(text string) []string {
	var out []string
	var buf []rune
	for _, r := range text {
		if !isWordRune(r) {
			if len(buf) >= 2 {
				out = append(out, string(buf))
			}
			buf = buf[:0]
			continue
		}
		buf = append(buf, r)
	}
	if len(buf) >= 2 {
		out = append(out, string(buf))
	}
	return out
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		(r >= 0x4e00 && r <= 0x9fff) || // CJK 统一表意文字
		(r >= 0x3400 && r <= 0x4dbf) || // CJK 扩展A
		r == '\''
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
			if defaultLogger.record != nil {
				eventctx.ExtSet(ctx.Ext(), eventctx.OutboundObserverExt{Observer: defaultLogger})
			}
			err := next(ctx)
			pe := ctx.GetPlatformEvent()
			if pe != nil && defaultLogger.record != nil {
				defaultLogger.RecordAsync(pe, ctx)
			}
			return err
		}
	}
}
