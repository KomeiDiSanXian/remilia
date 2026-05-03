package engine

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ────────────────────────────────────────────────

func matcherWithEventType(et string) *Matcher {
	return &Matcher{EventType: et, Source: "test"}
}

func matcherWithMockCoord(et string) *Matcher {
	m := &Matcher{EventType: et, Source: "test", coordinator: &mockCoordinator{}}
	return m
}

type mockCoordinator struct {
	deleteCalled        bool
	rebuildCalled       bool
	invalidated         string
	migratedToTemp      bool
	migratedFromTemp    bool
	priorityUpdated     bool
	commandUpdated      bool
	commandCacheUpdated bool
}

func (c *mockCoordinator) DeleteMatcher(_ *Matcher)             { c.deleteCalled = true }
func (c *mockCoordinator) RebuildMatcherChain(_ *Matcher)       { c.rebuildCalled = true }
func (c *mockCoordinator) InvalidateSortedCache(et EventType)   { c.invalidated = string(et) }
func (c *mockCoordinator) MigrateMatcherToTemp(_ *Matcher)      { c.migratedToTemp = true }
func (c *mockCoordinator) MigrateMatcherFromTemp(_ *Matcher)    { c.migratedFromTemp = true }
func (c *mockCoordinator) UpdateTempMatcherPriority(_ *Matcher) { c.priorityUpdated = true }
func (c *mockCoordinator) UpdateMatcherIndex(_ *Matcher)        {}
func (c *mockCoordinator) UpdateMatcherCommand(_ *Matcher)      { c.commandUpdated = true }
func (c *mockCoordinator) UpdateCommandCache(_ *Matcher)        { c.commandCacheUpdated = true }

func testContext() *context.Context {
	evt := newTestPlatformEvent(platform.EventKindPrivateMessage)
	return context.AcquireContextFromEvent(evt, nil)
}

func releaseCtx(ctx *context.Context) {
	context.ReleaseContextFromEvent(ctx)
}

func TestMatcher_CopySemantics(t *testing.T) {
	t.Run("deep copy rules and middlewares", func(t *testing.T) {
		m := &Matcher{
			EventType:   "test",
			Source:      "original",
			Rules:       []context.Rule{func(*context.Context) bool { return true }},
			middlewares: []context.Middleware{func(next context.Handler) context.Handler { return next }},
			Handler:     func(*context.Context) error { return nil },
		}
		m.priority.Store(50)
		m.isBlock.Store(true)

		cp := m.copy()

		assert.Equal(t, "test", cp.EventType)
		assert.Equal(t, "original", cp.Source)
		assert.Equal(t, uint64(50), cp.priority.Load())
		assert.True(t, cp.isBlock.Load())
		assert.NotNil(t, cp.Handler)
	})

	t.Run("nil matcher nil-safety", func(t *testing.T) {
		assert.Equal(t, "", (*Matcher)(nil).GetCommand())
		assert.Equal(t, "", (*Matcher)(nil).GetSource())
		assert.Equal(t, uint(0), (*Matcher)(nil).getPriority())
		assert.False(t, (*Matcher)(nil).isBlocking())
	})
}

func TestMatcher_MatchLogic(t *testing.T) {
	t.Run("deleted matcher never matches", func(t *testing.T) {
		m := matcherWithEventType("test")
		ctx := testContext()
		defer releaseCtx(ctx)

		assert.True(t, m.Match(ctx))
		m.rt.deleted.Store(true)
		assert.False(t, m.Match(ctx))
	})

	t.Run("disabled matcher never matches", func(t *testing.T) {
		m := matcherWithEventType("test")
		ctx := testContext()
		defer releaseCtx(ctx)

		m.disable()
		assert.False(t, m.Match(ctx))
		m.enable()
		assert.True(t, m.Match(ctx))
	})

	t.Run("rules are evaluated", func(t *testing.T) {
		m := matcherWithEventType("test")
		called := false
		m.Rules = []context.Rule{func(ctx *context.Context) bool { called = true; return true }}
		ctx := testContext()
		defer releaseCtx(ctx)

		assert.True(t, m.Match(ctx))
		assert.True(t, called)
	})

	t.Run("rule returning false prevents match", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.Rules = []context.Rule{func(ctx *context.Context) bool { return false }}
		ctx := testContext()
		defer releaseCtx(ctx)

		assert.False(t, m.Match(ctx))
	})

	t.Run("commandIndexed skips first rule", func(t *testing.T) {
		m := matcherWithEventType("test")
		skipped := false
		executed := false
		m.Rules = []context.Rule{
			func(ctx *context.Context) bool { skipped = true; return false },
			func(ctx *context.Context) bool { executed = true; return true },
		}
		m.commandIndexed.Store(true)
		ctx := testContext()
		defer releaseCtx(ctx)

		assert.True(t, m.Match(ctx))
		assert.False(t, skipped)
		assert.True(t, executed)
	})

	t.Run("match checks deleted after rules", func(t *testing.T) {
		m := matcherWithEventType("test")
		ctx := testContext()
		defer releaseCtx(ctx)
		assert.True(t, m.Match(ctx))

		m.rt.deleted.Store(true)
		assert.False(t, m.Match(ctx))
	})
}

func TestMatcher_HandleBehavior(t *testing.T) {
	t.Run("sets handler and increments version", func(t *testing.T) {
		m := matcherWithMockCoord("test")
		v0 := m.compiledVersion.Load()
		h := func(*context.Context) error { return nil }
		m.Handle(h)
		assert.NotNil(t, m.Handler)
		assert.Equal(t, v0+1, m.compiledVersion.Load())
		assert.True(t, m.coordinator.(*mockCoordinator).rebuildCalled)
	})

	t.Run("noop matcher ignores Handle", func(t *testing.T) {
		m := &Matcher{Source: "noop"}
		m.Handle(func(*context.Context) error { return nil })
		assert.Nil(t, m.Handler)
	})

	t.Run("triggers alias registration", func(t *testing.T) {
		m := matcherWithMockCoord("test")
		registrarCalled := false
		m.definition = &command.Definition{Name: "ping", Aliases: []string{"p"}}
		m.aliasRegistrar = func(def *command.Definition, h context.Handler) {
			registrarCalled = true
			assert.Equal(t, "ping", def.Name)
		}
		m.Handle(func(*context.Context) error { return nil })
		assert.True(t, registrarCalled)
		assert.Nil(t, m.aliasRegistrar)
	})

	t.Run("alias registrar only fires once", func(t *testing.T) {
		m := matcherWithMockCoord("test")
		count := 0
		m.definition = &command.Definition{Name: "ping", Aliases: []string{"p"}}
		m.aliasRegistrar = func(def *command.Definition, h context.Handler) { count++ }
		m.Handle(func(*context.Context) error { return nil })
		m.Handle(func(*context.Context) error { return nil })
		assert.Equal(t, 1, count)
	})
}

func TestMatcher_SetPriorityBehavior(t *testing.T) {
	t.Run("changes priority and invalidates cache", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{EventType: "test", coordinator: coord}
		m.SetPriority(100)
		assert.Equal(t, uint64(100), m.priority.Load())
		assert.Equal(t, "test", coord.invalidated)
	})

	t.Run("noop matcher ignores SetPriority", func(t *testing.T) {
		m := &Matcher{Source: "noop"}
		m.SetPriority(100)
		assert.Equal(t, uint64(0), m.priority.Load())
	})

	t.Run("no coordinator call when priority unchanged", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{EventType: "test", coordinator: coord, priority: atomic.Uint64{}}
		m.priority.Store(50)
		m.SetPriority(50)
		assert.Empty(t, coord.invalidated)
	})
}

func TestMatcher_SetBlockBehavior(t *testing.T) {
	m := matcherWithEventType("test")
	assert.False(t, m.isBlock.Load())
	m.SetBlock(true)
	assert.True(t, m.isBlock.Load())
	m.SetBlock(false)
	assert.False(t, m.isBlock.Load())
}

func TestMatcher_DeleteBehavior(t *testing.T) {
	t.Run("marks deleted and calls coordinator", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		m.Delete()
		assert.True(t, m.rt.deleted.Load())
		assert.True(t, coord.deleteCalled)
	})

	t.Run("idempotent on double delete", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		m.Delete()
		coord.deleteCalled = false
		m.Delete()
		assert.False(t, coord.deleteCalled)
	})
}

func TestMatcher_SetTempBehavior(t *testing.T) {
	t.Run("SetTemp(true) marks as temp and migrates", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		m.SetTemp(true)
		assert.True(t, m.IsTemp())
		assert.True(t, coord.migratedToTemp)
	})

	t.Run("SetTemp(false) unmarks temp and migrates back", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		m.SetTemp(true)
		coord.migratedToTemp = false
		m.SetTemp(false)
		assert.False(t, m.IsTemp())
		assert.True(t, coord.migratedFromTemp)
	})

	t.Run("SetTemp true on already-temp does not double-migrate", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		m.SetTemp(true)
		coord.migratedToTemp = false
		m.SetTemp(true)
		assert.False(t, coord.migratedToTemp)
	})

	t.Run("SetTempWithMaxUse sets use count", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		m.SetTempWithMaxUse(5)
		assert.True(t, m.IsTemp())
		assert.Equal(t, int32(5), m.rt.maxUseCount)
	})

	t.Run("SetTempWithTimeout sets expiresAt", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		before := time.Now()
		m.SetTempWithTimeout(10 * time.Second)
		assert.True(t, m.IsTemp())
		assert.True(t, m.rt.expiresAt.After(before))
		assert.True(t, m.rt.expiresAt.Before(time.Now().Add(11*time.Second)))
	})

	t.Run("noop matcher ignores SetTemp", func(t *testing.T) {
		m := &Matcher{Source: "noop"}
		m.SetTemp(true)
		assert.False(t, m.IsTemp())
	})
}

func TestMatcher_Use(t *testing.T) {
	t.Run("appends middleware and triggers rebuild", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		mw := func(next context.Handler) context.Handler { return next }

		m.Use(mw)
		require.Equal(t, 1, len(m.middlewares))
		assert.True(t, coord.rebuildCalled)
	})

	t.Run("increments compiledVersion", func(t *testing.T) {
		m := matcherWithEventType("test")
		v0 := m.compiledVersion.Load()
		m.Use(func(next context.Handler) context.Handler { return next })
		assert.Greater(t, m.compiledVersion.Load(), v0)
	})
}

func TestMatcher_Command(t *testing.T) {
	m := matcherWithEventType("test")
	m.Command("/ping")
	assert.Len(t, m.Rules, 1)
}

func TestMatcher_Keyword(t *testing.T) {
	m := matcherWithEventType("test")
	m.Keyword("hello")
	assert.Len(t, m.Rules, 1)
}

func TestMatcher_Prefix(t *testing.T) {
	m := matcherWithEventType("test")
	m.Prefix("/")
	assert.Len(t, m.Rules, 1)
}

func TestMatcher_Suffix(t *testing.T) {
	m := matcherWithEventType("test")
	m.Suffix("!")
	assert.Len(t, m.Rules, 1)
}

func TestMatcher_FullMatch(t *testing.T) {
	m := matcherWithEventType("test")
	m.FullMatch("/ping")
	assert.Len(t, m.Rules, 1)
}

func TestMatcher_Regex(t *testing.T) {
	m := matcherWithEventType("test")
	m.Regex(`^/\w+$`)
	assert.Len(t, m.Rules, 1)
}

func TestMatcher_Where(t *testing.T) {
	m := matcherWithEventType("test")
	m.Where(func(*context.Context) bool { return true })
	assert.Len(t, m.Rules, 1)
}

func TestMatcher_ChainCombined(t *testing.T) {
	t.Run("Command + Handle chains correctly", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.Command("/ping").Handle(func(*context.Context) error {
			return nil
		})
		assert.NotNil(t, m.Handler)
		assert.Len(t, m.Rules, 1)
	})

	t.Run("Use + Handle chains", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.Use(func(next context.Handler) context.Handler {
			return func(ctx *context.Context) error {
				return next(ctx)
			}
		})
		m.Handle(func(*context.Context) error { return nil })
		assert.NotNil(t, m.Handler)
	})
}

func TestMatcher_EnsureChainBehavior(t *testing.T) {
	t.Run("first call builds chain", func(t *testing.T) {
		m := matcherWithEventType("test")
		globalChain := []context.Middleware{func(next context.Handler) context.Handler { return next }}
		m.ensureChain(globalChain, 1, nil, 0)
		chain, gGen, pGen := m.getChainCache()
		assert.NotNil(t, chain)
		assert.Equal(t, uint64(1), gGen)
		assert.Equal(t, uint64(0), pGen)
	})

	t.Run("second call with same gen reuses cache", func(t *testing.T) {
		m := matcherWithEventType("test")
		globalChain := []context.Middleware{func(next context.Handler) context.Handler { return next }}
		m.ensureChain(globalChain, 1, nil, 0)
		v0 := m.compiledVersion.Load()
		m.ensureChain(globalChain, 1, nil, 0)
		assert.Equal(t, v0, m.compiledVersion.Load())
	})

	t.Run("different gen triggers rebuild", func(t *testing.T) {
		m := matcherWithEventType("test")
		globalChain := []context.Middleware{func(next context.Handler) context.Handler { return next }}
		m.ensureChain(globalChain, 1, nil, 0)
		v0 := m.compiledVersion.Load()
		m.ensureChain(globalChain, 2, nil, 0)
		assert.Greater(t, m.compiledVersion.Load(), v0)
	})
}

func TestMatcher_InvalidateCombinedChain(t *testing.T) {
	m := matcherWithEventType("test")
	globalChain := []context.Middleware{func(next context.Handler) context.Handler { return next }}
	m.ensureChain(globalChain, 1, nil, 0)
	v0 := m.compiledVersion.Load()

	m.invalidateCombinedChain()
	chain, _, _ := m.getChainCache()
	assert.Nil(t, chain)
	assert.Greater(t, m.compiledVersion.Load(), v0)
}

func TestMatcher_IsNoopBehavior(t *testing.T) {
	assert.True(t, (&Matcher{Source: "noop"}).isNoop())
	assert.False(t, (&Matcher{Source: "test"}).isNoop())
	assert.False(t, (*Matcher)(nil).isNoop())
}

func TestMatcher_GetSetGroup(t *testing.T) {
	coord := &mockCoordinator{}
	m := &Matcher{coordinator: coord}
	assert.Empty(t, m.GetGroup())

	m.SetGroup("admin")
	assert.Equal(t, "admin", m.GetGroup())
	assert.True(t, coord.rebuildCalled)
}

func TestMatcher_GetSetDefinition(t *testing.T) {
	t.Run("SetDefinition triggers command cache update", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		def := &command.Definition{Name: "ping", Description: "test command"}
		m.SetDefinition(def)
		assert.Equal(t, def, m.GetDefinition())
		assert.True(t, coord.commandCacheUpdated)
	})

	t.Run("SetDescription creates definition", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.SetDescription("a test command")
		assert.NotNil(t, m.GetDefinition())
		assert.Equal(t, "a test command", m.GetDefinition().Description)
	})

	t.Run("SetUsage creates definition", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.SetUsage("/test <arg>")
		assert.Equal(t, "/test <arg>", m.GetDefinition().Usage)
	})

	t.Run("SetCategory creates definition", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.SetCategory("utility")
		assert.Equal(t, "utility", m.GetDefinition().Category)
	})

	t.Run("SetAliases creates definition", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.SetAliases("t", "testcmd")
		assert.Equal(t, []string{"t", "testcmd"}, m.GetDefinition().Aliases)
	})

	t.Run("SetExamples creates definition", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.SetExamples("/test arg1", "/test arg2")
		assert.Len(t, m.GetDefinition().Examples, 2)
	})

	t.Run("SetHidden creates definition", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.SetHidden(true)
		assert.True(t, m.GetDefinition().Hidden)
	})

	t.Run("SetPermissions creates definition", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.SetPermissions("admin", "mod")
		assert.Equal(t, []string{"admin", "mod"}, m.GetDefinition().Permissions)
	})

	t.Run("BindCommand creates definition with name", func(t *testing.T) {
		coord := &mockCoordinator{}
		m := &Matcher{coordinator: coord}
		m.BindCommand("/ping")
		assert.Equal(t, "ping", m.GetDefinition().Name)
		assert.True(t, coord.commandUpdated)
	})

	t.Run("BindCommand without slash", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.BindCommand("ping")
		assert.Equal(t, "ping", m.GetDefinition().Name)
	})
}

func TestMatcher_SetAliasRegistrar(t *testing.T) {
	m := matcherWithEventType("test")
	m.SetAliasRegistrar(func(def *command.Definition, h context.Handler) {})
	assert.NotNil(t, m.aliasRegistrar)
}

func TestMatcher_GetCommandBehavior(t *testing.T) {
	t.Run("returns command string with slash prefix", func(t *testing.T) {
		m := matcherWithEventType("test")
		m.definition = &command.Definition{Name: "ping"}
		assert.Equal(t, "/ping", m.GetCommand())
	})

	t.Run("returns empty string when no definition", func(t *testing.T) {
		m := matcherWithEventType("test")
		assert.Empty(t, m.GetCommand())
	})
}

func TestMatcher_GetSource(t *testing.T) {
	m := matcherWithEventType("test")
	assert.Equal(t, "test", m.GetSource())
	m.SetSource("plugin:myplugin")
	assert.Equal(t, "plugin:myplugin", m.GetSource())
}

func TestMatcher_IsTemp_IsDeleted_IsDisabled(t *testing.T) {
	m := matcherWithEventType("test")
	assert.False(t, m.IsTemp())
	assert.False(t, m.IsDeleted())
	assert.False(t, m.IsDisabled())

	m.disable()
	assert.True(t, m.IsDisabled())

	m.enable()
	assert.False(t, m.IsDisabled())

	m.SetTemp(true)
	assert.True(t, m.IsTemp())

	m.rt.deleted.Store(true)
	assert.True(t, m.IsDeleted())
}

func TestMatcher_ConcurrentAccess(t *testing.T) {
	// Verify no race condition on concurrent read/write
	m := matcherWithEventType("test")
	done := make(chan struct{})
	go func() {
		for range 100 {
			m.Match(testContext())
			m.GetCommand()
			m.GetSource()
			m.isBlocking()
			m.getPriority()
		}
		close(done)
	}()

	for range 100 {
		m.SetPriority(50)
		m.SetBlock(true)
		m.SetTemp(true)
		m.SetTemp(false)
		m.SetGroup("g")
		m.Use(func(next context.Handler) context.Handler { return next })
	}
	<-done
}
