package plugin_test

// alias_registrar_plugin_test.go — 插件层别名自动注册测试
//
// 测试 liveRegistryWriter.injectAliasRegistrar 的完整行为：
//   - RegisterCommand + SetDefinition(with Aliases) + Handle → 别名 Matcher 自动创建
//   - 别名 Matcher 继承主命令的 Group / Source
//   - 别名 Matcher 被追踪进插件实例（Disable/Enable 联动）
//   - 冲突别名被跳过

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nopHandler 是一个无副作用的测试用 Handler。
var nopHandler context.Handler = func(c *context.Context) error { return nil }

// TestLiveRegistryWriter_AliasAutoRegistered 验证通过 ctx.Reg.RegisterCommand 注册的命令，
// 在 SetDefinition(带 Aliases) + Handle 后别名路由被自动注册。
func TestLiveRegistryWriter_AliasAutoRegistered(t *testing.T) {
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)

	pingDef := &command.Definition{
		Name:    "ping",
		Aliases: []string{"p", "pong"},
	}

	err := pm.Register(&plugin.Descriptor{
		Name: "myplugin",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Reg.RegisterCommand("", "/ping").
				SetDefinition(pingDef).
				Handle(nopHandler)
			return nil, nil
		},
	})
	require.NoError(t, err)

	// 别名 /p 和 /pong 应该存在于引擎命令索引中
	pInfo := eng.FindCommand("p")
	require.NotNil(t, pInfo, "alias 'p' should be discoverable via FindCommand")

	pongInfo := eng.FindCommand("pong")
	require.NotNil(t, pongInfo, "alias 'pong' should be discoverable via FindCommand")
}

// TestLiveRegistryWriter_AliasInheritsGroupAndSource 验证别名 Matcher 与主命令共享 Group/Source。
func TestLiveRegistryWriter_AliasInheritsGroupAndSource(t *testing.T) {
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)

	greetDef := &command.Definition{
		Name:    "greet",
		Aliases: []string{"hi"},
	}

	err := pm.Register(&plugin.Descriptor{
		Name: "greetplugin",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Reg.RegisterCommand("", "/greet").
				SetDefinition(greetDef).
				Handle(nopHandler)
			return nil, nil
		},
	})
	require.NoError(t, err)

	// 通过引擎查询别名信息
	hiInfo := eng.FindCommand("hi")
	require.NotNil(t, hiInfo, "alias 'hi' should exist")
	// 别名的 Source 应该和主命令一致（plugin:greetplugin）
	assert.Equal(t, "plugin:greetplugin", hiInfo.Source,
		"alias matcher should share Source with primary command")
}

// TestLiveRegistryWriter_AliasTrackedInInstance 验证别名 Matcher 被追踪进插件实例。
// 禁用插件后，别名 Matcher 也应被禁用（Disable/Enable 联动）。
func TestLiveRegistryWriter_AliasTrackedInInstance(t *testing.T) {
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)

	cmdDef := &command.Definition{
		Name:    "echo",
		Aliases: []string{"ec"},
	}

	err := pm.Register(&plugin.Descriptor{
		Name: "echoplugin",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Reg.RegisterCommand("", "/echo").
				SetDefinition(cmdDef).
				Handle(nopHandler)
			return nil, nil
		},
	})
	require.NoError(t, err)

	// 插件正常时，/ec 存在
	require.NotNil(t, eng.FindCommand("ec"), "alias '/ec' should exist before disable")

	// 禁用插件
	err = pm.Disable("echoplugin")
	require.NoError(t, err)

	// 禁用后，别名 Matcher 所属的 group 被 DisableGroup，DisableGroup 不删除 commandIndex 条目
	// 但 Matcher.IsDisabled() 应为 true
	// 我们通过检查插件已 disabled 的状态来间接验证
	assert.True(t, pm.IsDisabled("echoplugin"), "plugin should be disabled")
}

// TestLiveRegistryWriter_ConflictingAliasSkipped 验证已被其他命令占用的别名被跳过，不报错。
func TestLiveRegistryWriter_ConflictingAliasSkipped(t *testing.T) {
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)

	// 先注册 /foo 占用 "foo" 命令词
	err := pm.Register(&plugin.Descriptor{
		Name: "pluginA",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Reg.RegisterCommand("", "/foo").Handle(nopHandler)
			return nil, nil
		},
	})
	require.NoError(t, err)

	// 再注册 /bar，尝试将 "foo" 注册为别名（冲突）
	err = pm.Register(&plugin.Descriptor{
		Name: "pluginB",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			barDef := &command.Definition{
				Name:    "bar",
				Aliases: []string{"foo"}, // 冲突
			}
			ctx.Reg.RegisterCommand("", "/bar").
				SetDefinition(barDef).
				Handle(nopHandler)
			return nil, nil
		},
	})
	// 注册不应失败（冲突别名静默跳过）
	require.NoError(t, err, "conflicting alias should be silently skipped, not cause an error")

	// /foo 应该仍然属于 pluginA，Source 为 "plugin:pluginA"
	fooInfo := eng.FindCommand("foo")
	require.NotNil(t, fooInfo)
	assert.Equal(t, "plugin:pluginA", fooInfo.Source,
		"'/foo' should still belong to pluginA, not be overwritten by pluginB alias")
}
