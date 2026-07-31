package ai

import (
	"context"
	"fmt"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func makeContext(content string) *eventctx.Context {
	evt := platform.NewSyntheticEvent("c2c", content,
		platform.WithSyntheticSender(platform.UserInfo{ID: "user", DisplayName: "User"}))
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
		sm:         NewSessionManager(100, 20, time.Hour, nil),
		cfg:        cfg,
		triggerCmd: "/ai",
		reg:        NewToolRegistry(),
		skillReg:   NewSkillRegistry(),
	}

	ctx := makeContext("/ai skill add my_skill You are a test skill for testing")
	err := p.handleSkillCommand(ctx)
	if err != nil {
		t.Fatalf("handleSkillAdd failed: %v", err)
	}

	skills := p.skillReg.ListByOwner("user")
	if len(skills) != 1 {
		t.Fatalf("expected 1 registered skill, got %d", len(skills))
	}
	if skills[0].Name != "u_my_skill" {
		t.Errorf("expected skill name u_my_skill, got %q", skills[0].Name)
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

func TestHandleSkillPromoteWithPrefixedName(t *testing.T) {
	cfg := &Config{MaxUserSkills: 10, MaxUserSkillPromptLen: 2000, TriggerCmd: "/ai"}
	p := &Plugin{
		sm:         NewSessionManager(100, 20, time.Hour, nil),
		cfg:        cfg,
		triggerCmd: "/ai",
		reg:        NewToolRegistry(),
		skillReg:   NewSkillRegistry(),
	}

	if err := p.skillReg.Add(Skill{
		Name:        "u_my_skill",
		OwnerID:     "user",
		Prompt:      "test prompt",
		Description: "desc",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("add skill: %v", err)
	}

	// 带 u_ 前缀调用 promote，需要管理员身份
	evt := platform.NewSyntheticEvent("c2c", "/ai skill promote u_my_skill",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user", DisplayName: "User"}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	pm := eventctx.NewPermissionManager()
	if err := pm.AssignRole("user", "admin"); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	ctx.SetPermissionManager(pm)

	if err := p.handleSkillCommand(ctx); err != nil {
		t.Fatalf("handleSkillCommand promote failed: %v", err)
	}

	// 提升后：系统技能存在（去前缀）且已注册为工具
	if _, ok := p.skillReg.GetSystem("my_skill"); !ok {
		t.Error("expected system skill my_skill after promote")
	}
	if _, ok := p.reg.Get("my_skill"); !ok {
		t.Error("expected promoted skill to be registered as a tool")
	}
	// 用户技能应已移除
	if _, ok := p.skillReg.GetByOwner("user", "u_my_skill"); ok {
		t.Error("expected user skill u_my_skill to be removed after promote")
	}
}

// blockingProvider 用于测试 summary 单飞：Chat 阻塞直到 release。
type blockingProvider struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	close(b.started)
	select {
	case <-b.release:
		return &ChatResponse{Content: "summary done"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *blockingProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	return nil, fmt.Errorf("not used in summary test")
}

func TestExecSubCommandSummarySingleFlight(t *testing.T) {
	prov := &blockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	p := &Plugin{
		sm:           NewSessionManager(100, 20, time.Hour, nil),
		cfg:          &Config{TriggerCmd: "/ai", APITimeout: time.Second},
		prov:         prov,
		summaries:    make(map[string]bool),
		lifecycleCtx: context.Background(),
	}
	sessionID := "synthetic::user"
	session := p.sm.GetOrCreate(sessionID, "user", "")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hello"})
	p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: "hi"})

	ctx := makeContext("/ai summary")
	if err := p.execSubCommand(ctx, "summary"); err != nil {
		t.Fatalf("execSubCommand summary failed: %v", err)
	}

	// 等待后台总结 goroutine 启动并阻塞
	select {
	case <-prov.started:
	case <-time.After(2 * time.Second):
		t.Fatal("summary goroutine did not start")
	}

	// 第二次调用不应再启动新的 goroutine
	if err := p.execSubCommand(ctx, "summary"); err != nil {
		t.Fatalf("second execSubCommand summary failed: %v", err)
	}

	p.summaryMu.Lock()
	inFlight := p.summaries[sessionID]
	p.summaryMu.Unlock()
	if !inFlight {
		t.Error("expected summary in-flight flag to be set")
	}

	// 释放后应清理标记
	close(prov.release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.summaryMu.Lock()
		inFlight = p.summaries[sessionID]
		p.summaryMu.Unlock()
		if !inFlight {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if inFlight {
		t.Error("expected summary in-flight flag cleared after completion")
	}
}
