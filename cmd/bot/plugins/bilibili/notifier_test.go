package bilibili

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/mock"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
)

// newNotifierRegistry 构造带 SessionNotifier 能力的 mock registry。
func newNotifierRegistry() *platform.Registry {
	reg := platform.NewRegistry()
	reg.Register(mock.NewAdapter(
		mock.WithPlatform("qq"),
		mock.WithSender(&mock.MockSender{}),
	))
	return reg
}

// TestSetupRegistersNotifierFromRegistry 验证：注入 platform.Registry 后，
// Setup 阶段无需任何事件即可注册主动推送能力（重启后订阅通知自动恢复）。
func TestSetupRegistersNotifierFromRegistry(t *testing.T) {
	desc := New(WithPlatformRegistry(newNotifierRegistry()))
	ctx := plugintest.NewSetupContext("bilibili", nil)
	defer plugintest.StopSetupContext(ctx)

	inst, err := desc.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	p, ok := inst.(*Plugin)
	if !ok {
		t.Fatalf("Setup 返回类型异常: %T", inst)
	}
	if !p.watch.hasNotifier() {
		t.Fatal("注入 registry 后 Setup 应自动注册 notifier（无需事件）")
	}
}

// TestSetupWithoutRegistry 验证：未注入 registry 时不 panic，notifier 未注册。
func TestSetupWithoutRegistry(t *testing.T) {
	desc := New()
	ctx := plugintest.NewSetupContext("bilibili", nil)
	defer plugintest.StopSetupContext(ctx)

	inst, err := desc.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	p, ok := inst.(*Plugin)
	if !ok {
		t.Fatalf("Setup 返回类型异常: %T", inst)
	}
	if p.watch.hasNotifier() {
		t.Fatal("未注入 registry 时不应注册 notifier")
	}
}

// TestNotifyChatThroughNotifier 验证 notifier 实际推送链路。
func TestNotifyChatThroughNotifier(t *testing.T) {
	p := &Plugin{
		watch: newWatchManager(t.TempDir()+"/bili", 0),
		reg:   newNotifierRegistry(),
	}
	p.registerNotifier(nil)
	if !p.watch.hasNotifier() {
		t.Fatal("registry 注入后应注册 notifier")
	}
	if err := p.notifyChat("group-1", "test 开播通知"); err != nil {
		t.Fatalf("notifyChat failed: %v", err)
	}
	_ = context.Background()
}
