package telemetry

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestCommandCounter 验证命令使用统计（telemetry 中间件）。
func TestCommandCounter(t *testing.T) {
	before := testutil.ToFloat64(commands.WithLabelValues("testcmd"))

	// 预置已解析命令（命令规则在 handler 前运行并缓存到 ctx）
	ctx := context.NewContextFromEvent(nil, nil)
	ctx.SetParsedCommand(&command.Parsed{
		CommandPath: []string{"testcmd"},
		Definition:  &command.Definition{Name: "testcmd"},
	})

	mw := PrometheusMetrics("remilia")
	handler := mw(func(ctx *context.Context) error { return nil })
	if err := handler(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if after := testutil.ToFloat64(commands.WithLabelValues("testcmd")); after != before+1 {
		t.Errorf("命令计数: before=%v after=%v", before, after)
	}
}
