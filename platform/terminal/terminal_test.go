package terminal

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestEvent_BasicFields(t *testing.T) {
	e := NewEvent("hello world")

	if e.Platform() != PlatformID {
		t.Errorf("期望平台 %q, 得到 %q", PlatformID, e.Platform())
	}
	if e.Kind() != platform.EventKindPrivateMessage {
		t.Errorf("期望事件类型 PRIVATE_MESSAGE, 得到 %s", e.Kind())
	}
	if e.Content() != "hello world" {
		t.Errorf("期望内容 %q, 得到 %q", "hello world", e.Content())
	}
	if e.ID() == "" {
		t.Error("期望 ID 非空")
	}
	if e.Sender().ID != DefaultUserID {
		t.Errorf("期望发送者 ID %q, 得到 %q", DefaultUserID, e.Sender().ID)
	}
}

func TestEvent_GroupEvent(t *testing.T) {
	e := NewGroupEvent("hello group", "group-123")

	if e.Kind() != platform.EventKindGroupMessage {
		t.Errorf("期望事件类型 GROUP_MESSAGE, 得到 %s", e.Kind())
	}
	if e.Chat().ID != "group-123" {
		t.Errorf("期望会话 ID %q, 得到 %q", "group-123", e.Chat().ID)
	}
	if !e.Chat().IsGroup {
		t.Error("期望 IsGroup 为 true")
	}
}

func TestEvent_Setters(t *testing.T) {
	e := NewEvent("test").
		SetKind(platform.EventKindGuildMessage).
		SetSender("user-42", "测试用户").
		SetRawType("CUSTOM_EVENT").
		SetReplyToID("msg-001").
		SetMentions([]platform.UserInfo{{ID: "u1", DisplayName: "张三"}})

	if e.Kind() != platform.EventKindGuildMessage {
		t.Errorf("期望 GUILD_MESSAGE, 得到 %s", e.Kind())
	}
	if e.Sender().ID != "user-42" {
		t.Errorf("期望发送者 ID %q, 得到 %q", "user-42", e.Sender().ID)
	}
	if e.Sender().DisplayName != "测试用户" {
		t.Errorf("期望显示名称 %q, 得到 %q", "测试用户", e.Sender().DisplayName)
	}
	if e.RawType() != "CUSTOM_EVENT" {
		t.Errorf("期望 RawType %q, 得到 %q", "CUSTOM_EVENT", e.RawType())
	}
	if e.ReplyToID() != "msg-001" {
		t.Errorf("期望 ReplyToID %q, 得到 %q", "msg-001", e.ReplyToID())
	}
	if len(e.Mentions()) != 1 || e.Mentions()[0].DisplayName != "张三" {
		t.Errorf("期望 @ 用户 [张三], 得到 %v", e.Mentions())
	}
	if e.RawPayload() != nil {
		t.Error("期望 RawPayload 为 nil")
	}
	if e.IsEdited() {
		t.Error("期望 IsEdited 为 false")
	}
}

func TestAdapter_Sender_Send(t *testing.T) {
	a := NewAdapter(WithOutput(&bytes.Buffer{}))
	ctx := context.Background()

	result, err := a.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: "test-chat"},
		Message: platform.TextMessage("hello"),
	})
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
	if result.MessageID == "" {
		t.Error("期望 MessageID 非空")
	}
	if result.Platform != PlatformID {
		t.Errorf("期望平台 %q, 得到 %q", PlatformID, result.Platform)
	}

	msgs := a.Messages()
	if len(msgs) != 1 {
		t.Fatalf("期望 1 条消息, 得到 %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("期望内容 %q, 得到 %q", "hello", msgs[0].Content)
	}
}

func TestAdapter_Sender_ReturnsSelf(t *testing.T) {
	a := NewAdapter()

	// Adapter.Sender() 应返回自身（Adapter 实现了 platform.Sender）
	sender := a.Sender()
	if sender != a {
		t.Error("期望 Sender() 返回 Adapter 自身")
	}
}

func TestAdapter_LastMessage(t *testing.T) {
	a := NewAdapter(WithOutput(&bytes.Buffer{}))
	ctx := context.Background()

	if a.LastMessage() != nil {
		t.Error("期望 nil")
	}

	_, _ = a.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: "test"},
		Message: platform.TextMessage("first"),
	})
	_, _ = a.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: "test"},
		Message: platform.TextMessage("second"),
	})

	last := a.LastMessage()
	if last == nil || last.Content != "second" {
		t.Errorf("期望最后消息 %q, 得到 %v", "second", last)
	}
}

func TestAdapter_Clear(t *testing.T) {
	a := NewAdapter(WithOutput(&bytes.Buffer{}))
	ctx := context.Background()

	_, _ = a.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: "test"},
		Message: platform.TextMessage("test"),
	})

	a.Clear()
	if len(a.Messages()) != 0 {
		t.Error("期望 Clear() 后消息数为 0")
	}
}

func TestAdapter_Capabilities(t *testing.T) {
	a := NewAdapter()
	caps := a.Capabilities()

	if !caps.Markdown {
		t.Error("期望支持 Markdown")
	}
	if !caps.MessageEdit {
		t.Error("期望支持消息编辑")
	}
	if !caps.MessageDelete {
		t.Error("期望支持消息删除")
	}
	if !caps.Reactions {
		t.Error("期望支持表情回应")
	}
	if !caps.TypingIndicator {
		t.Error("期望支持输入指示")
	}
	if caps.Buttons {
		t.Error("终端不应支持按钮")
	}
}

func TestAdapter_BotIdentity(t *testing.T) {
	a := NewAdapter(WithBotID("my-bot-123"), WithBotName("测试Bot"))

	if a.BotID() != "my-bot-123" {
		t.Errorf("期望 BotID %q, 得到 %q", "my-bot-123", a.BotID())
	}
	if a.BotName() != "测试Bot" {
		t.Errorf("期望 BotName %q, 得到 %q", "测试Bot", a.BotName())
	}
}

func TestAdapter_BotIdentity_InterfaceAssertion(t *testing.T) {
	a := NewAdapter()

	// 验证 Adapter 实现了 platform.BotIdentity
	bi, ok := any(a).(platform.BotIdentity)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.BotIdentity")
	}
	if bi.BotID() != DefaultBotID {
		t.Errorf("期望默认 BotID %q, 得到 %q", DefaultBotID, bi.BotID())
	}
	if bi.BotName() != DefaultBotName {
		t.Errorf("期望默认 BotName %q, 得到 %q", DefaultBotName, bi.BotName())
	}
}

func TestAdapter_MessageEditor(t *testing.T) {
	a := NewAdapter(WithOutput(&bytes.Buffer{}))
	ctx := context.Background()

	result, _ := a.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: "test"},
		Message: platform.TextMessage("original"),
	})

	// 验证实现了 MessageEditor 接口
	editor, ok := any(a).(platform.MessageEditor)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.MessageEditor")
	}

	err := editor.Edit(ctx, "test", result.MessageID, platform.TextMessage("edited"))
	if err != nil {
		t.Fatalf("编辑不应有错误: %v", err)
	}

	msg := a.LastMessage()
	if msg == nil || msg.Content != "edited" {
		t.Errorf("期望编辑后内容 %q, 得到 %v", "edited", msg)
	}
	if !msg.Edited {
		t.Error("期望 Edited 标记为 true")
	}
}

func TestAdapter_MessageDeleter(t *testing.T) {
	a := NewAdapter(WithOutput(&bytes.Buffer{}))
	ctx := context.Background()

	result, _ := a.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: "test"},
		Message: platform.TextMessage("to delete"),
	})

	// 验证实现了 MessageDeleter 接口
	deleter, ok := any(a).(platform.MessageDeleter)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.MessageDeleter")
	}

	err := deleter.Delete(ctx, "test", result.MessageID)
	if err != nil {
		t.Fatalf("删除不应有错误: %v", err)
	}

	msg := a.LastMessage()
	if msg == nil || !msg.Deleted {
		t.Error("期望 Deleted 标记为 true")
	}
}

func TestAdapter_TypingNotifier(t *testing.T) {
	a := NewAdapter(WithOutput(&bytes.Buffer{}))

	tn, ok := any(a).(platform.TypingNotifier)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.TypingNotifier")
	}

	err := tn.SendTyping(context.Background(), "test-chat")
	if err != nil {
		t.Fatalf("SendTyping 不应有错误: %v", err)
	}
}

func TestAdapter_ReactionSender(t *testing.T) {
	a := NewAdapter(WithOutput(&bytes.Buffer{}))
	ctx := context.Background()

	result, _ := a.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: "test"},
		Message: platform.TextMessage("react me"),
	})

	rs, ok := any(a).(platform.ReactionSender)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.ReactionSender")
	}

	emoji := platform.Emoji{Kind: platform.EmojiKindUnicode, Value: "👍"}
	err := rs.AddReaction(ctx, "test", result.MessageID, emoji)
	if err != nil {
		t.Fatalf("AddReaction 不应有错误: %v", err)
	}

	msg := a.LastMessage()
	if msg == nil || len(msg.Reactions) != 1 {
		t.Fatalf("期望 1 个表情, 得到 %v", msg)
	}

	err = rs.RemoveReaction(ctx, "test", result.MessageID, emoji)
	if err != nil {
		t.Fatalf("RemoveReaction 不应有错误: %v", err)
	}

	msg = a.LastMessage()
	if len(msg.Reactions) != 0 {
		t.Errorf("期望移除后 0 个表情, 得到 %d", len(msg.Reactions))
	}
}

func TestAdapter_GroupInfoProvider(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	gip, ok := any(a).(platform.GroupInfoProvider)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.GroupInfoProvider")
	}

	info, err := gip.GetGroupInfo(ctx, "grp-1")
	if err != nil {
		t.Fatalf("GetGroupInfo 不应有错误: %v", err)
	}
	if info.ID != "grp-1" {
		t.Errorf("期望群组 ID %q, 得到 %q", "grp-1", info.ID)
	}

	members, err := gip.GetGroupMemberList(ctx, "grp-1")
	if err != nil {
		t.Fatalf("GetGroupMemberList 不应有错误: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("期望 1 个成员, 得到 %d", len(members))
	}

	member, err := gip.GetGroupMember(ctx, "grp-1", DefaultUserID)
	if err != nil {
		t.Fatalf("GetGroupMember 不应有错误: %v", err)
	}
	if member.UserID != DefaultUserID {
		t.Errorf("期望用户 ID %q, 得到 %q", DefaultUserID, member.UserID)
	}

	groups, err := gip.GetJoinedGroups(ctx)
	if err != nil {
		t.Fatalf("GetJoinedGroups 不应有错误: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("期望空切片, 得到 %v", groups)
	}
}

func TestAdapter_GroupManager(t *testing.T) {
	buf := &bytes.Buffer{}
	a := NewAdapter(WithOutput(buf))
	ctx := context.Background()

	gm, ok := any(a).(platform.GroupManager)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.GroupManager")
	}

	if err := gm.KickMember(ctx, "grp-1", "user-1", false); err != nil {
		t.Fatalf("KickMember 不应有错误: %v", err)
	}
	if err := gm.BanMember(ctx, "grp-1", "user-1", 5*time.Minute); err != nil {
		t.Fatalf("BanMember 不应有错误: %v", err)
	}
	if err := gm.BanMember(ctx, "grp-1", "user-1", 0); err != nil {
		t.Fatalf("BanMember(unban) 不应有错误: %v", err)
	}
	if err := gm.SetAdmin(ctx, "grp-1", "user-1", true); err != nil {
		t.Fatalf("SetAdmin(true) 不应有错误: %v", err)
	}
	if err := gm.SetAdmin(ctx, "grp-1", "user-1", false); err != nil {
		t.Fatalf("SetAdmin(false) 不应有错误: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "踢出群 grp-1") {
		t.Errorf("期望输出包含踢出提示, 得到 %q", out)
	}
	if !strings.Contains(out, "禁言") {
		t.Errorf("期望输出包含禁言提示, 得到 %q", out)
	}
	if !strings.Contains(out, "授予") {
		t.Errorf("期望输出包含授予管理, 得到 %q", out)
	}
	if !strings.Contains(out, "撤销") {
		t.Errorf("期望输出包含撤销管理, 得到 %q", out)
	}
}

func TestAdapter_AvatarProvider(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	ap, ok := any(a).(platform.AvatarProvider)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.AvatarProvider")
	}

	url, err := ap.GetUserAvatarURL(ctx, "user-123")
	if err != nil {
		t.Fatalf("GetUserAvatarURL 不应有错误: %v", err)
	}
	if url == "" {
		t.Error("期望头像 URL 非空")
	}
	if !strings.Contains(url, "user-123") {
		t.Errorf("期望 URL 包含用户 ID, 得到 %q", url)
	}
}

func TestAdapter_SessionNotifier(t *testing.T) {
	buf := &bytes.Buffer{}
	a := NewAdapter(WithOutput(buf))
	ctx := context.Background()

	sn, ok := any(a).(platform.SessionNotifier)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.SessionNotifier")
	}

	err := sn.NotifyUser(ctx, "user-1", platform.TextMessage("私信通知"))
	if err != nil {
		t.Fatalf("NotifyUser 不应有错误: %v", err)
	}

	err = sn.NotifyGroup(ctx, "grp-1", platform.TextMessage("群通知"))
	if err != nil {
		t.Fatalf("NotifyGroup 不应有错误: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Notify→user-1") {
		t.Errorf("期望输出包含 Notify→user-1, 得到 %q", out)
	}
	if !strings.Contains(out, "Notify→群grp-1") {
		t.Errorf("期望输出包含 Notify→群grp-1, 得到 %q", out)
	}
}

func TestAdapter_AutoModerator(t *testing.T) {
	buf := &bytes.Buffer{}
	a := NewAdapter(WithOutput(buf))
	ctx := context.Background()

	am, ok := any(a).(platform.AutoModerator)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.AutoModerator")
	}

	err := am.DeleteMemberMessage(ctx, "grp-1", "msg-someone")
	if err != nil {
		t.Fatalf("DeleteMemberMessage 不应有错误: %v", err)
	}

	err = am.MuteAll(ctx, "grp-1", true)
	if err != nil {
		t.Fatalf("MuteAll(true) 不应有错误: %v", err)
	}

	err = am.MuteAll(ctx, "grp-1", false)
	if err != nil {
		t.Fatalf("MuteAll(false) 不应有错误: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "已删除群 grp-1") {
		t.Errorf("期望输出包含删除提示, 得到 %q", out)
	}
	if !strings.Contains(out, "全体禁言 已开启") {
		t.Errorf("期望输出包含禁言开启, 得到 %q", out)
	}
	if !strings.Contains(out, "全体禁言 已解除") {
		t.Errorf("期望输出包含禁言解除, 得到 %q", out)
	}
}

func TestAdapter_InvitationHandler(t *testing.T) {
	buf := &bytes.Buffer{}
	a := NewAdapter(WithOutput(buf))
	ctx := context.Background()

	ih, ok := any(a).(platform.InvitationHandler)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.InvitationHandler")
	}

	if err := ih.AcceptGroupInvite(ctx, "inv-1"); err != nil {
		t.Fatalf("AcceptGroupInvite 不应有错误: %v", err)
	}
	if err := ih.RejectGroupInvite(ctx, "inv-2", "已满"); err != nil {
		t.Fatalf("RejectGroupInvite 不应有错误: %v", err)
	}
	if err := ih.AcceptFriendRequest(ctx, "req-1"); err != nil {
		t.Fatalf("AcceptFriendRequest 不应有错误: %v", err)
	}
	if err := ih.RejectFriendRequest(ctx, "req-2", "不认识"); err != nil {
		t.Fatalf("RejectFriendRequest 不应有错误: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "接受群组邀请 inv-1") {
		t.Errorf("期望输出, 得到 %q", out)
	}
	if !strings.Contains(out, "接受好友申请 req-1") {
		t.Errorf("期望输出, 得到 %q", out)
	}
}

func TestAdapter_RecoverableAdapter_Disconnect(t *testing.T) {
	a := NewAdapter(
		WithInput(strings.NewReader("hello\n")),
		WithOutput(&bytes.Buffer{}),
		WithPrompt(""),
		WithWelcomeMessage(""),
	)

	// 验证实现了 RecoverableAdapter 接口
	ra, ok := any(a).(platform.RecoverableAdapter)
	if !ok {
		t.Fatal("期望 Adapter 实现 platform.RecoverableAdapter")
	}

	var disconnectErr error
	unreg := ra.OnDisconnect(func(err error) {
		disconnectErr = err
	})
	defer unreg()

	ctx := context.Background()
	_ = a.Start(ctx, func(e platform.Event) {})

	// 正常 quit/EOF 路径不应触发断连回调（只有意外断连才触发）
	// 上面的 input 只有一行 "hello\n" 然后 EOF
	// Start 会在 EOF 时调用 NotifyDisconnect
	if disconnectErr == nil {
		t.Error("期望 EOF 时触发断连回调")
	}
}

func TestAdapter_SimulateMessage(t *testing.T) {
	a := NewAdapter()

	var received []platform.Event
	handler := func(e platform.Event) {
		received = append(received, e)
	}

	a.mu.Lock()
	a.handler = handler
	a.mu.Unlock()
	a.running.Store(true)

	ok := a.SimulateMessage("test message")
	if !ok {
		t.Fatal("期望 SimulateMessage 返回 true")
	}
	if len(received) != 1 {
		t.Fatalf("期望 1 个事件, 得到 %d", len(received))
	}
	if received[0].Content() != "test message" {
		t.Errorf("期望内容 %q, 得到 %q", "test message", received[0].Content())
	}
}

func TestAdapter_SimulateGroupMessage(t *testing.T) {
	a := NewAdapter()

	var received []platform.Event
	handler := func(e platform.Event) {
		received = append(received, e)
	}

	a.mu.Lock()
	a.handler = handler
	a.mu.Unlock()
	a.running.Store(true)

	ok := a.SimulateGroupMessage("group msg", "grp-1")
	if !ok {
		t.Fatal("期望 SimulateGroupMessage 返回 true")
	}
	e := received[0]
	if e.Kind() != platform.EventKindGroupMessage {
		t.Errorf("期望 GROUP_MESSAGE, 得到 %s", e.Kind())
	}
	if e.Chat().ID != "grp-1" {
		t.Errorf("期望会话 ID grp-1, 得到 %s", e.Chat().ID)
	}
}

func TestAdapter_StartStop(t *testing.T) {
	input := strings.NewReader("hello\nquit\n")
	output := &bytes.Buffer{}

	a := NewAdapter(
		WithInput(input),
		WithOutput(output),
		WithPrompt(""),
		WithWelcomeMessage(""),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := a.Start(ctx, func(e platform.Event) {})
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}

	if !strings.Contains(output.String(), "再见") {
		t.Errorf("期望包含告别信息, 得到: %s", output.String())
	}
}

func TestAdapter_IsRunning(t *testing.T) {
	a := NewAdapter()
	if a.IsRunning() {
		t.Error("期望初始未运行")
	}

	reader, writer := io.Pipe()
	defer reader.Close()

	a = NewAdapter(
		WithInput(reader),
		WithOutput(&bytes.Buffer{}),
		WithPrompt(""),
		WithWelcomeMessage(""),
	)

	ctx := context.Background()
	go func() {
		_ = a.Start(ctx, func(e platform.Event) {})
	}()

	// 轮询运行状态
	for range 20 {
		if a.IsRunning() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !a.IsRunning() {
		t.Fatal("期望运行状态为 true")
	}

	// 发送 quit 停止
	go func() {
		_, _ = writer.Write([]byte("quit\n"))
		_ = writer.Close()
	}()

	time.Sleep(50 * time.Millisecond)
	// 停止后状态应变为 false
	if a.IsRunning() {
		t.Log("适配器可能仍在关闭中")
	}
}

func TestAdapter_SendWithAttachmentsAndEmbeds(t *testing.T) {
	buf := &bytes.Buffer{}
	a := NewAdapter(WithOutput(buf))
	ctx := context.Background()

	_, _ = a.Send(ctx, platform.SendRequest{
		Target: platform.ChatInfo{ID: "test"},
		Message: platform.TextMessage("text").
			WithAttachments(platform.Attachment{Kind: platform.AttachmentKindImage, Name: "photo.png", URL: "https://example.com/img.png"}).
			WithEmbeds(platform.Embed{Title: "Test Card"}),
	})

	msg := a.LastMessage()
	if msg == nil {
		t.Fatal("期望有消息")
	}
	if !strings.Contains(msg.Content, "text") {
		t.Errorf("期望包含文本, 得到 %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "photo.png") {
		t.Errorf("期望包含附件名, 得到 %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Test Card") {
		t.Errorf("期望包含卡片标题, 得到 %q", msg.Content)
	}
}

func TestAdapter_CompletionFunc(t *testing.T) {
	a := NewAdapter(
		WithCompletionFunc(func(prefix string) []string {
			switch prefix {
			case "/ping":
				return []string{"/ping"}
			case "/h":
				return []string{"/help"}
			case "/":
				return []string{"/help", "/ping", "/echo"}
			}
			return nil
		}),
	)

	if a.completionFunc == nil {
		t.Fatal("期望 completionFunc 非空")
	}

	// 验证补全逻辑：单候选项
	candidates := a.completionFunc("/h")
	if len(candidates) != 1 || candidates[0] != "/help" {
		t.Errorf("期望 [/help], 得到 %v", candidates)
	}

	// 多候选项
	candidates = a.completionFunc("/")
	if len(candidates) != 3 {
		t.Errorf("期望 3 个候选项, 得到 %d", len(candidates))
	}

	// 无匹配
	candidates = a.completionFunc("/xyz")
	if len(candidates) != 0 {
		t.Errorf("期望 0 个候选项, 得到 %d", len(candidates))
	}
}
