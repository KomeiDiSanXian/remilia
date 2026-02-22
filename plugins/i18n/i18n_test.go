package i18n_test

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/i18n"
)

func newI18nPlugin(t *testing.T, cfg i18n.Config) *i18n.Plugin {
	t.Helper()
	p, desc := i18n.NewPlugin(cfg)
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)
	if err := pm.RegisterV2(desc); err != nil {
		t.Fatalf("register: %v", err)
	}
	return p
}
func makePlainCtx() *context.Context {
	detail, _ := json.Marshal(dto.C2CMessageCreateEvent{})
	return context.NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, nil)
}
func TestI18n_LoadBytes_T(t *testing.T) {
	p := newI18nPlugin(t, i18n.Config{DefaultLocale: "zh-CN"})
	if err := p.LoadBytes("zh-CN", []byte("help: \"帮助菜单\"")); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	ctx := makePlainCtx()
	if got := p.T(ctx, "help"); got != "帮助菜单" {
		t.Errorf("expected '帮助菜单', got %q", got)
	}
}
func TestI18n_Template(t *testing.T) {
	p := newI18nPlugin(t, i18n.Config{DefaultLocale: "zh-CN"})
	p.LoadBytes("zh-CN", []byte("welcome: \"欢迎, {{.name}}！\""))
	ctx := makePlainCtx()
	if got := p.T(ctx, "welcome", map[string]any{"name": "Alice"}); got != "欢迎, Alice！" {
		t.Errorf("unexpected: %q", got)
	}
}
func TestI18n_Fallback(t *testing.T) {
	p := newI18nPlugin(t, i18n.Config{DefaultLocale: "zh-CN", Fallback: "zh-CN"})
	p.LoadBytes("zh-CN", []byte("foo: bar"))
	ctx := makePlainCtx()
	p.SetLocale(ctx, "en-US")
	if got := p.T(ctx, "foo"); got != "bar" {
		t.Errorf("expected fallback 'bar', got %q", got)
	}
}
func TestI18n_MissingKey_ReturnKey(t *testing.T) {
	p := newI18nPlugin(t, i18n.Config{DefaultLocale: "zh-CN"})
	ctx := makePlainCtx()
	if got := p.T(ctx, "nonexistent.key"); got != "nonexistent.key" {
		t.Errorf("expected key as fallback, got %q", got)
	}
}
func TestI18n_SetLocale(t *testing.T) {
	p := newI18nPlugin(t, i18n.Config{DefaultLocale: "zh-CN"})
	p.LoadBytes("zh-CN", []byte("hi: 你好"))
	p.LoadBytes("en-US", []byte("hi: Hello"))
	ctx := makePlainCtx()
	p.SetLocale(ctx, "en-US")
	if p.GetLocale(ctx) != "en-US" {
		t.Error("expected locale en-US")
	}
	if got := p.T(ctx, "hi"); got != "Hello" {
		t.Errorf("expected 'Hello', got %q", got)
	}
}
func TestI18n_Tf(t *testing.T) {
	p := newI18nPlugin(t, i18n.Config{DefaultLocale: "en"})
	p.LoadBytes("en", []byte("msg: \"Dear {{.user}}\""))
	if got := p.Tf("en", "msg", map[string]any{"user": "Bob"}); got != "Dear Bob" {
		t.Errorf("expected 'Dear Bob', got %q", got)
	}
}
