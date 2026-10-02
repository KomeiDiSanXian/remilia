package songdle

import (
	"path/filepath"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
)

// TestSetup_WiresDependencies 用真实的 i18n 与（临时文件）SQLite 走一遍 Setup，
// 验证依赖解析、建表、语言包注册与曲库加载。
func TestSetup_WiresDependencies(t *testing.T) {
	db, err := infrastorage.Open(infrastorage.WithDSN(filepath.Join(t.TempDir(), "songdle.db")))
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

	api, err := New().Setup(ctx)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	p, ok := api.(*Plugin)
	if !ok {
		t.Fatalf("Setup 返回类型 = %T, 期望 *Plugin", api)
	}
	if p.pool == nil || p.pool.Len() == 0 {
		t.Fatal("Setup 后曲库不应为空")
	}
	if p.sessions == nil || p.store == nil {
		t.Fatal("Setup 后 sessions/store 不应为空")
	}
	if p.cfg.DefaultTries != defaultTries ||
		p.cfg.MaxTries != defaultMaxTries ||
		p.cfg.DefaultScope != ScopeGroup ||
		p.cfg.DefaultType != "" ||
		p.cfg.DefaultGenre != "" ||
		p.cfg.RevealArtist {
		t.Fatalf("默认配置不符合预期: %+v", p.cfg)
	}

	// 语言包已在 Setup 中合并进 i18n。
	if got := svc.Tf(localeZH, "songdle.attr.title", nil); got == "" || got == "songdle.attr.title" {
		t.Errorf("Setup 后 zh-CN 缺少 songdle.attr.title")
	}
	if got := svc.Tf(localeEN, "songdle.board.legend_img", nil); got == "" || got == "songdle.board.legend_img" {
		t.Errorf("Setup 后 en-US 缺少 songdle.board.legend_img")
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
		t.Fatal("应声明 i18n 依赖")
	}
}

func TestCommandDef_Aliases(t *testing.T) {
	def := (&Plugin{}).commandDef()
	if def.Name != "songdle" {
		t.Fatalf("命令名 = %q", def.Name)
	}
	if len(def.Aliases) == 0 {
		t.Fatal("应提供别名（如 猜歌 / 猜曲 / sg）")
	}
	if len(def.SubCommands) == 0 || len(def.Flags) == 0 {
		t.Fatal("应包含子命令与标志")
	}
}
