package about

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	qq "github.com/KomeiDiSanXian/remilia/platform/qq"
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
	if d.Version != "1.1.0" {
		t.Errorf("expected version %q, got %q", "1.1.0", d.Version)
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
	md, text := p.buildInfo("TestBot", "qq")
	if md == "" || text == "" {
		t.Fatal("buildInfo returned empty output")
	}
	for _, field := range []string{
		"框架版本", "Go 版本", "仓库", "已加载插件", "运行时长", "/help",
		"机器人名称", "当前平台", "注册命令", "Matcher",
		"操作系统", "CPU 核心", "系统内存", "进程内存", "Goroutine",
	} {
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
	if !strings.Contains(md, "TestBot") || !strings.Contains(md, "qq") {
		t.Error("bot name or platform not rendered")
	}
}

// TestBuildInfoWithCoordinator 验证 Coordinator 非 nil 时命令/Matcher 统计正常渲染。
func TestBuildInfoWithCoordinator(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	defer eng.Shutdown(t.Context())
	eng.On("test_event", nil)

	p := &Plugin{
		info: &plugintest.MockPluginInfo{
			Plugins: map[string]*plugin.Metadata{
				"ping": {Name: "ping"},
			},
			CoordinatorValue: engine.NewEngineReader(eng),
		},
		startTime: time.Now(),
	}
	md, text := p.buildInfo("", "")
	if !strings.Contains(md, "**注册命令**: 0 个") {
		t.Errorf("expected command count 0, got: %q", md)
	}
	if !strings.Contains(text, "Matcher: 1 个") {
		t.Errorf("expected matcher count 1, got: %q", text)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{10 * 1024 * 1024, "10.0 MB"},
		{1536 * 1024 * 1024, "1.5 GB"},
		{16 * 1024 * 1024 * 1024, "16.0 GB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.in); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// captureAboutSender 记录 handleAbout 发送的消息。
type captureAboutSender struct {
	mu   sync.Mutex
	msgs []platform.OutboundMessage
}

func (s *captureAboutSender) Send(_ context.Context, req platform.SendRequest) (platform.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, req.Message)
	return platform.SendResult{MessageID: "mock-msg"}, nil
}

func (s *captureAboutSender) last() (platform.OutboundMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.msgs) == 0 {
		return platform.OutboundMessage{}, false
	}
	return s.msgs[len(s.msgs)-1], true
}

// inlineAboutDispatcher 同步执行出站任务：handleAbout 返回时消息已发送完毕。
type inlineAboutDispatcher struct{}

func (inlineAboutDispatcher) Submit(_ string, task func(context.Context) error) error {
	return task(context.Background())
}

// newAboutContext 构造支持按钮能力的测试上下文。
func newAboutContext(platformName string, chat platform.ChatInfo) (*eventctx.Context, *captureAboutSender) {
	kind := platform.EventKindPrivateMessage
	if chat.IsGroup {
		kind = platform.EventKindGroupMessage
	}
	evt := platform.NewSyntheticEvent(kind, "/about",
		platform.WithSyntheticPlatform(platformName),
		platform.WithSyntheticChat(chat))
	sender := &captureAboutSender{}
	ctx := eventctx.NewContextFromEvent(evt, sender)
	ctx.SetDispatcher(inlineAboutDispatcher{})
	ctx.SetPlatformCapabilities(platform.Capabilities{Markdown: true, Buttons: true})
	return ctx, sender
}

func TestHandleAboutQQButtonAutoSend(t *testing.T) {
	p := &Plugin{startTime: time.Now()}

	// QQ 单聊（C2C）：按钮应携带 Enter=true（手机端 8983+ 点击自动发送 /help）。
	ctx, sender := newAboutContext("qq", platform.ChatInfo{ID: "u1"})
	if err := p.handleAbout(ctx); err != nil {
		t.Fatalf("handleAbout(qq c2c): %v", err)
	}
	msg, ok := sender.last()
	if !ok {
		t.Fatal("handleAbout 未发送消息")
	}
	if len(msg.Buttons) != 1 || msg.Buttons[0].Command != "/help" {
		t.Fatalf("应附加单个查看命令列表按钮：%+v", msg.Buttons)
	}
	ext, ok := msg.Buttons[0].Extra[qq.ExtraKeyButton].(*qq.ButtonExtra)
	if !ok || !ext.Enter {
		t.Error("QQ 单聊按钮应携带 Enter=true（手机端点击自动发送）")
	}

	// QQ 群聊：附加按钮但不自动发送（Enter 仅单聊可用，点击仅填入输入框）。
	ctxG, senderG := newAboutContext("qq", platform.ChatInfo{ID: "g1", IsGroup: true})
	if err := p.handleAbout(ctxG); err != nil {
		t.Fatalf("handleAbout(qq group): %v", err)
	}
	msgG, ok := senderG.last()
	if !ok {
		t.Fatal("handleAbout 未发送群聊消息")
	}
	if len(msgG.Buttons) != 1 {
		t.Fatalf("QQ 群聊应附加查看命令列表按钮：%+v", msgG.Buttons)
	}
	if len(msgG.Buttons[0].Extra) != 0 {
		t.Errorf("QQ 群聊按钮不应携带 Enter（Extra 应为空）：%+v", msgG.Buttons[0].Extra)
	}

	// QQ 频道：同样不自动发送。
	ctxC, senderC := newAboutContext("qq", platform.ChatInfo{ID: "c1", ParentID: "guild1", IsGroup: true})
	if err := p.handleAbout(ctxC); err != nil {
		t.Fatalf("handleAbout(qq channel): %v", err)
	}
	msgC, ok := senderC.last()
	if !ok || len(msgC.Buttons) != 1 {
		t.Fatalf("QQ 频道应附加查看命令列表按钮：%+v", msgC.Buttons)
	}
	if len(msgC.Buttons[0].Extra) != 0 {
		t.Errorf("QQ 频道按钮不应携带 Enter：%+v", msgC.Buttons[0].Extra)
	}
}

func TestHandleAboutNonQQButtonNoEnter(t *testing.T) {
	// 非 QQ 平台：按钮按平台既有形态附加，不引入 QQ Extra。
	p := &Plugin{startTime: time.Now()}
	ctx, sender := newAboutContext("telegram", platform.ChatInfo{ID: "u1"})
	if err := p.handleAbout(ctx); err != nil {
		t.Fatalf("handleAbout(telegram): %v", err)
	}
	msg, ok := sender.last()
	if !ok || len(msg.Buttons) != 1 {
		t.Fatalf("telegram 应附加查看命令列表按钮：%+v", msg.Buttons)
	}
	if len(msg.Buttons[0].Extra) != 0 {
		t.Errorf("非 QQ 平台按钮不应携带 QQ Extra：%+v", msg.Buttons[0].Extra)
	}
}
