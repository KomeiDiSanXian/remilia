package ai

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

type mockProvider struct {
	chatFn       func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error)
	chatStreamFn func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error)
}

func (m *mockProvider) Chat(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
	if m.chatFn != nil {
		return m.chatFn(ctx, req)
	}
	return &protocol.ChatResponse{Content: "mock response"}, nil
}

func (m *mockProvider) ChatStream(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
	if m.chatStreamFn != nil {
		return m.chatStreamFn(ctx, req)
	}
	ch := make(chan protocol.StreamEvent, 2)
	ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "mock stream"}
	ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
	close(ch)
	return ch, nil
}

func TestRepairToolCallSequence(t *testing.T) {
	assistant2 := protocol.Message{Role: protocol.RoleAssistant, ToolCalls: []protocol.ToolCall{{ID: "c1"}, {ID: "c2"}}}
	assistant1 := protocol.Message{Role: protocol.RoleAssistant, ToolCalls: []protocol.ToolCall{{ID: "c1"}}}

	tests := []struct {
		name string
		in   []protocol.Message
		want []protocol.Message
	}{
		{
			name: "完整序列不变",
			in: []protocol.Message{
				assistant2,
				{Role: protocol.RoleTool, ToolCallID: "c1"},
				{Role: protocol.RoleTool, ToolCallID: "c2"},
				{Role: protocol.RoleUser, Content: "ok"},
			},
			want: []protocol.Message{
				assistant2,
				{Role: protocol.RoleTool, ToolCallID: "c1"},
				{Role: protocol.RoleTool, ToolCallID: "c2"},
				{Role: protocol.RoleUser, Content: "ok"},
			},
		},
		{
			name: "缺失中间工具响应补位",
			in: []protocol.Message{
				assistant2,
				{Role: protocol.RoleTool, ToolCallID: "c1"},
				{Role: protocol.RoleUser, Content: "hi"},
			},
			want: []protocol.Message{
				assistant2,
				{Role: protocol.RoleTool, ToolCallID: "c1"},
				{Role: protocol.RoleTool, ToolCallID: "c2", Content: runtime.ToolResultMissing},
				{Role: protocol.RoleUser, Content: "hi"},
			},
		},
		{
			name: "末尾全部缺失补位",
			in: []protocol.Message{
				assistant1,
				{Role: protocol.RoleUser, Content: "新问题"},
			},
			want: []protocol.Message{
				assistant1,
				{Role: protocol.RoleTool, ToolCallID: "c1", Content: runtime.ToolResultMissing},
				{Role: protocol.RoleUser, Content: "新问题"},
			},
		},
		{
			name: "孤儿 tool 消息被丢弃且待补清单不受影响",
			in: []protocol.Message{
				assistant1,
				{Role: protocol.RoleTool, ToolCallID: "cX"},
			},
			want: []protocol.Message{
				assistant1,
				{Role: protocol.RoleTool, ToolCallID: "c1", Content: runtime.ToolResultMissing},
			},
		},
		{
			name: "前置孤儿 tool 消息被丢弃",
			in: []protocol.Message{
				{Role: protocol.RoleTool, ToolCallID: "c1"},
				assistant1,
				{Role: protocol.RoleTool, ToolCallID: "c1"},
				{Role: protocol.RoleUser, Content: "hi"},
			},
			want: []protocol.Message{
				assistant1,
				{Role: protocol.RoleTool, ToolCallID: "c1"},
				{Role: protocol.RoleUser, Content: "hi"},
			},
		},
		{
			name: "多轮工具调用互不影响",
			in: []protocol.Message{
				assistant1,
				{Role: protocol.RoleTool, ToolCallID: "c1"},
				assistant1,
				{Role: protocol.RoleUser, Content: "end"},
			},
			want: []protocol.Message{
				assistant1,
				{Role: protocol.RoleTool, ToolCallID: "c1"},
				assistant1,
				{Role: protocol.RoleTool, ToolCallID: "c1", Content: runtime.ToolResultMissing},
				{Role: protocol.RoleUser, Content: "end"},
			},
		},
		{
			name: "无工具调用原样返回",
			in: []protocol.Message{
				{Role: protocol.RoleUser, Content: "hi"},
				{Role: protocol.RoleAssistant, Content: "hello"},
			},
			want: []protocol.Message{
				{Role: protocol.RoleUser, Content: "hi"},
				{Role: protocol.RoleAssistant, Content: "hello"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runtime.RepairToolCallSequence(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("repairToolCallSequence mismatch\ngot:  %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

// TestProcessWithToolsSkippedToolCallsGetPlaceholderResponses 验证中断跳过的
// 工具调用也会补占位 tool 消息：assistant(tool_calls) 之后每个 tool_call_id
// 都必须有对应 tool 响应（API 硬性约束，缺失会导致下次请求 400）。
func TestProcessWithToolsSkippedToolCallsGetPlaceholderResponses(t *testing.T) {
	var sess *session.Session
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolParallel: 2},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				// 模拟 LLM 流式返回工具调用期间用户发新消息抢占
				sess.RequestInterrupt()
				ch := make(chan protocol.StreamEvent, 3)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "c1", Name: "t1"}}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "c2", Name: "t2"}}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	p.reg.Register(toolkit.Tool{Name: "t1", Execute: func(ctx context.Context, args map[string]any) (string, error) {
		return "ok", nil
	}})
	p.reg.Register(toolkit.Tool{Name: "t2", Execute: func(ctx context.Context, args map[string]any) (string, error) {
		return "ok", nil
	}})

	sess = p.sm.GetOrCreate("test:skip", "user", "chat")
	if !sess.BeginTurn() {
		t.Fatal("BeginTurn failed")
	}
	defer sess.EndTurn()
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "run"})

	evt := platform.NewSyntheticEvent("c2c", "run")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if _, err := p.processWithTools(ctx, sess); err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}

	msgs := sess.SnapshotMessages()
	assistantIdx := -1
	for i, m := range msgs {
		if m.Role == protocol.RoleAssistant && len(m.ToolCalls) == 2 {
			assistantIdx = i
			break
		}
	}
	if assistantIdx < 0 {
		t.Fatalf("expected assistant with 2 tool calls, got %+v", msgs)
	}
	if assistantIdx+2 >= len(msgs) {
		t.Fatalf("tool calls must be followed by tool messages, got %+v", msgs)
	}
	first, second := msgs[assistantIdx+1], msgs[assistantIdx+2]
	if first.Role != protocol.RoleTool || second.Role != protocol.RoleTool {
		t.Fatalf("tool calls must be followed by tool messages, got %+v %+v", first, second)
	}
	contents := map[string]string{first.ToolCallID: first.Content, second.ToolCallID: second.Content}
	for _, id := range []string{"c1", "c2"} {
		content, ok := contents[id]
		if !ok {
			t.Fatalf("missing tool response for %s (got %+v)", id, msgs)
		}
		if !strings.Contains(content, "未执行") {
			t.Errorf("skipped call %s should have placeholder content, got %q", id, content)
		}
	}
}

func TestEffectiveTurnTimeout(t *testing.T) {
	// 默认推导：api_timeout × max(2, min(max_depth, 5))
	p := &Plugin{cfg: &config.Config{APITimeout: 60 * time.Second, MaxDepth: 5}}
	if got := runtime.EffectiveTurnTimeout(p.cfg); got != 5*time.Minute {
		t.Errorf("default turn timeout = %v, want 5m", got)
	}
	// max_depth 很大时封顶 5 轮
	p.cfg.MaxDepth = 15
	if got := runtime.EffectiveTurnTimeout(p.cfg); got != 5*time.Minute {
		t.Errorf("capped turn timeout = %v, want 5m", got)
	}
	// max_depth 很小时至少 2 轮
	p.cfg.MaxDepth = 1
	if got := runtime.EffectiveTurnTimeout(p.cfg); got != 2*time.Minute {
		t.Errorf("min turn timeout = %v, want 2m", got)
	}
	// 显式配置优先
	p.cfg = &config.Config{APITimeout: 60 * time.Second, MaxDepth: 5, TurnTimeout: 90 * time.Second}
	if got := runtime.EffectiveTurnTimeout(p.cfg); got != 90*time.Second {
		t.Errorf("explicit turn timeout = %v, want 90s", got)
	}
	// api_timeout 未配置时回退 60s
	p.cfg = &config.Config{}
	if got := runtime.EffectiveTurnTimeout(p.cfg); got != 5*time.Minute {
		t.Errorf("fallback turn timeout = %v, want 5m", got)
	}
}

func TestLiftEventDeadline(t *testing.T) {
	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)
	// 模拟全局 Timeout 中间件注入的 30s deadline
	shortCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx.SetStdContext(shortCtx)

	p := &Plugin{cfg: &config.Config{APITimeout: 60 * time.Second, MaxDepth: 5}}
	restore := runtime.LiftEventDeadline(ctx, runtime.EffectiveTurnTimeout(p.cfg))

	// 替换后：使用独立预算（默认推导 5m），不再继承 30s 中间件 deadline
	deadline, ok := ctx.Context().Deadline()
	if !ok {
		t.Fatal("lifted context should have a deadline")
	}
	if remaining := time.Until(deadline); remaining < 4*time.Minute || remaining > 6*time.Minute {
		t.Errorf("lifted deadline remaining = %v, want ~5m", remaining)
	}

	restore()
	// 恢复后回到原始上下文
	restoredDL, ok := ctx.Context().Deadline()
	if !ok {
		t.Fatal("restored context should keep the original deadline")
	}
	if time.Until(restoredDL) > 2*time.Minute {
		t.Errorf("restored deadline should be the original 30s, got %v remaining", time.Until(restoredDL))
	}
}

// TestProcessWithToolsUsesTurnBudgetNotEventDeadline 复现生产场景：
// 事件上下文携带已过期的 deadline（全局 Timeout 中间件注入），
// 多轮工具循环必须使用插件独立预算而非继承该 deadline，
// 否则第一轮请求就因 deadline 已过而被切断。
func TestProcessWithToolsUsesTurnBudgetNotEventDeadline(t *testing.T) {
	var streamCalls atomic.Int32
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolParallel: 1},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				// 模拟真实 provider：context 已死时返回超时错误
				if ctx.Err() != nil {
					ch := make(chan protocol.StreamEvent, 1)
					ch <- protocol.StreamEvent{Type: protocol.StreamEventError, Err: ctx.Err()}
					close(ch)
					return ch, nil
				}
				streamCalls.Add(1)
				ch := make(chan protocol.StreamEvent, 2)
				if streamCalls.Load() == 1 {
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "c1", Name: "t1"}}
				} else {
					ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "done"}
				}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	p.reg.Register(toolkit.Tool{Name: "t1", Execute: func(ctx context.Context, args map[string]any) (string, error) {
		return "ok", nil
	}})

	sess := p.sm.GetOrCreate("test:deadline", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "run"})

	evt := platform.NewSyntheticEvent("c2c", "run")
	ctx := eventctx.NewContextFromEvent(evt, nil)
	// 事件上下文 deadline 已过期（全局 Timeout 中间件的极端情形）
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	ctx.SetStdContext(expired)

	result, err := p.processWithTools(ctx, sess)
	if err != nil {
		t.Fatalf("multi-round loop should survive expired event deadline, got err: %v", err)
	}
	if result.Text != "done" {
		t.Errorf("expected final text %q, got %q", "done", result.Text)
	}
	if got := streamCalls.Load(); got != 2 {
		t.Errorf("expected 2 LLM rounds (tool call + final), got %d", got)
	}
}

func TestProcessWithToolsNoTools(t *testing.T) {
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		prov:     &mockProvider{},
		skillReg: toolkit.NewSkillRegistry(),
	}

	sess := p.sm.GetOrCreate("test:chat:user", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "hello"})

	evt := platform.NewSyntheticEvent("c2c", "hello")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, sess)
	if err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	if result.Text != "mock stream" {
		t.Errorf("expected %q, got %q", "mock stream", result.Text)
	}
}

func TestProcessWithToolsMaxDepth(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{MaxDepth: 1, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:  session.NewSessionManager(100, 20, time.Hour, nil),
		reg: toolkit.NewToolRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				ch := make(chan protocol.StreamEvent, 3)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "using tool"}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{
					ID: "call_1", Name: "test_tool",
				}}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
		cmdPatterns: make(map[string]string),
		skillReg:    toolkit.NewSkillRegistry(),
	}

	p.reg.Register(toolkit.Tool{
		Name:        "test_tool",
		Description: "test",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "tool result", nil
		},
	})

	sess := p.sm.GetOrCreate("test:depth", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "do something"})

	evt := platform.NewSyntheticEvent("c2c", "do something")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, sess)
	if err == nil {
		t.Errorf("expected error for max depth, got result: %q", result.Text)
	}
}

func TestExecuteToolSkill(t *testing.T) {
	p := &Plugin{
		cfg:      &config.Config{SkillTimeout: 5 * time.Second, SkillMaxDepth: 1},
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				return &protocol.ChatResponse{Content: "skill result"}, nil
			},
		},
	}

	p.skillReg.Add(toolkit.Skill{
		Name:        "test_skill",
		OwnerID:     toolkit.OwnerSystem,
		Description: "a test skill",
		Prompt:      "You are a helper",
		Enabled:     true,
	})

	evt := platform.NewSyntheticEvent("c2c", "do skill")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeToolResult(ctx, protocol.ToolCall{Name: "test_skill", Arguments: map[string]any{"query": "help"}}, context.Background(), &execution.CaptureSender{}, nil).Text
	if result == "" {
		t.Error("expected non-empty result from skill execution")
	}
}

func TestExecuteToolNotFound(t *testing.T) {
	p := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeToolResult(ctx, protocol.ToolCall{Name: "nonexistent"}, context.Background(), &execution.CaptureSender{}, nil).Text
	if result == "" {
		t.Error("expected error message for nonexistent tool")
	}
}

func TestExecuteToolDirect(t *testing.T) {
	p := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}

	p.reg.Register(toolkit.Tool{
		Name:        "echo",
		Description: "echo tool",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "echo: hello", nil
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeToolResult(ctx, protocol.ToolCall{Name: "echo"}, context.Background(), &execution.CaptureSender{}, nil).Text
	if result != "echo: hello" {
		t.Errorf("expected %q, got %q", "echo: hello", result)
	}
}

func TestExecuteToolError(t *testing.T) {
	p := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}

	p.reg.Register(toolkit.Tool{
		Name:        "failing_tool",
		Description: "always fails",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "", errors.New("internal error")
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeToolResult(ctx, protocol.ToolCall{Name: "failing_tool"}, context.Background(), &execution.CaptureSender{}, nil).Text
	if result == "" {
		t.Error("expected error message for failing tool")
	}
}

func TestExecuteToolTimeout(t *testing.T) {
	p := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}

	p.reg.Register(toolkit.Tool{
		Name:        "slow_tool",
		Description: "very slow",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "", context.DeadlineExceeded
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result := p.executeToolResult(ctx, protocol.ToolCall{Name: "slow_tool"}, context.Background(), &execution.CaptureSender{}, nil).Text
	if result == "" {
		t.Error("expected timeout message")
	}
}

func TestExecuteSkillMaxDepth(t *testing.T) {
	p := &Plugin{
		cfg:      &config.Config{SkillTimeout: 5 * time.Second, SkillMaxDepth: 1},
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				return &protocol.ChatResponse{
					ToolCalls: []protocol.ToolCall{{ID: "call_1", Name: "recursive"}},
				}, nil
			},
		},
	}

	skill := toolkit.Skill{
		Name:    "recursive_skill",
		Prompt:  "Do stuff",
		Enabled: true,
		Tools: []toolkit.Tool{
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

func TestUserSkillActionsFiltersDisabled(t *testing.T) {
	p := &Plugin{
		skillReg: toolkit.NewSkillRegistry(),
		cfg:      &config.Config{SkillTimeout: 5 * time.Second, SkillMaxDepth: 1},
		prov:     &mockProvider{},
	}

	p.skillReg.Add(toolkit.Skill{
		Name:    "u_test",
		OwnerID: "user1",
		Prompt:  "test",
		Enabled: true,
	})
	p.skillReg.Add(toolkit.Skill{
		Name:    "u_disabled",
		OwnerID: "user1",
		Prompt:  "disabled",
		Enabled: false,
	})

	actions := p.userSkillActions("user1")
	if len(actions) != 1 {
		t.Errorf("expected 1 enabled action, got %d", len(actions))
	}
}

func TestProcessWithToolsSelectsSubset(t *testing.T) {
	var gotTools []string
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 1, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolSelectMax: 3},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				for _, t := range req.Tools {
					gotTools = append(gotTools, t.Name)
				}
				ch := make(chan protocol.StreamEvent, 2)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "done"}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	tools := []struct {
		name string
		desc string
	}{
		{"get_weather", "查询指定城市的天气情况，返回温度、湿度与风力"},
		{"get_bilibili_live_status", "查询B站UP主直播状态与在线人数"},
		{"search_anime", "搜索番剧与动画作品信息"},
		{"roll_dice", "掷骰子并进行技能检定"},
		{"query_minecraft_server", "查询我的世界服务器状态"},
		{"draw_tarot", "进行塔罗牌占卜"},
	}
	for _, tt := range tools {
		p.reg.Register(toolkit.Tool{Name: tt.name, Description: tt.desc, Categories: []string{tt.name}})
	}

	sess := p.sm.GetOrCreate("test:sel", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "今天天气怎么样"})

	evt := platform.NewSyntheticEvent("c2c", "今天天气怎么样")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if _, err := p.processWithTools(ctx, sess); err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	if len(gotTools) > p.cfg.ToolSelectMax {
		t.Errorf("expected at most %d tools, got %d: %v", p.cfg.ToolSelectMax, len(gotTools), gotTools)
	}
	found := false
	for _, n := range gotTools {
		if n == "get_weather" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected get_weather in selected tools, got %v", gotTools)
	}
}

func TestRealCommandExecution(t *testing.T) {
	p := &Plugin{
		reg:         toolkit.NewToolRegistry(),
		skillReg:    toolkit.NewSkillRegistry(),
		cfg:         &config.Config{},
		cmdPatterns: map[string]string{"ping": "/ping"},
	}

	p.reg.Register(toolkit.Tool{
		Name:        "ping",
		Description: "ping",
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return "pong", nil
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	cs := &execution.CaptureSender{}
	result := p.executeToolResult(ctx, protocol.ToolCall{Name: "ping"}, context.Background(), cs, nil).Text
	if result == "" {
		t.Error("expected non-empty result from ping tool")
	}
}
