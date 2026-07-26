package ai

import (
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func makeContext(content string) *eventctx.Context {
	evt := platform.NewSyntheticEvent("c2c", content)
	return eventctx.NewContextFromEvent(evt, nil)
}

func TestExecSubCommandReset(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}
	// Create a session first
	_ = p.sm.GetOrCreate("discord:chat:user", "user", "chat")

	ctx := makeContext("/ai reset")
	err := p.execSubCommand(ctx, "reset")
	if err != nil {
		t.Fatalf("execSubCommand reset failed: %v", err)
	}
}

func TestExecSubCommandUndoEmpty(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ctx := makeContext("/ai undo")
	err := p.execSubCommand(ctx, "undo")
	if err != nil {
		t.Fatalf("execSubCommand undo on empty failed: %v", err)
	}
}

func TestExecSubCommandUndo(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}
	session := p.sm.GetOrCreate("discord:chat:user", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hello"})
	p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: "hi"})

	ctx := makeContext("/ai undo")
	err := p.execSubCommand(ctx, "undo")
	if err != nil {
		t.Fatalf("execSubCommand undo failed: %v", err)
	}
}

func TestExecSubCommandRetryEmpty(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ctx := makeContext("/ai retry")
	err := p.execSubCommand(ctx, "retry")
	if err != nil {
		t.Fatalf("execSubCommand retry on empty failed: %v", err)
	}
}

func TestExecSubCommandStatusEmpty(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ctx := makeContext("/ai status")
	err := p.execSubCommand(ctx, "status")
	if err != nil {
		t.Fatalf("execSubCommand status on empty failed: %v", err)
	}
}

func TestExecSubCommandStatusWithSession(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{Provider: "openai", Model: "gpt-4o-mini"},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}
	session := p.sm.GetOrCreate("discord:chat:user", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hello"})
	p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: "hi"})

	ctx := makeContext("/ai status")
	err := p.execSubCommand(ctx, "status")
	if err != nil {
		t.Fatalf("execSubCommand status failed: %v", err)
	}
}

func TestExecSubCommandStatsEmpty(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ctx := makeContext("/ai stats")
	err := p.execSubCommand(ctx, "stats")
	if err != nil {
		t.Fatalf("execSubCommand stats on empty failed: %v", err)
	}
}

func TestExecSubCommandTools(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{TriggerCmd: "/ai"},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	p.reg.Register(Tool{Name: "ping", Description: "Ping tool"})

	ctx := makeContext("/ai tools")
	err := p.execSubCommand(ctx, "tools")
	if err != nil {
		t.Fatalf("execSubCommand tools failed: %v", err)
	}
}

func TestExecSubCommandSkill(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{TriggerCmd: "/ai", MaxUserSkills: 10, MaxUserSkillPromptLen: 2000},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ctx := makeContext("/ai skill")
	err := p.execSubCommand(ctx, "skill")
	if err != nil {
		t.Fatalf("execSubCommand skill failed: %v", err)
	}
}

func TestHandleSubCommandReset(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{TriggerCmd: "/ai"},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ok := p.handleSubCommand(makeContext("reset"), "reset")
	if !ok {
		t.Error("handleSubCommand reset should return true")
	}
}

func TestHandleSubCommandUnknown(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ok := p.handleSubCommand(makeContext("unknown"), "unknown")
	if ok {
		t.Error("handleSubCommand unknown should return false")
	}
}

func TestHandleSubCommandChinese(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ok := p.handleSubCommand(makeContext("重置"), "重置")
	if !ok {
		t.Error("handleSubCommand 重置 should return true")
	}

	ok2 := p.handleSubCommand(makeContext("总结"), "总结")
	if !ok2 {
		t.Error("handleSubCommand 总结 should return true")
	}

	ok3 := p.handleSubCommand(makeContext("帮助"), "帮助")
	if !ok3 {
		t.Error("handleSubCommand 帮助 should return true")
	}
}

func TestHandleSkillListEmpty(t *testing.T) {
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      &Config{TriggerCmd: "/ai"},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ctx := makeContext("/ai skill list")
	err := p.handleSkillCommand(ctx)
	if err != nil {
		t.Fatalf("handleSkillCommand on empty list failed: %v", err)
	}
}

func TestHandleSkillAddAndList(t *testing.T) {
	cfg := &Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000, TriggerCmd: "/ai"}
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      cfg,
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	ctx := makeContext("/ai skill add my_skill You are a test skill for testing")
	err := p.handleSkillCommand(ctx)
	if err != nil {
		t.Fatalf("handleSkillAdd failed: %v", err)
	}

	listCtx := makeContext("/ai skill list")
	err = p.handleSkillCommand(listCtx)
	if err != nil {
		t.Fatalf("handleSkillList failed: %v", err)
	}
}

func TestHandleSkillToggle(t *testing.T) {
	cfg := &Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000, TriggerCmd: "/ai"}
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      cfg,
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	p.skillReg.Add(Skill{
		Name:    "u_test_skill",
		OwnerID: "user",
		Prompt:  "test",
		Enabled: true,
	})

	enableCtx := makeContext("/ai skill enable test_skill")
	err := p.handleSkillCommand(enableCtx)
	if err != nil {
		t.Fatalf("handleSkillToggle enable failed: %v", err)
	}

	disableCtx := makeContext("/ai skill disable test_skill")
	err = p.handleSkillCommand(disableCtx)
	if err != nil {
		t.Fatalf("handleSkillToggle disable failed: %v", err)
	}
}

func TestHandleSkillRemove(t *testing.T) {
	cfg := &Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000, TriggerCmd: "/ai"}
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      cfg,
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	p.skillReg.Add(Skill{
		Name:    "u_removable",
		OwnerID: "user",
		Prompt:  "test",
	})

	rmCtx := makeContext("/ai skill remove removable")
	err := p.handleSkillCommand(rmCtx)
	if err != nil {
		t.Fatalf("handleSkillRemove failed: %v", err)
	}
}

func TestHandleSkillInfo(t *testing.T) {
	cfg := &Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000, TriggerCmd: "/ai"}
	p := &Plugin{
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		cfg:      cfg,
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	p.skillReg.Add(Skill{
		Name:    "u_info_test",
		OwnerID: "user",
		Prompt:  "test info skill",
		Enabled: true,
	})

	infoCtx := makeContext("/ai skill info info_test")
	err := p.handleSkillCommand(infoCtx)
	if err != nil {
		t.Fatalf("handleSkillInfo failed: %v", err)
	}
}
