// Package router 提供轻量级的策略驱动事件分发层，
// 位于 Bot 的平台事件处理器和底层 Engine 之间。
//
// Router 按顺序评估规则，决定事件应由标准 Engine 还是 Agent 处理。
// 规则按优先级顺序评估，第一个匹配者胜出。
// 如果没有规则匹配，事件回退到标准 Engine，保持现有行为不变。
//
// FSM（有限状态机）是内建的一级路由，不受规则声明顺序影响：
//   - 有活跃 FSM 会话的消息 → FSM 引擎优先处理
//   - 匹配 FSM 启动事件的消息 → 自动创建会话
//   - 其余消息 → 按用户声明的规则分发
//
// 用户无需将 WithFSMRoute 放首位——FSM 始终最先检查。
package router

import (
	"strings"
	"unicode"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Strategy 指示哪个处理器应处理匹配的事件。
type Strategy int

const (
	// StrategyEngine 路由到标准 Engine。
	StrategyEngine Strategy = iota
	// StrategyAgent 路由到 LLM Agent（可选占位）。
	StrategyAgent
)

// RouteRule 定义一条带有匹配谓词和目标策略的路由规则。
type RouteRule struct {
	// Name 是用于调试的人类可读标识。
	Name string
	// Strategy 是此规则匹配时要使用的处理器。
	Strategy Strategy
	// Match 是判断此规则是否适用的谓词。
	Match func(ctx *corectx.Context) bool
	// Priority 决定评估顺序（较低的值先评估）。
	Priority int
}

// Router 按顺序评估路由规则并将事件分发到相应的处理器。
// FSM 是内建的一级路由，不受规则声明顺序影响——始终最先检查。
type Router struct {
	engine        *engine.Engine
	engineManager *engine.EngineManager
	fsmEngine     *fsm.Engine
	rules         []*RouteRule
}

// New 创建一个 Router，使用给定的 Engine 和可选的 FSM 引擎。
// 如果 fsmEngine 为 nil，FSM 内建路由不会生效。
func New(e *engine.Engine, fsmEngine *fsm.Engine) *Router {
	return &Router{
		engine:    e,
		fsmEngine: fsmEngine,
		rules:     make([]*RouteRule, 0),
	}
}

// WithEngineManager 将 EngineManager 附加到 Router。
// 设置后，StrategyEngine 规则通过 EngineManager 分派到 per-channel Engine，
// 而非直接调 Engine.ProcessEvent。实现 Router + EngineManager 组合使用。
func (r *Router) WithEngineManager(em *engine.EngineManager) *Router {
	r.engineManager = em
	return r
}

// Route 添加一条路由规则。规则按添加顺序评估（第一个匹配者胜出）。
func (r *Router) Route(rule *RouteRule) {
	if rule == nil || rule.Match == nil {
		return
	}
	r.rules = append(r.rules, rule)
}

// Dispatch 分发事件。FSM 始终最先检查（不受规则顺序影响），
// 然后按用户声明的规则顺序评估，最后回退到标准 Engine。
func (r *Router) Dispatch(ctx *corectx.Context) {
	if ctx == nil {
		return
	}

	// FSM 是内建的一级路由，始终在用户规则之前检查。
	// 用户无需关心 WithFSMRoute 的声明顺序。
	if r.fsmEngine != nil && r.handleFSM(ctx) {
		return
	}

	// 用户声明的规则
	for _, rule := range r.rules {
		if !rule.Match(ctx) {
			continue
		}
		switch rule.Strategy {
		case StrategyEngine:
			r.dispatchToEngine(ctx)
			return
		case StrategyAgent:
			if r.handleAgent(ctx) {
				return
			}
			continue
		}
	}

	// Fallback: 无规则匹配
	r.dispatchToEngine(ctx)
}

// handleFSM 先尝试迁移（已有会话），再尝试启动（匹配启动事件）。
// 返回 true 表示 FSM 已处理该事件。
func (r *Router) handleFSM(ctx *corectx.Context) bool {
	sessionID := makeSessionID(ctx)
	state, ok, err := r.fsmEngine.TryTransition(ctx, sessionID)
	if err != nil {
		logger.WithError(err).Warn("[router] FSM TryTransition error")
	}
	if ok {
		logger.Debugf("[router] FSM transition: %s", state)
		return true
	}
	started, err := r.fsmEngine.TryStartSession(ctx, sessionID)
	if err != nil {
		logger.WithError(err).Warn("[router] FSM TryStartSession error")
	}
	if started {
		logger.Debug("[router] FSM session started")
		return true
	}
	return false
}

// dispatchToEngine 通过 engineManager（如有）或直接调 Engine 分发事件。
func (r *Router) dispatchToEngine(ctx *corectx.Context) {
	if r.engineManager != nil {
		r.engineManager.Dispatch(ctx)
	} else {
		r.engine.ProcessEvent(ctx)
	}
}

// handleAgent 是 LLM Agent 路由的占位符。
// 当前始终返回 false，导致回退到 Engine。
func (r *Router) handleAgent(ctx *corectx.Context) bool {
	return false
}

// makeSessionID 从事件上下文构造会话标识符。
// 格式："platform:chatID"
func makeSessionID(ctx *corectx.Context) string {
	platform := ctx.GetEventPlatform()
	chat := ctx.GetChatInfo()
	if chat.ID == "" {
		return platform
	}
	return platform + ":" + chat.ID
}

// extractCommand 从内容中提取第一个空白分隔的 token。
// 等同于未导出的 engine.extractCommand。
func extractCommand(content string) string {
	trimmed := strings.TrimSpace(content)
	idx := strings.IndexFunc(trimmed, unicode.IsSpace)
	if idx == -1 {
		return trimmed
	}
	return trimmed[:idx]
}
