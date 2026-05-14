// Package router 提供优先级驱动的路由分发层。
//
// 所有路由规则（包括内建 FSM）都是 [RouteRule]，按 Priority 排序后统一执行。
// Priority 越小越优先。FSM 内建规则优先于所有用户声明的规则。
//
// 使用方式：
//
//	rtr := router.New(eng, fsmMgr.Engine())
//	rtr.Route(router.WithCommandPrefix())
//	rtr.Route(router.WithFSMRoute())
package router

import (
	"sort"
	"strings"
	"unicode"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

const (
	// FSMPriority FSM 内建路由的优先级，低于所有用户规则。
	FSMPriority = -1000
)

// RouteRule 定义一条路由规则。
type RouteRule struct {
	// Name 是用于调试的可读标识。
	Name string
	// Priority 决定评估顺序。越小越优先。用户规则默认从 0 开始。
	Priority int
	// Match 判断此规则是否适用。
	Match func(ctx *corectx.Context) bool
	// Handle 执行路由逻辑。返回 true 表示已处理，后续规则不再执行。
	Handle func(ctx *corectx.Context) bool
}

// Router 按优先级评估路由规则并将事件分发到对应的处理器。
type Router struct {
	engine    *engine.Engine
	fsmEngine *fsm.Engine
	rules     []*RouteRule
}

// New 创建一个 Router。若 fsmEngine 非 nil，自动注册一条 Priority=-1000 的 FSM 规则。
func New(e *engine.Engine, fsmEngine *fsm.Engine) *Router {
	r := &Router{
		engine:    e,
		fsmEngine: fsmEngine,
		rules:     make([]*RouteRule, 0),
	}
	if fsmEngine != nil {
		r.rules = append(r.rules, &RouteRule{
			Name:     "fsm",
			Priority: FSMPriority,
			Match:    func(ctx *corectx.Context) bool { return true },
			Handle:   r.handleFSM,
		})
	}
	return r
}

// Route 添加一条路由规则。规则按 Priority 排序评估，相同 Priority 按添加顺序。
//
// 若 rule.Handle 为 nil，自动设置为 dispatchToEngine（匹配时路由到 Engine 并停止后续规则）。
// 这使大多数 "匹配即路由到 Engine" 的规则（如 WithCommandPrefix）无需显式设置 Handle。
func (r *Router) Route(rule *RouteRule) {
	if rule == nil || rule.Match == nil {
		return
	}
	if rule.Handle == nil {
		rule.Handle = func(ctx *corectx.Context) bool {
			r.dispatchToEngine(ctx)
			return true
		}
	}
	if rule.Name == "" {
		rule.Name = "unnamed"
	}
	r.rules = append(r.rules, rule)
}

// Dispatch 按 Priority 评估所有路由规则，第一个 Handle 返回 true 的规则胜出。
// 若无规则匹配，回退到标准 Engine。
func (r *Router) Dispatch(ctx *corectx.Context) {
	if ctx == nil {
		return
	}

	sorted := make([]*RouteRule, len(r.rules))
	copy(sorted, r.rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	for _, rule := range sorted {
		if rule.Match(ctx) && rule.Handle(ctx) {
			return
		}
	}

	r.dispatchToEngine(ctx)
}

// handleFSM 先尝试迁移（已有会话），再尝试启动（匹配启动事件）。
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

// dispatchToEngine 直接调用引擎处理事件。
func (r *Router) dispatchToEngine(ctx *corectx.Context) {
	r.engine.ProcessEvent(ctx)
}

// extractCommand 从内容中提取第一个空白分隔的 token。
func extractCommand(content string) string {
	trimmed := strings.TrimSpace(content)
	idx := strings.IndexFunc(trimmed, unicode.IsSpace)
	if idx == -1 {
		return trimmed
	}
	return trimmed[:idx]
}

// makeSessionID 从事件上下文构造会话标识符。格式："platform:chatID"
func makeSessionID(ctx *corectx.Context) string {
	platform := ctx.GetEventPlatform()
	chat := ctx.GetChatInfo()
	if chat.ID == "" {
		return platform
	}
	return platform + ":" + chat.ID
}
