package i18n_test

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// i18n.Plugin 的初始化（含默认值设置）全在 NewPlugin() 中完成，
// 直接构造即可，无需走 manager 注册流程。
func newI18nPlugin(cfg i18n.Config) *i18n.Plugin {
	return i18n.NewPlugin(cfg)
}
func makePlainCtx() *context.Context {
	return context.NewContextFromEvent(&mockPlainEvent{}, nil)
}

// mockPlainEvent is a minimal platform.Event for i18n tests.
type mockPlainEvent struct{}

func (e *mockPlainEvent) Platform() string                          { return "test" }
func (e *mockPlainEvent) Kind() platform.EventKind                  { return platform.EventKindPrivateMessage }
func (e *mockPlainEvent) RawType() string                           { return "PRIVATE_MESSAGE" }
func (e *mockPlainEvent) Content() string                           { return "" }
func (e *mockPlainEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{} }
func (e *mockPlainEvent) Sender() platform.UserInfo                 { return platform.UserInfo{} }
func (e *mockPlainEvent) Timestamp() time.Time                      { return time.Time{} }
func (e *mockPlainEvent) ID() string                                { return "" }
func (e *mockPlainEvent) RawPayload() any                           { return nil }
func (e *mockPlainEvent) Attachments() []platform.InboundAttachment { return nil }
func TestI18n_LoadBytes_T(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "zh-CN"})
	if err := p.LoadBytes("zh-CN", []byte("help: \"帮助菜单\"")); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if got := p.T(makePlainCtx(), "help"); got != "帮助菜单" {
		t.Errorf("expected '帮助菜单', got %q", got)
	}
}
func TestI18n_Template(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "zh-CN"})
	p.LoadBytes("zh-CN", []byte("welcome: \"欢迎, {{.name}}！\""))
	if got := p.T(makePlainCtx(), "welcome", map[string]any{"name": "Alice"}); got != "欢迎, Alice！" {
		t.Errorf("unexpected: %q", got)
	}
}
func TestI18n_Fallback(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "zh-CN", Fallback: "zh-CN"})
	p.LoadBytes("zh-CN", []byte("foo: bar"))
	ctx := makePlainCtx()
	p.SetLocale(ctx, "en-US") // en-US 未加载，回退到 zh-CN
	if got := p.T(ctx, "foo"); got != "bar" {
		t.Errorf("expected 'bar', got %q", got)
	}
}
func TestI18n_MissingKey_ReturnKey(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "zh-CN"})
	if got := p.T(makePlainCtx(), "nonexistent.key"); got != "nonexistent.key" {
		t.Errorf("expected key as fallback, got %q", got)
	}
}
func TestI18n_SetLocale(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "zh-CN"})
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
	p := newI18nPlugin(i18n.Config{DefaultLocale: "en"})
	p.LoadBytes("en", []byte("msg: \"Dear {{.user}}\""))
	if got := p.Tf("en", "msg", map[string]any{"user": "Bob"}); got != "Dear Bob" {
		t.Errorf("expected 'Dear Bob', got %q", got)
	}
}

// TestI18n_TemplateCache 验证模板被缓存（重复调用 T 不重复 Parse）
func TestI18n_TemplateCache(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "en"})
	p.LoadBytes("en", []byte("tpl: \"Hello {{.name}}\""))
	ctx := makePlainCtx()

	// 多次调用，应全部返回正确结果（命中缓存）
	for i := range 5 {
		if got := p.T(ctx, "tpl", map[string]any{"name": "World"}); got != "Hello World" {
			t.Errorf("call %d: expected 'Hello World', got %q", i, got)
		}
	}

	// 重新加载语言包后，旧缓存应失效
	p.LoadBytes("en", []byte("tpl: \"Hi {{.name}}\""))
	if got := p.T(ctx, "tpl", map[string]any{"name": "World"}); got != "Hi World" {
		t.Errorf("after reload: expected 'Hi World', got %q", got)
	}
}

// TestI18n_Tn_Plural 验证复数形式 Tn API
func TestI18n_Tn_Plural(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "en"})
	yaml := "items.zero: \"no items\"\nitems.one: \"{{.Count}} item\"\nitems.other: \"{{.Count}} items\"\n"
	p.LoadBytes("en", []byte(yaml))
	ctx := makePlainCtx()

	cases := []struct {
		count    int
		expected string
	}{
		{0, "no items"},
		{1, "1 item"},
		{2, "2 items"},
		{5, "5 items"},
		{100, "100 items"},
	}
	for _, c := range cases {
		got := p.Tn(ctx, "items", c.count, nil)
		if got != c.expected {
			t.Errorf("Tn(items, %d): expected %q, got %q", c.count, c.expected, got)
		}
	}
}

// TestI18n_Tn_FallbackToOther 当特定复数 key 不存在时应 fallback 到 .other
func TestI18n_Tn_FallbackToOther(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "en"})
	p.LoadBytes("en", []byte("items.other: \"{{.Count}} items\""))
	ctx := makePlainCtx()

	// .zero / .one 不存在，应该 fallback 到 .other
	if got := p.Tn(ctx, "items", 0, nil); got != "0 items" {
		t.Errorf("Tn fallback .zero → .other: expected '0 items', got %q", got)
	}
	if got := p.Tn(ctx, "items", 1, nil); got != "1 items" {
		t.Errorf("Tn fallback .one → .other: expected '1 items', got %q", got)
	}
}

// TestI18n_Tn_CustomArgs 验证 Tn 可以传入额外参数
func TestI18n_Tn_CustomArgs(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "en"})
	p.LoadBytes("en", []byte("files.other: \"{{.Count}} files in {{.Dir}}\""))
	ctx := makePlainCtx()

	got := p.Tn(ctx, "files", 3, map[string]any{"Dir": "/tmp"})
	if got != "3 files in /tmp" {
		t.Errorf("expected '3 files in /tmp', got %q", got)
	}
}

// TestI18n_Tn_MissingKey 当 key 不存在时 Tn 应返回 key 本身
func TestI18n_Tn_MissingKey(t *testing.T) {
	p := newI18nPlugin(i18n.Config{DefaultLocale: "en"})
	ctx := makePlainCtx()
	if got := p.Tn(ctx, "no.such.key", 1, nil); got != "no.such.key" {
		t.Errorf("expected key fallback, got %q", got)
	}
}
