package help

import (
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
)

// ...existing code...

func TestRenderHelpImage_ContainsValidPNG(t *testing.T) {
	png, err := renderHelpImage(sampleHelpPage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// PNG magic bytes \x89PNG
	if string(png[:4]) != "\x89PNG" {
		t.Fatalf("output is not a valid PNG (first 4 bytes: %x)", png[:4])
	}
	// Watermark is baked into the image – just ensure the PNG is non-trivially sized
	if len(png) < 1024 {
		t.Fatalf("PNG too small (%d bytes), watermark probably missing", len(png))
	}
}

func TestFormatImageDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Nanosecond, "0us"},
		{800 * time.Microsecond, "800us"},
		{120 * time.Millisecond, "120ms"},
		{1500 * time.Millisecond, "1.50s"},
		{2*time.Second + 300*time.Millisecond, "2.30s"},
	}
	for _, c := range cases {
		got := formatImageDuration(c.d)
		if got != c.want {
			t.Errorf("formatImageDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestWithImageRender_FlagOff(t *testing.T) {
	p := newHelpPluginInternal()
	if !p.imageRender {
		t.Fatal("imageRender should be true by default")
	}

	WithImageRender(false)(p)
	if p.imageRender {
		t.Fatal("imageRender should be false after WithImageRender(false)")
	}

	WithImageRender(true)(p)
	if !p.imageRender {
		t.Fatal("imageRender should be true after WithImageRender(true)")
	}
}

func TestNewPlugin_WithImageRenderOption(t *testing.T) {
	desc := New(WithImageRender(false))
	if desc == nil {
		t.Fatal("New() returned nil descriptor")
	}
}

func TestForceTextFlag_Parsing(t *testing.T) {
	// Verify that Args.GetFlagBool correctly reads --text / -t
	cases := []struct {
		input     string
		wantForce bool
	}{
		{"/help", false},
		{"/help 2", false},
		{"/help --text", true},
		{"/help -t", true},
		{"/help weather --text", true},
		{"/help 2 -t", true},
		{"/help --text=false", false}, // explicit false
	}
	for _, c := range cases {
		args, err := command.ParseCommandLine(c.input)
		if err != nil {
			t.Errorf("ParseCommandLine(%q) error: %v", c.input, err)
			continue
		}
		got := args.GetFlagBool("text") || args.GetFlagBool("t")
		if got != c.wantForce {
			t.Errorf("forceText(%q) = %v, want %v", c.input, got, c.wantForce)
		}
	}
}

// sampleHelpPage mimics the output of showCommandsPage
const sampleHelpPage = `📖 可用命令列表 (第 1/2 页)
==============================

【系统】
  /help (h, help)
    查看可用命令和插件信息

  /status
    显示系统状态与运行时间

【实用工具】
  /weather <城市>
    查询指定城市的天气信息

==============================
💡 使用方法:
  /help <命令名> - 查看命令详情
  /help <页码> - 查看其他页(共 2 页)

📊 统计: 共 3 个命令`

// samplePluginList mimics the output of showAllPlugins
const samplePluginList = `📦 已加载插件列表 (共 2 个)
==============================

【系统】
  🔌 help v2.0.0
     提供命令和插件的帮助信息查询功能
     👤 Remilia | 🏷️  帮助, 文档, 命令

==============================
💡 使用方法:
  /help <插件名> - 查看插件的详细信息和命令
  /help <命令名> - 查看命令详情`

// sampleCommandDetail mimics the output of showCommandDetail
const sampleCommandDetail = `📝 命令详情
==============================

命令: /help
别名: h, help
插件: help
分类: 系统

描述:
  查看可用命令和插件信息

用法:
  /help [页码|命令名|插件名]

示例:
  /help
  /help 2
  /help myPlugin`

// sampleNotFound mimics the output of showCommandNotFound
const sampleNotFound = `❌ 未找到: foo

💡 你可能想找:
  /help
  /status`

func TestRenderHelpImage_CommandsPage(t *testing.T) {
	png, err := renderHelpImage(sampleHelpPage)
	if err != nil {
		t.Fatalf("renderHelpImage (commands page) error: %v", err)
	}
	if len(png) < 100 {
		t.Fatalf("expected a real PNG, got %d bytes", len(png))
	}
	// PNG magic bytes
	if string(png[:4]) != "\x89PNG" {
		t.Fatalf("output is not a valid PNG (first 4 bytes: %x)", png[:4])
	}
}

func TestRenderHelpImage_PluginList(t *testing.T) {
	png, err := renderHelpImage(samplePluginList)
	if err != nil {
		t.Fatalf("renderHelpImage (plugin list) error: %v", err)
	}
	if string(png[:4]) != "\x89PNG" {
		t.Fatalf("output is not a valid PNG")
	}
}

func TestRenderHelpImage_CommandDetail(t *testing.T) {
	png, err := renderHelpImage(sampleCommandDetail)
	if err != nil {
		t.Fatalf("renderHelpImage (command detail) error: %v", err)
	}
	if string(png[:4]) != "\x89PNG" {
		t.Fatalf("output is not a valid PNG")
	}
}

func TestRenderHelpImage_NotFound(t *testing.T) {
	png, err := renderHelpImage(sampleNotFound)
	if err != nil {
		t.Fatalf("renderHelpImage (not found) error: %v", err)
	}
	if string(png[:4]) != "\x89PNG" {
		t.Fatalf("output is not a valid PNG")
	}
}

func TestRenderHelpImage_EmptyText(t *testing.T) {
	// Should not panic; may produce a minimal image or an error
	_, _ = renderHelpImage("")
}

func TestCleanForImage_EmojiReplacement(t *testing.T) {
	cases := []struct {
		input     string
		noGarbage bool
	}{
		{"📖 命令列表", true},
		{"💡 提示: 使用 /help", true},
		{"❌ 未找到: foo", true},
		{"👤 作者 | 🏷️ 标签", true},
	}
	for _, c := range cases {
		out := cleanForImage(c.input)
		// All remaining runes should be renderable (ASCII, CJK, geometric shapes, etc.)
		for _, r := range out {
			if r > 0x1FFFF {
				t.Errorf("cleanForImage(%q) left high codepoint U+%04X in output %q", c.input, r, out)
			}
		}
	}
}

func TestFilterUnsupportedRunes_Passthrough(t *testing.T) {
	safe := "Hello, 世界! ─────"
	out := filterUnsupportedRunes(safe)
	if out != safe {
		t.Errorf("filterUnsupportedRunes(%q) changed safe string to %q", safe, out)
	}
}

func TestFilterUnsupportedRunes_StripsEmoji(t *testing.T) {
	in := "start📖end"
	out := filterUnsupportedRunes(in)
	if strings.Contains(out, "📖") {
		t.Errorf("filterUnsupportedRunes should have stripped 📖, got %q", out)
	}
	if !strings.Contains(out, "start") || !strings.Contains(out, "end") {
		t.Errorf("filterUnsupportedRunes stripped ASCII chars unexpectedly: %q", out)
	}
}
