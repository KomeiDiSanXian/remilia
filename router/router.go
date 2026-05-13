// Package router 提供轻量级的策略驱动事件分发层，
// 位于 Bot 的平台事件处理器和底层 Engine 之间。
//
// Router 按顺序评估规则，决定事件应由标准 Engine、
// FSM 引擎还是 Agent 处理。规则按优先级顺序评估，第一个匹配者胜出。
// 如果没有规则匹配，事件回退到标准 Engine，保持现有行为不变。
//
// 默认路由：
//   - 命令（以非字母数字前缀开头的消息，如 "/"）→ Engine
//   - 有活跃 FSM 会话的消息 → FSM 引擎（若无状态变更则回退到 Engine）
//   - 其他所有消息 → Engine
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
	// StrategyFSM 路由到 FSM 引擎。
	StrategyFSM
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
// 它是一个轻量的、无状态的协调器。
type Router struct {
	engine        *engine.Engine
	engineManager *engine.EngineManager
	fsmEngine     *fsm.Engine
	rules         []*RouteRule
}

// New 创建一个 Router，使用给定的 Engine 和可选的 FSM 引擎。
// 如果 fsmEngine 为 nil，StrategyFSM 规则将回退到 Engine。
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

// Dispatch 对所有规则评估上下文并路由到匹配的处理器。
// 如果没有规则匹配，事件回退到标准 Engine（保持现有行为不变）。
func (r *Router) Dispatch(ctx *corectx.Context) {
	if ctx == nil {
		return
	}

	for _, rule := range r.rules {
		if !rule.Match(ctx) {
			continue
		}
		switch rule.Strategy {
		case StrategyEngine:
			if r.engineManager != nil {
				r.engineManager.Dispatch(ctx)
			} else {
				r.engine.ProcessEvent(ctx)
			}
			return
		case StrategyFSM:
			if r.fsmEngine != nil {
				sessionID := makeSessionID(ctx)
				// 1) 已有活跃会话 → 迁移
				state, ok, err := r.fsmEngine.TryTransition(ctx, sessionID)
				if err != nil {
					logger.WithError(err).Warn("[router] FSM TryTransition error")
				}
				if ok {
					logger.Debugf("[router] FSM transition: %s", state)
					return
				}
				// 2) 无活跃会话 → 尝试启动新 FSM 会话
				started, err := r.fsmEngine.TryStartSession(ctx, sessionID)
				if err != nil {
					logger.WithError(err).Warn("[router] FSM TryStartSession error")
				}
				if started {
					logger.Debug("[router] FSM session started")
					return
				}
				// 均未命中 → fallthrough 到下一规则
				continue
			}
		case StrategyAgent:
			if r.handleAgent(ctx) {
				return
			}
			continue
		}
	}

	// Fallback: no rule matched or FSM/Agent fell through
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
