// Package ai prompt_cache_test.go — 提示词前缀缓存的结构不变量回归测试。
//
// LLM 侧的前缀缓存（DeepSeek 磁盘缓存、OpenAI / Anthropic prompt cache）只在
// 请求前缀逐字节一致时才命中。本文件锁定四条结构不变量，防止后续改动把
// 缓存命中率重新打回低位：
//
//   - System 消息只含稳定内容（框架 + 自定义指令），逐轮字节一致
//   - 逐轮变化的动态上下文挂在最后一条 user 消息上，且不写回会话历史
//   - 执行计划附在消息序列末尾，不插到稳定前缀之后
//   - 工具列表顺序确定（注册表底层是 map，必须排序）
package ai

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestToolRegistryListIsDeterministic 验证工具注册表遍历顺序稳定且按名称排序。
// 注册表底层是 map，未排序时每次 List 的顺序都不同，tools 段会整段失效缓存。
func TestToolRegistryListIsDeterministic(t *testing.T) {
	reg := toolkit.NewToolRegistry()
	want := make([]string, 0, 24)
	for i := range 24 {
		name := fmt.Sprintf("tool_%02d", i)
		reg.Register(toolkit.Tool{Name: name})
		want = append(want, name)
	}
	slices.Sort(want)

	for round := range 10 {
		got := make([]string, 0, len(want))
		for _, tl := range reg.List() {
			got = append(got, tl.Name)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("round %d: ToolRegistry.List must be name-sorted and stable, got %v want %v", round, got, want)
		}
	}
}

// TestStaticSystemPromptStableAcrossTurns 验证稳定系统提示词不随事件/时间变化，
// 且绝不包含任何动态小节。
func TestStaticSystemPromptStableAcrossTurns(t *testing.T) {
	p := &Plugin{cfg: &config.Config{SystemPrompt: "你是蕾米莉亚", ContextGroupMessages: 20, IncludeRuntimeContext: true}}

	evt1 := platform.NewSyntheticEvent("c2c", "hi", platform.WithSyntheticChat(platform.ChatInfo{ID: "c1"}))
	ctx1 := eventctx.NewContextFromEvent(evt1, nil)
	evt2 := platform.NewSyntheticEvent("c2c", "另一个用户", platform.WithSyntheticChat(platform.ChatInfo{ID: "c2"}))
	ctx2 := eventctx.NewContextFromEvent(evt2, nil)

	first := p.buildStaticSystemPrompt(ctx1)
	second := p.buildStaticSystemPrompt(ctx2)
	if first != second {
		t.Errorf("static system prompt must not depend on the event:\n%q\n%q", first, second)
	}
	if !strings.Contains(first, DefaultFrameworkPrompt) || !strings.Contains(first, "你是蕾米莉亚") {
		t.Errorf("static prompt must carry framework + custom instructions, got %q", first)
	}
	for _, marker := range []string{"运行时上下文", "群聊最近消息", "长期记忆", "相关历史消息", "当前执行计划", "动态上下文"} {
		if strings.Contains(first, marker) {
			t.Errorf("static system prompt must not contain dynamic section %q, got %q", marker, first)
		}
	}
}

// TestInjectDynamicContextAttachesToLastUserMessage 验证动态上下文附着在最后一条
// user 消息上（而不是新增消息或写进 System），且不触碰稳定前缀。
func TestInjectDynamicContextAttachesToLastUserMessage(t *testing.T) {
	newMsgs := func() []protocol.Message {
		return []protocol.Message{
			{Role: protocol.RoleSystem, Content: "SYS"},
			{Role: protocol.RoleUser, Content: "第一轮问题"},
			{Role: protocol.RoleAssistant, Content: "第一轮回答"},
			{Role: protocol.RoleUser, Content: "本轮问题"},
		}
	}

	got := runtime.InjectDynamicContext(newMsgs(), "当前时间: 2026-09-17 12:00:00")
	if len(got) != 4 {
		t.Fatalf("injectDynamicContext must not add messages, got %d", len(got))
	}
	want := "===== 动态上下文 =====\n当前时间: 2026-09-17 12:00:00\n\n本轮问题"
	if got[3].Content != want {
		t.Errorf("dynamic block should prefix the last user message, got %q want %q", got[3].Content, want)
	}
	if got[0].Content != "SYS" || got[1].Content != "第一轮问题" || got[2].Content != "第一轮回答" {
		t.Errorf("stable prefix must stay untouched, got %+v", got)
	}

	// 空动态上下文：原样返回（稳定前缀不受影响）
	plain := newMsgs()
	if out := runtime.InjectDynamicContext(plain, ""); out[3].Content != "本轮问题" {
		t.Errorf("empty dynamic context must not modify messages, got %q", out[3].Content)
	}

	// 无 user 消息：原样返回，不 panic
	noUser := []protocol.Message{{Role: protocol.RoleSystem, Content: "SYS"}, {Role: protocol.RoleTool, Content: "r"}}
	if out := runtime.InjectDynamicContext(noUser, "ctx"); len(out) != 2 || out[1].Content != "r" {
		t.Errorf("messages without user turn must be returned unchanged, got %+v", out)
	}
}

// TestInjectDynamicContextMultimodal 验证多模态消息的前置 text part 语义：
// 上下文成为首个 text part，原 parts 顺序与底层数组不被破坏。
func TestInjectDynamicContextMultimodal(t *testing.T) {
	parts := []protocol.ContentPart{
		{Type: protocol.ContentPartText, Text: "看看这张图"},
		{Type: protocol.ContentPartImage, Data: []byte("img")},
	}
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "SYS"},
		{Role: protocol.RoleUser, ContentParts: parts},
	}

	got := runtime.InjectDynamicContext(msgs, "群聊最近消息: A: hi")
	gotParts := got[1].ContentParts
	if len(gotParts) != 3 {
		t.Fatalf("expected 3 parts after injection, got %d", len(gotParts))
	}
	if gotParts[0].Type != protocol.ContentPartText || !strings.Contains(gotParts[0].Text, "动态上下文") {
		t.Errorf("dynamic context should be the first text part, got %+v", gotParts[0])
	}
	if gotParts[1].Text != "看看这张图" || gotParts[2].Type != protocol.ContentPartImage {
		t.Errorf("original content parts must keep their order, got %+v", gotParts)
	}
	if len(parts) != 2 || parts[0].Text != "看看这张图" {
		t.Errorf("original parts slice must not be modified, got %+v", parts)
	}
}

// TestPromptPrefixStableAcrossTurns 端到端验证多轮请求的缓存前缀：
//   - System 消息在每一轮请求中逐字节一致
//   - 动态上下文出现在本轮最后一条 user 消息里，且不写回会话历史
//   - 相邻两轮的公共前缀覆盖到"上一轮之前"的全部历史（只有最近一轮问答
//     与当前轮属于必然失效的尾巴）
func TestPromptPrefixStableAcrossTurns(t *testing.T) {
	var (
		mu   sync.Mutex
		reqs []*protocol.ChatRequest
	)
	p := &Plugin{
		cfg: &config.Config{
			SystemPrompt:           "自定义指令",
			IncludeRuntimeContext:  true,
			ContextGroupMessages:   0,
			MaxDepth:               3,
			APITimeout:             5 * time.Second,
			ToolTimeout:            3 * time.Second,
			ContextRAGMessages:     0,
			MemoryInjectMax:        0,
			ContextWindow:          0,
			IncludeReplyContext:    false,
			ContextGroupIncludeBot: false,
		},
		sm:       session.NewSessionManager(100, 50, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
			mu.Lock()
			snapshot := &protocol.ChatRequest{Model: req.Model, Messages: append([]protocol.Message(nil), req.Messages...), Tools: req.Tools}
			reqs = append(reqs, snapshot)
			mu.Unlock()
			ch := make(chan protocol.StreamEvent, 2)
			ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "回答"}
			ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
			close(ch)
			return ch, nil
		}},
	}

	sess := p.sm.GetOrCreate("cache:t1", "u1", "c1")
	evt := platform.NewSyntheticEvent("c2c", "hi", platform.WithSyntheticChat(platform.ChatInfo{ID: "c1"}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	for _, question := range []string{"第一个问题", "第二个问题", "第三个问题"} {
		runtime.SetSystemMessage(sess, p.buildStaticSystemPrompt(ctx))
		p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: question})
		if !sess.BeginTurn() {
			t.Fatal("BeginTurn failed")
		}
		if _, err := p.processWithTools(ctx, sess); err != nil {
			sess.EndTurn()
			t.Fatalf("processWithTools failed: %v", err)
		}
		sess.EndTurn()
	}

	mu.Lock()
	defer mu.Unlock()
	if len(reqs) != 3 {
		t.Fatalf("expected 3 LLM requests, got %d", len(reqs))
	}

	// 1) System 消息逐字节一致，且不含动态小节
	sys := reqs[0].Messages[0].Content
	if reqs[0].Messages[0].Role != protocol.RoleSystem {
		t.Fatalf("first message must be the system prompt, got %+v", reqs[0].Messages[0])
	}
	for i, r := range reqs {
		if r.Messages[0].Content != sys {
			t.Errorf("turn %d: system prompt changed between turns:\n%q\n%q", i+1, sys, r.Messages[0].Content)
		}
		for _, marker := range []string{"运行时上下文", "动态上下文"} {
			if strings.Contains(r.Messages[0].Content, marker) {
				t.Errorf("turn %d: system prompt must not carry dynamic section %q", i+1, marker)
			}
		}
	}

	// 2) 动态上下文出现在本轮最后一条 user 消息里
	for i, r := range reqs {
		lastUser := -1
		for j, m := range r.Messages {
			if m.Role == protocol.RoleUser {
				lastUser = j
			}
		}
		if lastUser < 0 || !strings.Contains(r.Messages[lastUser].Content, "===== 动态上下文 =====") {
			t.Errorf("turn %d: runtime context should be attached to the current user message, got %+v", i+1, r.Messages)
		}
	}

	// 3) 动态上下文不写回会话历史（否则旧时间/旧窗口会污染后续前缀）
	for _, m := range sess.SnapshotMessages() {
		if strings.Contains(m.Content, "动态上下文") {
			t.Errorf("dynamic context must not be persisted into session history: %q", m.Content)
		}
	}

	// 4) 第 3 轮与第 2 轮的公共前缀至少覆盖 System + 第一轮问答
	//    （最近一轮问答 + 当前轮属于必然失效的尾巴）
	if common := commonMessagePrefix(reqs[1].Messages, reqs[2].Messages); common < 3 {
		t.Errorf("expected shared prompt prefix of at least 3 messages, got %d\nreq2=%+v\nreq3=%+v",
			common, reqs[1].Messages, reqs[2].Messages)
	}
}

// TestPlanMessageAppendedAtTail 验证执行计划作为消息序列最后一条发送，
// 不插到稳定前缀（System 与历史）之后的中段。
func TestPlanMessageAppendedAtTail(t *testing.T) {
	var (
		mu   sync.Mutex
		reqs []*protocol.ChatRequest
	)
	p := &Plugin{
		cfg: &config.Config{
			IncludeRuntimeContext: true,
			MaxDepth:              3,
			APITimeout:            5 * time.Second,
			ToolTimeout:           3 * time.Second,
		},
		sm:       session.NewSessionManager(100, 50, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
			mu.Lock()
			reqs = append(reqs, &protocol.ChatRequest{Messages: append([]protocol.Message(nil), req.Messages...)})
			mu.Unlock()
			ch := make(chan protocol.StreamEvent, 2)
			ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "回答"}
			ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
			close(ch)
			return ch, nil
		}},
	}

	sess := p.sm.GetOrCreate("plan:t1", "u1", "c1")
	sess.SetPlan(&session.Plan{
		Task:   "写一份报告",
		Active: true,
		Steps:  []session.PlanStep{{ID: "s1", Description: "收集资料", Status: session.PlanInProgress}},
	})

	evt := platform.NewSyntheticEvent("c2c", "hi", platform.WithSyntheticChat(platform.ChatInfo{ID: "c1"}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	runtime.SetSystemMessage(sess, p.buildStaticSystemPrompt(ctx))
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "开始"})
	if !sess.BeginTurn() {
		t.Fatal("BeginTurn failed")
	}
	if _, err := p.processWithTools(ctx, sess); err != nil {
		sess.EndTurn()
		t.Fatalf("processWithTools failed: %v", err)
	}
	sess.EndTurn()

	mu.Lock()
	defer mu.Unlock()
	if len(reqs) == 0 {
		t.Fatal("expected at least one LLM request")
	}
	msgs := reqs[0].Messages
	last := msgs[len(msgs)-1]
	if last.Role != protocol.RoleSystem || !strings.Contains(last.Content, "当前执行计划") {
		t.Errorf("plan must be the trailing message, got %+v", msgs)
	}
	// 计划不得出现在稳定前缀（System 与历史）之间
	for i := 0; i < len(msgs)-1; i++ {
		if strings.Contains(msgs[i].Content, "当前执行计划") {
			t.Errorf("plan must not be inserted inside the stable prefix (index %d)", i)
		}
	}
}

// commonMessagePrefix 返回两组消息从头部开始完全一致（role + content）的条数。
func commonMessagePrefix(a, b []protocol.Message) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content {
			return i
		}
	}
	return n
}
