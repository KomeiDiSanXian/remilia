package wordle

import (
	"path/filepath"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"

	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
)

// TestSetup_WiresDependencies 用一个真实的（临时文件）SQLite 与真实 i18n 实例
// 走一遍 Setup，验证依赖解析、建表、语言包注册与词库预加载。
func TestSetup_WiresDependencies(t *testing.T) {
	db, err := infrastorage.Open(infrastorage.WithDSN(filepath.Join(t.TempDir(), "wordle.db")))
	if err != nil {
		t.Fatalf("打开测试存储失败: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	svc := i18n.NewPlugin(i18n.Config{DefaultLocale: localeZH, Fallback: localeZH})
	ctx := plugintest.NewSetupContextWithDeps(pluginName, map[string]any{
		"i18n":    svc,
		"storage": db,
	}, nil)
	defer plugintest.StopSetupContext(ctx)

	desc := New()
	api, err := desc.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	p, ok := api.(*Plugin)
	if !ok {
		t.Fatalf("Setup 返回类型 = %T, 期望 *Plugin", api)
	}
	if p.sessions == nil || p.store == nil {
		t.Fatal("Setup 后 sessions/store 不应为空")
	}
	// 未配置 default_length/default_tries 时应为 0，表示"长度随机 4-7、次数按长度推导"。
	if p.cfg.DefaultLength != 0 || p.cfg.DefaultTries != 0 {
		t.Fatalf("默认配置应为未固定（0/0），实际 %+v", p.cfg)
	}
	if p.cfg.DefaultScope != ScopeGroup {
		t.Fatalf("默认隔离维度应为 group，实际 %v", p.cfg.DefaultScope)
	}
	if p.cfg.MaxFreeHints != 2 {
		t.Fatalf("默认免费提示上限应为 2，实际 %d", p.cfg.MaxFreeHints)
	}

	// 语言包已在 Setup 中合并进 i18n。
	if got := svc.Tf(localeZH, "wordle.button.guess", nil); got == "" || got == "wordle.button.guess" {
		t.Errorf("Setup 后 zh-CN 缺少 wordle.button.guess")
	}
}

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
	if len(desc.Deps) == 0 {
		t.Fatal("应声明 i18n/storage 依赖")
	}
}
