package about

import (
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
)

func TestAboutDescriptor(t *testing.T) {
	d := New()
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Name != "about" {
		t.Errorf("expected name %q, got %q", "about", d.Name)
	}
	if d.Version != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", d.Version)
	}
	if d.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if d.Meta.Repository != RepositoryURL {
		t.Errorf("unexpected repository: %q", d.Meta.Repository)
	}
	if d.Meta.Description == "" {
		t.Error("Description is empty")
	}
}

func TestAboutSetup(t *testing.T) {
	d := New()
	if d.Setup == nil {
		t.Fatal("Setup is nil")
	}

	ctx := plugintest.NewSetupContext("about", nil)
	svc, err := d.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if svc == nil {
		t.Error("expected non-nil service (Plugin API)")
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{500 * time.Millisecond, "刚刚启动"},
		{5 * time.Second, "5秒"},
		{2 * time.Minute, "2分钟"},
		{1*time.Hour + 3*time.Minute + 7*time.Second, "1小时 3分钟 7秒"},
		{25*time.Hour + 30*time.Minute, "1天 1小时 30分钟"},
	}
	for _, c := range cases {
		if got := formatDuration(c.in); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildInfo(t *testing.T) {
	p := &Plugin{
		info: &plugintest.MockPluginInfo{
			Plugins: map[string]*plugin.Metadata{
				"ping": {Name: "ping"},
				"help": {Name: "help"},
			},
		},
		startTime: time.Now().Add(-2 * time.Minute),
	}
	md, text := p.buildInfo()
	if md == "" || text == "" {
		t.Fatal("buildInfo returned empty output")
	}
	for _, field := range []string{"框架版本", "Go 版本", "仓库", "已加载插件", "运行时长", "/help"} {
		if !strings.Contains(text, field) {
			t.Errorf("text output missing %q: %q", field, text)
		}
		if !strings.Contains(md, field) {
			t.Errorf("markdown output missing %q: %q", field, md)
		}
	}
	if !strings.Contains(md, RepositoryURL) {
		t.Error("markdown output missing repository URL")
	}
	if !strings.Contains(md, "**2 个**") && !strings.Contains(text, "2 个") {
		t.Error("plugin count not rendered from Info")
	}
}
