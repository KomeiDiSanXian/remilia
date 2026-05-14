// Package subscription 提供通用的推送订阅框架。
//
// # 设计概述
//
// 该框架将"数据源"（Source）与"订阅目标"（群/私聊）解耦，
// 通过定时轮询检测新内容并推送给所有订阅者：
//
//	数据源（RSS/B站/自定义） → 轮询 → 新内容 → 分发 → 订阅目标（群/私聊）
//
// # 快速开始
//
//	// 1. 创建自定义数据源
//	type MySource struct{}
//	func (s *MySource) Name() string        { return "my_source" }
//	func (s *MySource) Description() string { return "我的自定义数据源" }
//	func (s *MySource) Poll(ctx context.Context, param string) ([]subscription.Item, error) {
//	    // 获取最新内容...
//	    return []subscription.Item{{ID: "1", Title: "新内容", Body: "详情"}}, nil
//	}
//	func (s *MySource) ValidateParam(param string) error { return nil }
//
//	// 2. 注册插件（在 bot 初始化时）
//	subPlugin := subscription.NewPlugin(
//	    subscription.WithDispatcher(func(ctx context.Context, target subscription.Target, item subscription.Item) error {
//	        return sender.Send(ctx, platform.SendRequest{
//	            Target:  platform.ChatInfo{ID: target.ChatID, IsGroup: target.IsGroup},
//	            Message: platform.TextMessage(item.Title + "\n" + item.Body),
//	        })
//	    }),
//	    subscription.WithPollInterval(5 * time.Minute),
//	)
//	subPlugin.RegisterSource(&MySource{})
//	pm.Register(subPlugin.Descriptor())
//
//	// 3. 在 Handler 中管理订阅
//	mgrSvc := plugin.Service[*subscription.Manager](ctx, "subscription")
//	id, err := mgr.Subscribe("my_source", "param", subscription.Target{ChatID: "group-001", IsGroup: true})
package subscription

import (
	stdctx "context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/scheduler"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// ─── 公共类型 ─────────────────────────────────────────────────────────────────

// Item 是数据源推送的单条内容。
type Item struct {
	// ID 内容的唯一标识符（用于去重；不同调用间必须稳定）
	ID string
	// Title 内容标题（简短，用于通知首行）
	Title string
	// Body 内容正文（可选，较长的详情文字）
	Body string
	// URL 原始链接（可选）
	URL string
	// Extra 数据源特有的附加字段（类型断言后访问）
	Extra map[string]any
}

// Target 订阅目标：一个群或私聊会话。
type Target struct {
	// ChatID 目标会话 ID（群 ID 或用户 ID）
	ChatID string
	// IsGroup 是否为群组会话
	IsGroup bool
}

// Subscription 一条订阅记录。
type Subscription struct {
	// ID 订阅的唯一标识（UUID 格式，Subscribe 成功时返回）
	ID string
	// SourceName 数据源名称（对应 Source.Name()）
	SourceName string
	// Param 数据源参数（如 RSS URL、UP 主 ID 等）
	Param string
	// Target 订阅目标
	Target Target
	// CreatedAt 订阅创建时间
	CreatedAt time.Time
}

// SourceInfo 数据源的描述信息（用于 ListSources）。
type SourceInfo struct {
	Name        string
	Description string
}

// ─── 接口 ─────────────────────────────────────────────────────────────────────

// Source 数据源接口，插件开发者实现此接口以接入订阅框架。
//
// 实现要求：
//   - Name() 返回的名称必须全局唯一、稳定不变（用作 map key）
//   - Poll() 应只返回新内容（或全量，由 Manager 去重）
//   - Poll() 必须尊重 ctx 的取消，避免阻塞 shutdown
//   - ValidateParam() 在 Subscribe 时被调用以校验参数合法性
type Source interface {
	// Name 数据源唯一名称（如 "rss"、"bilibili"）
	Name() string
	// Description 人类可读的数据源描述
	Description() string
	// Poll 拉取最新内容。ctx 用于取消控制；param 为订阅时传入的参数。
	// 返回所有当前可用的内容（Manager 负责去重，不要求只返回增量）。
	Poll(ctx stdctx.Context, param string) ([]Item, error)
	// ValidateParam 校验订阅参数是否合法（Subscribe 时调用）。
	// 不合法时返回描述问题的 error，合法时返回 nil。
	ValidateParam(param string) error
}

// DispatchFunc 内容分发函数。
// Manager 在检测到新内容时通过此函数将 item 推送给 target。
type DispatchFunc func(ctx stdctx.Context, target Target, item Item) error

// ─── 常量与错误 ───────────────────────────────────────────────────────────────

// ErrSourceNotFound 数据源未注册。
var ErrSourceNotFound = errors.New("subscription: source not found")

// ErrAlreadySubscribed 目标已订阅该数据源+参数组合。
var ErrAlreadySubscribed = errors.New("subscription: already subscribed")

// ErrSubscriptionNotFound 订阅记录不存在。
var ErrSubscriptionNotFound = errors.New("subscription: subscription not found")

// defaultPollInterval 未指定时的默认轮询间隔。
const defaultPollInterval = 5 * time.Minute

// ─── options ──────────────────────────────────────────────────────────────────

// Option 函数式选项。
type Option func(*managerOpts)

type managerOpts struct {
	dispatcher   DispatchFunc
	pollInterval time.Duration
}

// WithDispatcher 设置内容分发函数（必须设置，否则新内容将被静默丢弃）。
func WithDispatcher(fn DispatchFunc) Option {
	return func(o *managerOpts) { o.dispatcher = fn }
}

// WithPollInterval 设置全局轮询间隔（默认 5 分钟；最小 10 秒）。
func WithPollInterval(d time.Duration) Option {
	return func(o *managerOpts) {
		if d < 10*time.Second {
			d = 10 * time.Second
		}
		o.pollInterval = d
	}
}

// ─── Manager ──────────────────────────────────────────────────────────────────

// Manager 订阅管理器。
//
// 负责注册数据源、管理订阅记录、协调定时轮询和内容分发。
// 所有方法均线程安全。
type Manager struct {
	mu      sync.RWMutex
	sources map[string]Source        // 已注册数据源
	subs    map[string]*Subscription // subscriptionID -> Subscription

	// sourceKey = "sourceName:param"，用于聚合相同来源+参数的订阅
	sourceJobs map[string]scheduler.JobID     // sourceKey -> scheduler job ID
	seen       map[string]map[string]struct{} // sourceKey -> seen item IDs

	schedSvc    *plugin.ServiceProxy[*scheduler.Plugin] // 防过期的服务代理
	dispatch    DispatchFunc
	pollInterval time.Duration
}

// newManager 创建 Manager 实例。
func newManager(opts managerOpts) *Manager {
	interval := opts.pollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	return &Manager{
		sources:      make(map[string]Source),
		subs:         make(map[string]*Subscription),
		sourceJobs:   make(map[string]scheduler.JobID),
		seen:         make(map[string]map[string]struct{}),
		dispatch:     opts.dispatcher,
		pollInterval: interval,
	}
}

// sched 返回当前 scheduler 插件实例（防过期的延迟解析）。
func (m *Manager) sched() *scheduler.Plugin {
	if m.schedSvc == nil {
		return nil
	}
	s, _ := m.schedSvc.Get()
	return s
}

// RegisterSource 注册一个数据源。
// 若已注册同名数据源，将覆盖旧的注册（方便重新加载场景）。
func (m *Manager) RegisterSource(src Source) {
	m.mu.Lock()
	m.sources[src.Name()] = src
	m.mu.Unlock()
	logger.Infof("[Subscription] Registered source %q", src.Name())
}

// ListSources 返回所有已注册数据源的描述信息。
func (m *Manager) ListSources() []SourceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SourceInfo, 0, len(m.sources))
	for _, src := range m.sources {
		out = append(out, SourceInfo{Name: src.Name(), Description: src.Description()})
	}
	return out
}

// Subscribe 为 target 订阅指定数据源+参数。
// 返回新订阅的 ID（UUID 格式），或错误。
//
//   - 若数据源不存在，返回 [ErrSourceNotFound]
//   - 若参数非法，返回数据源 ValidateParam 的错误
//   - 若 target 已订阅该 sourceKey，返回 [ErrAlreadySubscribed]
func (m *Manager) Subscribe(sourceName, param string, target Target) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	src, ok := m.sources[sourceName]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrSourceNotFound, sourceName)
	}
	if err := src.ValidateParam(param); err != nil {
		return "", fmt.Errorf("subscription: invalid param for source %q: %w", sourceName, err)
	}

	// 重复订阅检查
	for _, sub := range m.subs {
		if sub.SourceName == sourceName && sub.Param == param &&
			sub.Target.ChatID == target.ChatID && sub.Target.IsGroup == target.IsGroup {
			return "", ErrAlreadySubscribed
		}
	}

	id := generateID()
	sub := &Subscription{
		ID:         id,
		SourceName: sourceName,
		Param:      param,
		Target:     target,
		CreatedAt:  time.Now(),
	}
	m.subs[id] = sub

	// 若该 sourceKey 尚无轮询任务，启动一个
	sourceKey := buildSourceKey(sourceName, param)
	if _, exists := m.sourceJobs[sourceKey]; !exists && m.sched() != nil {
		jobID := m.startPollJob(src, param, sourceKey)
		m.sourceJobs[sourceKey] = jobID
	}

	logger.Infof("[Subscription] %q subscribed to source=%q param=%q (id=%s)",
		target.ChatID, sourceName, param, id)
	return id, nil
}

// Unsubscribe 移除指定 ID 的订阅。
// 若该 sourceKey 不再有任何订阅者，自动停止对应的轮询任务。
func (m *Manager) Unsubscribe(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub, ok := m.subs[id]
	if !ok {
		return ErrSubscriptionNotFound
	}
	delete(m.subs, id)

	// 检查该 sourceKey 是否还有其他订阅者
	sourceKey := buildSourceKey(sub.SourceName, sub.Param)
	hasMore := false
	for _, s := range m.subs {
		if buildSourceKey(s.SourceName, s.Param) == sourceKey {
			hasMore = true
			break
		}
	}
	if !hasMore {
		if jobID, exists := m.sourceJobs[sourceKey]; exists {
			if m.sched() != nil {
				m.sched().Remove(jobID)
			}
			delete(m.sourceJobs, sourceKey)
			delete(m.seen, sourceKey)
		}
	}

	logger.Infof("[Subscription] Removed subscription id=%s (source=%q param=%q target=%q)",
		id, sub.SourceName, sub.Param, sub.Target.ChatID)
	return nil
}

// ListSubscriptions 返回指定 chatID 的所有订阅记录（传空字符串返回全部）。
func (m *Manager) ListSubscriptions(chatID string) []Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Subscription, 0)
	for _, sub := range m.subs {
		if chatID == "" || sub.Target.ChatID == chatID {
			out = append(out, *sub)
		}
	}
	return out
}

// ─── 轮询逻辑 ─────────────────────────────────────────────────────────────────

// startPollJob 向 scheduler 注册一个定时轮询任务。
// 调用前必须持有 m.mu（写锁）。
func (m *Manager) startPollJob(src Source, param, sourceKey string) scheduler.JobID {
	jobName := "sub:" + sourceKey
	return m.sched().EveryNamed(jobName, m.pollInterval, func() {
		m.poll(src, param, sourceKey)
	})
}

// poll 执行一次轮询，分发新内容给所有该 sourceKey 的订阅者。
func (m *Manager) poll(src Source, param, sourceKey string) {
	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), 30*time.Second)
	defer cancel()

	items, err := src.Poll(ctx, param)
	if err != nil {
		logger.WithError(err).Warnf("[Subscription] Poll failed for source=%q param=%q", src.Name(), param)
		return
	}
	if len(items) == 0 {
		return
	}

	// 找出新内容（未在 seen 集合中的 item）
	m.mu.Lock()
	if m.seen[sourceKey] == nil {
		m.seen[sourceKey] = make(map[string]struct{})
	}
	var newItems []Item
	for _, item := range items {
		if _, alreadySeen := m.seen[sourceKey][item.ID]; !alreadySeen {
			newItems = append(newItems, item)
			m.seen[sourceKey][item.ID] = struct{}{}
		}
	}
	// 收集该 sourceKey 的订阅目标
	var targets []Target
	for _, sub := range m.subs {
		if buildSourceKey(sub.SourceName, sub.Param) == sourceKey {
			targets = append(targets, sub.Target)
		}
	}
	m.mu.Unlock()

	if len(newItems) == 0 || len(targets) == 0 || m.dispatch == nil {
		return
	}

	// 分发新内容
	dispCtx, dcancel := stdctx.WithTimeout(stdctx.Background(), 60*time.Second)
	defer dcancel()
	for _, item := range newItems {
		for _, target := range targets {
			if err := m.dispatch(dispCtx, target, item); err != nil {
				logger.WithError(err).Warnf("[Subscription] Dispatch failed for target=%q item=%q",
					target.ChatID, item.ID)
			}
		}
	}
}

// ─── 辅助函数 ─────────────────────────────────────────────────────────────────

// buildSourceKey 生成用于聚合同一来源+参数的字符串键。
func buildSourceKey(sourceName, param string) string {
	return sourceName + "\x00" + param
}

// idCounter 是全局原子计数器，确保 generateID 在同一进程内始终返回唯一值。
var idCounter atomic.Int64

// generateID 生成一个进程内唯一的订阅 ID。
func generateID() string {
	seq := idCounter.Add(1)
	return fmt.Sprintf("sub-%d-%d", time.Now().UnixNano(), seq)
}

// ─── Plugin ───────────────────────────────────────────────────────────────────

// PluginHandle 持有 Manager 引用和尚未绑定调度器的数据源预注册列表，
// 允许在 pm.Register 之前调用 RegisterSource。
type PluginHandle struct {
	manager *Manager
	opts    managerOpts
	sources []Source // 在 Setup 之前预注册的数据源
}

// NewPlugin 创建一个订阅插件句柄。
//
// 可在调用 pm.Register(h.Descriptor()) 之前先调用 h.RegisterSource() 预注册数据源。
//
// 示例：
//
//	h := subscription.NewPlugin(
//	    subscription.WithDispatcher(dispatchFn),
//	    subscription.WithPollInterval(3 * time.Minute),
//	)
//	h.RegisterSource(&RSSSource{})
//	pm.Register(h.Descriptor())
func NewPlugin(opts ...Option) *PluginHandle {
	o := managerOpts{pollInterval: defaultPollInterval}
	for _, fn := range opts {
		fn(&o)
	}
	return &PluginHandle{
		manager: newManager(o),
		opts:    o,
	}
}

// RegisterSource 预注册一个数据源（可在 pm.Register 之前调用）。
func (h *PluginHandle) RegisterSource(src Source) {
	h.sources = append(h.sources, src)
}

// Manager 返回底层 Manager（在 Setup 完成后可安全使用）。
func (h *PluginHandle) Manager() *Manager {
	return h.manager
}

// Descriptor 返回可传给 pm.Register 的插件描述符。
//
// 声明对 "scheduler" 插件的依赖：插件系统会确保 scheduler 先于本插件初始化。
func (h *PluginHandle) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "subscription",
		Version: "1.0.0",
		Deps:    []string{"scheduler"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "推送订阅框架，支持多数据源定时轮询和多目标推送",
			Category:    "内置",
			Tags:        []string{"订阅", "推送", "RSS", "轮询"},
			HelpText: `订阅插件使用说明：
  h := subscription.NewPlugin(subscription.WithDispatcher(dispatchFn))
  h.RegisterSource(&RSSSource{})
  pm.Register(h.Descriptor())

  // 在 Handler 中订阅/退订
  mgr := plugin.Service[*subscription.Manager](ctx, "subscription")
  id, _ := mgr.Subscribe("rss", "https://...", subscription.Target{ChatID: groupID, IsGroup: true})
  _ = mgr.Unsubscribe(id)`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			m := h.manager
			// 获取 scheduler 插件（Service 确保热重载后仍有效）
			m.schedSvc = plugin.Service[*scheduler.Plugin](ctx, "scheduler")

			// 注册预注册的数据源
			for _, src := range h.sources {
				m.RegisterSource(src)
			}

			ctx.Log.Infof("Subscription plugin loaded (interval=%s, sources=%d)",
				m.pollInterval, len(m.sources))
			return m, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			m, ok := ctx.API.(*Manager)
			if !ok || m == nil {
				return nil
			}
			// 停止所有轮询任务（scheduler 本身也会在 Teardown 时停止，此处为安全起见）
			m.mu.Lock()
			for sourceKey, jobID := range m.sourceJobs {
				if m.sched() != nil {
					m.sched().Remove(jobID)
				}
				delete(m.sourceJobs, sourceKey)
			}
			m.mu.Unlock()
			ctx.Log.Info("Subscription plugin stopped")
			return nil
		},
	}
}
