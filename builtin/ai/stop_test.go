// Package ai stop_test.go — /ai stop（停止生成）测试。
package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// newStopContext 构造 QQ 单聊"停止生成"命令的测试上下文（捕获 sender + 异步调度）。
func newStopContext() (*eventctx.Context, *approvalCtxSender) {
	evt := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "/ai stop",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "u1"}))
	sender := &approvalCtxSender{}
	ctx := eventctx.NewContextFromEvent(evt, sender)
	ctx.SetDispatcher(runTaskDispatcher{})
	return ctx, sender
}

func TestStopCommandIdle(t *testing.T) {
	p := &Plugin{sm: NewSessionManager(100, 20, time.Hour, nil)}
	ctx, sender := newStopContext()
	if err := p.handleStopCommand(ctx); err != nil {
		t.Fatalf("handleStopCommand(idle): %v", err)
	}
	waitReplies(t, sender, 1)
	if !strings.Contains(sender.replies[0].Text, "没有正在进行的生成") {
		t.Errorf("空闲提示不符：%q", sender.replies[0].Text)
	}
}

func TestStopCommandInterruptsActiveTurn(t *testing.T) {
	const sid = "qq:u1:u1"
	p := &Plugin{sm: NewSessionManager(100, 20, time.Hour, nil)}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	if !session.BeginTurn() {
		t.Fatal("BeginTurn 应成功（模拟生成中）")
	}
	defer session.EndTurn()

	ctx, sender := newStopContext()
	if err := p.handleStopCommand(ctx); err != nil {
		t.Fatalf("handleStopCommand(active): %v", err)
	}
	if !session.Interrupted() {
		t.Error("stop 命令应设置中断信号")
	}
	if !session.TurnActive() {
		t.Error("中断后回合仍在收尾中，TurnActive 应保持 true 直到 EndTurn")
	}
	waitReplies(t, sender, 1)
	if !strings.Contains(sender.replies[0].Text, "已停止") {
		t.Errorf("停止提示不符：%q", sender.replies[0].Text)
	}
}

func TestStopCommandCancelsActivePlan(t *testing.T) {
	// /ai stop 中断进行中回合的同时，应一并取消会话中尚未结束的任务计划
	// （否则剩余步骤会在后续回合/后台自动推进继续执行）。
	const sid = "qq:u1:u1"
	p := &Plugin{sm: NewSessionManager(100, 20, time.Hour, nil)}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	session.setPlan(&Plan{
		Task:   "查天气",
		Active: true,
		Steps:  []PlanStep{{ID: "step_1", Description: "查询城市天气", Status: PlanPending}},
	})
	if !session.BeginTurn() {
		t.Fatal("BeginTurn 应成功（模拟计划在长回合中执行）")
	}
	defer session.EndTurn()

	ctx, sender := newStopContext()
	if err := p.handleStopCommand(ctx); err != nil {
		t.Fatalf("handleStopCommand(active+plan): %v", err)
	}
	if !session.Interrupted() {
		t.Error("stop 应设置中断信号")
	}
	if snap := session.planSnapshot(); snap != nil && snap.Active {
		t.Error("stop 应一并取消进行中的任务计划")
	}
	waitReplies(t, sender, 1)
	if !strings.Contains(sender.replies[0].Text, "已停止") ||
		!strings.Contains(sender.replies[0].Text, "取消") {
		t.Errorf("停止+取消计划提示不符：%q", sender.replies[0].Text)
	}
}

func TestStopCommandCancelsPlanWhenIdle(t *testing.T) {
	// 回合空闲（自动推进轮间隔窗口）但计划仍 Active：/ai stop 应取消计划，
	// 阻止已调度的后台推进轮继续按旧计划执行。
	const sid = "qq:u1:u1"
	p := &Plugin{sm: NewSessionManager(100, 20, time.Hour, nil)}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	session.setPlan(&Plan{
		Task:   "查天气",
		Active: true,
		Steps:  []PlanStep{{ID: "step_1", Description: "查询城市天气", Status: PlanPending}},
	})

	ctx, sender := newStopContext()
	if err := p.handleStopCommand(ctx); err != nil {
		t.Fatalf("handleStopCommand(idle+plan): %v", err)
	}
	if snap := session.planSnapshot(); snap != nil && snap.Active {
		t.Error("空闲但有进行中计划时 stop 应取消计划")
	}
	waitReplies(t, sender, 1)
	if !strings.Contains(sender.replies[0].Text, "已取消") {
		t.Errorf("取消计划提示不符：%q", sender.replies[0].Text)
	}
}

// TestProcessWithToolsStopMidStreamSilentClose 覆盖 OpenAI 类 provider：
// 流在中断后被静默关闭（无 [DONE]、无错误事件），processWithTools 应把
// 已到手部分作为最终回复返回，而不是报错。
func TestProcessWithToolsStopMidStreamSilentClose(t *testing.T) {
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				ch := make(chan StreamEvent, 4)
				go func() {
					defer close(ch)
					select {
					case ch <- StreamEvent{Type: StreamEventText, Content: "已经生成的部分"}:
					case <-ctx.Done():
						return
					}
					<-ctx.Done() // 模拟流在中断时被连接取消静默终止
				}()
				return ch, nil
			},
		},
	}

	const sid = "test:stop1"
	session := p.sm.GetOrCreate(sid, "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	if !session.BeginTurn() {
		t.Fatal("BeginTurn 应成功")
	}
	defer session.EndTurn()

	evt := platform.NewSyntheticEvent("c2c", "hi")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	done := make(chan struct{})
	var result *ChatResult
	var resultErr error
	go func() {
		result, resultErr = p.processWithTools(ctx, session)
		close(done)
	}()

	// 等待流进入阻塞（已产出首片内容），再触发停止。
	time.Sleep(100 * time.Millisecond)
	session.RequestInterrupt()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("停止后 processWithTools 未及时返回")
	}
	if resultErr != nil {
		t.Fatalf("主动停止不应报错，got: %v", resultErr)
	}
	if result == nil || result.Text != "已经生成的部分" {
		t.Errorf("停止后应保留已生成部分，got %+v", result)
	}
	msgs := session.SnapshotMessages()
	if len(msgs) != 2 || msgs[1].Role != RoleAssistant || msgs[1].Content != "已经生成的部分" {
		t.Errorf("部分内容应记入会话历史，got %+v", msgs)
	}
}

// TestProcessWithToolsStopOnStreamError 覆盖 Anthropic 类 provider：
// 流被取消后以错误事件收尾，processWithTools 应把该错误视为主动停止
// （不向用户报错），保留已到手部分。
func TestProcessWithToolsStopOnStreamError(t *testing.T) {
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				ch := make(chan StreamEvent, 4)
				go func() {
					defer close(ch)
					ch <- StreamEvent{Type: StreamEventText, Content: "半截回答"}
					<-ctx.Done()
					// 模拟 Anthropic 在连接被取消后以错误事件收尾。
					ch <- StreamEvent{Type: StreamEventError, Err: errors.New("context canceled")}
				}()
				return ch, nil
			},
		},
	}

	const sid = "test:stop2"
	session := p.sm.GetOrCreate(sid, "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	if !session.BeginTurn() {
		t.Fatal("BeginTurn 应成功")
	}
	defer session.EndTurn()

	evt := platform.NewSyntheticEvent("c2c", "hi")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	done := make(chan struct{})
	var result *ChatResult
	var resultErr error
	go func() {
		result, resultErr = p.processWithTools(ctx, session)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	session.RequestInterrupt()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("停止后 processWithTools 未及时返回")
	}
	if resultErr != nil {
		t.Fatalf("主动停止不应把流错误报给用户，got: %v", resultErr)
	}
	if result == nil || result.Text != "半截回答" {
		t.Errorf("停止后应保留已生成部分，got %+v", result)
	}
}
