package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
)

type testToolProvider struct{}

func (t testToolProvider) ListTools() []Tool {
	return []Tool{
		{Name: "provider_tool", Description: "from provider"},
	}
}

type testSkillProvider struct{}

func (t testSkillProvider) ListSkills() []Skill {
	return []Skill{
		{Name: "provider_skill", OwnerID: OwnerSystem, Description: "from provider", Prompt: "test"},
	}
}

func TestRegisterToolProvider(t *testing.T) {
	p := &Plugin{reg: NewToolRegistry()}
	p.RegisterToolProvider(testToolProvider{})

	_, ok := p.reg.Get("provider_tool")
	if !ok {
		t.Error("expected tool from provider to be registered")
	}
}

func TestRegisterToolProviderMultiple(t *testing.T) {
	p := &Plugin{reg: NewToolRegistry()}
	p.RegisterToolProvider(testToolProvider{})
	p.RegisterToolProvider(testToolProvider{})

	list := p.reg.List()
	if len(list) != 1 {
		t.Errorf("expected 1 tool, got %d", len(list))
	}
}

func TestRegisterSkill(t *testing.T) {
	p := &Plugin{reg: NewToolRegistry(), skillReg: NewSkillRegistry()}

	p.RegisterSkill(Skill{
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
	p := &Plugin{reg: NewToolRegistry(), skillReg: NewSkillRegistry()}

	p.RegisterSkill(Skill{Name: "custom_owner", OwnerID: "my_plugin", Description: "custom", Prompt: "test"})

	_, ok := p.skillReg.GetByOwner("my_plugin", "custom_owner")
	if !ok {
		t.Error("expected skill with custom owner to be found")
	}
}

func TestRegisterSkillDefaultOwner(t *testing.T) {
	p := &Plugin{reg: NewToolRegistry(), skillReg: NewSkillRegistry()}

	p.RegisterSkill(Skill{Name: "no_owner", Description: "no owner", Prompt: "test"})

	_, ok := p.skillReg.GetSystem("no_owner")
	if !ok {
		t.Error("expected skill with empty owner to default to system")
	}
}

func TestRegisterUserSkill(t *testing.T) {
	p := &Plugin{
		cfg: &Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000},
		reg: NewToolRegistry(), skillReg: NewSkillRegistry(),
	}

	err := p.RegisterUserSkill(Skill{Name: "my_custom", Description: "my skill", Prompt: "You are custom", Enabled: true}, "user123")
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
		cfg: &Config{MaxUserSkills: 1, MaxUserSkillPromptLen: 2000},
		reg: NewToolRegistry(), skillReg: NewSkillRegistry(),
	}

	p.RegisterUserSkill(Skill{Name: "s1", Prompt: "test", Enabled: true}, "user1")
	err := p.RegisterUserSkill(Skill{Name: "s2", Prompt: "test", Enabled: true}, "user1")
	if err == nil {
		t.Error("expected error when exceeding MaxUserSkills")
	}
}

func TestRegisterUserSkillPromptTooLong(t *testing.T) {
	p := &Plugin{
		cfg: &Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 5},
		reg: NewToolRegistry(), skillReg: NewSkillRegistry(),
	}

	err := p.RegisterUserSkill(Skill{Name: "long_prompt", Prompt: "this prompt is way too long", Enabled: true}, "user1")
	if err == nil {
		t.Error("expected error when prompt exceeds MaxUserSkillPromptLen")
	}
}

func TestRegisterUserSkillInvalidName(t *testing.T) {
	p := &Plugin{
		cfg: &Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000},
		reg: NewToolRegistry(), skillReg: NewSkillRegistry(),
	}

	err := p.RegisterUserSkill(Skill{Name: "invalid name with spaces!", Prompt: "test", Enabled: true}, "user1")
	if err == nil {
		t.Error("expected error for invalid skill name")
	}
}

func TestRegisterUserSkillDefaultsToEnabled(t *testing.T) {
	p := &Plugin{
		cfg: &Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000},
		reg: NewToolRegistry(), skillReg: NewSkillRegistry(),
	}

	p.RegisterUserSkill(Skill{Name: "disabled_test", Prompt: "test", Enabled: false}, "user1")
	s, ok := p.skillReg.GetByOwner("user1", "u_disabled_test")
	if !ok {
		t.Fatal("expected skill to exist")
	}
	if !s.Enabled {
		t.Error("expected disabled user skill to be force-enabled")
	}
}

func TestRegisterSkillProvider(t *testing.T) {
	p := &Plugin{reg: NewToolRegistry(), skillReg: NewSkillRegistry()}

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

func TestNoopSessionStore(t *testing.T) {
	store := &noopSessionStore{}
	s, err := store.Load("test")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if s != nil {
		t.Error("expected nil session from noop store")
	}
	if err := store.Save(&Session{ID: "test"}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.Delete("test"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestMakeSkillAddSessionID(t *testing.T) {
	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)
	id := makeSkillAddSessionID(ctx)
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
		cfg:         &Config{TriggerCmd: "/chat"},
		coord:       coord,
		reg:         NewToolRegistry(),
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

func (sauceProvider) ListTools() []Tool {
	return []Tool{
		{Name: "sauce", Description: "explicit sauce tool", Execute: func(context.Context, map[string]any) (string, error) { return "explicit", nil }},
	}
}

// TestRegisterToolProviderOverridesAutoDiscovered 验证显式注册的工具
// 覆盖自动发现的同名命令工具：注册表条目被替换，且命令映射被清除
// （否则 executeRealCommand 会抢走执行权）。
func TestRegisterToolProviderOverridesAutoDiscovered(t *testing.T) {
	coord := &mockReader{
		commands: []engine.CommandInfo{
			{Command: "/sauce", Description: "以图搜图"},
		},
	}
	p := &Plugin{
		cfg:         &Config{},
		coord:       coord,
		reg:         NewToolRegistry(),
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
		cfg:         &Config{},
		coord:       coord,
		reg:         NewToolRegistry(),
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

func TestRegisterSkillAsTool(t *testing.T) {
	p := &Plugin{reg: NewToolRegistry(), skillReg: NewSkillRegistry()}
	skill := Skill{Name: "tool_skill", OwnerID: OwnerSystem, Description: "a skill that becomes a tool", Prompt: "test"}
	p.registerSkillAsTool(skill)
	_, ok := p.reg.Get("tool_skill")
	if !ok {
		t.Error("expected skill to be registered as tool")
	}
}

func TestHealthCheckers(t *testing.T) {
	p := &Plugin{cfg: &Config{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test"}}
	checkers := p.HealthCheckers()
	if len(checkers) != 1 {
		t.Errorf("expected 1 health checker, got %d", len(checkers))
	}
}
