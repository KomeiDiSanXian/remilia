// Package ai qqaction_test.go — QQ 操作按钮（重新生成/清空会话）接入测试。
// 覆盖指令按钮（type=2，Command+Enter）装配、文本命令路径（指令按钮自动
// 发送 /ai retry、/ai reset）的防重复触发，以及 type=1 回调/原生 type=14
// 的兜底处理。
package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	qq "github.com/KomeiDiSanXian/remilia/platform/qq"
)

func TestShouldAttachQQButtons(t *testing.T) {
	msg := platform.OutboundMessage{Markdown: "回答"}
	chat := platform.ChatInfo{ID: "c2c_1"} // 单聊
	if !shouldAttachQQButtons(true, "qq", chat, msg) {
		t.Error("QQ 单聊 Markdown 回复应附加操作按钮")
	}
	groupChat := platform.ChatInfo{ID: "g1", IsGroup: true} // QQ 群聊（无 ParentID）
	if !shouldAttachQQButtons(true, "qq", groupChat, msg) {
		t.Error("QQ 群聊 Markdown 回复应附加操作按钮")
	}

	cases := []struct {
		name    string
		enabled bool
		plat    string
		chat    platform.ChatInfo
		msg     platform.OutboundMessage
	}{
		{"配置关闭", false, "qq", chat, msg},
		{"非 QQ 平台", true, "telegram", chat, msg},
		{"频道", true, "qq", platform.ChatInfo{ID: "c1", ParentID: "guild1", IsGroup: true}, msg},
		{"空正文", true, "qq", chat, platform.OutboundMessage{}},
		{"纯文本非 Markdown", true, "qq", chat, platform.TextMessage("x")},
		{"带附件", true, "qq", chat, platform.OutboundMessage{Markdown: "x", Attachments: []platform.Attachment{{Kind: platform.AttachmentKindImage}}}},
		{"带按钮", true, "qq", chat, platform.OutboundMessage{Markdown: "x", Buttons: []platform.Button{{ID: "b", Label: "B"}}}},
		{"带段", true, "qq", chat, platform.OutboundMessage{Markdown: "x", Segments: []platform.Segment{{Type: platform.SegmentText, Text: "x"}}}},
	}
	for _, tc := range cases {
		if shouldAttachQQButtons(tc.enabled, tc.plat, tc.chat, tc.msg) {
			t.Errorf("%s: 不应附加 QQ 操作按钮", tc.name)
		}
	}
}

func TestMaybeAttachQQButtons(t *testing.T) {
	// QQ 单聊 Markdown → 附加操作按钮。
	evt := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "hi",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "u1"}))
	ctx := eventctx.NewContextFromEvent(evt, fixedIDSender{})

	// 双开（默认）：同一行两个按钮——重新生成（primary）+ 清空会话（默认样式）。
	p := &Plugin{cfg: &Config{QQRegenButton: true, QQClearButton: true}}
	got := p.maybeAttachQQButtons(ctx, platform.OutboundMessage{Markdown: "回答"})
	if len(got.Buttons) != 2 {
		t.Fatalf("双开时应附加 2 个按钮，got %d", len(got.Buttons))
	}
	if got.Buttons[0].ID != regenButtonData || got.Buttons[0].Label != "重新生成" ||
		got.Buttons[0].Style != platform.ButtonStylePrimary || got.Buttons[0].Command != regenButtonCommand {
		t.Errorf("重新生成按钮配置不符：%+v", got.Buttons[0])
	}
	if got.Buttons[1].ID != clearButtonData || got.Buttons[1].Label != "清空会话" ||
		got.Buttons[1].Command != clearButtonCommand {
		t.Errorf("清空会话按钮配置不符：%+v", got.Buttons[1])
	}
	if !qqButtonEnterEnabled(got.Buttons[0]) {
		t.Error("单聊重新生成应携带 Enter=true（手机端点击后自动发送）")
	}
	if qqButtonEnterEnabled(got.Buttons[1]) {
		t.Error("清空会话不应携带 Enter（破坏性动作，不自动发送，避免误触）")
	}
	if got.Buttons[0].Row == 0 || got.Buttons[0].Row != got.Buttons[1].Row {
		t.Errorf("两个按钮应显式排在同一行（Row>0 且相等）：%+v", got.Buttons)
	}
	if got.Markdown != "回答" {
		t.Errorf("正文不应被改动，got %q", got.Markdown)
	}

	// 仅开重新生成。
	pRegen := &Plugin{cfg: &Config{QQRegenButton: true}}
	gotRegen := pRegen.maybeAttachQQButtons(ctx, platform.OutboundMessage{Markdown: "回答"})
	if len(gotRegen.Buttons) != 1 || gotRegen.Buttons[0].ID != regenButtonData {
		t.Errorf("仅开 qq_regen_button 时应只附加重新生成：%+v", gotRegen.Buttons)
	}

	// 仅开清空会话。
	pClear := &Plugin{cfg: &Config{QQClearButton: true}}
	gotClear := pClear.maybeAttachQQButtons(ctx, platform.OutboundMessage{Markdown: "回答"})
	if len(gotClear.Buttons) != 1 || gotClear.Buttons[0].ID != clearButtonData {
		t.Errorf("仅开 qq_clear_button 时应只附加清空会话：%+v", gotClear.Buttons)
	}

	// 群聊 → 附加，但不携带 Enter（群聊不支持点击自动发送，仅填入输入框）。
	evtG := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctxG := eventctx.NewContextFromEvent(evtG, fixedIDSender{})
	gotG := p.maybeAttachQQButtons(ctxG, platform.OutboundMessage{Markdown: "回答"})
	if len(gotG.Buttons) != 2 {
		t.Fatalf("QQ 群聊 Markdown 回复应附加 2 个按钮，got %d", len(gotG.Buttons))
	}
	if qqButtonEnterEnabled(gotG.Buttons[0]) || qqButtonEnterEnabled(gotG.Buttons[1]) {
		t.Error("群聊按钮不应携带 Enter（Enter 仅 QQ 单聊可用）")
	}
	if gotG.Buttons[0].Row == 0 || gotG.Buttons[0].Row != gotG.Buttons[1].Row {
		t.Errorf("群聊两个按钮应排在同一行（Row>0 且相等）：%+v", gotG.Buttons)
	}

	// 纯文本回复（非 Markdown）→ 不附加（QQ keyboard 只能挂 markdown 消息）。
	gotText := p.maybeAttachQQButtons(ctx, platform.TextMessage("回答"))
	if len(gotText.Buttons) != 0 {
		t.Error("QQ 单聊纯文本回复不应附加操作按钮")
	}

	// 配置全部关闭 → 不附加。
	pOff := &Plugin{cfg: &Config{}}
	gotOff := pOff.maybeAttachQQButtons(ctx, platform.OutboundMessage{Markdown: "回答"})
	if len(gotOff.Buttons) != 0 {
		t.Error("qq_regen_button/qq_clear_button 均关闭时不应附加操作按钮")
	}
}

func TestMaybeAttachQQPlanButtons(t *testing.T) {
	qqC2C := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "x",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "u1"}))
	ctx := eventctx.NewContextFromEvent(qqC2C, fixedIDSender{})

	// 默认开 + QQ 单聊 Markdown + 回合进行中 → 查看计划 + 停止生成。
	p := &Plugin{cfg: &Config{QQPlanButton: true}}
	got := p.maybeAttachQQPlanButtons(ctx, platform.OutboundMessage{Markdown: "计划已创建"}, true)
	if len(got.Buttons) != 2 {
		t.Fatalf("进行中时应附加 查看计划+停止生成 两个按钮，got %d", len(got.Buttons))
	}
	if got.Buttons[0].ID != planViewButtonData || got.Buttons[0].Label != "查看计划" ||
		got.Buttons[0].Command != planViewButtonCommand {
		t.Errorf("查看计划按钮配置不符：%+v", got.Buttons[0])
	}
	if got.Buttons[1].ID != planStopButtonData || got.Buttons[1].Label != "停止生成" ||
		got.Buttons[1].Command != planStopButtonCommand {
		t.Errorf("停止生成按钮配置不符：%+v", got.Buttons[1])
	}
	if !qqButtonEnterEnabled(got.Buttons[0]) {
		t.Error("单聊查看计划应携带 Enter=true（手机端点击后自动发送）")
	}
	if qqButtonEnterEnabled(got.Buttons[1]) {
		t.Error("停止生成不应携带 Enter（破坏性动作，不自动发送，避免误触）")
	}

	// 回合空闲 → 仅查看计划（无停止生成）。
	gotIdle := p.maybeAttachQQPlanButtons(ctx, platform.OutboundMessage{Markdown: "计划已创建"}, false)
	if len(gotIdle.Buttons) != 1 || gotIdle.Buttons[0].Command != planViewButtonCommand {
		t.Errorf("空闲时应只附加查看计划：%+v", gotIdle.Buttons)
	}
	if !qqButtonEnterEnabled(gotIdle.Buttons[0]) {
		t.Error("单聊空闲时的查看计划按钮仍应携带 Enter=true")
	}

	// QQ 群聊 Markdown + 回合进行中 → 查看计划 + 停止生成，均不携带 Enter。
	qqGroup := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "x",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctxG := eventctx.NewContextFromEvent(qqGroup, fixedIDSender{})
	gotG := p.maybeAttachQQPlanButtons(ctxG, platform.OutboundMessage{Markdown: "计划已创建"}, true)
	if len(gotG.Buttons) != 2 {
		t.Fatalf("群聊进行中时应附加 查看计划+停止生成 两个按钮，got %d", len(gotG.Buttons))
	}
	if qqButtonEnterEnabled(gotG.Buttons[0]) || qqButtonEnterEnabled(gotG.Buttons[1]) {
		t.Error("群聊计划按钮不应携带 Enter（Enter 仅 QQ 单聊可用）")
	}

	cases := []struct {
		name string
		cfg  *Config
		plat string
		chat platform.ChatInfo
		msg  platform.OutboundMessage
	}{
		{"配置关闭", &Config{}, "qq", platform.ChatInfo{ID: "u1"}, platform.OutboundMessage{Markdown: "x"}},
		{"非 QQ", &Config{QQPlanButton: true}, "telegram", platform.ChatInfo{ID: "u1"}, platform.OutboundMessage{Markdown: "x"}},
		{"频道", &Config{QQPlanButton: true}, "qq", platform.ChatInfo{ID: "c1", ParentID: "guild1", IsGroup: true}, platform.OutboundMessage{Markdown: "x"}},
		{"纯文本非 Markdown", &Config{QQPlanButton: true}, "qq", platform.ChatInfo{ID: "u1"}, platform.TextMessage("x")},
	}
	for _, tc := range cases {
		evt := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "x",
			platform.WithSyntheticPlatform(tc.plat),
			platform.WithSyntheticChat(tc.chat))
		c := eventctx.NewContextFromEvent(evt, fixedIDSender{})
		if got := (&Plugin{cfg: tc.cfg}).maybeAttachQQPlanButtons(c, tc.msg, true); len(got.Buttons) != 0 {
			t.Errorf("%s: 不应附加计划按钮：%+v", tc.name, got.Buttons)
		}
	}
}

func TestHandlePlanCommandStatusAttachesQQPlanButtons(t *testing.T) {
	// /ai plan 状态回复：计划进行中（回合活跃）时附加 查看计划+停止生成。
	const sid = "qq:u1:u1"
	p := &Plugin{
		cfg: &Config{Markdown: true, QQPlanButton: true, TriggerCmd: "/ai"},
		sm:  NewSessionManager(100, 20, time.Hour, nil),
	}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	session.setPlan(&Plan{
		Task:   "查天气",
		Active: true,
		Steps:  []PlanStep{{ID: "step_1", Description: "查询城市天气", Status: PlanInProgress}},
	})
	if !session.BeginTurn() {
		t.Fatal("BeginTurn 应成功（模拟计划仍在长回合中执行）")
	}
	defer session.EndTurn()

	ctx, sender := newQQPrivateContext("/ai plan")
	if err := p.handlePlanCommand(ctx, ""); err != nil {
		t.Fatalf("handlePlanCommand(status): %v", err)
	}
	waitReplies(t, sender, 1)
	msg := sender.replies[0]
	if msg.Markdown == "" {
		t.Error("markdown 配置下计划状态应发送 Markdown 消息")
	}
	if len(msg.Buttons) != 2 || msg.Buttons[0].Command != planViewButtonCommand ||
		msg.Buttons[1].Command != planStopButtonCommand {
		t.Errorf("/ai plan 进行中回复应附带 查看计划+停止生成：%+v", msg.Buttons)
	}
}

func TestHandleInteractionIgnoresUnknownPayload(t *testing.T) {
	// 未知回调内容走审批路径的 ignore 分支，不应报错。
	evt := platform.NewSyntheticEvent(platform.EventKindInteraction, "unknown:data",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "u1"}))
	ctx := eventctx.NewContextFromEvent(evt, fixedIDSender{})
	p := &Plugin{}
	if err := p.handleInteraction(ctx); err != nil {
		t.Fatalf("handleInteraction(unknown): %v", err)
	}
}

func TestQQActionClickCooldown(t *testing.T) {
	p := &Plugin{}
	const sid = "qq:u1:u1"
	if !p.qqActionClickAllowed(regenButtonData, sid) {
		t.Error("首次点击应被接受")
	}
	if p.qqActionClickAllowed(regenButtonData, sid) {
		t.Error("冷却窗口内同一动作重复点击应被静默忽略")
	}
	if !p.qqActionClickAllowed(clearButtonData, sid) {
		t.Error("点击重新生成后立刻点清空会话应生效（动作独立计冷却）")
	}
	if p.qqActionClickAllowed(clearButtonData, sid) {
		t.Error("冷却窗口内同一动作重复点击应被静默忽略")
	}
	if !p.qqActionClickAllowed(regenButtonData, "qq:u2:u2") {
		t.Error("不同会话的点击不应互相影响")
	}
}

func TestQQActionBusyNoticeThrottle(t *testing.T) {
	p := &Plugin{}
	const sid = "qq:u1:u1"
	if !p.qqActionBusyNoticeAllowed("重新生成", sid) {
		t.Error("首次忙时提示应放行")
	}
	if p.qqActionBusyNoticeAllowed("重新生成", sid) {
		t.Error("节流窗口内同一会话重复忙时提示应被抑制")
	}
	if !p.qqActionBusyNoticeAllowed("清空会话", sid) {
		t.Error("不同动作的忙时提示节流应独立")
	}
	if !p.qqActionBusyNoticeAllowed("重新生成", "qq:u2:u2") {
		t.Error("不同会话的忙时提示不应互相影响")
	}
}

// newQQInteractionContext 构造 QQ 单聊按钮回调的测试上下文（content 为回调
// button_data / 平台合成内容），使用异步调度器与捕获 sender 验证出站消息。
func newQQInteractionContext(content string) (*eventctx.Context, *approvalCtxSender) {
	evt := platform.NewSyntheticEvent(platform.EventKindInteraction, content,
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "u1"}))
	sender := &approvalCtxSender{}
	ctx := eventctx.NewContextFromEvent(evt, sender)
	ctx.SetDispatcher(runTaskDispatcher{})
	return ctx, sender
}

// newQQPrivateContext 构造 QQ 单聊普通文本消息上下文（content 为消息内容，
// 如 "/ai retry"），用于验证子命令路径——QQ 指令按钮点击后自动发送的文本
// 命令正是以普通消息进入该路径。
func newQQPrivateContext(content string) (*eventctx.Context, *approvalCtxSender) {
	evt := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, content,
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "u1"}))
	sender := &approvalCtxSender{}
	ctx := eventctx.NewContextFromEvent(evt, sender)
	ctx.SetDispatcher(runTaskDispatcher{})
	return ctx, sender
}

// qqButtonEnterEnabled 检查按钮是否携带 QQ 指令按钮扩展（Enter=true，手机端
// 8983+ 点击后自动发送命令）。
func qqButtonEnterEnabled(b platform.Button) bool {
	ext, ok := b.Extra[qq.ExtraKeyButton].(*qq.ButtonExtra)
	return ok && ext.Enter
}

// waitReplies 轮询等待捕获 sender 收到 n 条回复。
func waitReplies(t *testing.T, sender *approvalCtxSender, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sender.mu.Lock()
		got := len(sender.replies)
		sender.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待 %d 条回复超时（当前 %d）", n, len(sender.replies))
}

// senderReplyCount 返回捕获 sender 已收到的回复数。
func senderReplyCount(sender *approvalCtxSender) int {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return len(sender.replies)
}

func TestHandleRegenActionBusyDoesNotQueue(t *testing.T) {
	const sid = "qq:u1:u1"
	chatCalled := false
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				chatCalled = true
				return nil, nil
			},
		},
	}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	if !session.BeginTurn() {
		t.Fatal("BeginTurn 应成功（模拟生成中）")
	}
	defer session.EndTurn()

	ctx, sender := newQQInteractionContext(regenButtonData)
	if err := p.handleRegenAction(ctx); err != nil {
		t.Fatalf("handleRegenAction(busy): %v", err)
	}
	waitReplies(t, sender, 1)
	if !strings.Contains(sender.replies[0].Text, "正在生成") {
		t.Errorf("忙时提示文案不符：%q", sender.replies[0].Text)
	}

	// 忙时第二次点击：不应再发起 LLM 调用、不应排队，提示被节流抑制。
	if err := p.handleRegenAction(ctx); err != nil {
		t.Fatalf("handleRegenAction(busy again): %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if chatCalled {
		t.Error("忙时不应发起新的 LLM 调用")
	}
	if got := senderReplyCount(sender); got != 1 {
		t.Errorf("忙时提示应被节流，回复数 = %d，want 1", got)
	}
	if msgs := session.SnapshotMessages(); len(msgs) != 1 {
		t.Errorf("忙时点击不应改动会话历史，len = %d", len(msgs))
	}
}

func TestHandleRegenActionIdleRegenerates(t *testing.T) {
	const sid = "qq:u1:u1"
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, Markdown: false},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov:     &mockProvider{}, // 默认流返回 "mock stream"
	}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: "旧回复"})

	ctx, sender := newQQInteractionContext(regenButtonData)
	if err := p.handleRegenAction(ctx); err != nil {
		t.Fatalf("handleRegenAction(idle): %v", err)
	}
	waitReplies(t, sender, 1)

	msgs := session.SnapshotMessages()
	if len(msgs) != 2 {
		t.Fatalf("重新生成后会话应保留 user + 新 assistant，len = %d", len(msgs))
	}
	if msgs[0].Role != RoleUser || msgs[1].Role != RoleAssistant {
		t.Errorf("会话角色异常：%v", msgs)
	}
	if msgs[1].Content == "旧回复" {
		t.Error("旧回复应被移除并由新回复取代")
	}
	if !strings.Contains(msgs[1].Content, "mock") {
		t.Errorf("新回复内容不符：%q", msgs[1].Content)
	}
	if session.TurnActive() {
		t.Error("重新生成结束后回合应已结束（TurnActive=false）")
	}
}

func TestHandleClearActionBusyDoesNotClear(t *testing.T) {
	const sid = "qq:u1:u1"
	p := &Plugin{
		cfg: &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:  NewSessionManager(100, 20, time.Hour, nil),
	}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: "回答"})
	if !session.BeginTurn() {
		t.Fatal("BeginTurn 应成功（模拟生成中）")
	}
	defer session.EndTurn()

	ctx, sender := newQQInteractionContext(clearButtonData)
	if err := p.handleClearAction(ctx); err != nil {
		t.Fatalf("handleClearAction(busy): %v", err)
	}
	waitReplies(t, sender, 1)
	if !strings.Contains(sender.replies[0].Text, "正在生成") {
		t.Errorf("忙时提示文案不符：%q", sender.replies[0].Text)
	}
	if p.sm.Peek(sid) == nil {
		t.Error("忙时点击清空不应删除会话")
	}

	// 忙时第二次点击：提示被节流抑制、会话仍保留。
	if err := p.handleClearAction(ctx); err != nil {
		t.Fatalf("handleClearAction(busy again): %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := senderReplyCount(sender); got != 1 {
		t.Errorf("忙时提示应被节流，回复数 = %d，want 1", got)
	}
	if p.sm.Peek(sid) == nil {
		t.Error("忙时二次点击仍不应删除会话")
	}
}

func TestHandleClearActionIdleClears(t *testing.T) {
	const sid = "qq:u1:u1"
	p := &Plugin{
		cfg: &Config{},
		sm:  NewSessionManager(100, 20, time.Hour, nil),
	}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: "回答"})

	ctx, sender := newQQInteractionContext(clearButtonData)
	if err := p.handleClearAction(ctx); err != nil {
		t.Fatalf("handleClearAction(idle): %v", err)
	}
	waitReplies(t, sender, 1)
	if sender.replies[0].Text != sessionClearedText {
		t.Errorf("清空确认文案不符：%q", sender.replies[0].Text)
	}
	if p.sm.Peek(sid) != nil {
		t.Error("清空后会话应被删除")
	}

	// 冷却窗口内再次点击：静默忽略，不重复回确认文案。
	if err := p.handleClearAction(ctx); err != nil {
		t.Fatalf("handleClearAction(idle again): %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := senderReplyCount(sender); got != 1 {
		t.Errorf("冷却窗口内重复点击应静默忽略，回复数 = %d，want 1", got)
	}
}

func TestHandleInteractionNativeClearSession(t *testing.T) {
	// 平台层为原生 type=14（CLEAR_SESSION）合成内容 clear_session，
	// handleInteraction 应将其分派到清空会话处理。
	const sid = "qq:u1:u1"
	p := &Plugin{
		cfg: &Config{},
		sm:  NewSessionManager(100, 20, time.Hour, nil),
	}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})

	ctx, sender := newQQInteractionContext(clearSessionNativeContent)
	if err := p.handleInteraction(ctx); err != nil {
		t.Fatalf("handleInteraction(clear_session): %v", err)
	}
	waitReplies(t, sender, 1)
	if !strings.Contains(sender.replies[0].Text, "已清空") {
		t.Errorf("原生 type=14 清空确认文案不符：%q", sender.replies[0].Text)
	}
	if p.sm.Peek(sid) != nil {
		t.Error("原生 type=14 清空会话事件应删除会话")
	}
}

func TestExecSubCommandRetryCooldownDropsRapidDuplicate(t *testing.T) {
	// QQ 指令按钮点击会以用户消息形式连发 /ai retry：execSubCommand 入口的
	// 会话级冷却应静默吸收短时间内的重复触发，避免多次重新生成刷屏。
	const sid = "qq:u1:u1"
	p := &Plugin{
		cfg:      &Config{MaxDepth: 10, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second, Markdown: false},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov:     &mockProvider{}, // 默认流返回 "mock stream"
	}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: "旧回复"})

	ctx, sender := newQQPrivateContext("/ai retry")
	if err := p.execSubCommand(ctx, "retry"); err != nil {
		t.Fatalf("首次 /ai retry: %v", err)
	}
	waitReplies(t, sender, 1)

	if err := p.execSubCommand(ctx, "retry"); err != nil {
		t.Fatalf("冷却窗口内重复 /ai retry: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := senderReplyCount(sender); got != 1 {
		t.Errorf("冷却窗口内重复 /ai retry 应被静默忽略，回复数 = %d，want 1", got)
	}
	if msgs := session.SnapshotMessages(); len(msgs) != 2 {
		t.Errorf("重复触发不应再次改动会话历史，len = %d", len(msgs))
	}
}

func TestExecSubCommandResetWhileBusyRefuses(t *testing.T) {
	// /ai reset 与"清空会话"按钮共用 handleClearAction：生成中拒绝清空
	//（进行中回合持有会话指针），并给出节流提示。
	const sid = "qq:u1:u1"
	p := &Plugin{
		cfg: &Config{},
		sm:  NewSessionManager(100, 20, time.Hour, nil),
	}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: "回答"})
	if !session.BeginTurn() {
		t.Fatal("BeginTurn 应成功（模拟生成中）")
	}
	defer session.EndTurn()

	ctx, sender := newQQPrivateContext("/ai reset")
	if err := p.execSubCommand(ctx, "reset"); err != nil {
		t.Fatalf("execSubCommand(reset, busy): %v", err)
	}
	waitReplies(t, sender, 1)
	if !strings.Contains(sender.replies[0].Text, "正在生成") {
		t.Errorf("忙时 /ai reset 提示文案不符：%q", sender.replies[0].Text)
	}
	if p.sm.Peek(sid) == nil {
		t.Error("忙时 /ai reset 不应删除会话")
	}
}

func TestExecSubCommandResetIdleClears(t *testing.T) {
	// 空闲时 /ai reset 与"清空会话"按钮语义一致：删除会话历史并确认。
	const sid = "qq:u1:u1"
	p := &Plugin{
		cfg: &Config{},
		sm:  NewSessionManager(100, 20, time.Hour, nil),
	}
	session := p.sm.GetOrCreate(sid, "u1", "u1")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "hi"})
	p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: "回答"})

	ctx, sender := newQQPrivateContext("/ai reset")
	if err := p.execSubCommand(ctx, "reset"); err != nil {
		t.Fatalf("execSubCommand(reset, idle): %v", err)
	}
	waitReplies(t, sender, 1)
	if sender.replies[0].Text != sessionClearedText {
		t.Errorf("空闲 /ai reset 确认文案不符：%q", sender.replies[0].Text)
	}
	if p.sm.Peek(sid) != nil {
		t.Error("空闲 /ai reset 应删除会话")
	}
}
