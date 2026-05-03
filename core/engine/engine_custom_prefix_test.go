package engine

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_OnCommand_CustomPrefix(t *testing.T) {
	t.Run("bang prefix extraction", func(t *testing.T) {
		eng := newEngineForTest(t)
		var called atomic.Bool

		eng.OnCommand("", "!ping").
			Handle(func(c *ctx.Context) error {
				called.Store(true)
				return nil
			})

		// 验证 definition.Name 正确提取（不含前缀）
		cmds := eng.GetAllCommands()
		require.Len(t, cmds, 1)
		assert.Equal(t, "!ping", cmds[0].Command)
		assert.NotNil(t, cmds[0].Definition)
		assert.Equal(t, "ping", cmds[0].Definition.Name)

		// 验证 GetCommand()
		cmd := cmds[0].Command
		assert.Equal(t, "!ping", cmd)
	})

	t.Run("dollar prefix dispatch", func(t *testing.T) {
		eng := newEngineForTest(t)
		var called atomic.Bool

		eng.OnCommand("", "$hello").
			Handle(func(c *ctx.Context) error {
				called.Store(true)
				return nil
			})

		// 发送 "$hello world" 消息触发
		evt := newTestPlatformEventWithContent("", "$hello world")
		eng.ProcessPlatformEvent(evt, nil)
		assert.True(t, called.Load(), "command with $ prefix should dispatch")
	})

	t.Run("dot prefix dispatch", func(t *testing.T) {
		eng := newEngineForTest(t)
		var called atomic.Bool

		eng.OnCommand("", ".status").
			Handle(func(c *ctx.Context) error {
				called.Store(true)
				return nil
			})

		evt := newTestPlatformEventWithContent("", ".status show all")
		eng.ProcessPlatformEvent(evt, nil)
		assert.True(t, called.Load(), "command with . prefix should dispatch")
	})

	t.Run("non-matching prefix not dispatched", func(t *testing.T) {
		eng := newEngineForTest(t)
		var called atomic.Bool

		eng.OnCommand("", "/ping").
			Handle(func(c *ctx.Context) error {
				called.Store(true)
				return nil
			})

		// bang prefix should NOT trigger slash command
		evt := newTestPlatformEventWithContent("", "!ping")
		eng.ProcessPlatformEvent(evt, nil)
		assert.False(t, called.Load(), "wrong prefix should not trigger command")
	})
}

func TestEngine_RegisterCommandWithPrefix(t *testing.T) {
	t.Run("bang prefix registration", func(t *testing.T) {
		eng := newEngineForTest(t)
		var called atomic.Bool

		def := &command.Definition{
			Name:        "greet",
			Description: "Say hello",
			Handler: func(any) {
				called.Store(true)
			},
		}

		eng.RegisterCommandWithPrefix("!", def)
		cmds := eng.GetAllCommands()
		require.Len(t, cmds, 1)
		assert.Equal(t, "!greet", cmds[0].Command)
		assert.Equal(t, "greet", cmds[0].Definition.Name)

		// 验证 dispatch
		evt := newTestPlatformEventWithContent("", "!greet world")
		eng.ProcessPlatformEvent(evt, nil)
		assert.True(t, called.Load())
	})
}

func TestEngine_RegisterCommandDefWithPrefix(t *testing.T) {
	t.Run("bang prefix command def", func(t *testing.T) {
		eng := newEngineForTest(t)
		var called atomic.Bool

		def := &command.Definition{
			Name:        "search",
			Description: "Search something",
			Aliases:     []string{"s"},
			Handler: func(any) {
				called.Store(true)
			},
		}

		eng.RegisterCommandDefWithPrefix("", "!", def)
		cmds := eng.GetAllCommands()
		require.Len(t, cmds, 1)
		assert.Equal(t, "!search", cmds[0].Command)
		assert.Equal(t, "search", cmds[0].Definition.Name)
		assert.Equal(t, []string{"s"}, cmds[0].Aliases)

		// 主命令 dispatch
		evt := newTestPlatformEventWithContent("", "!search keyword")
		eng.ProcessPlatformEvent(evt, nil)
		assert.True(t, called.Load(), "main command with ! prefix should dispatch")
	})

	t.Run("alias auto-registration with OnCommand uses custom prefix", func(t *testing.T) {
		eng := newEngineForTest(t)
		var called atomic.Bool

		m := eng.OnCommand("", "!search").
			SetAliases("s")
		m.Handle(func(c *ctx.Context) error {
			called.Store(true)
			return nil
		})

		// 主命令 dispatch
		called.Store(false)
		evt := newTestPlatformEventWithContent("", "!search keyword")
		eng.ProcessPlatformEvent(evt, nil)
		assert.True(t, called.Load(), "main command with ! prefix should dispatch")

		// 别名 dispatch（OnCommand 级别自动注册别名，共享同一 handler）
		called.Store(false)
		evt2 := newTestPlatformEventWithContent("", "!s")
		eng.ProcessPlatformEvent(evt2, nil)
		assert.True(t, called.Load(), "alias !s with ! prefix should dispatch")
	})

	t.Run("FindCommand works with custom prefix", func(t *testing.T) {
		eng := newEngineForTest(t)

		def := &command.Definition{
			Name:        "stats",
			Description: "Show statistics",
		}

		eng.RegisterCommandDefWithPrefix("", "$", def)

		// 通过完整前缀查找
		info := eng.FindCommand("$stats")
		require.NotNil(t, info)
		assert.Equal(t, "$stats", info.Command)

		// 通过不带前缀的名称查找（按 Definition.Name 匹配）
		info = eng.FindCommand("stats")
		require.NotNil(t, info)
		assert.Equal(t, "$stats", info.Command)
	})
}

func TestEngine_BackwardCompatibility(t *testing.T) {
	t.Run("slash prefix still works", func(t *testing.T) {
		eng := newEngineForTest(t)
		var called atomic.Bool

		eng.OnCommand("", "/ping").
			Handle(func(c *ctx.Context) error {
				called.Store(true)
				return nil
			})

		evt := newTestPlatformEventWithContent("", "/ping")
		eng.ProcessPlatformEvent(evt, nil)
		assert.True(t, called.Load())

		// GetCommand still returns "/ping"
		cmds := eng.GetAllCommands()
		require.Len(t, cmds, 1)
		assert.Equal(t, "/ping", cmds[0].Command)
	})

	t.Run("RegisterCommandDef still uses /", func(t *testing.T) {
		eng := newEngineForTest(t)
		def := &command.Definition{
			Name: "test",
		}
		eng.RegisterCommandDef("", def)
		cmds := eng.GetAllCommands()
		require.Len(t, cmds, 1)
		assert.Equal(t, "/test", cmds[0].Command)
	})

	t.Run("FindCommand still works with /", func(t *testing.T) {
		eng := newEngineForTest(t)
		def := &command.Definition{Name: "help"}
		eng.RegisterCommandDef("", def)

		info := eng.FindCommand("/help")
		require.NotNil(t, info)

		info = eng.FindCommand("help")
		require.NotNil(t, info)
	})
}

func TestMatcher_GetCommand_CustomPrefix(t *testing.T) {
	t.Run("GetCommand returns correct prefix", func(t *testing.T) {
		eng := newEngineForTest(t)

		m := eng.OnCommand("", "!ping")
		assert.Equal(t, "!ping", m.GetCommand())

		m2 := eng.OnCommand("", "/ping")
		assert.Equal(t, "/ping", m2.GetCommand())
	})
}

func TestMatcher_BindCommand_CustomPrefix(t *testing.T) {
	t.Run("BindCommand backward compat with /", func(t *testing.T) {
		eng := newEngineForTest(t)

		m := eng.On("")
		m.BindCommand("/hello")
		assert.Equal(t, "/hello", m.GetCommand())
		def := m.GetDefinition()
		require.NotNil(t, def)
		assert.Equal(t, "hello", def.Name)
	})

	t.Run("BindCommand without prefix keeps name as-is", func(t *testing.T) {
		eng := newEngineForTest(t)

		m := eng.On("")
		m.BindCommand("hello")
		assert.Equal(t, "/hello", m.GetCommand()) // defaults to / prefix
		def := m.GetDefinition()
		require.NotNil(t, def)
		assert.Equal(t, "hello", def.Name)
	})
}
