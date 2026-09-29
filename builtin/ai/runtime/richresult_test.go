package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

func TestFuncInvokerPrefersRich(t *testing.T) {
	inv := FuncInvoker{
		Name: "rich",
		Fn: func(context.Context, map[string]any) (string, error) {
			t.Fatal("plain Fn must not run when Rich is set")
			return "", nil
		},
		Rich: func(context.Context, map[string]any) (toolkit.ToolResult, error) {
			return toolkit.ToolResult{Parts: []toolkit.ResultPart{
				{Kind: toolkit.ResultText, Text: "回答"},
				{Kind: toolkit.ResultImage, URI: "https://x/y.png", MimeType: "image/png"},
			}}, nil
		},
	}
	got := inv.Invoke(context.Background())
	if got.Err != nil {
		t.Fatalf("unexpected err: %v", got.Err)
	}
	if got.Text != "回答\n[图片 (image/png) https://x/y.png]" {
		t.Fatalf("Text = %q", got.Text)
	}
	if len(got.Parts) != 2 {
		t.Fatalf("Parts not propagated: %+v", got.Parts)
	}
}

func TestFuncInvokerRichError(t *testing.T) {
	inv := FuncInvoker{
		Name: "rich",
		Rich: func(context.Context, map[string]any) (toolkit.ToolResult, error) {
			return toolkit.ToolResult{}, errors.New("boom")
		},
	}
	got := inv.Invoke(context.Background())
	if got.Err == nil {
		t.Fatal("expected typed error")
	}
	if got.Text != `错误: 工具 "rich" 执行失败: boom` {
		t.Fatalf("failure text = %q", got.Text)
	}
}
