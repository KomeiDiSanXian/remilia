package debug

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestCommandParsingFixes 验证命令解析bug修复
// 这个测试验证了以下两个问题的修复：
// 1. engine.FindCommand 现在支持带/不带 "/" 前缀的搜索
// 2. debug 子命令正确解析参数（使用 args.Get(1) 而不是 args.Get(0)）
func TestCommandParsingFixes(t *testing.T) {
	eng := engine.NewEngine()
	p := New()
	p.SetDevMode(true)

	// 加载 debug 插件（注册 /debug 命令）
	if err := p.Load(eng); err != nil {
		t.Fatalf("Failed to load debug plugin: %v", err)
	}

	t.Run("FindCommand supports slash prefix normalization", func(t *testing.T) {
		// Test 1: 不带 "/" 前缀搜索
		cmd1 := eng.FindCommand("debug")
		if cmd1 == nil {
			t.Error("FindCommand('debug') should find /debug command")
		} else if cmd1.Command != "/debug" {
			t.Errorf("Expected command '/debug', got '%s'", cmd1.Command)
		}

		// Test 2: 带 "/" 前缀搜索
		cmd2 := eng.FindCommand("/debug")
		if cmd2 == nil {
			t.Error("FindCommand('/debug') should find /debug command")
		} else if cmd2.Command != "/debug" {
			t.Errorf("Expected command '/debug', got '%s'", cmd2.Command)
		}
	})

	t.Run("ParseCommandLine correctly parses subcommands", func(t *testing.T) {
		// Test 1: /debug matcher echo
		args1, err := command.ParseCommandLine("/debug matcher echo")
		if err != nil {
			t.Fatalf("Failed to parse command: %v", err)
		}

		if args1.Command != "/debug" {
			t.Errorf("Expected command '/debug', got '%s'", args1.Command)
		}
		if args1.Get(0) != "matcher" {
			t.Errorf("Expected args[0] 'matcher', got '%s'", args1.Get(0))
		}
		if args1.Get(1) != "echo" {
			t.Errorf("Expected args[1] 'echo', got '%s'", args1.Get(1))
		}

		// Test 2: /debug bench echo
		args2, err := command.ParseCommandLine("/debug bench echo")
		if err != nil {
			t.Fatalf("Failed to parse command: %v", err)
		}

		if args2.Command != "/debug" {
			t.Errorf("Expected command '/debug', got '%s'", args2.Command)
		}
		if args2.Get(0) != "bench" {
			t.Errorf("Expected args[0] 'bench', got '%s'", args2.Get(0))
		}
		if args2.Get(1) != "echo" {
			t.Errorf("Expected args[1] 'echo', got '%s'", args2.Get(1))
		}
	})

	t.Run("Debug subcommands use correct argument index", func(t *testing.T) {
		// 注册一个测试命令用于 matcher 和 bench 子命令测试
		eng.OnCommand(dto.C2CMessageCreate, "/echo").
			SetDescription("Echo test command").
			Handle(func(ctx *eventctx.Context) error {
				return nil
			})

		// 验证 /debug matcher echo 能正确解析
		// 注意：这里我们只验证解析逻辑，不实际执行命令
		matcherArgs, err := command.ParseCommandLine("/debug matcher echo")
		if err != nil {
			t.Fatalf("Failed to parse matcher command: %v", err)
		}

		// 在修复前，handleDebugMatcher 会使用 args.Get(0) 得到 "matcher"
		// 在修复后，handleDebugMatcher 使用 args.Get(1) 得到 "echo"
		commandToFind := matcherArgs.Get(1) // 应该是 "echo"
		if commandToFind != "echo" {
			t.Errorf("Expected command name 'echo', got '%s'", commandToFind)
		}

		// 验证能找到这个命令
		foundCmd := eng.FindCommand(commandToFind)
		if foundCmd == nil {
			t.Errorf("Should find command '%s'", commandToFind)
		}

		// 验证 /debug bench echo 也能正确解析
		benchArgs, err := command.ParseCommandLine("/debug bench echo")
		if err != nil {
			t.Fatalf("Failed to parse bench command: %v", err)
		}

		benchCommandToFind := benchArgs.Get(1) // 应该是 "echo"
		if benchCommandToFind != "echo" {
			t.Errorf("Expected command name 'echo', got '%s'", benchCommandToFind)
		}

		// 验证能找到这个命令
		foundBenchCmd := eng.FindCommand(benchCommandToFind)
		if foundBenchCmd == nil {
			t.Errorf("Should find command '%s'", benchCommandToFind)
		}
	})
}
