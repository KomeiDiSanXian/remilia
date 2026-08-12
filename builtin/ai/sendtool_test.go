package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncDispatcher 同步执行发送任务，便于测试中立即拿到发送结果。
type syncDispatcher struct{}

func (d *syncDispatcher) Submit(_ string, task func(context.Context) error) error {
	return task(context.Background())
}

// newSendTestContext 构造私聊事件上下文（带平台发送器与同步调度器）。
func newSendTestContext(sender platform.Sender) *eventctx.Context {
	return newSendTestCtx(sender, false)
}

// newGroupSendTestContext 构造群聊事件上下文。
func newGroupSendTestContext(sender platform.Sender) *eventctx.Context {
	return newSendTestCtx(sender, true)
}

func newSendTestCtx(sender platform.Sender, group bool) *eventctx.Context {
	evt := platform.NewSyntheticEvent(
		platform.EventKindPrivateMessage,
		"hello",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "chat_1", IsGroup: group}),
	)
	if group {
		evt = platform.NewSyntheticEvent(
			platform.EventKindGroupMessage,
			"hello",
			platform.WithSyntheticSender(platform.UserInfo{ID: "user1"}),
			platform.WithSyntheticChat(platform.ChatInfo{ID: "group_1", IsGroup: true}),
		)
	}
	ctx := eventctx.NewContextFromEvent(evt, sender)
	ctx.SetDispatcher(&syncDispatcher{})
	return ctx
}

// withHistory 构造带 messagelog 近期发言记录（UserID: 昵称）的插件。
func withHistory(p *Plugin, groupChat bool, speakers map[string]string) *Plugin {
	lg := messagelog.New(100)
	chatID := "chat_1"
	if groupChat {
		chatID = "group_1"
	}
	i := 0
	for uid, name := range speakers {
		i++
		lg.Record(messagelog.RecordEntry{
			EventID:  fmt.Sprintf("e%d", i),
			ChatID:   chatID,
			UserID:   uid,
			UserName: name,
			Content:  "hi",
			IsGroup:  groupChat,
		})
	}
	p.history = lg
	return p
}

// TestProgressReportingGuidance 保证"长任务报进度"的行为规范同时存在于
// 框架提示词与 send_message 工具描述（防止后续重构删掉其中一侧）。
func TestProgressReportingGuidance(t *testing.T) {
	if !strings.Contains(DefaultFrameworkPrompt, "先用 send_message 向用户报告进度") {
		t.Error("framework prompt should guide progress reporting via send_message")
	}
	if !strings.Contains(DefaultFrameworkPrompt, "单轮即可完成的任务不要调用 send_message") {
		t.Error("framework prompt should restrict send_message to multi-round tasks")
	}
	p := &Plugin{cfg: &Config{}}
	tools := p.buildSendTools()
	var msgTool *Tool
	for i := range tools {
		if tools[i].Name == sendMessageToolName {
			msgTool = &tools[i]
			break
		}
	}
	if msgTool == nil {
		t.Fatal("send_message tool not built")
	}
	if !strings.Contains(msgTool.Description, "每完成一步先调用本工具报告进度") {
		t.Error("send_message description should instruct step-by-step progress reporting")
	}
	if !strings.Contains(msgTool.Description, "单轮即可完成的任务不要调用") {
		t.Error("send_message description should discourage single-round usage")
	}
}

func TestToolSenderContext(t *testing.T) {
	ctx := context.Background()
	_, ok := ToolSenderFromContext(ctx)
	assert.False(t, ok)

	sender := &loopToolSender{}
	ctx = WithToolSender(ctx, sender)
	got, ok := ToolSenderFromContext(ctx)
	assert.True(t, ok)
	assert.Same(t, sender, got)
}

func TestBuildOutboundMessage(t *testing.T) {
	mdPlugin := &Plugin{cfg: &Config{Markdown: true}}
	textPlugin := &Plugin{cfg: &Config{Markdown: false}}

	// 默认跟随插件 markdown 配置（Markdown=true → Markdown 渲染）
	msg, err := mdPlugin.buildOutboundMessage(map[string]any{"message": "hello"})
	require.NoError(t, err)
	assert.Empty(t, msg.Text)
	assert.Equal(t, "hello", msg.Markdown)

	// Markdown=false → 纯文本
	msg, err = textPlugin.buildOutboundMessage(map[string]any{"message": "hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello", msg.Text)
	assert.Empty(t, msg.Markdown)

	// format 显式覆盖：markdown=false 配置下指定 markdown
	msg, err = textPlugin.buildOutboundMessage(map[string]any{"message": "# title", "format": "markdown"})
	require.NoError(t, err)
	assert.Empty(t, msg.Text)
	assert.Equal(t, "# title", msg.Markdown)

	// format 显式覆盖：markdown=true 配置下指定 text
	msg, err = mdPlugin.buildOutboundMessage(map[string]any{"message": "# title", "format": "text"})
	require.NoError(t, err)
	assert.Equal(t, "# title", msg.Text)
	assert.Empty(t, msg.Markdown)

	msg, err = mdPlugin.buildOutboundMessage(map[string]any{
		"message":     "pic",
		"image_url":   "https://example.com/a.png",
		"mention_ids": []any{"u1", "u2"},
	})
	require.NoError(t, err)
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, platform.AttachmentKindImage, msg.Attachments[0].Kind)
	assert.Equal(t, "https://example.com/a.png", msg.Attachments[0].URL)
	assert.Equal(t, []string{"u1", "u2"}, msg.Mentions)

	_, err = mdPlugin.buildOutboundMessage(map[string]any{})
	assert.Error(t, err)

	long := strings.Repeat("a", maxSendMessageRunes+1)
	_, err = mdPlugin.buildOutboundMessage(map[string]any{"message": long})
	assert.Error(t, err)
}

func TestLoopToolSenderResolveTarget(t *testing.T) {
	groupCtx := newGroupSendTestContext(mock.NewSender())
	privateCtx := newSendTestContext(mock.NewSender())
	groupPlugin := withHistory(&Plugin{}, true, map[string]string{"u1": "张三"})
	privatePlugin := withHistory(&Plugin{}, false, map[string]string{"chat_1": "张三"})

	tests := []struct {
		name     string
		ctx      *eventctx.Context
		p        *Plugin
		raw      string
		isGroup  bool
		want     ChatTarget
		wantErr  bool
		wantExpr string
	}{
		{"本群（群聊）", groupCtx, &Plugin{}, "本群", false, ChatTarget{ID: "group_1", IsGroup: true}, false, "本群（group_1）"},
		{"本群（私聊报错）", privateCtx, &Plugin{}, "这个群", false, ChatTarget{}, true, ""},
		{"我（调用者）", groupCtx, &Plugin{}, "我", false, ChatTarget{ID: "user1"}, false, "我（user1）"},
		{"对方（私聊）", privateCtx, &Plugin{}, "对方", false, ChatTarget{ID: "chat_1"}, false, "对方（chat_1）"},
		{"对方（群聊报错）", groupCtx, &Plugin{}, "这里", false, ChatTarget{}, true, ""},
		{"群成员昵称", groupCtx, groupPlugin, "张三", false, ChatTarget{ID: "u1"}, false, "张三（u1）"},
		{"私聊对方昵称", privateCtx, privatePlugin, "张三", false, ChatTarget{ID: "chat_1"}, false, "张三（chat_1）"},
		{"原始 ID", groupCtx, &Plugin{}, "rawid123", true, ChatTarget{ID: "rawid123", IsGroup: true}, false, "rawid123"},
		{"未知昵称", groupCtx, &Plugin{}, "没有人", false, ChatTarget{}, true, ""},
		{"含空格的未知目标", groupCtx, &Plugin{}, "not a target", false, ChatTarget{}, true, ""},
		{"空目标", groupCtx, &Plugin{}, "  ", false, ChatTarget{}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sender := &loopToolSender{ctx: tc.ctx, p: tc.p}
			target, display, err := sender.ResolveTarget(context.Background(), tc.raw, tc.isGroup)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, target)
			assert.Equal(t, tc.wantExpr, display)
		})
	}
}

func TestResolveTargetCaseInsensitive(t *testing.T) {
	ctx := newGroupSendTestContext(mock.NewSender())
	p := withHistory(&Plugin{}, true, map[string]string{"u9": "Zhang San"})
	sender := &loopToolSender{ctx: ctx, p: p}

	target, display, err := sender.ResolveTarget(context.Background(), "zhang san", false)
	require.NoError(t, err)
	assert.Equal(t, ChatTarget{ID: "u9"}, target)
	assert.Equal(t, "zhang san（u9）", display)
}

func TestResolveTargetAmbiguity(t *testing.T) {
	ctx := newGroupSendTestContext(mock.NewSender())
	p := withHistory(&Plugin{}, true, map[string]string{"u1": "张三", "u2": "张三"})
	sender := &loopToolSender{ctx: ctx, p: p}

	_, _, err := sender.ResolveTarget(context.Background(), "张三", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "多个近期发言者")
	assert.Contains(t, err.Error(), "u1")
	assert.Contains(t, err.Error(), "u2")
}

func TestResolveTargetJoinedGroupName(t *testing.T) {
	// 自定义 sender 实现 GroupInfoProvider，提供已加入群列表
	ctx := newGroupSendTestContext(&groupNameSender{})
	p := &Plugin{}
	s := &loopToolSender{ctx: ctx, p: p}

	target, display, err := s.ResolveTarget(context.Background(), "开发群", false)
	require.NoError(t, err)
	assert.Equal(t, ChatTarget{ID: "g9", IsGroup: true}, target)
	assert.Equal(t, "开发群（g9）", display)

	// 群名不匹配时跳过（回退原始 ID）
	target, display, err = s.ResolveTarget(context.Background(), "u99", false)
	require.NoError(t, err)
	assert.Equal(t, ChatTarget{ID: "u99"}, target)
	assert.Equal(t, "u99", display)
}

// filterCalls 按方法名过滤 MockSender 的调用记录。
func filterCalls(calls []mock.SenderCall, method string) []mock.SenderCall {
	var out []mock.SenderCall
	for _, c := range calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// groupNameSender 实现 GroupInfoProvider，用于群名匹配测试。
type groupNameSender struct {
	platform.NoopSender
}

func (s *groupNameSender) GetGroupInfo(_ context.Context, _ string) (platform.GroupInfo, error) {
	return platform.GroupInfo{}, platform.ErrNotSupported
}

func (s *groupNameSender) GetGroupMemberList(_ context.Context, _ string) ([]platform.GroupMemberInfo, error) {
	return nil, platform.ErrNotSupported
}

func (s *groupNameSender) GetGroupMember(_ context.Context, _, _ string) (platform.GroupMemberInfo, error) {
	return platform.GroupMemberInfo{}, platform.ErrNotSupported
}

func (s *groupNameSender) GetJoinedGroups(_ context.Context) ([]platform.GroupInfo, error) {
	return []platform.GroupInfo{
		{ID: "g9", Name: "开发群"},
		{ID: "g10", Name: "闲聊群"},
	}, nil
}

func TestSendBudget(t *testing.T) {
	b := &sendBudget{limit: 2}
	assert.True(t, b.tryUse())
	assert.True(t, b.tryUse())
	assert.False(t, b.tryUse())

	unlimited := &sendBudget{limit: 0}
	for range 100 {
		assert.True(t, unlimited.tryUse())
	}
}

func TestLoopToolSenderSendTo(t *testing.T) {
	mockSender := mock.NewSender()
	ctx := newSendTestContext(mockSender)
	sender := &loopToolSender{ctx: ctx, p: &Plugin{}, budget: &sendBudget{limit: 10}}

	// 未授权（未过审批门）：拒绝
	_, err := sender.SendTo(context.Background(), ChatTarget{ID: "u1"}, platform.TextMessage("hi"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "审批")

	// 授权后：私聊用户走 SessionNotifier.NotifyUser
	sender.sendToAllowed = true
	_, err = sender.SendTo(context.Background(), ChatTarget{ID: "u1"}, platform.TextMessage("hi"))
	require.NoError(t, err)
	calls := mockSender.Snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "NotifyUser", calls[0].Method)
	assert.Equal(t, "u1", calls[0].ChatID)

	// 群聊目标走 NotifyGroup
	_, err = sender.SendTo(context.Background(), ChatTarget{ID: "g1", IsGroup: true}, platform.TextMessage("hi"))
	require.NoError(t, err)
	calls = mockSender.Snapshot()
	assert.Equal(t, "NotifyGroup", calls[1].Method)
	assert.Equal(t, "g1", calls[1].ChatID)

	// 空目标 ID
	_, err = sender.SendTo(context.Background(), ChatTarget{}, platform.TextMessage("hi"))
	assert.Error(t, err)

	// 空消息
	_, err = sender.SendTo(context.Background(), ChatTarget{ID: "u1"}, platform.OutboundMessage{})
	assert.Error(t, err)
}

func TestLoopToolSenderSendToFallback(t *testing.T) {
	plain := &approvalCtxSender{}
	ctx := newSendTestContext(plain)
	sender := &loopToolSender{ctx: ctx, p: &Plugin{}, sendToAllowed: true, budget: &sendBudget{limit: 10}}

	// 平台 Sender 未实现 SessionNotifier 时回退普通 Send
	_, err := sender.SendTo(context.Background(), ChatTarget{ID: "u9"}, platform.TextMessage("hi"))
	require.NoError(t, err)
	plain.mu.Lock()
	require.Len(t, plain.replies, 1)
	plain.mu.Unlock()
}

func TestLoopToolSenderReplyToChat(t *testing.T) {
	mockSender := mock.NewSender()
	ctx := newSendTestContext(mockSender)
	sender := &loopToolSender{ctx: ctx, p: &Plugin{}, budget: &sendBudget{limit: 10}}

	_, err := sender.ReplyToChat(context.Background(), platform.TextMessage("progress"))
	require.NoError(t, err)
	calls := mockSender.Snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "Send", calls[0].Method)
	assert.Equal(t, "progress", calls[0].Msg.Text)

	// 预算耗尽：拒绝继续发送
	limited := &loopToolSender{ctx: ctx, p: &Plugin{}, budget: &sendBudget{limit: 1}}
	_, err = limited.ReplyToChat(context.Background(), platform.TextMessage("a"))
	require.NoError(t, err)
	_, err = limited.ReplyToChat(context.Background(), platform.TextMessage("b"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "上限")
}

func TestSendToolsThroughExecuteTool(t *testing.T) {
	mockSender := mock.NewSender()
	ctx := newGroupSendTestContext(mockSender)
	pm := eventctx.NewPermissionManager()
	pm.GrantPermission("user1", permission.Permission{Resource: "ai.message", Action: "send"})
	ctx.SetPermissionManager(pm)
	p := withHistory(&Plugin{
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
	}, true, map[string]string{"u1": "张三"})
	for _, tool := range p.buildSendTools() {
		p.reg.Register(tool)
	}

	sender := &loopToolSender{ctx: ctx, p: p, sendToAllowed: true, budget: &sendBudget{limit: 10}}

	// send_message：向当前会话发送
	res := p.executeTool(ctx, ToolCall{Name: sendMessageToolName, Arguments: map[string]any{"message": "进度 1/2"}},
		context.Background(), &captureSender{}, sender)
	assert.Equal(t, "消息已发送", res)
	calls := mockSender.Snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "进度 1/2", calls[0].Msg.Text)

	// send_to：昵称自动解析（messagelog 近期发言者）
	res = p.executeTool(ctx, ToolCall{Name: sendToToolName, Arguments: map[string]any{"target": "张三", "message": "通知"}},
		context.Background(), &captureSender{}, sender)
	assert.Equal(t, "消息已发送到 张三（u1）", res)
	calls = mockSender.Snapshot()
	notifyCalls := filterCalls(calls, "NotifyUser")
	require.Len(t, notifyCalls, 1)
	assert.Equal(t, "u1", notifyCalls[0].ChatID)

	// send_to：原始 ID + 群聊
	res = p.executeTool(ctx, ToolCall{Name: sendToToolName, Arguments: map[string]any{"target": "u2", "is_group": true, "message": "群通知"}},
		context.Background(), &captureSender{}, sender)
	assert.Equal(t, "消息已发送到 u2", res)
	calls = mockSender.Snapshot()
	groupCalls := filterCalls(calls, "NotifyGroup")
	require.Len(t, groupCalls, 1)
	assert.Equal(t, "u2", groupCalls[0].ChatID)

	// 无发送能力上下文：工具报错
	res = p.executeTool(ctx, ToolCall{Name: sendMessageToolName, Arguments: map[string]any{"message": "x"}},
		context.Background(), &captureSender{}, nil)
	assert.Contains(t, res, "无消息发送能力")

	// 无法解析的目标：报错
	res = p.executeTool(ctx, ToolCall{Name: sendToToolName, Arguments: map[string]any{"target": "no such user", "message": "x"}},
		context.Background(), &captureSender{}, sender)
	assert.Contains(t, res, "无法解析目标")
}

func TestSendToApprovalForced(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(Tool{Name: sendToToolName, AlwaysRequireApproval: true, RequiresApproval: true})
	reg.Register(Tool{Name: "plain_tool"})
	p := &Plugin{reg: reg, cfg: &Config{ToolApproval: "off"}}
	ctx, _ := newApprovalTestContext("u1")

	assert.True(t, p.approvalModeFor(ctx, sendToToolName), "off 模式下 send_to 也必须审批")
	assert.False(t, p.approvalModeFor(ctx, "plain_tool"), "off 模式下普通工具不审批")

	p.cfg.ToolApproval = "restricted"
	assert.True(t, p.approvalModeFor(ctx, sendToToolName))
}

func TestExecOneToolSendToDeniedWithoutPermission(t *testing.T) {
	ctx := newSendTestContext(mock.NewSender())
	p := &Plugin{
		cfg:       &Config{ToolTimeout: 5 * time.Second},
		sm:        NewSessionManager(100, 20, time.Hour, nil),
		reg:       NewToolRegistry(),
		skillReg:  NewSkillRegistry(),
		approvals: newApprovalManager(),
	}
	for _, tool := range p.buildSendTools() {
		p.reg.Register(tool)
	}
	session := p.sm.GetOrCreate("send:deny", "user1", "chat_1")

	res := p.execOneTool(ctx, session, &captureSender{}, &sendBudget{limit: 10}, ToolCall{
		Name:      sendToToolName,
		Arguments: map[string]any{"target": "张三", "message": "hi"},
	})
	assert.Contains(t, res.result, "需要权限")
	p.approvals.mu.Lock()
	assert.Len(t, p.approvals.pending, 0, "无权限时不应发起审批")
	p.approvals.mu.Unlock()
}

func TestExecOneToolSendToApprovalFlow(t *testing.T) {
	mockSender := mock.NewSender()
	ctx := newGroupSendTestContext(mockSender)
	pm := eventctx.NewPermissionManager()
	pm.GrantPermission("user1", permission.Permission{Resource: "ai.message", Action: "send"})
	ctx.SetPermissionManager(pm)

	p := withHistory(&Plugin{
		cfg:       &Config{ToolTimeout: 5 * time.Second},
		sm:        NewSessionManager(100, 20, time.Hour, nil),
		reg:       NewToolRegistry(),
		skillReg:  NewSkillRegistry(),
		approvals: newApprovalManager(),
	}, true, map[string]string{"u1": "张三"})
	for _, tool := range p.buildSendTools() {
		p.reg.Register(tool)
	}
	session := p.sm.GetOrCreate("send:approve", "user1", "group_1")

	done := make(chan toolExecResult, 1)
	go func() {
		done <- p.execOneTool(ctx, session, &captureSender{}, &sendBudget{limit: 10}, ToolCall{
			Name:      sendToToolName,
			Arguments: map[string]any{"target": "张三", "message": "你好"},
		})
	}()

	// 等待审批请求注册并由发起者批准
	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && id == "" {
		p.approvals.mu.Lock()
		for k := range p.approvals.pending {
			id = k
			break
		}
		p.approvals.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	require.NotEmpty(t, id, "send_to 应发起审批")
	assert.True(t, p.approvals.resolve(id, "user1", true))

	select {
	case res := <-done:
		assert.Contains(t, res.result, "消息已发送到 张三（u1）")
	case <-time.After(3 * time.Second):
		t.Fatal("execOneTool did not finish after approval")
	}

	// 调用记录：审批请求消息（Send，含解析后的目标）+ 实际发送（NotifyUser）
	calls := mockSender.Snapshot()
	require.Len(t, calls, 2)
	assert.Equal(t, "Send", calls[0].Method)
	assert.Contains(t, calls[0].Msg.Text, "张三（u1）", "审批消息应显示解析后的目标")
	assert.Equal(t, "NotifyUser", calls[1].Method)
	assert.Equal(t, "u1", calls[1].ChatID)
}
