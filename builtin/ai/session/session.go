// Package session session.go — 会话运行时状态：字段、锁与回合生命周期。
//
// 会话是 AI 插件中被各层共享的唯一可变状态：消息历史、回合标志、计划推进预算、
// 附件缓存，以及选路/检索/工具集三类会话级缓存。本包只回答"会话是什么、如何被
// 安全读写"，不含选择、检索与执行策略。
package session

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
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
	Messages  []protocol.Message
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

	contentCache map[string]*CachedContent `json:"-"`

	// 工具选择缓存（json:"-" 不持久化，重启后首次消息重新选择）。
	selCache *SelectionCache `json:"-"`

	// toolSet 会话级工具集稳定状态（json:"-" 不持久化，重启后重新收敛）。
	// 用于抑制工具集抖动——tools 是请求前缀的前段，集合一变其后的
	// 历史全部失去前缀缓存（见 toolset.go）。
	toolSet *ToolSetState `json:"-"`

	// toolFailures 各工具在本会话中的连续失败次数（json:"-" 不持久化）。
	// 用于工具执行的重试预算与反思引导；成功执行后归零。
	toolFailures map[string]int `json:"-"`

	// plan 当前任务计划（json:"-" 不持久化，重启后计划丢失）。
	// 由 create_plan 创建、update_plan_step 更新；每轮 LLM 调用前注入。
	plan *Plan `json:"-"`

	// ragCache 历史消息检索缓存（json:"-" 不持久化）。
	ragCache *RAGCache `json:"-"`

	// trace 工具调用追踪（json:"-" 不持久化，诊断用途）。
	trace []ToolTraceEntry `json:"-"`

	// planAutoRounds 当前计划已后台自动推进的轮次（json:"-" 不持久化）。
	// 用户发新消息或 create_plan 时重置；planAutoStopped 为无进度停止标记。
	planAutoRounds  int  `json:"-"`
	planAutoStopped bool `json:"-"`

	// pendingImage 群聊"先发图再发字"的未消费图片（json:"-" 不持久化）。
	// 纯图片消息（无实质文本、未 @、未引用）到达时记录，等待窗口内文字
	// 合并为一条多模态消息；窗口超时或下一条已处理消息到达时清除。
	pendingImage *PendingImageState `json:"-"`
	// imageOverflowNotified 本次会话是否已提示过图片数量超限（json:"-" 不持久化）。
	imageOverflowNotified bool `json:"-"`
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

// SnapshotMessages 返回会话消息副本，供不修改历史的读取路径使用。
func (s *Session) SnapshotMessages() []protocol.Message {
	s.Lock()
	defer s.Unlock()
	msgs := make([]protocol.Message, len(s.Messages))
	copy(msgs, s.Messages)
	return msgs
}
