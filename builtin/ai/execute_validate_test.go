package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestExecuteToolRejectsInvalidArguments 参数不满足 Schema 时工具不得执行，
// 失败以工具级结果回填给模型；参数补齐后正常执行。
func TestExecuteToolRejectsInvalidArguments(t *testing.T) {
	p := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}
	called := false
	p.reg.Register(toolkit.Tool{
		Name: "needs_name",
		Parameters: protocol.ToolParamSchema{
			Type: "object",
			Properties: map[string]protocol.ToolParamSchema{
				"name": {Type: "string"},
			},
			Required: []string{"name"},
		},
		Execute: func(context.Context, *toolkit.Invocation, map[string]any) (toolkit.ToolResult, error) {
			called = true
			return toolkit.TextResult("ok"), nil
		},
	})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	res := p.executeToolResult(ctx, protocol.ToolCall{Name: "needs_name"},
		context.Background(), &execution.CaptureSender{}, nil)
	if res.Err == nil {
		t.Fatal("missing required argument should fail")
	}
	if called {
		t.Fatal("tool must not run when arguments are invalid")
	}
	if !strings.Contains(res.Text, "name") {
		t.Fatalf("error should locate the missing field: %q", res.Text)
	}

	res = p.executeToolResult(ctx, protocol.ToolCall{
		Name:      "needs_name",
		Arguments: map[string]any{"name": "remilia"},
	}, context.Background(), &execution.CaptureSender{}, nil)
	if res.Err != nil || !called {
		t.Fatalf("valid arguments should run the tool: %+v", res)
	}
	if res.Text != "ok" {
		t.Fatalf("result = %q, want ok", res.Text)
	}
}
