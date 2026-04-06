package engine

import (
	stdctx "context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestOptimization_GetAllCommandsCache 测试 GetAllCommands 缓存优化
func TestOptimization_GetAllCommandsCache(t *testing.T) {
	eng := NewEngine()
	defer eng.Shutdown(stdctx.Background())

	// 注册一些命令
	def1 := &command.Definition{
		Name:        "test1",
		Description: "Test command 1",
		Category:    "testing",
	}
	def2 := &command.Definition{
		Name:        "test2",
		Description: "Test command 2",
		Category:    "testing",
	}
	def3 := &command.Definition{
		Name:        "hidden",
		Description: "Hidden command",
		Hidden:      true,
	}

	eng.RegisterCommandDef(string(platform.EventKindPrivateMessage), def1)
	eng.RegisterCommandDef(string(platform.EventKindGroupMessage), def2)
	eng.RegisterCommandDef(string(platform.EventKindPrivateMessage), def3)

	// 获取所有命令
	commands := eng.GetAllCommands()

	// 调试输出
	t.Logf("Got %d commands:", len(commands))
	for _, cmd := range commands {
		t.Logf("  Command: %s, Desc: %q, Hidden: %v", cmd.Command, cmd.Description, cmd.Definition != nil && cmd.Definition.Hidden)
	}

	// 验证只返回非隐藏命令
	if len(commands) != 2 {
		t.Errorf("Expected 2 commands, got %d", len(commands))
	}

	// 验证命令信息
	foundTest1 := false
	foundTest2 := false
	for _, cmd := range commands {
		if cmd.Command == "/test1" {
			foundTest1 = true
			if cmd.Description != "Test command 1" {
				t.Errorf("Expected description 'Test command 1', got %q", cmd.Description)
			}
		}
		if cmd.Command == "/test2" {
			foundTest2 = true
			if cmd.Description != "Test command 2" {
				t.Errorf("Expected description 'Test command 2', got %q", cmd.Description)
			}
		}
		if cmd.Command == "/hidden" {
			t.Error("Hidden command should not be returned")
		}
	}

	if !foundTest1 {
		t.Error("test1 command not found")
	}
	if !foundTest2 {
		t.Error("test2 command not found")
	}
}

// BenchmarkGetAllCommands_WithCache 基准测试缓存版本
func BenchmarkGetAllCommands_WithCache(b *testing.B) {
	eng := NewEngine()
	defer eng.Shutdown(stdctx.Background())

	// 注册100个命令
	for i := range 100 {
		def := &command.Definition{
			Name:        string('a'+i%26) + string('0'+i/26),
			Description: "Test command",
		}
		eng.RegisterCommandDef(string(platform.EventKindPrivateMessage), def)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eng.GetAllCommands()
	}
}
