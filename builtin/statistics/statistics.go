// Package statistics 提供基于 messagelog 事实层的派生统计数据。
//
// 依赖方向：statistics → messagelog（统计依赖事实层，事实层不感知统计）。
// 统计数据（词频 / 每日消息量 / 用户消息量 / 会话消息量）属于派生数据：
// 可随时从 messagelog 全量重建，不实时扫描 message_records。
//
// 增量来源：订阅 messagelog 的 TopicMessageRecorded 事件（flush 提交成功后广播）。
// 事件丢失不影响事实层——通过 Rebuild 从 messagelog 全量重建聚合即可。
package statistics

import (
	"context"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Config 统计插件配置。
type Config struct {
	DBPath        string
	FlushInterval time.Duration
}

// Option 配置项。
type Option func(*Plugin)

// WithStore 设置派生数据 SQLite 路径（默认 data/db/statistics.db）。
func WithStore(path string) Option {
	return func(p *Plugin) {
		if path != "" {
			p.path = path
		}
	}
}

// WithFlushInterval 设置聚合落盘间隔（默认 500ms）。
func WithFlushInterval(d time.Duration) Option {
	return func(p *Plugin) {
		if d > 0 {
			p.flushInterval = d
		}
	}
}

// aggCmd 聚合命令：rebuild = 全量重建；entry = 一条增量事件。
type aggCmd struct {
	rebuild bool
	entry   *messagelog.RecordEntry
}

// key 聚合键定义（仅聚合 goroutine 访问）。
type wordKey struct {
	chat string
	word string
}

type dailyKey struct {
	chat     string
	day      string
	outbound bool
}

type userKey struct {
	chat string
	user string
	day  string
}

type chatKey struct {
	chat string
	day  string
}

// ScanSource 事实层扫描接口（默认 messagelog.Default()，测试可注入假实现）。
type ScanSource interface {
	ScanAfter(afterTS time.Time, afterID int64, fn func(messagelog.RecordEntry) error) error
}

// Plugin 统计插件实例。通过 ctx.Service[*statistics.Plugin]("statistics") 获取。
type Plugin struct {
	path          string
	flushInterval time.Duration
	source        ScanSource

	db *gorm.DB

	ch      chan aggCmd
	dropped atomic.Int64

	// 以下聚合状态仅由聚合 goroutine 访问，无需锁。
	words map[wordKey]int
	daily map[dailyKey]int
	users map[userKey]int
	chats map[chatKey]int
	dirty bool
}

// NewPlugin 创建 Plugin 实例（不注册到插件系统）。
func NewPlugin(opts ...Option) *Plugin {
	p := &Plugin{
		path:          "data/db/statistics.db",
		flushInterval: 500 * time.Millisecond,
		source:        messagelog.Default(),
		words:         make(map[wordKey]int),
		daily:         make(map[dailyKey]int),
		users:         make(map[userKey]int),
		chats:         make(map[chatKey]int),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// New 创建统计插件的描述符，注册到 PluginManager 后可通过依赖注入获取。
func New(opts ...Option) *plugin.Descriptor {
	p := NewPlugin(opts...)
	return &plugin.Descriptor{
		Name:    "statistics",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "基于 messagelog 事实层的派生统计（词频 / 每日 / 用户 / 会话消息量），可从事实层全量重建",
			Category:    "数据",
			Tags:        []string{"统计", "词频", "词云", "派生数据"},
			HelpText:    "statistics 插件订阅 messagelog 的 MessageRecorded 事件做增量聚合，崩溃后可从 messagelog 全量重建。",
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			if ctx.DryRun {
				return p, nil
			}
			db, err := OpenDB(p.path)
			if err != nil {
				return nil, err
			}
			p.db = db

			// 订阅消息事实事件（生命周期绑定 Scope，插件卸载自动取消）
			if _, err := ctx.Subscribe(messagelog.TopicMessageRecorded, func(_ context.Context, data any) error {
				ev, ok := data.(messagelog.MessageRecorded)
				if !ok {
					return nil
				}
				p.handleRecorded(ev)
				return nil
			}); err != nil {
				return nil, err
			}

			p.ch = make(chan aggCmd, 100000)
			// 聚合 goroutine 生命周期绑定插件：框架在 Teardown 前取消 context
			// 并等待退出（详见 SetupContext.SpawnNamed 文档）。
			ctx.SpawnNamed("statistics-aggregator", func(runCtx context.Context) {
				// 启动时全量重建（阻塞直到完成），保证派生数据与事实层一致
				p.rebuild(runCtx)
				p.aggregatorLoop(runCtx)
			})
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			// 聚合 goroutine 已由框架取消并等待退出，可安全关闭派生数据 DB
			if p.db != nil {
				if sqlDB, err := p.db.DB(); err == nil {
					sqlDB.Close()
				}
				p.db = nil
			}
			return nil
		},
	}
}

// handleRecorded 处理一条 MessageRecorded 事件（订阅回调入口）。
func (p *Plugin) handleRecorded(ev messagelog.MessageRecorded) {
	p.enqueue(&ev.Record)
}

// enqueue 投递一条增量事件到聚合器；队列满则丢弃（可经 Rebuild 重建）。
func (p *Plugin) enqueue(e *messagelog.RecordEntry) {
	if p.ch == nil {
		return
	}
	select {
	case p.ch <- aggCmd{entry: e}:
	default:
		p.dropped.Add(1)
		logger.Warn("[Statistics] event channel full, event dropped (run Rebuild to recover)")
	}
}

// Rebuild 从 messagelog 全量重建派生数据（异步，聚合器串行执行）。
func (p *Plugin) Rebuild() {
	if p.ch == nil {
		return
	}
	select {
	case p.ch <- aggCmd{rebuild: true}:
	default:
		logger.Warn("[Statistics] rebuild command dropped (aggregator busy)")
	}
}

// DroppedEvents 返回因队列满被丢弃的事件数（可经 Rebuild 恢复）。
func (p *Plugin) DroppedEvents() int64 {
	return p.dropped.Load()
}

// aggregatorLoop 聚合主循环：增量事件 → 内存聚合 → 定时批量落盘。
func (p *Plugin) aggregatorLoop(ctx context.Context) {
	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.flush()
			return
		case cmd := <-p.ch:
			if cmd.rebuild {
				p.rebuild(ctx)
				continue
			}
			if cmd.entry != nil {
				p.apply(*cmd.entry)
				p.dirty = true
			}
		case <-ticker.C:
			if p.dirty {
				p.flush()
				p.dirty = false
			}
		}
	}
}

// rebuild 全量重建（必须在聚合 goroutine 内调用）：
//  1. 清空派生表；
//  2. 全量扫描 messagelog 事实库聚合；
//  3. 排空重建期间到达的事件，按 event_id 去重后补聚合。
//
// 重建期间广播事件与扫描可能覆盖同一记录，去重集合仅在重建窗口存活，
// 之后清空，避免长期驻留内存。
func (p *Plugin) rebuild(ctx context.Context) {
	if p.db == nil {
		return
	}
	p.truncateAll()
	p.resetAggregates()

	seen := make(map[string]struct{})
	if err := p.source.ScanAfter(time.Time{}, 0, func(e messagelog.RecordEntry) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		p.apply(e)
		seen[e.EventID] = struct{}{}
		return nil
	}); err != nil && ctx.Err() == nil {
		logger.WithError(err).Warn("[Statistics] rebuild scan failed, derived data may be incomplete")
	}

	// 排空重建期间到达的事件（去重后聚合）
	for {
		select {
		case cmd := <-p.ch:
			if cmd.entry == nil {
				continue
			}
			if _, dup := seen[cmd.entry.EventID]; dup {
				continue
			}
			seen[cmd.entry.EventID] = struct{}{}
			p.apply(*cmd.entry)
			p.dirty = true
		default:
			p.flush()
			return
		}
	}
}

// truncateAll 清空全部派生表。
func (p *Plugin) truncateAll() {
	if p.db == nil {
		return
	}
	for _, m := range []any{&WordStat{}, &DailyMessageStat{}, &UserMessageStat{}, &ChatMessageStat{}} {
		if err := p.db.Session(&gorm.Session{}).Where("1 = 1").Delete(m).Error; err != nil {
			logger.WithError(err).Warn("[Statistics] failed to truncate derived table")
		}
	}
}

// resetAggregates 清空内存聚合。
func (p *Plugin) resetAggregates() {
	clear(p.words)
	clear(p.daily)
	clear(p.users)
	clear(p.chats)
	p.dirty = false
}

// apply 将一条消息记录累加到内存聚合。
func (p *Plugin) apply(e messagelog.RecordEntry) {
	day := e.Timestamp.UTC().Format("2006-01-02")
	p.chats[chatKey{chat: e.ChatID, day: day}]++
	p.daily[dailyKey{chat: e.ChatID, day: day, outbound: e.IsOutbound}]++
	if e.UserID != "" {
		p.users[userKey{chat: e.ChatID, user: e.UserID, day: day}]++
	}
	for _, w := range tokenize(e.Content) {
		p.words[wordKey{chat: e.ChatID, word: w}]++
	}
}

// flush 将内存聚合批量 upsert 到派生表（整值覆盖：全量重建路径下结果精确）。
// 四张派生表在同一事务内提交：读者不会观察到「words 已更新而 daily/chats 未更新」
// 的中间状态（词频可见 ⇔ 全部派生表可见）。
func (p *Plugin) flush() {
	if p.db == nil {
		return
	}
	if err := p.db.Transaction(func(tx *gorm.DB) error {
		if len(p.words) > 0 {
			rows := make([]WordStat, 0, len(p.words))
			now := time.Now().UnixNano()
			for k, v := range p.words {
				rows = append(rows, WordStat{Word: k.word, ChatID: k.chat, Count: v, UpdatedAt: now})
			}
			if err := p.upsert(tx, []string{"word", "chat_id"}, []string{"count", "updated_at"}, rows); err != nil {
				return err
			}
		}
		if len(p.daily) > 0 {
			rows := make([]DailyMessageStat, 0, len(p.daily))
			for k, v := range p.daily {
				rows = append(rows, DailyMessageStat{ChatID: k.chat, Day: k.day, IsOutbound: k.outbound, Count: v})
			}
			if err := p.upsert(tx, []string{"chat_id", "day", "is_outbound"}, []string{"count"}, rows); err != nil {
				return err
			}
		}
		if len(p.users) > 0 {
			rows := make([]UserMessageStat, 0, len(p.users))
			for k, v := range p.users {
				rows = append(rows, UserMessageStat{ChatID: k.chat, UserID: k.user, Day: k.day, Count: v})
			}
			if err := p.upsert(tx, []string{"chat_id", "user_id", "day"}, []string{"count"}, rows); err != nil {
				return err
			}
		}
		if len(p.chats) > 0 {
			rows := make([]ChatMessageStat, 0, len(p.chats))
			for k, v := range p.chats {
				rows = append(rows, ChatMessageStat{ChatID: k.chat, Day: k.day, Count: v})
			}
			if err := p.upsert(tx, []string{"chat_id", "day"}, []string{"count"}, rows); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		logger.WithError(err).Warn("[Statistics] failed to flush aggregates")
	}
}

// upsert 在给定事务内批量 upsert（ON CONFLICT 更新指定列）。
func (p *Plugin) upsert(tx *gorm.DB, conflictCols, updateCols []string, rows any) error {
	cols := make([]clause.Column, len(conflictCols))
	for i, c := range conflictCols {
		cols[i] = clause.Column{Name: c}
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   cols,
		DoUpdates: clause.AssignmentColumns(updateCols),
	}).CreateInBatches(rows, 500).Error
}

// --- 查询 API ---

// WordFreqEntry 词频统计结果条目。
type WordFreqEntry struct {
	Word  string
	Count int
}

// WordFreq 返回会话 chatID 的词频 TopN（跨全部历史，按总量倒序）。
func (p *Plugin) WordFreq(chatID string, n int) ([]WordFreqEntry, error) {
	if p.db == nil || chatID == "" || n <= 0 {
		return nil, nil
	}
	var rows []WordStat
	if err := p.db.Where("chat_id = ?", chatID).Order("count DESC, word ASC").Limit(n).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]WordFreqEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, WordFreqEntry{Word: r.Word, Count: r.Count})
	}
	return out, nil
}

// DailyStat 每日消息量。
type DailyStat struct {
	Day        string
	IsOutbound bool
	Count      int
}

// DailyStats 返回会话 chatID 在 [since, until] 日期区间内的每日消息量。
func (p *Plugin) DailyStats(chatID string, since, until time.Time) ([]DailyStat, error) {
	if p.db == nil || chatID == "" {
		return nil, nil
	}
	var rows []DailyMessageStat
	if err := p.db.Where("chat_id = ? AND day BETWEEN ? AND ?", chatID, since.UTC().Format("2006-01-02"), until.UTC().Format("2006-01-02")).
		Order("day ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]DailyStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, DailyStat{Day: r.Day, IsOutbound: r.IsOutbound, Count: r.Count})
	}
	return out, nil
}

// UserStatEntry 用户消息量。
type UserStatEntry struct {
	UserID string
	Count  int
}

// UserStats 返回会话 chatID 在日期区间内消息量 TopN 的用户。
func (p *Plugin) UserStats(chatID string, since, until time.Time, limit int) ([]UserStatEntry, error) {
	if p.db == nil || chatID == "" || limit <= 0 {
		return nil, nil
	}
	var rows []UserMessageStat
	if err := p.db.Where("chat_id = ? AND day BETWEEN ? AND ?", chatID, since.UTC().Format("2006-01-02"), until.UTC().Format("2006-01-02")).
		Order("count DESC, user_id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]UserStatEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, UserStatEntry{UserID: r.UserID, Count: r.Count})
	}
	return out, nil
}

// ChatStatEntry 会话消息量。
type ChatStatEntry struct {
	ChatID string
	Count  int
}

// ChatStats 返回日期区间内消息量 TopN 的会话。
func (p *Plugin) ChatStats(since, until time.Time, limit int) ([]ChatStatEntry, error) {
	if p.db == nil || limit <= 0 {
		return nil, nil
	}
	var rows []ChatMessageStat
	if err := p.db.Where("day BETWEEN ? AND ?", since.UTC().Format("2006-01-02"), until.UTC().Format("2006-01-02")).
		Order("count DESC, chat_id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ChatStatEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, ChatStatEntry{ChatID: r.ChatID, Count: r.Count})
	}
	return out, nil
}

// --- 分词（原 messagelog 统计职责迁移至此） ---

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
