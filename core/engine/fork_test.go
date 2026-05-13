package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

type forkTestEvent struct {
	platform string
	content  string
	chatID   string
}

func (e *forkTestEvent) Platform() string                          { return e.platform }
func (e *forkTestEvent) ID() string                                { return "" }
func (e *forkTestEvent) Kind() platform.EventKind                  { return platform.EventKindPrivateMessage }
func (e *forkTestEvent) Sender() platform.UserInfo                 { return platform.UserInfo{ID: "u1"} }
func (e *forkTestEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{ID: e.chatID} }
func (e *forkTestEvent) Content() string                           { return e.content }
func (e *forkTestEvent) Attachments() []platform.InboundAttachment { return nil }
func (e *forkTestEvent) Timestamp() time.Time                      { return time.Time{} }

func forkCtx(content string) *corectx.Context {
	evt := &forkTestEvent{platform: "test", content: content, chatID: "fork_ch"}
	return corectx.NewContextFromEvent(evt, &platform.NoopSender{})
}

func TestForkFrom_CopiesMatchers(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())
	var called bool
	tmpl.OnCommand("PRIVATE_MESSAGE", "/ping").Handle(func(ctx *corectx.Context) error {
		called = true
		return nil
	})

	child := NewEngine(WithNoBackgroundWorkers())
	child.ForkFrom(tmpl, "test:fork_ch")

	child.ProcessEvent(forkCtx("/ping"))
	child.WaitForAsyncHandlers()
	assert.True(t, called, "fork child should have template's matchers")
}

func TestForkFrom_IsFork(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())
	child := NewEngine(WithNoBackgroundWorkers())
	child.ForkFrom(tmpl, "test:fork_ch")

	assert.True(t, child.IsFork())
	assert.False(t, tmpl.IsFork(), "template should not be a fork")
}

func TestForkFrom_LazySyncOnVersionChange(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())
	child := NewEngine(WithNoBackgroundWorkers())
	child.ForkFrom(tmpl, "test:fork_ch")

	var called bool
	tmpl.OnCommand("PRIVATE_MESSAGE", "/newcmd").Handle(func(ctx *corectx.Context) error {
		called = true
		return nil
	})

	child.ProcessEvent(forkCtx("/newcmd"))
	child.WaitForAsyncHandlers()
	assert.True(t, called, "new template matcher should be lazily synced")
}

func TestVersionBump(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())
	v0 := tmpl.Version()

	m := tmpl.OnCommand("PRIVATE_MESSAGE", "/test")
	assert.Greater(t, tmpl.Version(), v0, "registerMatcher should bump version")

	v1 := tmpl.Version()
	m.Delete()
	assert.Greater(t, tmpl.Version(), v1, "DeleteMatcher should bump version")
}

func TestForkFrom_LastUsed(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())
	child := NewEngine(WithNoBackgroundWorkers())
	child.ForkFrom(tmpl, "test:fork_ch")

	assert.Equal(t, int64(0), child.LastUsed(), "initially zero")

	child.ProcessEvent(forkCtx("hello"))
	assert.Greater(t, child.LastUsed(), int64(0), "touch should set last used time")
}

func TestForkFrom_IndependentState(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())
	child := NewEngine(WithNoBackgroundWorkers())
	child.ForkFrom(tmpl, "test:fork_indep")

	tmpl.OnCommand("PRIVATE_MESSAGE", "/only_tmpl").Handle(func(ctx *corectx.Context) error {
		return nil
	})

	child.OnCommand("PRIVATE_MESSAGE", "/only_child").Handle(func(ctx *corectx.Context) error {
		return nil
	})

	require.Equal(t, 1, len(child.state.Load().matchers), "child should not see /only_tmpl if not synced")
	_ = tmpl
}

func TestForkFrom_SharedExecPool(t *testing.T) {
	tmpl := NewEngine()
	child := NewEngine(WithNoBackgroundWorkers())
	child.ForkFrom(tmpl, "test:share_pool")

	assert.Same(t, tmpl.internals.execPool, child.internals.execPool,
		"fork child should share template's ExecPool")
}

func TestForkFrom_SyncMiddleware(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())

	var mwCalled int
	testMW := func(next corectx.Handler) corectx.Handler {
		return func(ctx *corectx.Context) error {
			mwCalled++
			return next(ctx)
		}
	}
	tmpl.Use(testMW)

	tmpl.OnCommand("PRIVATE_MESSAGE", "/ping").Handle(func(ctx *corectx.Context) error {
		return nil
	})

	child := NewEngine(WithNoBackgroundWorkers())
	child.ForkFrom(tmpl, "test:mw_sync")

	child.ProcessEvent(forkCtx("/ping"))
	child.WaitForAsyncHandlers()
	assert.Equal(t, 1, mwCalled, "fork child should inherit template middleware")
}

func TestForkFrom_NonForkEngine(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())
	assert.False(t, tmpl.IsFork())
	assert.Equal(t, int64(0), tmpl.LastUsed())
}
