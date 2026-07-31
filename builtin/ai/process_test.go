package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

type mockProvider struct {
	chatFn       func(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	chatStreamFn func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)
}

func (m *mockProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if m.chatFn != nil {
		return m.chatFn(ctx, req)
	}
	return &ChatResponse{Content: "mock response"}, nil
}

func (m *mockProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	if m.chatStreamFn != nil {
		return m.chatStreamFn(ctx, req)
	}
	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Type: StreamEventText, Content: "mock stream"}
	ch <- StreamEvent{Type: StreamEventDone}
	close(ch)
	return ch, nil
}

func TestProcessWithToolsNoTools(t *testing.T) {
	p := &Plugin{
		cfg:      &Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		prov:     &mockProvider{},
		skillReg: NewSkillRegistry(),
	}

	session := p.sm.GetOrCreate("test:chat:user", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hello"})

	evt := platform.NewSyntheticEvent("c2c", "hello")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, session)
	if err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	if result.Text != "mock stream" {
		t.Errorf("expected %q, got %q", "mock stream", result.Text)
	}
}

func TestProcessWithToolsMaxDepth(t *testing.T) {
	p := &Plugin{
		cfg: &Config{MaxDepth: 1, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:  NewSessionManager(100, 20, time.Hour, nil),
		reg: NewToolRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				ch := make(chan StreamEvent, 3)
				ch <- StreamEvent{Type: StreamEventText, Content: "using tool"}
				ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
					ID: "call_1", Name: "test_tool",
				}}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
		cmdPatterns: make(map[string]string),
		skillReg:    NewSkillRegistry(),
	}

	p.reg.Register(Tool{
		Name:        "test_tool",
		Description: "test",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "tool result", nil
		},
	})

	session := p.sm.GetOrCreate("test:depth", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "do something"})

	evt := platform.NewSyntheticEvent("c2c", "do something")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, session)
	if err == nil {
		t.Errorf("expected error for max depth, got result: %q", result.Text)
	}
}

func TestExecuteToolSkill(t *testing.T) {
	p := &Plugin{
		cfg:      &Config{SkillTimeout: 5 * time.Second, SkillMaxDepth: 1},
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
				return &ChatResponse{Content: "skill result"}, nil
			},
		},
	}

	p.skillReg.Add(Skill{
		Name:        "test_skill",
		OwnerID:     OwnerSystem,
		Description: "a test skill",
		Prompt:      "You are a helper",
		Enabled:     true,
	})

	evt := platform.NewSyntheticEvent("c2c", "do skill")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeTool(ctx, ToolCall{Name: "test_skill", Arguments: map[string]any{"query": "help"}}, context.Background(), &captureSender{})
	if result == "" {
		t.Error("expected non-empty result from skill execution")
	}
}

func TestExecuteToolNotFound(t *testing.T) {
	p := &Plugin{
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeTool(ctx, ToolCall{Name: "nonexistent"}, context.Background(), &captureSender{})
	if result == "" {
		t.Error("expected error message for nonexistent tool")
	}
}

func TestExecuteToolDirect(t *testing.T) {
	p := &Plugin{
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	p.reg.Register(Tool{
		Name:        "echo",
		Description: "echo tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "echo: hello", nil
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeTool(ctx, ToolCall{Name: "echo"}, context.Background(), &captureSender{})
	if result != "echo: hello" {
		t.Errorf("expected %q, got %q", "echo: hello", result)
	}
}

func TestExecuteToolError(t *testing.T) {
	p := &Plugin{
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	p.reg.Register(Tool{
		Name:        "failing_tool",
		Description: "always fails",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "", errors.New("internal error")
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeTool(ctx, ToolCall{Name: "failing_tool"}, context.Background(), &captureSender{})
	if result == "" {
		t.Error("expected error message for failing tool")
	}
}

func TestExecuteToolTimeout(t *testing.T) {
	p := &Plugin{
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}

	p.reg.Register(Tool{
		Name:        "slow_tool",
		Description: "very slow",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "", context.DeadlineExceeded
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeTool(ctx, ToolCall{Name: "slow_tool"}, context.Background(), &captureSender{})
	if result == "" {
		t.Error("expected timeout message")
	}
}

func TestExecuteSkillMaxDepth(t *testing.T) {
	p := &Plugin{
		cfg:      &Config{SkillTimeout: 5 * time.Second, SkillMaxDepth: 1},
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
				return &ChatResponse{
					ToolCalls: []ToolCall{{ID: "call_1", Name: "recursive"}},
				}, nil
			},
		},
	}

	skill := Skill{
		Name:    "recursive_skill",
		Prompt:  "Do stuff",
		Enabled: true,
		Tools: []Tool{
			{Name: "recursive", Execute: func(ctx context.Context, args map[string]any) (string, error) {
				return "done", nil
			}},
		},
	}

	_, err := p.executeSkill(context.Background(), skill, map[string]any{"query": "do it"})
	if err == nil {
		t.Error("expected error for max skill depth")
	}
}

func TestBuildUserSkillTools(t *testing.T) {
	p := &Plugin{
		skillReg: NewSkillRegistry(),
		cfg:      &Config{SkillTimeout: 5 * time.Second, SkillMaxDepth: 1},
		prov:     &mockProvider{},
	}

	p.skillReg.Add(Skill{
		Name:    "u_test",
		OwnerID: "user1",
		Prompt:  "test",
		Enabled: true,
	})
	p.skillReg.Add(Skill{
		Name:    "u_disabled",
		OwnerID: "user1",
		Prompt:  "disabled",
		Enabled: false,
	})

	tools := p.buildUserSkillTools("user1")
	if len(tools) != 1 {
		t.Errorf("expected 1 enabled tool, got %d", len(tools))
	}
}

func TestRouteToolCategory(t *testing.T) {
	p := &Plugin{
		cfg:  &Config{},
		prov: &mockProvider{},
	}

	tools := []Tool{
		{Name: "weather", Categories: []string{"weather"}},
		{Name: "news", Categories: []string{"news"}},
	}

	cat, err := p.routeToolCategory(context.Background(), "what's the weather?", tools)
	if err != nil {
		t.Logf("routeToolCategory (expected without category select): %v", err)
	}
	_ = cat
}

func TestGetOrRouteCategoryCaches(t *testing.T) {
	var routeCalls int
	p := &Plugin{
		cfg: &Config{},
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
				routeCalls++
				return &ChatResponse{
					ToolCalls: []ToolCall{{
						ID:        "call_1",
						Name:      categorySelectToolName,
						Arguments: map[string]any{"category": "weather"},
					}},
				}, nil
			},
		},
	}

	evt := platform.NewSyntheticEvent("c2c", "what's the weather?")
	ctx := eventctx.NewContextFromEvent(evt, nil)
	tools := []Tool{
		{Name: "w", Categories: []string{"weather"}},
		{Name: "n", Categories: []string{"news"}},
	}
	cats := collectToolCategories(tools)

	// 第一次调用：发起路由
	session := &Session{}
	cat, err := p.getOrRouteCategory(ctx, session, "weather?", tools, cats)
	if err != nil {
		t.Fatalf("getOrRouteCategory failed: %v", err)
	}
	if cat != "weather" {
		t.Errorf("expected category weather, got %q", cat)
	}
	if routeCalls != 1 {
		t.Fatalf("expected 1 route call on first invoke, got %d", routeCalls)
	}

	// 第二次调用（TTL 内、工具集未变）：命中缓存，不重复路由
	cat2, err := p.getOrRouteCategory(ctx, session, "unrelated question", tools, cats)
	if err != nil {
		t.Fatalf("getOrRouteCategory failed: %v", err)
	}
	if cat2 != "weather" {
		t.Errorf("expected cached category weather, got %q", cat2)
	}
	if routeCalls != 1 {
		t.Fatalf("expected cache hit (1 route call), got %d", routeCalls)
	}

	// 工具集变化（数量不同）→ 缓存失效，重新路由
	tools2 := []Tool{
		{Name: "w", Categories: []string{"weather"}},
		{Name: "n", Categories: []string{"news"}},
		{Name: "s", Categories: []string{"sport"}},
	}
	cats2 := collectToolCategories(tools2)
	cat3, err := p.getOrRouteCategory(ctx, session, "weather again", tools2, cats2)
	if err != nil {
		t.Fatalf("getOrRouteCategory failed: %v", err)
	}
	if cat3 != "weather" {
		t.Errorf("expected category weather after re-route, got %q", cat3)
	}
	if routeCalls != 2 {
		t.Fatalf("expected 2 route calls after tool set change, got %d", routeCalls)
	}

	// TTL 过期 → 缓存失效，重新路由
	session.Lock()
	session.routeCategory = "weather"
	session.routeAt = time.Now().Add(-routeCacheTTL - time.Second)
	session.routeToolCount = len(tools2)
	session.Unlock()
	cat4, err := p.getOrRouteCategory(ctx, session, "weather once more", tools2, cats2)
	if err != nil {
		t.Fatalf("getOrRouteCategory failed: %v", err)
	}
	if cat4 != "weather" {
		t.Errorf("expected category weather after TTL expiry, got %q", cat4)
	}
	if routeCalls != 3 {
		t.Fatalf("expected 3 route calls after TTL expiry, got %d", routeCalls)
	}
}

func TestRealCommandExecution(t *testing.T) {
	p := &Plugin{
		reg:         NewToolRegistry(),
		skillReg:    NewSkillRegistry(),
		cfg:         &Config{},
		cmdPatterns: map[string]string{"ping": "/ping"},
	}

	p.reg.Register(Tool{
		Name:        "ping",
		Description: "ping",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "pong", nil
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	cs := &captureSender{}
	result := p.executeTool(ctx, ToolCall{Name: "ping"}, context.Background(), cs)
	if result == "" {
		t.Error("expected non-empty result from ping tool")
	}
}
