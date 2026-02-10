package debug

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/stretchr/testify/assert"
)

func TestPlugin_SubcommandRegistration(t *testing.T) {
	eng := engine.NewEngine()
	plugin := New()

	err := plugin.Load(eng)
	assert.NoError(t, err)

	// 验证主命令已注册
	commands := eng.GetAllCommands()

	t.Logf("Total commands found: %d", len(commands))
	for i, cmd := range commands {
		t.Logf("Command %d: %s (EventType: %s, Plugin: %s, Definition: %v)", i, cmd.Command, cmd.EventType, cmd.Plugin, cmd.Definition != nil)
	}

	// 应该有 1 个 debug 命令（GetAllCommands 会对相同命令名去重）
	debugCommands := 0
	for _, cmd := range commands {
		if cmd.Command == "/debug" || cmd.Command == "debug" {
			debugCommands++
			// 验证命令定义存在
			if !assert.NotNil(t, cmd.Definition, "命令定义不应为空") {
				continue
			}
			// 验证子命令已定义
			if cmd.Definition != nil {
				if !assert.NotEmpty(t, cmd.Definition.SubCommands, "应该有子命令") {
					continue
				}
				if !assert.Equal(t, 8, len(cmd.Definition.SubCommands), "应该有 8 个子命令") {
					t.Logf("实际子命令数: %d", len(cmd.Definition.SubCommands))
					continue
				}

				// 验证所有预期的子命令
				subCmdNames := make(map[string]bool)
				for _, subCmd := range cmd.Definition.SubCommands {
					subCmdNames[subCmd.Name] = true
				}

				expectedSubCmds := []string{"event", "ctx", "matcher", "runtime", "commands", "plugins", "bench", "stats"}
				for _, expected := range expectedSubCmds {
					assert.True(t, subCmdNames[expected], "应该包含子命令: %s", expected)
				}
			}
		}
	}

	assert.Equal(t, 1, debugCommands, "应该有 1 个 debug 命令注册（已去重）")
}

func TestPlugin_SubcommandDefinitions(t *testing.T) {
	eng := engine.NewEngine()
	plugin := New()

	err := plugin.Load(eng)
	assert.NoError(t, err)

	commands := eng.GetAllCommands()

	// 查找第一个 debug 命令
	var debugCmd *engine.CommandInfo
	for i, cmd := range commands {
		if cmd.Command == "/debug" || cmd.Command == "debug" {
			debugCmd = &commands[i]
			break
		}
	}

	assert.NotNil(t, debugCmd, "应该找到 debug 命令")
	assert.NotNil(t, debugCmd.Definition, "命令定义不应为空")

	if debugCmd.Definition != nil {
		// 验证主命令信息
		assert.Equal(t, "debug", debugCmd.Definition.Name)
		assert.Equal(t, "开发调试工具集合", debugCmd.Definition.Description)
		assert.Equal(t, "开发", debugCmd.Definition.Category)
		assert.Contains(t, debugCmd.Definition.Aliases, "dbg")

		// 验证 matcher 子命令（带参数）
		var matcherSubCmd *command.Definition
		for _, subCmd := range debugCmd.Definition.SubCommands {
			if subCmd.Name == "matcher" {
				matcherSubCmd = subCmd
				break
			}
		}

		assert.NotNil(t, matcherSubCmd, "应该找到 matcher 子命令")
		if matcherSubCmd != nil {
			assert.NotEmpty(t, matcherSubCmd.Arguments, "matcher 子命令应该有参数")
			assert.Equal(t, 1, len(matcherSubCmd.Arguments))
			assert.Equal(t, "command", matcherSubCmd.Arguments[0].Name)
			assert.True(t, matcherSubCmd.Arguments[0].Required)
		}

		// 验证 bench 子命令（带参数）
		var benchSubCmd *command.Definition
		for _, subCmd := range debugCmd.Definition.SubCommands {
			if subCmd.Name == "bench" {
				benchSubCmd = subCmd
				break
			}
		}

		assert.NotNil(t, benchSubCmd, "应该找到 bench 子命令")
		if benchSubCmd != nil {
			assert.NotEmpty(t, benchSubCmd.Arguments, "bench 子命令应该有参数")
			assert.Equal(t, 1, len(benchSubCmd.Arguments))
			assert.Equal(t, "command", benchSubCmd.Arguments[0].Name)
			assert.True(t, benchSubCmd.Arguments[0].Required)
		}
	}
}
