package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestCatalogGenerationMetrics 目录代数推进按原因计数、gauge 反映当前代数；
// 无成员变化的重同步不产生样本（保持信号不被稀释）。
func TestCatalogGenerationMetrics(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry()}
	dp := &dynamicToolProvider{tools: []toolkit.Tool{
		{Name: "metrics_tool", Source: toolkit.MCPSource("m")},
	}}

	addBefore := counterValue(catalogChanges, "add")
	removeBefore := counterValue(catalogChanges, "remove")

	p.RegisterToolProvider(dp)
	if got := counterValue(catalogChanges, "add") - addBefore; got != 1 {
		t.Fatalf("add delta = %v, want 1", got)
	}
	if got := testutil.ToFloat64(catalogGeneration); got != float64(p.catalogGeneration()) {
		t.Fatalf("catalog_generation gauge = %v, want %d", got, p.catalogGeneration())
	}

	// 无成员变化的重同步：不推进代数、不产生样本。
	addAfterRegister := counterValue(catalogChanges, "add")
	dp.set(toolkit.Tool{Name: "metrics_tool", Source: toolkit.MCPSource("m")})
	if got := counterValue(catalogChanges, "add") - addAfterRegister; got != 0 {
		t.Fatalf("identical resync must not advance generation, add delta = %v", got)
	}

	// 成员移除：按 remove 计数。
	dp.set()
	if got := counterValue(catalogChanges, "remove") - removeBefore; got != 1 {
		t.Fatalf("remove delta = %v, want 1", got)
	}
}
