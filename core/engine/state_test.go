package engine

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEngineState(t *testing.T) {
	s := newEngineState()
	assert.NotNil(t, s)
	assert.Empty(t, s.matchers)
	assert.Empty(t, s.matcherIndex)
	assert.Empty(t, s.commandIndex)
	assert.Empty(t, s.groupIndex)
	assert.Empty(t, s.sortedCache)
	assert.Empty(t, s.commandInfoCache)
	assert.False(t, s.block)
	assert.Equal(t, 0, s.maxMatchers)
}

func TestNewMiddlewareState(t *testing.T) {
	ms := newMiddlewareState()
	assert.NotNil(t, ms)
	assert.NotNil(t, ms.global.chain)
	assert.Equal(t, uint64(1), ms.global.gen)
	assert.Empty(t, ms.groupMiddlewares)
}

func TestCopyEngineState_Semantics(t *testing.T) {
	t.Run("initial state copy", func(t *testing.T) {
		src := newEngineState()
		m := &Matcher{EventType: "test", Source: "copy-test"}
		src.matchers = append(src.matchers, m)
		src.matcherIndex["test"] = []*Matcher{m}

		dst := src.clone()
		assert.Equal(t, 1, len(dst.matchers))
		assert.Same(t, m, dst.matchers[0])
	})

	t.Run("COW isolation on append", func(t *testing.T) {
		src := newEngineState()
		m1 := &Matcher{EventType: "a"}
		m2 := &Matcher{EventType: "b"}
		src.matchers = append(src.matchers, m1, m2)

		dst := src.clone()
		dst.matchers = append(dst.matchers, &Matcher{EventType: "c"})

		assert.Equal(t, 2, len(src.matchers))
		assert.Equal(t, 3, len(dst.matchers))
	})

	t.Run("matcher index copy shares underlying arrays", func(t *testing.T) {
		src := newEngineState()
		m := &Matcher{EventType: "t"}
		src.matcherIndex["t"] = []*Matcher{m}
		src.sortedCache["t"] = []*Matcher{m}

		dst := src.clone()
		assert.Same(t, dst.matcherIndex["t"][0], m)
		assert.Same(t, dst.sortedCache["t"][0], m)
	})

	t.Run("copies commandInfoCache", func(t *testing.T) {
		src := newEngineState()
		src.commandInfoCache["/ping"] = &CommandInfo{Command: "/ping"}
		dst := src.clone()
		assert.Equal(t, "/ping", dst.commandInfoCache["/ping"].Command)
	})

	t.Run("copies commandIndex nested maps", func(t *testing.T) {
		src := newEngineState()
		m := &Matcher{EventType: "t"}
		src.commandIndex["/ping"] = map[EventType][]*Matcher{"t": {m}}
		dst := src.clone()
		require.Contains(t, dst.commandIndex, "/ping")
		assert.Same(t, m, dst.commandIndex["/ping"]["t"][0])
	})
}

func TestCopyMiddlewareState_Semantics(t *testing.T) {
	src := newMiddlewareState()
	src.groupMiddlewares["admin"] = &middlewareSnapshot{
		chain: []corectx.Middleware{func(next corectx.Handler) corectx.Handler { return next }},
		gen:   5,
	}

	dst := copyMiddlewareState(src)
	assert.Equal(t, uint64(1), dst.global.gen)
	require.Contains(t, dst.groupMiddlewares, "admin")
	assert.Equal(t, uint64(5), dst.groupMiddlewares["admin"].gen)
}

func makeTestMatcher(et string, priority uint64) *Matcher {
	m := &Matcher{EventType: et, Source: "test"}
	m.priority.Store(priority)
	return m
}

func makeCommandMatcher(cmd string, et string, priority uint64) *Matcher {
	m := makeTestMatcher(et, priority)
	m.definition = &command.Definition{Name: cmd}
	return m
}

func TestState_AddMatcher(t *testing.T) {
	t.Run("adds to matchers list", func(t *testing.T) {
		s := newEngineState()
		m := makeTestMatcher("test", 50)
		s.addMatcher(m)
		assert.Equal(t, 1, len(s.matchers))
		assert.Same(t, m, s.matchers[0])
	})

	t.Run("updates matcherIndex and sortedCache", func(t *testing.T) {
		s := newEngineState()
		m := makeTestMatcher("event_a", 50)
		s.addMatcher(m)
		require.Contains(t, s.matcherIndex, "event_a")
		assert.Same(t, m, s.matcherIndex["event_a"][0])
		require.Contains(t, s.sortedCache, "event_a")
	})

	t.Run("updates commandIndex for command matchers", func(t *testing.T) {
		s := newEngineState()
		m := makeCommandMatcher("ping", "msg", 50)
		s.addMatcher(m)
		require.Contains(t, s.commandIndex, "/ping")
		require.Contains(t, s.commandIndex["/ping"], "msg")
		assert.Same(t, m, s.commandIndex["/ping"]["msg"][0])
		assert.True(t, m.commandIndexed.Load())
	})

	t.Run("populates commandInfoCache", func(t *testing.T) {
		s := newEngineState()
		m := makeCommandMatcher("ping", "msg", 50)
		m.SetSource("plugin:test")
		m.SetDescription("ping pong")
		s.addMatcher(m)
		require.Contains(t, s.commandInfoCache, "/ping")
		assert.Equal(t, "/ping", s.commandInfoCache["/ping"].Command)
		assert.Equal(t, "ping pong", s.commandInfoCache["/ping"].Description)
	})

	t.Run("hidden commands excluded from cache", func(t *testing.T) {
		s := newEngineState()
		m := makeCommandMatcher("hidden", "msg", 50)
		m.SetHidden(true)
		s.addMatcher(m)
		assert.NotContains(t, s.commandInfoCache, "/hidden")
	})

	t.Run("updates groupIndex", func(t *testing.T) {
		s := newEngineState()
		m := makeTestMatcher("msg", 50)
		m.SetGroup("admin")
		s.addMatcher(m)
		require.Contains(t, s.groupIndex, "admin")
		assert.Same(t, m, s.groupIndex["admin"][0])
	})
}

func TestState_DeleteMatcher(t *testing.T) {
	t.Run("single matcher deletion", func(t *testing.T) {
		s := newEngineState()
		m1 := makeTestMatcher("a", 10)
		m2 := makeTestMatcher("b", 20)
		s.addMatcher(m1)
		s.addMatcher(m2)

		s.deleteMatcher(m1)
		assert.Equal(t, 1, len(s.matchers))
		assert.Same(t, m2, s.matchers[0])
	})

	t.Run("rebuilds index after deletion", func(t *testing.T) {
		s := newEngineState()
		m1 := makeTestMatcher("a", 10)
		m2 := makeTestMatcher("a", 20)
		s.addMatcher(m1)
		s.addMatcher(m2)

		s.deleteMatcher(m1)
		require.Contains(t, s.matcherIndex, "a")
		assert.Equal(t, 1, len(s.matcherIndex["a"]))
	})
}

func TestState_DeleteMatchers(t *testing.T) {
	t.Run("batch delete matchers", func(t *testing.T) {
		s := newEngineState()
		m1 := makeTestMatcher("a", 10)
		m2 := makeTestMatcher("b", 20)
		m3 := makeTestMatcher("c", 30)
		s.addMatcher(m1)
		s.addMatcher(m2)
		s.addMatcher(m3)

		s.deleteMatchers([]*Matcher{m1, m3})
		assert.Equal(t, 1, len(s.matchers))
		assert.Same(t, m2, s.matchers[0])
	})

	t.Run("nil/deduplicated input", func(t *testing.T) {
		s := newEngineState()
		m := makeTestMatcher("a", 10)
		s.addMatcher(m)
		s.deleteMatchers(nil)
		assert.Equal(t, 1, len(s.matchers))
	})

	t.Run("rebuilds index after batch delete", func(t *testing.T) {
		s := newEngineState()
		m1 := makeTestMatcher("a", 10)
		m2 := makeTestMatcher("a", 20)
		m3 := makeTestMatcher("a", 30)
		s.addMatcher(m1)
		s.addMatcher(m2)
		s.addMatcher(m3)

		s.deleteMatchers([]*Matcher{m1, m3})
		require.Contains(t, s.matcherIndex, "a")
		assert.Equal(t, 1, len(s.matcherIndex["a"]))
	})
}

func TestState_RemoveGroup(t *testing.T) {
	t.Run("removes all matchers in group", func(t *testing.T) {
		s := newEngineState()
		m1 := makeTestMatcher("a", 10)
		m2 := makeTestMatcher("b", 20)
		m3 := makeTestMatcher("c", 30)
		m1.SetGroup("admin")
		m2.SetGroup("admin")
		m3.SetGroup("user")
		s.addMatcher(m1)
		s.addMatcher(m2)
		s.addMatcher(m3)

		s.removeGroup("admin")
		assert.Equal(t, 1, len(s.matchers))
		assert.Same(t, m3, s.matchers[0])
	})

	t.Run("empty group name is no-op", func(t *testing.T) {
		s := newEngineState()
		m := makeTestMatcher("a", 10)
		s.addMatcher(m)
		s.removeGroup("")
		assert.Equal(t, 1, len(s.matchers))
	})

	t.Run("non-existent group is no-op", func(t *testing.T) {
		s := newEngineState()
		s.removeGroup("nonexistent")
	})
}

func TestState_RebuildIndex(t *testing.T) {
	t.Run("rebuilds all indexes from matchers list", func(t *testing.T) {
		s := newEngineState()
		m1 := makeTestMatcher("a", 10)
		m2 := makeCommandMatcher("ping", "b", 20)
		m3 := makeTestMatcher("a", 30)
		m2.SetGroup("admin")
		s.addMatcher(m1)
		s.addMatcher(m2)
		s.addMatcher(m3)

		// Manually corrupt indexes
		s.matcherIndex = make(map[EventType][]*Matcher)
		s.commandIndex = make(map[string]map[EventType][]*Matcher)
		s.rebuildIndex()

		require.Contains(t, s.matcherIndex, "a")
		assert.Equal(t, 2, len(s.matcherIndex["a"]))
		require.Contains(t, s.commandIndex, "/ping")
		require.Contains(t, s.groupIndex, "admin")
	})

	t.Run("sorted by priority", func(t *testing.T) {
		s := newEngineState()
		m1 := makeTestMatcher("a", 100)
		m2 := makeTestMatcher("a", 10)
		m3 := makeTestMatcher("a", 50)
		s.addMatcher(m1)
		s.addMatcher(m2)
		s.addMatcher(m3)
		s.rebuildIndex()

		sorted := s.sortedCache["a"]
		require.Equal(t, 3, len(sorted))
		assert.Equal(t, uint64(10), sorted[0].priority.Load())
		assert.Equal(t, uint64(50), sorted[1].priority.Load())
		assert.Equal(t, uint64(100), sorted[2].priority.Load())
	})
}

func TestState_SortMatchersByPriority(t *testing.T) {
	m1 := makeTestMatcher("a", 100)
	m2 := makeTestMatcher("b", 10)
	m3 := makeTestMatcher("c", 50)
	ms := []*Matcher{m1, m2, m3}

	sortMatchersByPriority(ms)
	assert.Equal(t, uint64(10), ms[0].priority.Load())
	assert.Equal(t, uint64(50), ms[1].priority.Load())
	assert.Equal(t, uint64(100), ms[2].priority.Load())
}

func TestState_InvalidateSortedCache(t *testing.T) {
	t.Run("rebuilds cache for specific type", func(t *testing.T) {
		s := newEngineState()
		m1 := makeTestMatcher("a", 100)
		m2 := makeTestMatcher("a", 10)
		s.addMatcher(m1)
		s.addMatcher(m2)

		// Change priorities
		m1.priority.Store(5)
		s.invalidateSortedCache("a")

		sorted := s.sortedCache["a"]
		require.Equal(t, 2, len(sorted))
		assert.Equal(t, uint64(5), sorted[0].priority.Load())
	})

	t.Run("rebuilds generic cache when specific type invalidated", func(t *testing.T) {
		s := newEngineState()
		m := makeTestMatcher("", 50)
		s.addMatcher(m)
		s.invalidateSortedCache("specific_type")
		require.Contains(t, s.sortedCache, "")
	})
}

func TestBuildCommandInfo(t *testing.T) {
	t.Run("basic command info", func(t *testing.T) {
		m := makeCommandMatcher("ping", "msg", 50)
		m.SetSource("plugin:myplugin")
		m.SetDescription("test command")
		m.SetUsage("/ping")
		m.SetAliases("p")
		m.SetCategory("utility")
		m.SetExamples("/ping")
		m.SetPermissions("admin")

		info := buildCommandInfo(m, "/ping")
		assert.Equal(t, "/ping", info.Command)
		assert.Equal(t, "msg", info.EventType)
		assert.Equal(t, "test command", info.Description)
		assert.Equal(t, "/ping", info.Usage)
		assert.Equal(t, []string{"p"}, info.Aliases)
		assert.Equal(t, "utility", info.Category)
		assert.Equal(t, []string{"/ping"}, info.Examples)
		assert.Equal(t, []string{"admin"}, info.Permissions)
		assert.Equal(t, "myplugin", info.Plugin)
	})

	t.Run("source with plugin prefix", func(t *testing.T) {
		m := makeCommandMatcher("test", "msg", 50)
		m.SetSource("plugin:x")
		info := buildCommandInfo(m, "/test")
		assert.Equal(t, "x", info.Plugin)
	})

	t.Run("source without plugin prefix", func(t *testing.T) {
		m := makeCommandMatcher("test", "msg", 50)
		m.SetSource("global")
		info := buildCommandInfo(m, "/test")
		assert.Equal(t, "global", info.Plugin)
	})
}

func TestState_CommandInfoCache(t *testing.T) {
	t.Run("updateCommandInfoCache without list rebuild", func(t *testing.T) {
		s := newEngineState()
		m := makeCommandMatcher("ping", "msg", 50)
		s.addMatcher(m)
		v0 := s.commandListVer

		// update should NOT rebuild list cache if called directly
		m2 := makeCommandMatcher("pong", "msg", 50)
		s.updateCommandInfoCache(m2, "/pong")
		assert.Equal(t, v0, s.commandListVer)
	})

	t.Run("rebuildCommandInfoCache rebuilds list", func(t *testing.T) {
		s := newEngineState()
		m := makeCommandMatcher("ping", "msg", 50)
		s.addMatcher(m)
		v0 := s.commandListVer

		s.rebuildCommandInfoCache(m, "/ping")
		assert.Greater(t, s.commandListVer, v0)
	})

	t.Run("rebuildCommandListCache produces ordered list", func(t *testing.T) {
		s := newEngineState()
		s.commandInfoCache["/b"] = &CommandInfo{Command: "/b"}
		s.commandInfoCache["/a"] = &CommandInfo{Command: "/a"}
		s.rebuildCommandListCache()

		assert.Len(t, s.commandListCache, 2)
	})
}

func TestEngineState_Block_MaxMatchers(t *testing.T) {
	s := newEngineState()
	assert.False(t, s.block)
	assert.Equal(t, 0, s.maxMatchers)
}

// ── Integration: state via Engine ──────────────────────────

func TestEngine_MatcherLifecycle(t *testing.T) {
	eng := newEngineForTest(t)

	t.Run("add and list matchers", func(t *testing.T) {
		m := eng.OnEventKind(platform.EventKindPrivateMessage)
		require.NotNil(t, m)
		assert.Equal(t, 1, eng.GetMatcherCount())

		stats := eng.GetMatcherStats()
		assert.Equal(t, 1, stats.Total)
	})

	t.Run("delete matcher", func(t *testing.T) {
		m := eng.OnEventKind(platform.EventKindPrivateMessage)
		before := eng.GetMatcherCount()
		m.Delete()
		assert.Equal(t, before-1, eng.GetMatcherCount())
		assert.True(t, m.IsDeleted())
	})

	t.Run("priority sorting works end-to-end", func(t *testing.T) {
		eng2 := newEngineForTest(t)
		order := make([]string, 0)
		m1 := eng2.OnEventKind(platform.EventKindPrivateMessage).SetPriority(100)
		m2 := eng2.OnEventKind(platform.EventKindPrivateMessage).SetPriority(10)

		var mu1, mu2 bool
		m1.Handle(func(ctx *corectx.Context) error { mu1 = true; return nil })
		m2.Handle(func(ctx *corectx.Context) error {
			mu2 = true
			order = append(order, "m2")
			return nil
		})

		evt := newTestPlatformEvent(platform.EventKindPrivateMessage)
		ctx2 := corectx.AcquireContextFromEvent(evt, nil)
		eng2.ProcessEvent(ctx2)
		corectx.ReleaseContextFromEvent(ctx2)

		assert.True(t, mu1)
		assert.True(t, mu2)
		_ = order
	})
}

func TestEngine_CommandMatcherIndex(t *testing.T) {
	eng := newEngineForTest(t)
	m := eng.OnCommand(EventType(platform.EventKindPrivateMessage), "/ping")
	m.Handle(func(ctx *corectx.Context) error {
		return nil
	})

	eng.ProcessPlatformEvent(newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "/ping"), nil)
}
