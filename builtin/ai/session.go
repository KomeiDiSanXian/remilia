// Package ai session.go — 会话管理：Session 定义、LRU 缓存、消息裁剪、GORM 持久化记录。
//
// 本文件实现 AI 对话会话的完整生命周期管理：
//   - Session: 单个对话会话的数据结构
//   - SessionManager: LRU 缓存 + TTL 过期 + 可选持久化的会话管理器
//   - SessionStore: 持久化存储接口
//   - trimMessages: 上下文窗口裁剪（保留 System 消息 + 最近 maxHistory 条）
//   - sessionRecord: GORM 数据库记录结构体及转换方法
package ai

import (
	"container/list"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Session 表示一个 AI 对话会话，按 platform:chatID:userID 维度隔离。
//
// 字段说明：
//   - ID: 会话唯一标识，格式 "{platform}:{chatID}:{userID}"
//   - UserID: 用户 ID
//   - ChatID: 群组/频道 ID
//   - Messages: 对话消息列表（受 mu 保护）
//   - CreatedAt: 会话创建时间
//   - UpdatedAt: 会话最后活跃时间（用于 TTL 过期判断）
//   - CallCount: 本轮对话中 LLM API 的累计调用次数
//   - ToolCount: 本轮对话中工具调用的累计次数
//   - contentCache: 附件二进制内容的内存缓存（按 URL key，限本轮会话有效，不持久化）
type Session struct {
	mu        sync.Mutex
	turnMu    sync.Mutex
	ID        string
	UserID    string
	ChatID    string
	Messages  []Message
	CreatedAt time.Time
	UpdatedAt time.Time
	CallCount int
	ToolCount int

	// turnActive / interruptCh 回合活跃标志与中断信号（用户抢占机制）。
	// BeginTurn 开始回合并创建新信号；RequestInterrupt 关闭信号让进行中的
	// 回合尽快收尾；EndTurn 清理。processWithTools 在轮次与工具之间检查。
	turnActive   atomic.Bool
	interruptCh  chan struct{}
	interruptOne sync.Once

	contentCache map[string]*cachedContent `json:"-"`

	// 工具选择缓存（json:"-" 不持久化，重启后首次消息重新选择）。
	selCache *selectionCache `json:"-"`

	// toolFailures 各工具在本会话中的连续失败次数（json:"-" 不持久化）。
	// 用于工具执行的重试预算与反思引导；成功执行后归零。
	toolFailures map[string]int `json:"-"`

	// plan 当前任务计划（json:"-" 不持久化，重启后计划丢失）。
	// 由 create_plan 创建、update_plan_step 更新；每轮 LLM 调用前注入。
	plan *Plan `json:"-"`

	// ragCache 历史消息检索缓存（json:"-" 不持久化）。
	ragCache *ragCache `json:"-"`

	// trace 工具调用追踪（json:"-" 不持久化，诊断用途）。
	trace []ToolTraceEntry `json:"-"`

	// planAutoRounds 当前计划已后台自动推进的轮次（json:"-" 不持久化）。
	// 用户发新消息或 create_plan 时重置；planAutoStopped 为无进度停止标记。
	planAutoRounds  int  `json:"-"`
	planAutoStopped bool `json:"-"`

	// pendingImage 群聊"先发图再发字"的未消费图片（json:"-" 不持久化）。
	// 纯图片消息（无实质文本、未 @、未引用）到达时记录，等待窗口内文字
	// 合并为一条多模态消息；窗口超时或下一条已处理消息到达时清除。
	pendingImage *pendingImageState `json:"-"`
	// imageOverflowNotified 本次会话是否已提示过图片数量超限（json:"-" 不持久化）。
	imageOverflowNotified bool `json:"-"`
}

// pendingImageRef 未消费纯图片消息的引用（不持有二进制）。
// 消费时经 messagelog 附件存储解析为多模态 ContentPart（URL 过期免疫）；
// 引用持久化于会话记录，重启后窗口内 follow-up 仍可合并。
type pendingImageRef struct {
	ChatID        string // 会话 ID（消费时解析 messagelog）
	PlatformMsgID string // 平台消息 ID（= messagelog platform_message_id）
	URL           string // 直链兜底（存储未落库/不可用时回退下载）
	MimeType      string // 直链兜底时的类型提示
}

// pendingImageState 未消费的纯图片消息（合并窗口状态）。
type pendingImageState struct {
	Parts     []pendingImageRef // 待合并的图片引用（不持有二进制）
	Extra     int               // 超出二进制保留上限的额外图片数（仅计数，不持有二进制）
	Timestamp time.Time         // 最近一张图片的到达时间
}

// extendPendingImage 追加图片到当前合并窗口；无有效窗口或窗口已过期时新建。
// 纯图片消息本身不回复，仅记录等待后续文字合并（表情包防误触发）。
// maxKeep 限制 pending 持有的二进制图片数（超出部分仅计数，不下载/持有），
// 防止群里连发表情包导致内存无界增长；合并时用计数补足总数判断超限。
func (s *Session) extendPendingImage(refs []pendingImageRef, at time.Time, window time.Duration, maxKeep int) {
	if len(refs) == 0 {
		return
	}
	s.Lock()
	defer s.Unlock()
	if pendingImageWithin(s.pendingImage, window, at) {
		keep := min(maxKeep-len(s.pendingImage.Parts), len(refs))
		if keep > 0 {
			s.pendingImage.Parts = append(s.pendingImage.Parts, refs[:keep]...)
		}
		s.pendingImage.Extra += len(refs) - keep
		s.pendingImage.Timestamp = at
		return
	}
	keep := min(maxKeep, len(refs))
	s.pendingImage = &pendingImageState{
		Parts:     append([]pendingImageRef(nil), refs[:keep]...),
		Extra:     len(refs) - keep,
		Timestamp: at,
	}
}

// consumePendingImage 返回并清除窗口内的未消费图片；窗口不存在或已过期返回 nil。
// 返回值 (parts, extra)：parts 为持有的二进制图片，extra 为仅计数的额外图片数。
// 每次处理真实回合时调用：无论是否合并，pending 都被消费（防止跨回合误合并）。
func (s *Session) consumePendingImage(window time.Duration, now time.Time) ([]pendingImageRef, int) {
	s.Lock()
	defer s.Unlock()
	p := s.pendingImage
	s.pendingImage = nil
	if !pendingImageWithin(p, window, now) {
		return nil, 0
	}
	return p.Parts, p.Extra
}

// clearPendingImage 清除未消费图片状态（如单条图片数超限拒绝时）。
func (s *Session) clearPendingImage() {
	s.Lock()
	defer s.Unlock()
	s.pendingImage = nil
}

// markImageOverflowNotified 标记已提示过图片数量超限；返回是否为首次提示。
func (s *Session) markImageOverflowNotified() bool {
	s.Lock()
	defer s.Unlock()
	if s.imageOverflowNotified {
		return false
	}
	s.imageOverflowNotified = true
	return true
}

// pendingImageWithin 判断 pending 是否处于合并窗口内。
func pendingImageWithin(p *pendingImageState, window time.Duration, now time.Time) bool {
	if p == nil || len(p.Parts) == 0 {
		return false
	}
	if window <= 0 || p.Timestamp.IsZero() {
		return true
	}
	return now.Sub(p.Timestamp) <= window
}

// Lock 锁定会话，禁止并发访问 Messages 等可变字段。
func (s *Session) Lock() { s.mu.Lock() }

// Unlock 解锁会话。
func (s *Session) Unlock() { s.mu.Unlock() }

// LockTurn 串行化同一会话中的完整对话回合，避免并发请求交错写入历史。
func (s *Session) LockTurn() { s.turnMu.Lock() }

// UnlockTurn 解锁当前会话回合。
func (s *Session) UnlockTurn() { s.turnMu.Unlock() }

// TryLockTurn 非阻塞获取回合锁（用于后台任务：用户回合进行中时跳过本轮）。
func (s *Session) TryLockTurn() bool { return s.turnMu.TryLock() }

// --- 用户中断/抢占 ---

// TurnActive 返回是否有回合正在执行。
func (s *Session) TurnActive() bool { return s.turnActive.Load() }

// BeginTurn 标记回合开始并重置中断信号。已活跃时返回 false（重复进入）。
func (s *Session) BeginTurn() bool {
	if !s.turnActive.CompareAndSwap(false, true) {
		return false
	}
	s.interruptCh = make(chan struct{})
	s.interruptOne = sync.Once{}
	return true
}

// EndTurn 结束回合并清理中断信号。
func (s *Session) EndTurn() {
	s.turnActive.Store(false)
	s.interruptCh = nil
}

// RequestInterrupt 请求中断进行中的回合（非阻塞；无活跃回合时 no-op）。
func (s *Session) RequestInterrupt() {
	ch := s.interruptCh
	if ch == nil {
		return
	}
	s.interruptOne.Do(func() { close(ch) })
}

// Interrupted 返回当前回合是否被请求中断（非阻塞）。
func (s *Session) Interrupted() bool {
	ch := s.interruptCh
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// TurnCtx 返回随当前回合中断信号自动取消的子上下文。
//
// RequestInterrupt 触发时会关闭 interruptCh，从而取消返回的上下文，使
// 进行中的 LLM 流请求尽快中止——/ai stop 与用户新消息抢占由此对"单轮流"
// 同样生效（此前中断只在工具轮之间的检查点生效，单轮流需等流自然结束）。
// 未处于回合（interruptCh 为 nil，如后台总结/校验子任务）时等价于 parent。
//
// 返回的 cancel 必须在流结束（本轮结束，含出错提前返回）时调用，以回收
// 监听 goroutine；cancel 会等待监听协程退出，不会泄漏。
func (s *Session) TurnCtx(parent context.Context) (context.Context, context.CancelFunc) {
	ch := s.interruptCh
	if ch == nil {
		return parent, func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		close(done)
	}()
	return ctx, func() {
		cancel()
		<-done
	}
}

// --- 计划后台自动推进状态 ---

// PlanAutoRounds 返回已自动推进轮次。
func (s *Session) PlanAutoRounds() int {
	s.Lock()
	defer s.Unlock()
	return s.planAutoRounds
}

// BumpPlanAutoRounds 递增自动推进轮次并返回新值。
func (s *Session) BumpPlanAutoRounds() int {
	s.Lock()
	defer s.Unlock()
	s.planAutoRounds++
	return s.planAutoRounds
}

// PlanAutoStopped 返回是否被无进度停止标记。
func (s *Session) PlanAutoStopped() bool {
	s.Lock()
	defer s.Unlock()
	return s.planAutoStopped
}

// StopPlanAuto 标记无进度停止（停止后续自动推进）。
func (s *Session) StopPlanAuto() {
	s.Lock()
	defer s.Unlock()
	s.planAutoStopped = true
}

// ResetPlanAuto 重置自动推进预算与停止标记（用户发消息 / 创建新计划时）。
func (s *Session) ResetPlanAuto() {
	s.Lock()
	defer s.Unlock()
	s.planAutoRounds = 0
	s.planAutoStopped = false
}

// getCachedContent 从内存缓存中获取附件内容。已过期或不存在返回 nil。
// 线程安全，持有 session 读锁。
func (s *Session) getCachedContent(url string) *cachedContent {
	s.Lock()
	defer s.Unlock()
	if s.contentCache == nil {
		return nil
	}
	c, ok := s.contentCache[url]
	if !ok {
		return nil
	}
	if time.Now().After(c.ExpireAt) {
		delete(s.contentCache, url)
		return nil
	}
	return c
}

// setCachedContent 将附件内容存入内存缓存，TTL 默认 10 分钟。
// 线程安全，持有 session 写锁。
func (s *Session) setCachedContent(url string, data []byte, mimeType, audioFormat string) {
	s.Lock()
	defer s.Unlock()
	if s.contentCache == nil {
		s.contentCache = make(map[string]*cachedContent)
	}
	s.contentCache[url] = &cachedContent{
		Data:        data,
		MimeType:    mimeType,
		AudioFormat: audioFormat,
		ExpireAt:    time.Now().Add(10 * time.Minute),
	}
}

// SessionManager 管理 AI 对话会话，使用 LRU 淘汰策略。
//
// 功能：
//   - GetOrCreate: 自动创建或获取会话（LRU 缓存 → 持久化存储 → 新建）
//   - Save: 持久化保存会话
//   - Delete: 删除会话（LRU + 持久化）
//   - AppendMessage: 追加消息并自动裁剪上下文窗口
//   - CleanupExpired: 清理超过 TTL 未活跃的会话
//   - evictLocked: LRU 淘汰（达到 maxSize 时淘汰最久未访问的）
type SessionManager struct {
	mu         sync.RWMutex
	sessions   map[string]*list.Element
	lru        *list.List
	maxSize    int
	maxHistory int
	ttl        time.Duration
	storage    SessionStore
}

// sessionEntry 包装 Session，作为 LRU 链表的节点值。
type sessionEntry struct {
	session *Session
}

// SessionStore 会话持久化存储接口。
//
// 实现此接口可将会话存储到不同后端（数据库、Redis 等）。
// 内置实现：gormSessionStore（基于 GORM）、noopSessionStore（空实现）。
type SessionStore interface {
	Load(sessionID string) (*Session, error)
	Save(session *Session) error
	Delete(sessionID string) error
}

// NewSessionManager 创建会话管理器。
//
// 参数：
//   - maxSize: LRU 缓存最大会话数，<= 0 时使用默认值 1000
//   - maxHistory: 保留的最大消息条数，<= 0 时使用默认值 20
//   - ttl: 会话 TTL，超过此时间未活跃的会话将被 CleanupExpired 清理
//   - storage: 持久化存储实现，为 nil 时不持久化
func NewSessionManager(maxSize, maxHistory int, ttl time.Duration, storage SessionStore) *SessionManager {
	sm := &SessionManager{
		sessions:   make(map[string]*list.Element),
		lru:        list.New(),
		maxSize:    maxSize,
		maxHistory: maxHistory,
		ttl:        ttl,
		storage:    storage,
	}
	if sm.maxSize <= 0 {
		sm.maxSize = 1000
	}
	if sm.maxHistory <= 0 {
		sm.maxHistory = 20
	}
	return sm
}

// GetOrCreate 获取或创建会话。
// 先从 LRU 缓存查找，未命中时尝试从持久化存储加载，
// 都未找到时创建全新的会话。
func (sm *SessionManager) GetOrCreate(sessionID, userID, chatID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if elem, ok := sm.sessions[sessionID]; ok {
		entry := elem.Value.(*sessionEntry)
		sm.lru.MoveToFront(elem)
		entry.session.Lock()
		entry.session.UpdatedAt = time.Now()
		trimMessages(entry.session, sm.maxHistory)
		entry.session.Unlock()
		return entry.session
	}

	session := &Session{
		ID:        sessionID,
		UserID:    userID,
		ChatID:    chatID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if sm.storage != nil {
		if stored, err := sm.storage.Load(sessionID); err == nil && stored != nil {
			session = stored
			session.Lock()
			session.UpdatedAt = time.Now()
			trimMessages(session, sm.maxHistory)
			session.Unlock()
		}
	}

	elem := sm.lru.PushFront(&sessionEntry{session: session})
	sm.sessions[sessionID] = elem

	sm.evictLocked()

	return session
}

// Peek 仅从内存缓存查找会话，不创建、不触碰持久化存储。
// 用于只需判断"会话是否存在/当前是否活跃"的场景（如 QQ 按钮回调忙时预检，
// 不应因误点旧消息的按钮而凭空创建一个空会话）；未命中返回 nil。
func (sm *SessionManager) Peek(sessionID string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if elem, ok := sm.sessions[sessionID]; ok {
		return elem.Value.(*sessionEntry).session
	}
	return nil
}

// Save 持久化保存会话到存储后端。调用方应已持有 session 锁。
//
// 如果调用方未持有锁，请使用 [SaveSession] 替代。
func (sm *SessionManager) saveNoLock(session *Session) {
	session.UpdatedAt = time.Now()
	trimMessages(session, sm.maxHistory)

	if sm.storage != nil {
		if err := sm.storage.Save(session); err != nil {
			logger.Errorf("[AI] Failed to persist session %s: %v", session.ID, err)
		}
	}
}

// SaveSession 线程安全地持久化保存会话，自动加锁。
func (sm *SessionManager) SaveSession(session *Session) {
	session.Lock()
	defer session.Unlock()
	sm.saveNoLock(session)
}

// Delete 删除指定会话（从 LRU 和持久化存储中）。
func (sm *SessionManager) Delete(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if elem, ok := sm.sessions[sessionID]; ok {
		sm.lru.Remove(elem)
		delete(sm.sessions, sessionID)
	}

	if sm.storage != nil {
		_ = sm.storage.Delete(sessionID)
	}
}

// CleanupExpired 清理过期的会话。
// 由插件 Setup 中启动的后台 goroutine 定期调用。
// 先持锁收集过期会话并移出缓存，释放锁后再执行持久化删除，避免 DB I/O 阻塞其他会话操作。
func (sm *SessionManager) CleanupExpired() {
	var expiredIDs []string

	sm.mu.Lock()
	now := time.Now()
	for id, elem := range sm.sessions {
		entry := elem.Value.(*sessionEntry)
		entry.session.Lock()
		expired := sm.ttl > 0 && now.After(entry.session.UpdatedAt.Add(sm.ttl))
		entry.session.Unlock()
		if expired {
			sm.lru.Remove(elem)
			delete(sm.sessions, id)
			expiredIDs = append(expiredIDs, id)
		}
	}
	sm.mu.Unlock()

	if sm.storage != nil {
		for _, id := range expiredIDs {
			_ = sm.storage.Delete(id)
		}
	}
}

// evictLocked 在持有锁时执行 LRU 淘汰。
func (sm *SessionManager) evictLocked() {
	for sm.lru.Len() > sm.maxSize {
		elem := sm.lru.Back()
		if elem == nil {
			break
		}
		entry := elem.Value.(*sessionEntry)
		delete(sm.sessions, entry.session.ID)
		sm.lru.Remove(elem)
	}
}

// trimMessages 裁剪消息列表，保留最近的 maxHistory 条消息。
// System 消息优先保留。
//
// 裁剪边界不会落在 tool 消息上：tool 消息必须紧邻其前的
// assistant(tool_calls)（OpenAI/Anthropic API 硬性约束），若 assistant
// 被裁掉而 tool 消息残留，会产生孤儿 tool 消息，下一次请求会被 400 拒绝。
// 因此起点会向前推进，把残留的 tool 响应连同其 assistant 一起裁掉。
func trimMessages(s *Session, maxHistory int) {
	if maxHistory <= 0 {
		return
	}

	usable := maxHistory
	if usable > len(s.Messages) {
		return
	}

	var systemMsgs []Message
	var otherMsgs []Message
	for _, m := range s.Messages {
		if m.Role == RoleSystem {
			systemMsgs = append(systemMsgs, m)
		} else {
			otherMsgs = append(otherMsgs, m)
		}
	}

	targetOther := max(usable-len(systemMsgs), 0)
	if len(otherMsgs) > targetOther {
		start := max(len(otherMsgs)-targetOther, 0)
		for start < len(otherMsgs) && otherMsgs[start].Role == RoleTool {
			start++
		}
		otherMsgs = otherMsgs[start:]
	}

	s.Messages = append(systemMsgs, otherMsgs...)
}

// AppendMessage 向会话追加一条消息，自动裁剪上下文窗口并持久化。
// 线程安全，持有 session 写锁。
func (sm *SessionManager) AppendMessage(session *Session, msg Message) {
	session.Lock()
	defer session.Unlock()
	session.Messages = append(session.Messages, msg)
	sm.saveNoLock(session)
}

// SnapshotMessages 返回会话消息副本，供不修改历史的读取路径使用。
func (s *Session) SnapshotMessages() []Message {
	s.Lock()
	defer s.Unlock()
	msgs := make([]Message, len(s.Messages))
	copy(msgs, s.Messages)
	return msgs
}

// ToolTraceEntry 一次工具调用的追踪记录（调用链可观测性）。
type ToolTraceEntry struct {
	// Time 调用开始时间。
	Time time.Time `json:"time"`
	// ToolName 工具名。
	ToolName string `json:"tool_name"`
	// Args 参数摘要（截断，防敏感信息泄漏）。
	Args string `json:"args,omitempty"`
	// Duration 执行耗时。
	Duration time.Duration `json:"duration"`
	// Err 非空表示执行失败（错误文本摘要）。
	Err string `json:"err,omitempty"`
}

// maxToolTrace 每个会话保留的最大工具调用追踪条数（超出淘汰最旧）。
const maxToolTrace = 50

// appendToolTrace 追加一条工具调用追踪（线程安全，超出上限淘汰最旧）。
func (s *Session) appendToolTrace(e ToolTraceEntry) {
	s.Lock()
	defer s.Unlock()
	s.trace = append(s.trace, e)
	if len(s.trace) > maxToolTrace {
		s.trace = s.trace[len(s.trace)-maxToolTrace:]
	}
}

// ToolTrace 返回会话的工具调用追踪副本（从旧到新）。
func (s *Session) ToolTrace() []ToolTraceEntry {
	s.Lock()
	defer s.Unlock()
	out := make([]ToolTraceEntry, len(s.trace))
	copy(out, s.trace)
	return out
}

// incrToolFailure 递增指定工具的连续失败次数并返回新值。
// 线程安全。
func (s *Session) incrToolFailure(name string) int {
	s.Lock()
	defer s.Unlock()
	if s.toolFailures == nil {
		s.toolFailures = make(map[string]int)
	}
	s.toolFailures[name]++
	return s.toolFailures[name]
}

// resetToolFailure 清除指定工具的连续失败计数（执行成功时调用）。
// 线程安全。
func (s *Session) resetToolFailure(name string) {
	s.Lock()
	defer s.Unlock()
	if s.toolFailures != nil {
		delete(s.toolFailures, name)
	}
}

// --- GORM 持久化记录 ---

// sessionRecord 对应数据库表，用于 GORM 持久化。
type sessionRecord struct {
	ID        string `gorm:"primaryKey"`
	UserID    string `gorm:"index"`
	ChatID    string `gorm:"index"`
	Messages  string `gorm:"type:text"`
	CallCount int    `gorm:"default:0"`
	ToolCount int    `gorm:"default:0"`
	// Plan 进行中的任务计划（JSON；跨重启继续执行）。
	Plan string `gorm:"type:text"`
	// PendingImages 未消费图片引用（JSON；跨重启窗口内仍可合并）。
	PendingImages string `gorm:"type:text"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// toRecord 将 Session 转换为数据库记录。
func (s *Session) toRecord() *sessionRecord {
	data, err := json.Marshal(messagesForPersistence(s.Messages))
	if err != nil {
		logger.Errorf("[AI] Failed to marshal session messages for %s: %v", s.ID, err)
		data = []byte("[]")
	}
	var planJSON string
	if s.plan != nil {
		if pb, perr := json.Marshal(s.plan); perr == nil {
			planJSON = string(pb)
		} else {
			logger.Errorf("[AI] Failed to marshal session plan for %s: %v", s.ID, perr)
		}
	}
	var pendingJSON string
	if s.pendingImage != nil {
		if pb, perr := json.Marshal(s.pendingImage); perr == nil {
			pendingJSON = string(pb)
		} else {
			logger.Errorf("[AI] Failed to marshal pending images for %s: %v", s.ID, perr)
		}
	}
	return &sessionRecord{
		ID:            s.ID,
		UserID:        s.UserID,
		ChatID:        s.ChatID,
		Messages:      string(data),
		CallCount:     s.CallCount,
		ToolCount:     s.ToolCount,
		Plan:          planJSON,
		PendingImages: pendingJSON,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

// messagesForPersistence 不保存附件二进制数据。重启后以文本占位保留上下文，
// 避免向 Provider 发送无法复原的空多模态内容。
func messagesForPersistence(messages []Message) []Message {
	result := make([]Message, len(messages))
	for i, message := range messages {
		result[i] = message
		if len(message.ContentParts) == 0 {
			continue
		}

		parts := make([]string, 0, len(message.ContentParts))
		for _, part := range message.ContentParts {
			switch part.Type {
			case ContentPartText:
				if part.Text != "" {
					parts = append(parts, part.Text)
				}
			case ContentPartImage:
				parts = append(parts, "[用户发送了图片，附件内容在重启后不可用]")
			case ContentPartAudio:
				parts = append(parts, "[用户发送了音频，附件内容在重启后不可用]")
			}
		}
		result[i].Content = strings.Join(parts, "\n")
		result[i].ContentParts = nil
	}
	return result
}

// toSession 将数据库记录还原为 Session。
func (r *sessionRecord) toSession() *Session {
	var msgs []Message
	if err := json.Unmarshal([]byte(r.Messages), &msgs); err != nil {
		logger.Warnf("[AI] Failed to unmarshal session messages (corrupted?): %v", err)
	}
	s := &Session{
		ID:        r.ID,
		UserID:    r.UserID,
		ChatID:    r.ChatID,
		Messages:  msgs,
		CallCount: r.CallCount,
		ToolCount: r.ToolCount,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	if r.Plan != "" {
		var plan Plan
		if err := json.Unmarshal([]byte(r.Plan), &plan); err == nil {
			s.plan = &plan
		} else {
			logger.Warnf("[AI] Failed to unmarshal session plan (corrupted?): %v", err)
		}
	}
	if r.PendingImages != "" {
		var pi pendingImageState
		if err := json.Unmarshal([]byte(r.PendingImages), &pi); err == nil {
			s.pendingImage = &pi
		} else {
			logger.Warnf("[AI] Failed to unmarshal pending images (corrupted?): %v", err)
		}
	}
	return s
}
