package ai

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// --- 并行工具执行 ---

func TestProcessWithToolsParallelTools(t *testing.T) {
	var maxConcurrent atomic.Int32
	var curConcurrent atomic.Int32
	var streamCalls atomic.Int32
	p := &Plugin{
		cfg:      &Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolParallel: 4},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				ch := make(chan StreamEvent, 4)
				if streamCalls.Add(1) == 1 {
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{ID: "c1", Name: "tool_a"}}
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{ID: "c2", Name: "tool_b"}}
				} else {
					ch <- StreamEvent{Type: StreamEventText, Content: "done"}
				}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	for _, name := range []string{"tool_a", "tool_b"} {
		p.reg.Register(Tool{
			Name: name,
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				v := curConcurrent.Add(1)
				if v > maxConcurrent.Load() {
					maxConcurrent.Store(v)
				}
				defer curConcurrent.Add(-1)
				// 工具内 sleep 提供阻塞点：synctest 调度器逐个运行 goroutine，
				// 没有阻塞点会串行跑完导致并发度观测为 1。
				time.Sleep(50 * time.Millisecond)
				return "ok", nil
			},
		})
	}

	session := p.sm.GetOrCreate("test:par", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "do both"})

	evt := platform.NewSyntheticEvent("c2c", "do both")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		if _, err := p.processWithTools(ctx, session); err != nil {
			t.Fatalf("processWithTools failed: %v", err)
		}
		elapsed := time.Since(start)
		if maxConcurrent.Load() < 2 {
			t.Errorf("expected parallel execution (concurrency >= 2), got %d", maxConcurrent.Load())
		}
		// 虚拟时钟下两个 50ms 工具并行耗时恰为 50ms；退化为串行时并发度断言
		// 已能确定性捕获，此处仅作兜底。
		if elapsed > time.Second {
			t.Errorf("expected parallel speedup, took %v", elapsed)
		}
		// 结果按原始顺序回填（tool_a 在前）
		msgs := session.SnapshotMessages()
		var toolOrder []string
		for _, m := range msgs {
			if m.Role == RoleTool {
				toolOrder = append(toolOrder, m.ToolCallID)
			}
		}
		if len(toolOrder) != 2 || toolOrder[0] != "c1" || toolOrder[1] != "c2" {
			t.Errorf("results should be in original order, got %v", toolOrder)
		}
	})
}

// --- 用户中断/抢占 ---

func TestProcessWithToolsInterrupted(t *testing.T) {
	var streamCalls atomic.Int32
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				streamCalls.Add(1)
				ch := make(chan StreamEvent, 2)
				ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{ID: "c", Name: "t"}}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	p.reg.Register(Tool{Name: "t", Execute: func(ctx context.Context, args map[string]any) (string, error) {
		return "ok", nil
	}})

	session := p.sm.GetOrCreate("test:int", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "long task"})
	// 抢占：回合开始前即请求中断 → 第一轮检查点直接收尾
	session.BeginTurn()
	session.RequestInterrupt()
	defer session.EndTurn()

	evt := platform.NewSyntheticEvent("c2c", "long task")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, session)
	if err != nil {
		t.Fatalf("interrupted turn should end gracefully, got err: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if streamCalls.Load() != 0 {
		t.Errorf("interrupted before first round should skip LLM call, got %d", streamCalls.Load())
	}
}

func TestSessionInterruptLifecycle(t *testing.T) {
	s := &Session{}
	if !s.BeginTurn() {
		t.Fatal("first BeginTurn should succeed")
	}
	if s.BeginTurn() {
		t.Error("second BeginTurn should fail while active")
	}
	if s.Interrupted() {
		t.Error("should not be interrupted initially")
	}
	s.RequestInterrupt()
	if !s.Interrupted() {
		t.Error("should be interrupted after RequestInterrupt")
	}
	s.EndTurn()
	if s.TurnActive() {
		t.Error("EndTurn should clear active")
	}
	if s.Interrupted() {
		t.Error("after EndTurn should not report interrupted")
	}
	// 无活跃回合时 RequestInterrupt 不 panic
	s.RequestInterrupt()
}

// --- 计划硬化：顺序强制 + 重规划闭环 ---

func TestUpdatePlanStepOrderEnforced(t *testing.T) {
	tools := buildPlanTools(8)
	var create, update *Tool
	for i := range tools {
		switch tools[i].Name {
		case planCreateToolName:
			create = &tools[i]
		case planUpdateToolName:
			update = &tools[i]
		}
	}
	session := &Session{}
	ctx := WithPlanSession(context.Background(), session)
	create.Execute(ctx, map[string]any{"task": "t", "steps": []any{"第一步", "第二步", "第三步"}})

	// 跳过 step_1 直接完成 step_2 → 拒绝
	result, _ := update.Execute(ctx, map[string]any{"step_id": "step_2", "status": "done"})
	if !strings.Contains(result, "错误") || !strings.Contains(result, "前序步骤") {
		t.Errorf("out-of-order step should be rejected: %q", result)
	}
	// step_1 失败后 step_2 可推进（终态前序）
	update.Execute(ctx, map[string]any{"step_id": "step_1", "status": "failed"})
	result, _ = update.Execute(ctx, map[string]any{"step_id": "step_2", "status": "done"})
	if strings.Contains(result, "错误") {
		t.Errorf("step after failed predecessor should be allowed: %q", result)
	}
}

func TestProcessWithToolsReplanClosedLoop(t *testing.T) {
	var streamCalls atomic.Int32
	p := &Plugin{
		cfg:      &Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				streamCalls.Add(1)
				ch := make(chan StreamEvent, 2)
				ch <- StreamEvent{Type: StreamEventText, Content: "继续"}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	session := p.sm.GetOrCreate("test:replan", "user", "chat")
	session.setPlan(&Plan{
		Task:   "t",
		Active: true,
		Steps: []PlanStep{
			{ID: "step_1", Description: "第一步", Status: PlanFailed},
			{ID: "step_2", Description: "第二步", Status: PlanPending},
		},
	})
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "do it"})

	evt := platform.NewSyntheticEvent("c2c", "do it")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if _, err := p.processWithTools(ctx, session); err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	// 前沿失败步骤 → 自动追加重规划指令
	hasReplan := false
	for _, m := range session.SnapshotMessages() {
		if m.Role == RoleUser && strings.HasPrefix(m.Content, "计划步骤") {
			hasReplan = true
		}
	}
	if !hasReplan {
		t.Error("failed frontier step should trigger replan instruction")
	}
}

func TestLastUserIsReplan(t *testing.T) {
	s := &Session{}
	s.Messages = []Message{
		{Role: RoleUser, Content: "普通消息"},
		{Role: RoleAssistant, Content: "ok"},
		{Role: RoleUser, Content: "计划步骤 `step_1`（第一步）已标记失败。请重新评估"},
	}
	if !lastUserIsReplan(s) {
		t.Error("last user message is replan, should detect")
	}
	s.Messages = append(s.Messages, Message{Role: RoleUser, Content: "新问题"})
	if lastUserIsReplan(s) {
		t.Error("last user message not replan, should not detect")
	}
}

// --- 多模型分层 ---

func TestVerifyAnswerUsesVerifyModel(t *testing.T) {
	var gotModel string
	p := &Plugin{
		cfg: &Config{APITimeout: 5 * time.Second, VerifyModel: "cheap-model", Model: "main-model"},
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
				gotModel = req.Model
				return &ChatResponse{Content: `{"verdict":"pass","reason":""}`}, nil
			},
		},
	}
	if _, err := p.verifyAnswer(context.Background(), "q", "a"); err != nil {
		t.Fatalf("verifyAnswer failed: %v", err)
	}
	if gotModel != "cheap-model" {
		t.Errorf("verify should use verify_model, got %q", gotModel)
	}
}

func TestExtractUsesExtractModel(t *testing.T) {
	var gotModel string
	p := &Plugin{
		cfg:    &Config{ExtractModel: "cheap-extract", Model: "main-model"},
		memory: newTestMemoryStore(t, 50, time.Minute),
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
				gotModel = req.Model
				return &ChatResponse{Content: `[]`}, nil
			},
		},
		lifecycleCtx: context.Background(),
	}
	session := &Session{ID: "s", UserID: "u", ChatID: "g"}
	session.Messages = []Message{{Role: RoleUser, Content: "我喜欢喝咖啡"}}
	if err := p.extractAndStore(userScope("u"), "u", platform.ChatInfo{IsGroup: true}, session); err != nil {
		t.Fatalf("extractAndStore failed: %v", err)
	}
	if gotModel != "cheap-extract" {
		t.Errorf("extract should use extract_model, got %q", gotModel)
	}
}

// --- 全局预算编排 ---

func TestEstimateTextTokens(t *testing.T) {
	if estimateTextTokens("") != 0 {
		t.Error("empty should be 0")
	}
	if estimateTextTokens("你好世界") <= 0 {
		t.Error("CJK text should estimate > 0")
	}
	if estimateTextTokens("hello world this is a test") <= 0 {
		t.Error("ASCII text should estimate > 0")
	}
}

func TestBuildSystemPromptBudgeted(t *testing.T) {
	evt := platform.NewSyntheticEvent("c2c", "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	session := &Session{ID: "s", UserID: "u", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: "hi"}}

	// 极小预算：只保留核心（框架+自定义），各节全部丢弃
	tiny := Plugin{cfg: &Config{ContextWindow: 60, SystemPrompt: "自定义指令内容", ContextGroupMessages: 10}}
	prompt := tiny.buildSystemPrompt(ctx, session)
	if !strings.Contains(prompt, "自定义指令内容") {
		t.Error("core custom prompt should survive tiny budget")
	}
	if strings.Contains(prompt, "群聊最近消息") || strings.Contains(prompt, "长期记忆") || strings.Contains(prompt, "相关历史消息") {
		t.Errorf("tiny budget should drop all sections, got %q", prompt)
	}

	// 大预算：运行时上下文 + 群窗口纳入（提供 history 才能构建群窗口）
	l, db := newRAGTestLogger(t)
	insertMessage(t, db, "g1", "张三", "服务器方案选型讨论", time.Hour, "")
	big := Plugin{history: l, cfg: &Config{ContextWindow: 100000, SystemPrompt: "自定义指令内容", ContextGroupMessages: 10, IncludeRuntimeContext: true}}
	prompt2 := big.buildSystemPrompt(ctx, session)
	if !strings.Contains(prompt2, "群聊最近消息") || !strings.Contains(prompt2, "运行时上下文") {
		t.Errorf("large budget should include sections, got %q", prompt2)
	}
}

// --- 计划后台自动推进 ---

func TestPlanAutoContinueRoundTrip(t *testing.T) {
	var streamCalls atomic.Int32
	lifecycleCtx := t.Context()

	p := &Plugin{
		cfg:          &Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, PlanAutoContinue: true, PlanAutoInterval: 50 * time.Millisecond, PlanAutoRounds: 5},
		sm:           NewSessionManager(100, 20, time.Hour, nil),
		reg:          NewToolRegistry(),
		skillReg:     NewSkillRegistry(),
		lifecycleCtx: lifecycleCtx,
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				ch := make(chan StreamEvent, 3)
				switch streamCalls.Add(1) {
				case 1:
					// 后台第一轮：完成 step_1
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
						ID: "c1", Name: planUpdateToolName,
						Arguments: map[string]any{"step_id": "step_1", "status": "done"},
					}}
				case 2:
					// 后台第二轮：完成 step_2 → 计划完成
					ch <- StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{
						ID: "c2", Name: planUpdateToolName,
						Arguments: map[string]any{"step_id": "step_2", "status": "done"},
					}}
				default:
					ch <- StreamEvent{Type: StreamEventText, Content: "全部完成"}
				}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	for _, tt := range buildPlanTools(8) {
		p.reg.Register(tt)
	}

	session := p.sm.GetOrCreate("test:runner", "user", "chat")
	session.setPlan(&Plan{
		Task: "t", Active: true,
		Steps: []PlanStep{
			{ID: "step_1", Description: "第一步", Status: PlanPending},
			{ID: "step_2", Description: "第二步", Status: PlanPending},
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "开始",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user", DisplayName: "U"}))
	ctx := eventctx.NewContextFromEvent(evt, &platform.NoopSender{})

	p.maybeContinuePlan(ctx, session)

	// 等待后台推进完成（最多 2s）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		plan := session.planSnapshot()
		if plan == nil || !plan.Active {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	plan := session.planSnapshot()
	if plan == nil || plan.Active {
		t.Fatalf("plan should be completed by auto-continue, got %+v", plan)
	}
	if streamCalls.Load() < 2 {
		t.Errorf("expected >= 2 auto-continue rounds, got %d", streamCalls.Load())
	}
}

func TestPlanAutoContinueStopsWithoutProgress(t *testing.T) {
	lifecycleCtx := t.Context()

	p := &Plugin{
		cfg:          &Config{MaxDepth: 3, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, PlanAutoContinue: true, PlanAutoInterval: 30 * time.Millisecond, PlanAutoRounds: 5},
		sm:           NewSessionManager(100, 20, time.Hour, nil),
		reg:          NewToolRegistry(),
		skillReg:     NewSkillRegistry(),
		lifecycleCtx: lifecycleCtx,
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				// 每轮只输出文本、不推进计划 → 无进度停止
				ch := make(chan StreamEvent, 2)
				ch <- StreamEvent{Type: StreamEventText, Content: "还在处理"}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	session := p.sm.GetOrCreate("test:runner2", "user", "chat")
	session.setPlan(&Plan{
		Task: "t", Active: true,
		Steps: []PlanStep{{ID: "step_1", Description: "第一步", Status: PlanPending}},
	})

	evt := platform.NewSyntheticEvent("c2c", "开始",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user", DisplayName: "U"}))
	ctx := eventctx.NewContextFromEvent(evt, &platform.NoopSender{})

	p.maybeContinuePlan(ctx, session)
	time.Sleep(300 * time.Millisecond)

	if !session.PlanAutoStopped() {
		t.Error("no-progress plan should stop auto-continue")
	}
}

// --- RAG 零命中语义兜底 ---

func TestRAGSemanticFallback(t *testing.T) {
	l, db := newRAGTestLogger(t)
	insertMessage(t, db, "g1", "张三", "最后定了阿里云方案", time.Hour, "e1")
	insertMessage(t, db, "g1", "李四", "食堂今日菜单", 2*time.Hour, "e2")

	query := "上次说的那个部署的事"
	emb := &mapEmbedder{vecs: map[string][]float32{
		query:       {1, 0},
		"最后定了阿里云方案": {1, 0}, // 无语义无关键词重叠 → 语义兜底命中
		"食堂今日菜单":    {0, 1},
	}}
	p := newRAGPlugin(t, l, Config{ContextRAGMessages: 3, ContextRAGInjectMax: 1})
	p.emb = newTextVectorCache(emb)

	evt := platform.NewSyntheticEvent("c2c", query,
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: query}}

	text := p.buildRAGContext(ctx, session)
	if !strings.Contains(text, "阿里云方案") {
		t.Errorf("semantic fallback should retrieve no-keyword-overlap history, got %q", text)
	}
	if strings.Contains(text, "食堂") {
		t.Errorf("irrelevant message should not be injected, got %q", text)
	}
}

// --- 记忆合并长度约束 ---

func TestMergeSimilarLengthGuard(t *testing.T) {
	if !mergeSimilar("用户喜欢喝咖啡", "用户爱喝咖啡") {
		t.Error("similar short facts should merge")
	}
	if mergeSimilar("用户喜欢喝咖啡", "用户喜欢喝咖啡且喜欢雨天散步看书") {
		t.Error("long fact should not merge with short share-snippet fact")
	}
	if mergeSimilar("用户喜欢玩原神", "用户喜欢玩星穹铁道") {
		t.Error("different game facts should not merge")
	}
}
