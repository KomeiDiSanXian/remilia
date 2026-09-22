package toolkit

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestToolSourceRoundTrip(t *testing.T) {
	ctx := WithToolSource(context.Background(), ToolSource{
		UserID:   "u1",
		ChatID:   "g1",
		Platform: "qq",
		IsGroup:  true,
	})
	src, ok := ToolSourceFromContext(ctx)
	if !ok {
		t.Fatal("应能提取会话源信息")
	}
	if src.UserID != "u1" || src.ChatID != "g1" || src.Platform != "qq" || !src.IsGroup {
		t.Errorf("字段不匹配: %+v", src)
	}

	// 无注入时返回 false
	if _, ok := ToolSourceFromContext(context.Background()); ok {
		t.Error("无注入的 context 不应返回 ok")
	}
}

// TestToolInvocationCarriesSender 区分两条注入路径：只注入源信息时发送器为空，
// 注入调用信息时源信息与发送器同时可用；无注入时两者都不可用。
func TestToolInvocationCarriesSender(t *testing.T) {
	sender := stubSender{}
	ctx := WithToolInvocation(context.Background(), ToolSource{ChatID: "g1", IsGroup: true}, sender)

	src, ok := ToolSourceFromContext(ctx)
	if !ok || src.ChatID != "g1" || !src.IsGroup {
		t.Fatalf("调用信息应同时携带源信息: %+v ok=%v", src, ok)
	}
	got, ok := PlatformSenderFromContext(ctx)
	if !ok || got != platform.Sender(sender) {
		t.Fatalf("应能提取平台发送器: %v ok=%v", got, ok)
	}

	srcOnly := WithToolSource(context.Background(), ToolSource{ChatID: "g1"})
	if _, ok := PlatformSenderFromContext(srcOnly); ok {
		t.Error("只注入源信息时发送器不应可用")
	}
	if _, ok := PlatformSenderFromContext(context.Background()); ok {
		t.Error("无注入时发送器不应可用")
	}
}

// stubSender 只用于标识"同一个发送器被传了出去"，不实现任何平台行为。
type stubSender struct{}

func (stubSender) Send(context.Context, platform.SendRequest) (platform.SendResult, error) {
	return platform.SendResult{}, nil
}
