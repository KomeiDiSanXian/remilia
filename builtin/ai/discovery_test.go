package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
)

type testToolProvider struct{}

func (t testToolProvider) ListTools() []toolkit.Tool {
	return []toolkit.Tool{
		{Name: "provider_tool", Description: "from provider"},
	}
}

type testSkillProvider struct{}

func (t testSkillProvider) ListSkills() []toolkit.Skill {
	return []toolkit.Skill{
		{Name: "provider_skill", OwnerID: toolkit.OwnerSystem, Description: "from provider", Prompt: "test"},
	}
}

func TestRegisterToolProvider(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry()}
	p.RegisterToolProvider(testToolProvider{})

	_, ok := p.reg.Get("provider_tool")
	if !ok {
		t.Error("expected tool from provider to be registered")
	}
}

func TestRegisterToolProviderMultiple(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry()}
	p.RegisterToolProvider(testToolProvider{})
	p.RegisterToolProvider(testToolProvider{})

	list := p.reg.List()
	if len(list) != 1 {
		t.Errorf("expected 1 tool, got %d", len(list))
	}
}

func TestRegisterSkill(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}

	p.RegisterSkill(toolkit.Skill{
		Name: "sys_skill", Description: "system skill", Prompt: "You are a system skill",
	})

	_, ok := p.skillReg.GetSystem("sys_skill")
	if !ok {
		t.Error("expected system skill to be registered")
	}
	_, ok = p.reg.Get("sys_skill")
	if !ok {
		t.Error("expected skill to also be registered as tool")
	}
}

func TestRegisterSkillWithOwnerID(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}

	p.RegisterSkill(toolkit.Skill{Name: "custom_owner", OwnerID: "my_plugin", Description: "custom", Prompt: "test"})

	_, ok := p.skillReg.GetByOwner("my_plugin", "custom_owner")
	if !ok {
		t.Error("expected skill with custom owner to be found")
	}
}

func TestRegisterSkillDefaultOwner(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}

	p.RegisterSkill(toolkit.Skill{Name: "no_owner", Description: "no owner", Prompt: "test"})

	_, ok := p.skillReg.GetSystem("no_owner")
	if !ok {
		t.Error("expected skill with empty owner to default to system")
	}
}

func TestRegisterUserSkill(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000},
		reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry(),
	}

	err := p.RegisterUserSkill(toolkit.Skill{Name: "my_custom", Description: "my skill", Prompt: "You are custom", Enabled: true}, "user123")
	if err != nil {
		t.Fatalf("RegisterUserSkill failed: %v", err)
	}
	_, ok := p.skillReg.GetByOwner("user123", "u_my_custom")
	if !ok {
		t.Error("expected user skill with u_ prefix")
	}
}

func TestRegisterUserSkillLimit(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{MaxUserSkills: 1, MaxUserSkillPromptLen: 2000},
		reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry(),
	}

	p.RegisterUserSkill(toolkit.Skill{Name: "s1", Prompt: "test", Enabled: true}, "user1")
	err := p.RegisterUserSkill(toolkit.Skill{Name: "s2", Prompt: "test", Enabled: true}, "user1")
	if err == nil {
		t.Error("expected error when exceeding MaxUserSkills")
	}
}

func TestRegisterUserSkillPromptTooLong(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 5},
		reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry(),
	}

	err := p.RegisterUserSkill(toolkit.Skill{Name: "long_prompt", Prompt: "this prompt is way too long", Enabled: true}, "user1")
	if err == nil {
		t.Error("expected error when prompt exceeds MaxUserSkillPromptLen")
	}
}

func TestRegisterUserSkillInvalidName(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000},
		reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry(),
	}

	err := p.RegisterUserSkill(toolkit.Skill{Name: "invalid name with spaces!", Prompt: "test", Enabled: true}, "user1")
	if err == nil {
		t.Error("expected error for invalid skill name")
	}
}

func TestRegisterUserSkillDefaultsToEnabled(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000},
		reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry(),
	}

	p.RegisterUserSkill(toolkit.Skill{Name: "disabled_test", Prompt: "test", Enabled: false}, "user1")
	s, ok := p.skillReg.GetByOwner("user1", "u_disabled_test")
	if !ok {
		t.Fatal("expected skill to exist")
	}
	if !s.Enabled {
		t.Error("expected disabled user skill to be force-enabled")
	}
}

func TestRegisterSkillProvider(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}

	p.RegisterSkillProvider(testSkillProvider{})
	_, ok := p.skillReg.GetSystem("provider_skill")
	if !ok {
		t.Error("expected skill from provider to be registered")
	}
}

func TestBuildAIDefinition(t *testing.T) {
	def := buildAIDefinition()
	if def == nil {
		t.Fatal("buildAIDefinition returned nil")
	}
	if def.Name != "ai" {
		t.Errorf("expected name %q, got %q", "ai", def.Name)
	}
	if len(def.SubCommands) == 0 {
		t.Error("expected AI command to have subcommands")
	}

	subNames := make(map[string]bool)
	for _, sub := range def.SubCommands {
		subNames[sub.Name] = true
	}
	for _, name := range []string{"reset", "undo", "retry", "summary", "status", "stats", "tools", "skill"} {
		if !subNames[name] {
			t.Errorf("expected subcommand %q in AI definition", name)
		}
	}

	skillDef := findSubDef(def, "skill")
	if skillDef == nil {
		t.Fatal("expected skill subcommand")
	}
	skillSubs := make(map[string]bool)
	for _, sub := range skillDef.SubCommands {
		skillSubs[sub.Name] = true
	}
	for _, name := range []string{"add", "list", "remove", "enable", "disable", "promote", "info"} {
		if !skillSubs[name] {
			t.Errorf("expected skill subcommand %q", name)
		}
	}
}

func findSubDef(def *command.Definition, name string) *command.Definition {
	for _, sub := range def.SubCommands {
		if sub.Name == name {
			return sub
		}
	}
	return nil
}

func TestMakeSkillAddSessionID(t *testing.T) {
	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)
	id := (&catalogState{}).skillAddSessionID(ctx)
	if id == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestDiscoverCommandsWithoutCoordinator(t *testing.T) {
	p := &Plugin{coord: nil}
	p.DiscoverCommands()
}

// mockReader 实现 engine.Reader，用于测试 discoverTools 的排除逻辑。
type mockReader struct {
	commands []engine.CommandInfo
}

func (m *mockReader) GetAllCommands() []engine.CommandInfo                   { return m.commands }
func (m *mockReader) FindCommand(name string) *engine.CommandInfo            { return nil }
func (m *mockReader) GetCommandsByPlugin() map[string][]engine.CommandInfo   { return nil }
func (m *mockReader) GetCommandsByCategory() map[string][]engine.CommandInfo { return nil }
func (m *mockReader) GetMatcherCount() int                                   { return 0 }
func (m *mockReader) GetMatcherStats() engine.MatcherStats                   { return engine.MatcherStats{} }
func (m *mockReader) GetMaxMatchers() int                                    { return 0 }
func (m *mockReader) GetTempMatcherCount() int                               { return 0 }

func TestDiscoverToolsExcludesTriggerCmd(t *testing.T) {
	coord := &mockReader{
		commands: []engine.CommandInfo{
			{Command: "/chat", Description: "AI trigger"},
			{Command: "/weather", Description: "Get weather"},
			{Command: "/secret", Description: "needs perms", Permissions: []string{"admin"}},
		},
	}
	p := &Plugin{
		cfg:         &config.Config{TriggerCmd: "/chat"},
		coord:       coord,
		reg:         toolkit.NewToolRegistry(),
		cmdMu:       sync.RWMutex{},
		cmdPatterns: make(map[string]string),
	}
	p.DiscoverCommands()

	// 触发命令 /chat 不应被暴露为工具
	if _, ok := p.reg.Get("chat"); ok {
		t.Error("trigger command /chat should not be exposed as a tool")
	}
	// 无权限命令应被发现
	if _, ok := p.reg.Get("weather"); !ok {
		t.Error("expected /weather to be discovered")
	}
	// 需权限命令不应被发现
	if _, ok := p.reg.Get("secret"); ok {
		t.Error("permission-gated command should not be discovered")
	}
}

// sauceProvider 注册与命令同名的工具，用于验证显式注册覆盖自动发现。
type sauceProvider struct{}

func (sauceProvider) ListTools() []toolkit.Tool {
	return []toolkit.Tool{
		{Name: "sauce", Description: "explicit sauce tool", Execute: func(context.Context, map[string]any) (string, error) { return "explicit", nil }},
	}
}

// TestRegisterToolProviderOverridesAutoDiscovered 验证显式注册的工具
// 覆盖自动发现的同名命令工具：注册表条目被替换，且命令映射被清除
// （否则 execution.RunCommand 会抢走执行权）。
func TestRegisterToolProviderOverridesAutoDiscovered(t *testing.T) {
	coord := &mockReader{
		commands: []engine.CommandInfo{
			{Command: "/sauce", Description: "以图搜图"},
		},
	}
	p := &Plugin{
		cfg:         &config.Config{},
		coord:       coord,
		reg:         toolkit.NewToolRegistry(),
		cmdMu:       sync.RWMutex{},
		cmdPatterns: make(map[string]string),
	}
	p.DiscoverCommands()

	if _, ok := p.reg.Get("sauce"); !ok {
		t.Fatal("expected /sauce to be auto-discovered first")
	}
	if _, ok := p.cmdPatterns["sauce"]; !ok {
		t.Fatal("expected cmdPatterns to have sauce before override")
	}

	p.RegisterToolProvider(sauceProvider{})

	tool, ok := p.reg.Get("sauce")
	if !ok {
		t.Fatal("expected sauce tool to remain after override")
	}
	if tool.Description != "explicit sauce tool" {
		t.Errorf("expected provider tool to win, got %q", tool.Description)
	}
	if _, ok := p.cmdPatterns["sauce"]; ok {
		t.Error("expected cmdPatterns entry for sauce to be removed after override")
	}
}

// TestDiscoverToolsSkipsExplicitlyRegistered 验证已显式注册的名称
// 不再被自动发现覆盖，且不会污染 cmdPatterns。
func TestDiscoverToolsSkipsExplicitlyRegistered(t *testing.T) {
	coord := &mockReader{
		commands: []engine.CommandInfo{
			{Command: "/sauce", Description: "以图搜图"},
		},
	}
	p := &Plugin{
		cfg:         &config.Config{},
		coord:       coord,
		reg:         toolkit.NewToolRegistry(),
		cmdMu:       sync.RWMutex{},
		cmdPatterns: make(map[string]string),
	}
	p.RegisterToolProvider(sauceProvider{})
	p.DiscoverCommands()

	tool, ok := p.reg.Get("sauce")
	if !ok {
		t.Fatal("expected sauce tool from provider")
	}
	if tool.Description != "explicit sauce tool" {
		t.Errorf("expected provider tool to win, got %q", tool.Description)
	}
	if _, ok := p.cmdPatterns["sauce"]; ok {
		t.Error("expected no cmdPatterns entry for explicitly registered tool")
	}
}

// TestDiscoverCommandsExcludesToolProviderPlugins 验证已提供显式 AI 工具
// 的插件，其命令不再自动发现为工具（去重）。
func TestDiscoverCommandsExcludesToolProviderPlugins(t *testing.T) {
	coord := &mockReader{
		commands: []engine.CommandInfo{
			{Command: "/search", Description: "搜索", Plugin: "websearch"},
			{Command: "/ping", Description: "延迟检测", Plugin: "ping"},
		},
	}
	p := &Plugin{
		cfg:         &config.Config{},
		coord:       coord,
		reg:         toolkit.NewToolRegistry(),
		cmdMu:       sync.RWMutex{},
		cmdPatterns: make(map[string]string),
	}
	p.DiscoverCommands("websearch")

	if _, ok := p.reg.Get("search"); ok {
		t.Error("command of a ToolProvider plugin should not be auto-discovered")
	}
	if _, ok := p.reg.Get("ping"); !ok {
		t.Error("command of a non-excluded plugin should still be discovered")
	}
}

// TestDiscoverCommandsAllowlistOverridesExclusion 验证 tool_allowlist 显式
// 列出被排除插件的命令时，该命令仍被发现（用户显式意图优先）。
func TestDiscoverCommandsAllowlistOverridesExclusion(t *testing.T) {
	coord := &mockReader{
		commands: []engine.CommandInfo{
			{Command: "/tarot", Description: "塔罗占卜", Plugin: "fortune"},
		},
	}
	p := &Plugin{
		cfg:         &config.Config{ToolAllowlist: []string{"tarot"}},
		coord:       coord,
		reg:         toolkit.NewToolRegistry(),
		cmdMu:       sync.RWMutex{},
		cmdPatterns: make(map[string]string),
	}
	p.DiscoverCommands("fortune")

	if _, ok := p.reg.Get("tarot"); !ok {
		t.Error("allowlisted command should override plugin exclusion")
	}
}

func TestRegisterSkillAsTool(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}
	skill := toolkit.Skill{Name: "tool_skill", OwnerID: toolkit.OwnerSystem, Description: "a skill that becomes a tool", Prompt: "test"}
	p.registerSkillAsTool(skill)
	_, ok := p.reg.Get("tool_skill")
	if !ok {
		t.Error("expected skill to be registered as tool")
	}
}

func TestHealthCheckers(t *testing.T) {
	p := &Plugin{cfg: &config.Config{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test"}}
	checkers := p.HealthCheckers()
	if len(checkers) != 1 {
		t.Errorf("expected 1 health checker, got %d", len(checkers))
	}
}
