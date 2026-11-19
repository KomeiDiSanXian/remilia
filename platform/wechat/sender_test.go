package wechat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// mirrorCapabilities 把 CapabilityFlag 位掩码与 Capabilities 字段对齐检查。
//
// 与 platform/consistency_test.go 同口径，但覆盖 wechat（后者不在其
// testAdapters 里：NewAdapter 需要真实配置）。
func mirrorCapabilities(t *testing.T, caps platform.Capabilities, flags platform.CapabilityFlag) {
	t.Helper()
	if caps.Has(flags) != true {
		t.Fatalf("Capabilities.Has(%b) = false，字段与位掩码不一致: %+v", flags, caps)
	}
}

// TestCapabilitiesFlagConsistency 能力声明与实际接口一致。
func TestCapabilitiesFlagConsistency(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	caps := a.Capabilities()
	mirrorCapabilities(t, caps, platform.CapMarkdown|platform.CapButtons|platform.CapFileUpload)

	if caps.Has(platform.CapMessageEdit) || caps.Has(platform.CapMessageDelete) ||
		caps.Has(platform.CapReactions) || caps.Has(platform.CapTypingIndicator) {
		t.Fatalf("不应声明编辑/撤回/表情/正在输入: %+v", caps)
	}
}

// TestMediaOrigins 声明了企业微信媒体直链域名。
func TestMediaOrigins(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	origins := a.MediaOrigins()
	if len(origins) == 0 {
		t.Fatal("MediaOrigins 不应为空")
	}
	found := false
	for _, o := range origins {
		if strings.Contains(o, "ww-aibot-img") {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少企业微信媒体直链域名: %v", origins)
	}
}

// TestSenderSendWithoutTokens 没有回复授权时必须明确报错。
//
// 这是最容易踩的坑：handler 里直接 ctx.Reply() 而事件不带授权信息时，
// 静默成功会让人以为"发出去但平台没显示"。
func TestSenderSendWithoutTokens(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	sender := a.Sender()

	_, err = sender.Send(t.Context(), platform.SendRequest{
		Target:  platform.ChatInfo{ID: "chat-1"},
		Message: platform.TextMessage("hello"),
	})
	if err == nil {
		t.Fatal("没有回复授权时应报错")
	}
	se, ok := platform.AsSendError(err)
	if !ok || se.Code != platform.SendErrInvalidTarget {
		t.Fatalf("错误码 = %v，期望 SendErrInvalidTarget", se)
	}
}

// TestSenderRejectsInvalidRequest SendRequest.Validate 的错误直接透传。
func TestSenderRejectsInvalidRequest(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	sender := a.Sender()

	if _, err := sender.Send(t.Context(), platform.SendRequest{
		Message: platform.TextMessage("hello"),
	}); err == nil {
		t.Fatal("缺少 Target.ID 应报错")
	}
}

// TestBuildTemplateCard 按钮映射成按钮交互型模板卡片。
func TestBuildTemplateCard(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	s := a.sender

	msg := platform.TextMessage("构建完成\n耗时 3s").WithButtons(
		platform.Button{ID: "retry", Label: "重试"},
		platform.Button{ID: "docs", Label: "文档", URL: "https://example.com"},
	)
	body, err := s.buildBody(t.Context(), msg)
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	if body.MsgType != "template_card" {
		t.Fatalf("msgtype = %q, want template_card", body.MsgType)
	}
	if body.TemplateCard == nil {
		t.Fatal("TemplateCard 不应为 nil")
	}
	card := *body.TemplateCard
	if card.MainTitle.Title != "构建完成" {
		t.Fatalf("标题 = %q", card.MainTitle.Title)
	}
	if card.SubTitle != "耗时 3s" {
		t.Fatalf("副标题 = %q", card.SubTitle)
	}
	if len(card.ButtonList) != 2 {
		t.Fatalf("按钮数量 = %d", len(card.ButtonList))
	}
	if card.ButtonList[0].Key != "retry" || card.ButtonList[0].Type != 0 {
		t.Fatalf("回调按钮映射错误: %+v", card.ButtonList[0])
	}
	if card.ButtonList[1].Type != 1 || card.ButtonList[1].URL != "https://example.com" {
		t.Fatalf("链接按钮映射错误: %+v", card.ButtonList[1])
	}
	if card.TaskID == "" {
		t.Fatal("TaskID 不应为空")
	}
}

// TestBuildBodyTooLong 超过 20480 字节必须报错而不是截断。
func TestBuildBodyTooLong(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	long := make([]byte, maxContentBytes+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err = a.sender.buildBody(t.Context(), platform.TextMessage(string(long)))
	if err == nil {
		t.Fatal("超长正文应报错")
	}
	if !platform.IsSendErrorCode(err, platform.SendErrMsgTooLong) {
		t.Fatalf("错误码应为 SendErrMsgTooLong，实际 %v", err)
	}
}

// TestBuildBodyEmpty 纯空消息返回 nil body（由调用方判定"无需发送"）。
func TestBuildBodyEmpty(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	body, err := a.sender.buildBody(t.Context(), platform.OutboundMessage{})
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	if body != nil {
		t.Fatalf("空消息应返回 nil body，实际 %+v", body)
	}
}

// TestSendErrCodeFor 企业微信 errcode 到标准错误码的映射。
//
// 映射决定了上层能否区分"退避重试有用"（频率限制）与"改配置才行"
// （凭证/会话不存在）；错分会让重试白跑或直接放弃可恢复错误。
func TestSendErrCodeFor(t *testing.T) {
	cases := map[int]platform.SendErrorCode{
		errCodeRateLimit:         platform.SendErrRateLimit,
		errCodeFrequencyLimit:    platform.SendErrRateLimit,
		errCodeContentTooLong:    platform.SendErrMsgTooLong,
		errCodeInvalidCredential: platform.SendErrTokenExpired,
		errCodeInvalidToken:      platform.SendErrTokenExpired,
		errCodeInvalidUserID:     platform.SendErrInvalidTarget,
		errCodeUserNotInChat:     platform.SendErrInvalidTarget,
		93000:                    platform.SendErrPlatform,
	}
	for code, want := range cases {
		if got := sendErrCodeFor(code); got != want {
			t.Errorf("sendErrCodeFor(%d) = %v, want %v", code, got, want)
		}
	}
}

// TestTemplateCardButtonLimit 卡片按钮超过平台上限时截断而不是丢整张卡片。
func TestTemplateCardButtonLimit(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	buttons := make([]platform.Button, 8)
	for i := range buttons {
		buttons[i] = platform.Button{ID: fmt.Sprintf("b%d", i), Label: "x"}
	}
	body, err := a.sender.buildBody(t.Context(), platform.TextMessage("t").WithButtons(buttons...))
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	if body.TemplateCard == nil {
		t.Fatal("应仍生成卡片")
	}
	if got := len(body.TemplateCard.ButtonList); got != 6 {
		t.Fatalf("按钮数 = %d, want 6（平台上限）", got)
	}
}
