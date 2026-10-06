package dealornodeal

import (
	"slices"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
)

func TestDescriptor_Meta(t *testing.T) {
	desc := New()
	if err := desc.Validate(); err != nil {
		t.Fatalf("Descriptor 校验失败: %v", err)
	}
	if desc.Name != pluginName {
		t.Errorf("Name = %q, 期望 %q", desc.Name, pluginName)
	}
	if desc.Meta == nil || desc.Meta.HelpText == "" {
		t.Fatal("应包含 Meta.HelpText")
	}
	if !slices.Contains(desc.Deps, "i18n") {
		t.Fatal("应声明 i18n 依赖")
	}
	if desc.Setup == nil {
		t.Fatal("应设置 Setup")
	}
}

func TestSetup_WiresDependencies(t *testing.T) {
	svc := i18n.NewPlugin(i18n.Config{DefaultLocale: localeZH, Fallback: localeZH})
	ctx := plugintest.NewSetupContextWithDeps(pluginName, map[string]any{"i18n": svc}, nil)
	defer plugintest.StopSetupContext(ctx)

	api, err := New().Setup(ctx)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	p, ok := api.(*Plugin)
	if !ok {
		t.Fatalf("Setup 返回类型 = %T, 期望 *Plugin", api)
	}
	if p.sessions == nil {
		t.Fatal("Setup 后 sessions 不应为空")
	}
	if p.cfg.DefaultScope != ScopeUser {
		t.Fatalf("默认维度应为单人 user, 实际 %v", p.cfg.DefaultScope)
	}
	if p.cfg.Currency != "$" {
		t.Fatalf("默认货币前缀应为 $, 实际 %q", p.cfg.Currency)
	}
	if p.cfg.Cases != CaseCount {
		t.Fatalf("默认箱数应为 %d, 实际 %d", CaseCount, p.cfg.Cases)
	}
	if p.cfg.TTL != 30*time.Minute {
		t.Fatalf("默认 TTL 应为 30m, 实际 %v", p.cfg.TTL)
	}
	// 语言包已在 Setup 中合并进 i18n。
	if got := svc.Tf(localeZH, "dond.button.deal", nil); got == "" || got == "dond.button.deal" {
		t.Errorf("Setup 后 zh-CN 缺少 dond.button.deal")
	}
	if got := svc.Tf(localeEN, "dond.help", nil); got == "" || got == "dond.help" {
		t.Errorf("Setup 后 en-US 缺少 dond.help")
	}
}

func TestClampSessionTTL(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		MinSessionTTL:    MinSessionTTL,
		30 * time.Minute: 30 * time.Minute,
		time.Second:      MinSessionTTL,
		5 * time.Second:  MinSessionTTL,
		48 * time.Hour:   MaxSessionTTL,
	}
	for in, want := range cases {
		if got := clampSessionTTL(in, nil); got != want {
			t.Errorf("clampSessionTTL(%v) = %v, 期望 %v", in, got, want)
		}
	}
}
