package router_test

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

type testEvent struct {
	platform string
	content  string
	chatID   string
	kind     platform.EventKind
}

func (e *testEvent) Platform() string                          { return e.platform }
func (e *testEvent) ID() string                                { return "" }
func (e *testEvent) Kind() platform.EventKind                  { return e.kind }
func (e *testEvent) Sender() platform.UserInfo                 { return platform.UserInfo{ID: "user1"} }
func (e *testEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{ID: e.chatID} }
func (e *testEvent) Content() string                           { return e.content }
func (e *testEvent) Attachments() []platform.InboundAttachment { return nil }
func (e *testEvent) Timestamp() time.Time                      { return time.Time{} }

func ctx(content string) *corectx.Context {
	evt := &testEvent{platform: "test", content: content, chatID: "ch1", kind: platform.EventKindPrivateMessage}
	return corectx.NewContextFromEvent(evt, &platform.NoopSender{})
}

func TestRouter_NoRules_FallbackToEngine(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	r := router.New(eng, nil)

	handled := false
	eng.OnCommand("PRIVATE_MESSAGE", "/ping").Handle(func(ctx *corectx.Context) error {
		handled = true
		return nil
	})

	r.Dispatch(ctx("/ping"))
	eng.WaitForAsyncHandlers()
	assert.True(t, handled, "should fallback to engine when no rules")
}

func TestRouter_CommandPrefix_RoutesToEngine(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	r := router.New(eng, nil)

	var handledAtomic bool
	eng.OnCommand("PRIVATE_MESSAGE", "/help").Handle(func(ctx *corectx.Context) error {
		handledAtomic = true
		return nil
	})

	r.Route(router.WithCommandPrefix())
	r.Dispatch(ctx("/help"))
	assert.True(t, handledAtomic, "command prefix should route to engine")
}

func TestRouter_CommandPrefix_DoesNotMatchPlainText(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	r := router.New(eng, nil)

	var matched bool
	eng.OnCommand("PRIVATE_MESSAGE", "/help").Handle(func(ctx *corectx.Context) error {
		matched = true
		return nil
	})

	r.Route(router.WithCommandPrefix())
	r.Dispatch(ctx("hello world"))
	assert.False(t, matched, "plain text should not be routed to command handler")
}

func TestRouter_FSM_Hit(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	mgr := fsm.NewManager(nil)
	r := router.New(eng, mgr.Engine())

	formFSM := &fsm.FSM{
		Name: "test", Initial: "s",
		Events: []fsm.Event{
			{Name: "go", From: "s", To: "d", Match: func(ctx *corectx.Context) bool {
				return ctx.GetMessageContent() == "start"
			}},
		},
	}
	require.NoError(t, mgr.Register(&fsm.FSMDescriptor{Name: "test", FSM: formFSM}))
	require.NoError(t, mgr.Engine().StartSession(ctx("start"), "test", "test:ch1"))

	// FSM 是内建路由，无需声明 WithFSMRoute
	r.Route(router.WithCommandPrefix())

	handled := false
	eng.OnEventKind(platform.EventKindPrivateMessage).Handle(func(ctx *corectx.Context) error {
		handled = true
		return nil
	})

	r.Dispatch(ctx("start"))

	sess := mgr.Engine().GetSession("test:ch1")
	require.NotNil(t, sess)
	assert.Equal(t, fsm.State("d"), sess.Current, "FSM should have transitioned")
	assert.False(t, handled, "FSM hit should prevent engine from handling")
}

func TestRouter_FSM_NoSession_FallthroughToEngine(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	mgr := fsm.NewManager(nil)
	r := router.New(eng, mgr.Engine())

	r.Route(router.WithCommandPrefix())

	handled := false
	eng.OnAny().Handle(func(ctx *corectx.Context) error {
		handled = true
		return nil
	})

	r.Dispatch(ctx("hello"))
	eng.WaitForAsyncHandlers()
	assert.True(t, handled, "no FSM session should fallthrough to engine")
}

func TestRouter_PriorityOrder(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	r := router.New(eng, nil)

	var order []string

	r.Route(&router.RouteRule{
		Name: "first", Priority: 1,
		Strategy: router.StrategyEngine,
		Match: func(ctx *corectx.Context) bool {
			order = append(order, "first")
			return false
		},
	})
	r.Route(&router.RouteRule{
		Name: "second", Priority: 2,
		Strategy: router.StrategyEngine,
		Match: func(ctx *corectx.Context) bool {
			order = append(order, "second")
			return true
		},
	})

	r.Dispatch(ctx("x"))
	assert.Equal(t, []string{"first", "second"}, order)
}

func TestRouter_MultipleRules_FirstWins(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	r := router.New(eng, nil)

	var matched string
	r.Route(&router.RouteRule{
		Name: "alpha", Strategy: router.StrategyEngine,
		Match: func(ctx *corectx.Context) bool {
			return ctx.GetMessageContent() == "alpha"
		},
	})
	r.Route(&router.RouteRule{
		Name: "beta", Strategy: router.StrategyEngine,
		Match: func(ctx *corectx.Context) bool {
			return ctx.GetMessageContent() == "alpha"
		},
	})

	eng.OnAny().Handle(func(ctx *corectx.Context) error {
		matched = "engine"
		return nil
	})

	r.Dispatch(ctx("alpha"))
	eng.WaitForAsyncHandlers()
	assert.Equal(t, "engine", matched, "first matching rule should win")
}

func TestRouter_NilContext(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	r := router.New(eng, nil)
	r.Route(router.WithCommandPrefix())
	r.Dispatch(nil)
}

func TestRouter_DoesNotMatchChinese(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	r := router.New(eng, nil)

	var matched bool
	eng.OnAny().Handle(func(ctx *corectx.Context) error {
		matched = true
		return nil
	})

	r.Route(router.WithCommandPrefix())
	r.Dispatch(ctx("帮助"))
	eng.WaitForAsyncHandlers()
	assert.True(t, matched, "chinese text without prefix should fallthrough to engine")
}

func TestRouter_Custom(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	r := router.New(eng, nil)

	var matched bool
	r.Route(router.WithCustom("custom", router.StrategyEngine, func(ctx *corectx.Context) bool {
		return ctx.GetMessageContent() == "custom"
	}))
	eng.OnAny().Handle(func(ctx *corectx.Context) error {
		matched = true
		return nil
	})

	r.Dispatch(ctx("custom"))
	eng.WaitForAsyncHandlers()
	assert.True(t, matched)
}
