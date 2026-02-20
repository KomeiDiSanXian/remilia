package engine

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestOptimization_GetAllCommandsCache 测试 GetAllCommands 缓存优化
func TestOptimization_GetAllCommandsCache(t *testing.T) {
	eng := NewEngine()
	defer eng.Close()

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

	eng.RegisterCommandDef(dto.C2CMessageCreate, def1)
	eng.RegisterCommandDef(dto.GroupAtMessageCreate, def2)
	eng.RegisterCommandDef(dto.C2CMessageCreate, def3)

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
	defer eng.Close()

	// 注册100个命令
	for i := 0; i < 100; i++ {
		def := &command.Definition{
			Name:        string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Description: "Test command",
		}
		eng.RegisterCommandDef(dto.C2CMessageCreate, def)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eng.GetAllCommands()
	}
}

// TestOptimization_MatcherCompiler 测试 Matcher 编译优化
func TestOptimization_MatcherCompiler(t *testing.T) {
	eng := NewEngine()
	defer eng.Close()

	// 注册一些命令
	def := &command.Definition{
		Name:        "test",
		Description: "Test command",
	}
	m := eng.RegisterCommandDef(dto.C2CMessageCreate, def)

	// 编译 matcher
	compiler := eng.GetCompiler()
	compiled := compiler.Compile(m)

	if compiled == nil {
		t.Fatal("Compiled matcher should not be nil")
	}

	if compiled.Source != m.Source {
		t.Errorf("Expected source %q, got %q", m.Source, compiled.Source)
	}

	if compiled.Priority != m.getPriority() {
		t.Errorf("Expected priority %d, got %d", m.getPriority(), compiled.Priority)
	}

	// 验证规则已编译
	if len(compiled.Rules) == 0 {
		t.Error("Compiled matcher should have rules")
	}

	t.Logf("Compiled %d rules", len(compiled.Rules))
}

// TestOptimization_CompileAllMatchers 测试批量编译
func TestOptimization_CompileAllMatchers(t *testing.T) {
	eng := NewEngine()
	defer eng.Close()

	// 注册多个命令
	for i := 0; i < 10; i++ {
		def := &command.Definition{
			Name:        "test" + string(rune('0'+i)),
			Description: "Test command",
		}
		eng.RegisterCommandDef(dto.C2CMessageCreate, def)
	}

	// 批量编译
	eng.CompileAllMatchers()

	// 验证编译缓存
	compiler := eng.GetCompiler()
	state := eng.state.Load()

	compiledCount := 0
	for _, m := range state.matchers {
		if _, ok := compiler.cache.Load(m); ok {
			compiledCount++
		}
	}

	if compiledCount != len(state.matchers) {
		t.Errorf("Expected %d compiled matchers, got %d", len(state.matchers), compiledCount)
	}

	t.Logf("Successfully compiled %d matchers", compiledCount)
}
