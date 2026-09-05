package ai

import (
	"context"
	"testing"
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
