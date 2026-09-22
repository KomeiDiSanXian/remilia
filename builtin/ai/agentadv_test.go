package ai

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/promptctx"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// --- 并行工具执行 ---

func TestProcessWithToolsParallelTools(t *testing.T) {
	var maxConcurrent atomic.Int32
	var curConcurrent atomic.Int32
	var streamCalls atomic.Int32
	// 就绪屏障：确保两个工具都进入执行体后再放行，避免依赖
	// synctest + -race 下的 goroutine 调度时序（若第二个工具 goroutine
	// 在第一个 sleep 完成前未被调度，并发度会被观测为 1，CI 偶发）。
	ready := make(chan struct{}, 2)
	startCh := make(chan struct{})
	p := &Plugin{
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, ToolParallel: 4},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				ch := make(chan protocol.StreamEvent, 4)
				if streamCalls.Add(1) == 1 {
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "c1", Name: "tool_a"}}
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "c2", Name: "tool_b"}}
				} else {
					ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "done"}
				}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	for _, name := range []string{"tool_a", "tool_b"} {
		p.reg.Register(toolkit.Tool{
			Name: name,
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				// 双方就绪后同时进入"并发窗口"，再各自 sleep 提供阻塞点：
				// 保证 Add(1) 必然在任一工具完成前被双方执行。
				ready <- struct{}{}
				<-startCh
				v := curConcurrent.Add(1)
				if v > maxConcurrent.Load() {
					maxConcurrent.Store(v)
				}
				defer curConcurrent.Add(-1)
				time.Sleep(50 * time.Millisecond)
				return "ok", nil
			},
		})
	}

	sess := p.sm.GetOrCreate("test:par", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "do both"})

	evt := platform.NewSyntheticEvent("c2c", "do both")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		done := make(chan struct{})
		go func() {
			defer close(done)
			if _, err := p.processWithTools(ctx, sess); err != nil {
				t.Errorf("processWithTools failed: %v", err)
			}
		}()
		// 等待两个工具都进入就绪点（防御：若提前结束说明流程异常）。
		for range 2 {
			select {
			case <-ready:
			case <-done:
				t.Fatalf("processWithTools finished before both tools became ready")
			}
		}
		close(startCh)
		<-done
		elapsed := time.Since(started)
		if maxConcurrent.Load() < 2 {
			t.Errorf("expected parallel execution (concurrency >= 2), got %d", maxConcurrent.Load())
		}
		// 虚拟时钟下两个 50ms 工具并行耗时恰为 50ms；退化为串行时并发度断言
		// 已能确定性捕获，此处仅作兜底。
		if elapsed > time.Second {
			t.Errorf("expected parallel speedup, took %v", elapsed)
		}
		// 结果按原始顺序回填（tool_a 在前）
		msgs := sess.SnapshotMessages()
		var toolOrder []string
		for _, m := range msgs {
			if m.Role == protocol.RoleTool {
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
		cfg:      &config.Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalls.Add(1)
				ch := make(chan protocol.StreamEvent, 2)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{ID: "c", Name: "t"}}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	p.reg.Register(toolkit.Tool{Name: "t", Execute: func(ctx context.Context, args map[string]any) (string, error) {
		return "ok", nil
	}})

	sess := p.sm.GetOrCreate("test:int", "user", "chat")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "long task"})
	// 抢占：回合开始前即请求中断 → 第一轮检查点直接收尾
	sess.BeginTurn()
	sess.RequestInterrupt()
	defer sess.EndTurn()

	evt := platform.NewSyntheticEvent("c2c", "long task")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, sess)
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
	s := &session.Session{}
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
	tools := catalog.BuildPlanTools(8)
	var create, update *toolkit.Tool
	for i := range tools {
		switch tools[i].Name {
		case catalog.PlanCreateToolName:
			create = &tools[i]
		case catalog.PlanUpdateToolName:
			update = &tools[i]
		}
	}
	sess := &session.Session{}
	ctx := runtime.WithPlanSession(context.Background(), sess)
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
		cfg:      &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       session.NewSessionManager(100, 20, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalls.Add(1)
				ch := make(chan protocol.StreamEvent, 2)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "继续"}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	sess := p.sm.GetOrCreate("test:replan", "user", "chat")
	sess.SetPlan(&session.Plan{
		Task:   "t",
		Active: true,
		Steps: []session.PlanStep{
			{ID: "step_1", Description: "第一步", Status: session.PlanFailed},
			{ID: "step_2", Description: "第二步", Status: session.PlanPending},
		},
	})
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "do it"})

	evt := platform.NewSyntheticEvent("c2c", "do it")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	if _, err := p.processWithTools(ctx, sess); err != nil {
		t.Fatalf("processWithTools failed: %v", err)
	}
	// 前沿失败步骤 → 自动追加重规划指令
	hasReplan := false
	for _, m := range sess.SnapshotMessages() {
		if m.Role == protocol.RoleUser && strings.HasPrefix(m.Content, "计划步骤") {
			hasReplan = true
		}
	}
	if !hasReplan {
		t.Error("failed frontier step should trigger replan instruction")
	}
}

func TestLastUserIsReplan(t *testing.T) {
	s := &session.Session{}
	s.Messages = []protocol.Message{
		{Role: protocol.RoleUser, Content: "普通消息"},
		{Role: protocol.RoleAssistant, Content: "ok"},
		{Role: protocol.RoleUser, Content: "计划步骤 `step_1`（第一步）已标记失败。请重新评估"},
	}
	if !runtime.LastUserIsReplan(s) {
		t.Error("last user message is replan, should detect")
	}
	s.Messages = append(s.Messages, protocol.Message{Role: protocol.RoleUser, Content: "新问题"})
	if runtime.LastUserIsReplan(s) {
		t.Error("last user message not replan, should not detect")
	}
}

// --- 多模型分层 ---

func TestVerifyAnswerUsesVerifyModel(t *testing.T) {
	var gotModel string
	p := &Plugin{
		cfg: &config.Config{APITimeout: 5 * time.Second, VerifyModel: "cheap-model", Model: "main-model"},
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				gotModel = req.Model
				return &protocol.ChatResponse{Content: `{"verdict":"pass","reason":""}`}, nil
			},
		},
	}
	if _, err := p.verifier().Verify(context.Background(), "q", "a"); err != nil {
		t.Fatalf("verifyAnswer failed: %v", err)
	}
	if gotModel != "cheap-model" {
		t.Errorf("verify should use verify_model, got %q", gotModel)
	}
}

func TestExtractUsesExtractModel(t *testing.T) {
	var gotModel string
	p := &Plugin{
		cfg:    &config.Config{ExtractModel: "cheap-extract", Model: "main-model"},
		memory: newTestMemoryStore(t, 50, time.Minute),
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				gotModel = req.Model
				return &protocol.ChatResponse{Content: `[]`}, nil
			},
		},
		lifecycleCtx: context.Background(),
	}
	sess := &session.Session{ID: "s", UserID: "u", ChatID: "g"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "我喜欢喝咖啡"}}
	if err := p.memoryExtractor().Extract(userScope("u"), "u", platform.ChatInfo{IsGroup: true}, sess); err != nil {
		t.Fatalf("extractAndStore failed: %v", err)
	}
	if gotModel != "cheap-extract" {
		t.Errorf("extract should use extract_model, got %q", gotModel)
	}
}

// --- 全局预算编排 ---

func TestEstimateTextTokens(t *testing.T) {
	if promptctx.EstimateTokens("") != 0 {
		t.Error("empty should be 0")
	}
	if promptctx.EstimateTokens("你好世界") <= 0 {
		t.Error("CJK text should estimate > 0")
	}
	if promptctx.EstimateTokens("hello world this is a test") <= 0 {
		t.Error("ASCII text should estimate > 0")
	}
}

func TestBuildDynamicContextBudgeted(t *testing.T) {
	evt := platform.NewSyntheticEvent("c2c", "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	sess := &session.Session{ID: "s", UserID: "u", ChatID: "g1"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}}

	// 稳定系统提示词只含框架 + 自定义指令，不受预算影响（它是唯一的 System 消息）
	tiny := Plugin{cfg: &config.Config{ContextWindow: 60, SystemPrompt: "自定义指令内容", ContextGroupMessages: 10}}
	static := tiny.buildStaticSystemPrompt(ctx)
	if !strings.Contains(static, "自定义指令内容") || !strings.Contains(static, DefaultFrameworkPrompt) {
		t.Errorf("static prompt should carry framework and custom instructions, got %q", static)
	}

	// 极小预算：动态各节全部丢弃
	if dyn := tiny.buildDynamicContext(ctx, sess); dyn != "" {
		t.Errorf("tiny budget should drop all dynamic sections, got %q", dyn)
	}

	// 大预算：运行时上下文 + 群窗口纳入（提供 history 才能构建群窗口）
	l, db := newRAGTestLogger(t)
	insertMessage(t, db, "g1", "张三", "服务器方案选型讨论", time.Hour, "")
	big := Plugin{history: l, cfg: &config.Config{ContextWindow: 100000, SystemPrompt: "自定义指令内容", ContextGroupMessages: 10, IncludeRuntimeContext: true}}
	dyn := big.buildDynamicContext(ctx, sess)
	if !strings.Contains(dyn, "群聊最近消息") || !strings.Contains(dyn, "运行时上下文") {
		t.Errorf("large budget should include sections, got %q", dyn)
	}
}

// --- 计划后台自动推进 ---

func TestPlanAutoContinueRoundTrip(t *testing.T) {
	var streamCalls atomic.Int32
	lifecycleCtx := t.Context()

	p := &Plugin{
		cfg:          &config.Config{MaxDepth: 5, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, PlanAutoContinue: true, PlanAutoInterval: 50 * time.Millisecond, PlanAutoRounds: 5},
		sm:           session.NewSessionManager(100, 20, time.Hour, nil),
		reg:          toolkit.NewToolRegistry(),
		skillReg:     toolkit.NewSkillRegistry(),
		lifecycleCtx: lifecycleCtx,
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				ch := make(chan protocol.StreamEvent, 3)
				switch streamCalls.Add(1) {
				case 1:
					// 后台第一轮：完成 step_1
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{
						ID: "c1", Name: catalog.PlanUpdateToolName,
						Arguments: map[string]any{"step_id": "step_1", "status": "done"},
					}}
				case 2:
					// 后台第二轮：完成 step_2 → 计划完成
					ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall, ToolCall: &protocol.ToolCall{
						ID: "c2", Name: catalog.PlanUpdateToolName,
						Arguments: map[string]any{"step_id": "step_2", "status": "done"},
					}}
				default:
					ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "全部完成"}
				}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	for _, tt := range catalog.BuildPlanTools(8) {
		p.reg.Register(tt)
	}

	sess := p.sm.GetOrCreate("test:runner", "user", "chat")
	sess.SetPlan(&session.Plan{
		Task: "t", Active: true,
		Steps: []session.PlanStep{
			{ID: "step_1", Description: "第一步", Status: session.PlanPending},
			{ID: "step_2", Description: "第二步", Status: session.PlanPending},
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "开始",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user", DisplayName: "U"}))
	ctx := eventctx.NewContextFromEvent(evt, &platform.NoopSender{})

	p.maybeContinuePlan(ctx, sess)

	// 等待后台推进完成（最多 2s）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		plan := sess.PlanSnapshot()
		if plan == nil || !plan.Active {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	plan := sess.PlanSnapshot()
	if plan == nil || plan.Active {
		t.Fatalf("plan should be Completed by auto-continue, got %+v", plan)
	}
	if streamCalls.Load() < 2 {
		t.Errorf("expected >= 2 auto-continue rounds, got %d", streamCalls.Load())
	}
}

func TestPlanAutoContinueStopsWithoutProgress(t *testing.T) {
	lifecycleCtx := t.Context()

	p := &Plugin{
		cfg:          &config.Config{MaxDepth: 3, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, PlanAutoContinue: true, PlanAutoInterval: 30 * time.Millisecond, PlanAutoRounds: 5},
		sm:           session.NewSessionManager(100, 20, time.Hour, nil),
		reg:          toolkit.NewToolRegistry(),
		skillReg:     toolkit.NewSkillRegistry(),
		lifecycleCtx: lifecycleCtx,
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				// 每轮只输出文本、不推进计划 → 无进度停止
				ch := make(chan protocol.StreamEvent, 2)
				ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "还在处理"}
				ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}
	sess := p.sm.GetOrCreate("test:runner2", "user", "chat")
	sess.SetPlan(&session.Plan{
		Task: "t", Active: true,
		Steps: []session.PlanStep{{ID: "step_1", Description: "第一步", Status: session.PlanPending}},
	})

	evt := platform.NewSyntheticEvent("c2c", "开始",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user", DisplayName: "U"}))
	ctx := eventctx.NewContextFromEvent(evt, &platform.NoopSender{})

	p.maybeContinuePlan(ctx, sess)
	time.Sleep(300 * time.Millisecond)

	if !sess.PlanAutoStopped() {
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
	p := newRAGPlugin(t, l, config.Config{ContextRAGMessages: 3, ContextRAGInjectMax: 1})
	p.emb = retrieval.NewTextVectorCache(emb)

	evt := platform.NewSyntheticEvent("c2c", query,
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	sess := &session.Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: query}}

	text := p.buildRAGContextN(ctx, sess, p.cfg.ContextRAGMessages)
	if !strings.Contains(text, "阿里云方案") {
		t.Errorf("semantic fallback should retrieve no-keyword-overlap history, got %q", text)
	}
	if strings.Contains(text, "食堂") {
		t.Errorf("irrelevant message should not be injected, got %q", text)
	}
}

// --- 记忆合并长度约束 ---

func TestMergeSimilarLengthGuard(t *testing.T) {
	store := &memoryStore{}
	if !store.mergeSimilar("用户喜欢喝咖啡", "用户爱喝咖啡") {
		t.Error("similar short facts should merge")
	}
	if store.mergeSimilar("用户喜欢喝咖啡", "用户喜欢喝咖啡且喜欢雨天散步看书") {
		t.Error("long fact should not merge with short share-snippet fact")
	}
	if store.mergeSimilar("用户喜欢玩原神", "用户喜欢玩星穹铁道") {
		t.Error("different game facts should not merge")
	}
}
