package ai

import (
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// dynamicToolProvider 模拟"工具集合会运行时变化"的来源，用于锁定目录对动态
// 来源的同步契约（实现 ToolProvider + ToolChangeNotifier）。
type dynamicToolProvider struct {
	mu    sync.Mutex
	tools []toolkit.Tool
	cb    func()
}

func (d *dynamicToolProvider) ListTools() []toolkit.Tool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]toolkit.Tool(nil), d.tools...)
}

func (d *dynamicToolProvider) OnToolsChanged(fn func()) {
	d.mu.Lock()
	d.cb = fn
	d.mu.Unlock()
}

// set 替换工具集合并触发变化通知（模拟来源刷新）。
func (d *dynamicToolProvider) set(tools ...toolkit.Tool) {
	d.mu.Lock()
	d.tools = tools
	cb := d.cb
	d.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// TestDynamicToolProviderSyncsOnChange 验证动态来源的变化被同步进目录：
// 新增登记、消失移除，且来源身份在注册表往返后保真。
func TestDynamicToolProviderSyncsOnChange(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry()}
	dp := &dynamicToolProvider{tools: []toolkit.Tool{
		{Name: "search", Description: "v1", Source: toolkit.MCPSource("github")},
	}}
	p.RegisterToolProvider(dp)

	got, ok := p.reg.Get("search")
	if !ok {
		t.Fatal("expected provider tool to be registered")
	}
	if got.Source != toolkit.MCPSource("github") {
		t.Fatalf("source not preserved: %q", got.Source)
	}

	// 来源新增工具：目录应出现新工具。
	dp.set(
		toolkit.Tool{Name: "search", Description: "v1", Source: toolkit.MCPSource("github")},
		toolkit.Tool{Name: "fetch", Description: "v1", Source: toolkit.MCPSource("github")},
	)
	if _, ok := p.reg.Get("fetch"); !ok {
		t.Fatal("expected added tool to appear in catalog")
	}

	// 来源移除工具：目录应同步移除。
	dp.set(toolkit.Tool{Name: "fetch", Description: "v1", Source: toolkit.MCPSource("github")})
	if _, ok := p.reg.Get("search"); ok {
		t.Fatal("expected removed tool to disappear from catalog")
	}
	if _, ok := p.reg.Get("fetch"); !ok {
		t.Fatal("expected remaining tool to stay")
	}
}

// TestCatalogGenerationAdvancesOnlyOnMembershipChange 锁定代数语义：只有成员
// 关系变化（增/删）才推进；重连同集合同名改 schema 都不推进——这是"连接抖动
// 不击穿选择缓存"的前提。
func TestCatalogGenerationAdvancesOnlyOnMembershipChange(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry()}
	dp := &dynamicToolProvider{tools: []toolkit.Tool{
		{Name: "search", Description: "v1", Source: toolkit.MCPSource("github")},
	}}
	p.RegisterToolProvider(dp)
	base := p.catalogGeneration()
	if base == 0 {
		t.Fatal("expected generation to advance after initial registration")
	}

	// 重连/刷新但集合同名同集：不推进。
	dp.set(toolkit.Tool{Name: "search", Description: "v1", Source: toolkit.MCPSource("github")})
	if got := p.catalogGeneration(); got != base {
		t.Fatalf("resync with identical membership advanced generation: %d→%d", base, got)
	}

	// 同名改 schema（定义变化但成员关系不变）：仍不推进。
	dp.set(toolkit.Tool{Name: "search", Description: "v2 changed", Source: toolkit.MCPSource("github")})
	if got := p.catalogGeneration(); got != base {
		t.Fatalf("same-name schema change advanced generation: %d→%d", base, got)
	}
	if tool, ok := p.reg.Get("search"); !ok || tool.Description != "v2 changed" {
		t.Fatalf("in-place definition update not applied: %+v", tool)
	}

	// 成员新增：推进。
	dp.set(
		toolkit.Tool{Name: "search", Description: "v2 changed", Source: toolkit.MCPSource("github")},
		toolkit.Tool{Name: "fetch", Source: toolkit.MCPSource("github")},
	)
	if got := p.catalogGeneration(); got <= base {
		t.Fatalf("membership addition did not advance generation: %d→%d", base, got)
	}
}

// TestCatalogSnapshotCarriesGeneration 目录快照与代数一致，且是聚合后的只读视图。
func TestCatalogSnapshotCarriesGeneration(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry()}
	p.registerCatalogTool(toolkit.Tool{Name: "only"})

	snap := p.catalogSnapshot()
	if snap.Generation() != p.catalogGeneration() {
		t.Fatalf("snapshot generation %d != catalog generation %d", snap.Generation(), p.catalogGeneration())
	}
	if _, ok := snap.Lookup("only"); !ok {
		t.Fatal("snapshot must contain the registered action")
	}
}

// TestRegisterCatalogToolIsIdempotentForGeneration 重复登记同一名字不推进代数，
// 因为成员关系未变。
func TestRegisterCatalogToolIsIdempotentForGeneration(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry()}
	p.registerCatalogTool(toolkit.Tool{Name: "x"})
	gen := p.catalogGeneration()
	p.registerCatalogTool(toolkit.Tool{Name: "x"})
	if p.catalogGeneration() != gen {
		t.Fatal("re-registering the same name must not advance generation")
	}
}
