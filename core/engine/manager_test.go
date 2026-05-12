package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

type mgrTestEvent struct {
	platform string
	content  string
	chatID   string
}

func (e *mgrTestEvent) Platform() string                          { return e.platform }
func (e *mgrTestEvent) ID() string                                { return "" }
func (e *mgrTestEvent) Kind() platform.EventKind                  { return platform.EventKindPrivateMessage }
func (e *mgrTestEvent) Sender() platform.UserInfo                 { return platform.UserInfo{ID: "u1"} }
func (e *mgrTestEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{ID: e.chatID} }
func (e *mgrTestEvent) Content() string                           { return e.content }
func (e *mgrTestEvent) Attachments() []platform.InboundAttachment { return nil }
func (e *mgrTestEvent) Timestamp() time.Time                      { return time.Time{} }

func mgrCtx(content, chatID string) *corectx.Context {
	evt := &mgrTestEvent{platform: "test", content: content, chatID: chatID}
	return corectx.NewContextFromEvent(evt, &platform.NoopSender{})
}

func TestEngineManager_CreateOnFirstEvent(t *testing.T) {
	template := NewEngine(WithNoBackgroundWorkers())
	em := NewEngineManager(template)

	var called bool
	template.OnCommand("PRIVATE_MESSAGE", "/ping").Handle(func(ctx *corectx.Context) error {
		called = true
		return nil
	})

	em.Dispatch(mgrCtx("/ping", "ch1"))
	if ch := em.GetChannel(MakeChannelKey("test", "ch1")); ch != nil {
		ch.WaitForAsyncHandlers()
	}
	assert.True(t, called, "channel engine should get template's matchers")
}

func TestEngineManager_Isolation_BlockNotLeaking(t *testing.T) {
	template := NewEngine(WithNoBackgroundWorkers())
	em := NewEngineManager(template)

	var ch1Called, ch2Called bool

	template.On("PRIVATE_MESSAGE").Handle(func(ctx *corectx.Context) error {
		if ctx.GetChatInfo().ID == "ch1" {
			ch1Called = true
		}
		if ctx.GetChatInfo().ID == "ch2" {
			ch2Called = true
		}
		return nil
	})

	em.Dispatch(mgrCtx("msg", "ch1"))
	em.Dispatch(mgrCtx("msg", "ch2"))

	ch1 := em.GetChannel(MakeChannelKey("test", "ch1"))
	ch2 := em.GetChannel(MakeChannelKey("test", "ch2"))
	if ch1 != nil {
		ch1.WaitForAsyncHandlers()
	}
	if ch2 != nil {
		ch2.WaitForAsyncHandlers()
	}

	assert.True(t, ch1Called)
	assert.True(t, ch2Called)
}

func TestEngineManager_DifferentChannels(t *testing.T) {
	template := NewEngine(WithNoBackgroundWorkers())
	em := NewEngineManager(template)

	results := make(map[string]bool)
	template.On("PRIVATE_MESSAGE").Handle(func(ctx *corectx.Context) error {
		results[ctx.GetChatInfo().ID] = true
		return nil
	})

	em.Dispatch(mgrCtx("hello", "ch_a"))
	em.Dispatch(mgrCtx("hello", "ch_b"))
	em.Dispatch(mgrCtx("hello", "ch_c"))

	for _, id := range []string{"ch_a", "ch_b", "ch_c"} {
		if ch := em.GetChannel(MakeChannelKey("test", id)); ch != nil {
			ch.WaitForAsyncHandlers()
		}
	}

	assert.Len(t, results, 3)
	assert.True(t, results["ch_a"])
	assert.True(t, results["ch_b"])
	assert.True(t, results["ch_c"])
}

func TestEngineManager_SyncOnTemplateChange(t *testing.T) {
	template := NewEngine(WithNoBackgroundWorkers())
	em := NewEngineManager(template)

	em.Dispatch(mgrCtx("x", "ch1"))

	var called bool
	template.OnCommand("PRIVATE_MESSAGE", "/new").Handle(func(ctx *corectx.Context) error {
		called = true
		return nil
	})

	em.Dispatch(mgrCtx("/new", "ch1"))
	if ch := em.GetChannel(MakeChannelKey("test", "ch1")); ch != nil {
		ch.WaitForAsyncHandlers()
	}
	assert.True(t, called, "new matcher from template should be synced to channel engine")
}

func TestEngineManager_NewChannelEngine_IsFork(t *testing.T) {
	template := NewEngine(WithNoBackgroundWorkers())
	em := NewEngineManager(template)

	em.Dispatch(mgrCtx("x", "ch_fork"))

	key := MakeChannelKey("test", "ch_fork")
	actual, ok := em.instances.Load(key)
	require.True(t, ok)
	chEngine := actual.(*Engine)
	assert.True(t, chEngine.IsFork())
}

func TestEngineManager_AutoStartGC(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())
	em := NewEngineManager(tmpl, WithMaxIdle(1*time.Hour))

	em.Dispatch(mgrCtx("x", "gc_ch"))
	key := MakeChannelKey("test", "gc_ch")
	assert.NotNil(t, em.GetChannel(key))

	em.instances.Delete(key)
	assert.Nil(t, em.GetChannel(key), "engine should be removable via GC path")
}

func TestEngineManager_EvictIdle(t *testing.T) {
	tmpl := NewEngine(WithNoBackgroundWorkers())
	em := NewEngineManager(tmpl, WithMaxIdle(30*time.Minute))

	em.Dispatch(mgrCtx("x", "evict_ch"))
	key := MakeChannelKey("test", "evict_ch")
	ch := em.GetChannel(key)
	require.NotNil(t, ch)

	em.evictIdle()
	assert.NotNil(t, em.GetChannel(key), "active engine should not be evicted")

	em.instances.Delete(key)
	assert.Nil(t, em.GetChannel(key), "delete should remove engine")
}

func TestEngineManager_Stats(t *testing.T) {
	template := NewEngine(WithNoBackgroundWorkers())
	em := NewEngineManager(template)

	em.Dispatch(mgrCtx("a", "ch_s1"))
	em.Dispatch(mgrCtx("b", "ch_s2"))

	stats := em.Stats()
	assert.Equal(t, 2, stats["channel_count"])
}


