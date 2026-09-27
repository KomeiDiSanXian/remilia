// Package ai metrics_invoker_test.go — 三种执行语义的指标覆盖契约。
//
// ai_tool_calls_total 此前只在 FuncInvoker 上报，命令通道与 Skill 子代理的
// 执行不进指标，导致"按工具名的调用/失败率"在这两条路径上系统性偏低。
// 本用例把"三条路径都上报"钉成可执行契约，并按增量断言（计数器进程级只增不减，
// 绝对值断言在 go test -count=N 下会失败）。
package ai

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestToolCallMetricCoversAllInvokerPaths 冻结"三种执行语义都上报
// ai_tool_calls_total"：普通动作、真实命令、Skill 各执行一次，
// ai_tool_calls_total{result=ok} 恰好在区间内 +3。
func TestToolCallMetricCoversAllInvokerPaths(t *testing.T) {
	const label = "metrics_probe_action"
	before := counterValue(toolCalls, label, "ok")

	evt := platform.NewSyntheticEvent("c2c", "test", platform.WithSyntheticChat(platform.ChatInfo{ID: "c1"}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	// 1) 普通动作（FuncInvoker）
	funcPlugin := &Plugin{reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}
	funcPlugin.reg.Register(toolkit.Tool{Name: label, Execute: func(context.Context, map[string]any) (string, error) {
		return "ok", nil
	}})
	r1 := funcPlugin.executeToolResult(ctx, protocol.ToolCall{Name: label}, context.Background(), &execution.CaptureSender{}, nil)
	if r1.Err != nil {
		t.Fatalf("普通动作应成功: %v", r1.Err)
	}

	// 2) 真实命令（CommandInvoker）：登记命令模式 → 走合成事件重放
	cmdPlugin := &Plugin{
		reg:         toolkit.NewToolRegistry(),
		skillReg:    toolkit.NewSkillRegistry(),
		cmdPatterns: map[string]string{label: "/probe"},
	}
	cmdPlugin.reg.Register(toolkit.Tool{Name: label})
	cmdPlugin.syncer = &fakeEventProcessor{onSync: func(sender platform.Sender) {
		_, _ = sender.Send(context.Background(), platform.SendRequest{
			Message: platform.OutboundMessage{Text: "ok"},
		})
	}}
	r2 := cmdPlugin.executeToolResult(ctx, protocol.ToolCall{Name: label}, context.Background(), &execution.CaptureSender{}, nil)
	if r2.Err != nil {
		t.Fatalf("真实命令应成功: %v", r2.Err)
	}

	// 3) Skill（SkillInvoker）
	skillPlugin := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		cfg:      &config.Config{SkillMaxDepth: 1, SkillTimeout: time.Minute},
		prov: &mockProvider{chatFn: func(context.Context, *protocol.ChatRequest) (*protocol.ChatResponse, error) {
			return &protocol.ChatResponse{Content: "done"}, nil
		}},
	}
	skillPlugin.skillReg.Register(toolkit.Skill{Name: label, OwnerID: toolkit.OwnerSystem, Prompt: "p", Enabled: true})
	r3 := skillPlugin.executeToolResult(ctx, protocol.ToolCall{Name: label}, context.Background(), &execution.CaptureSender{}, nil)
	if r3.Err != nil {
		t.Fatalf("Skill 应成功: %v", r3.Err)
	}

	checkDelta(t, "ai_tool_calls_total{result=ok}", before, counterValue(toolCalls, label, "ok"), 3)
}

// TestToolCallMetricCountsCommandFailure 冻结命令路径失败也计入指标（result=error），
// 与类型化 ActionResult.Err 的失败语义一致。
func TestToolCallMetricCountsCommandFailure(t *testing.T) {
	const label = "metrics_probe_cmd_fail"
	beforeErr := counterValue(toolCalls, label, "error")
	beforeOk := counterValue(toolCalls, label, "ok")

	evt := platform.NewSyntheticEvent("c2c", "test", platform.WithSyntheticChat(platform.ChatInfo{ID: "c1"}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	p := &Plugin{
		reg:         toolkit.NewToolRegistry(),
		skillReg:    toolkit.NewSkillRegistry(),
		cmdPatterns: map[string]string{label: "/probe"},
	}
	p.reg.Register(toolkit.Tool{Name: label})
	// syncer 为 nil → 命令模式已登记但无法重放，命令通道报错（不再回退占位成功）。
	got := p.executeToolResult(ctx, protocol.ToolCall{Name: label}, context.Background(), &execution.CaptureSender{}, nil)
	if got.Err == nil {
		t.Fatal("命令模式已登记但缺少事件处理器时必须失败，而不是回退成占位成功")
	}

	checkDelta(t, "ai_tool_calls_total{result=error}", beforeErr, counterValue(toolCalls, label, "error"), 1)
	checkDelta(t, "ai_tool_calls_total{result=ok}", beforeOk, counterValue(toolCalls, label, "ok"), 0)
}
