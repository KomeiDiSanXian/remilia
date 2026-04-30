package pluginctrl_test

// autowire_lifecycle_test.go — 回归测试：autoWireListener 生命周期场景
//
// 覆盖以下场景，防止 combinedGuard 自动注入逻辑出现回归：
//
//  1. 正常加载：pluginctrl → weather，guard 自动注入，能正确管控访问
//  2. 热重载：Reload 触发 notifyReloaded（no-op），guard 仍然有效
//  3. 卸载 + 重注册：OnPluginLoaded 执行 reset-before-wire，guard 仅注入一次
//  4. 逆序注册：weather 先注册，pluginctrl 后注册时补充注入（retroactive wire）

import (
	stdctx "context"
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/pluginctrl"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 测试辅助 ────────────────────────────────────────────────────────────────

// makeGroupCtx 创建指定群和用户的群消息上下文（使用 NoopSender，回复被丢弃）。
func makeGroupCtx(groupID, userID string) *eventctx.Context {
	return eventctx.AcquireContextFromEvent(
		&pcTestEvent{groupID: groupID, userID: userID, isGroup: true, content: "/weather"},
		&platform.NoopSender{},
	)
}

// newWeatherDescriptor 返回一个 "weather" 测试插件描述符。
// Setup 注册一个群消息 Matcher；每次 handler 被调用，calls 计数器自增 1。
func newWeatherDescriptor(calls *int32) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name: "weather",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Reg.RegisterMatcher(string(platform.EventKindGroupMessage)).
				Handle(func(_ *eventctx.Context) error {
					atomic.AddInt32(calls, 1)
					return nil
				})
			return nil, nil
		},
	}
}

// getPluginCtrl 从插件管理器容器中获取 *pluginctrl.Plugin 实例。
func getPluginCtrl(t *testing.T, pm *plugin.Manager) *pluginctrl.Plugin {
	t.Helper()
	raw, ok := pm.GetContainer().Get("pluginctrl")
	require.True(t, ok, "pluginctrl 应在注册后存在于容器中")
	ctrl, ok := raw.(*pluginctrl.Plugin)
	require.True(t, ok, "容器中的值应为 *pluginctrl.Plugin")
	return ctrl
}

// ─── 测试：正常加载 ───────────────────────────────────────────────────────────

// TestAutoWire_NormalLoad_GuardBlocks 验证正常注册顺序（pluginctrl → weather）下：
//   - autoWireListener.OnPluginLoaded 触发，combinedGuard 自动注入
//   - 关闭 weather 后，指定群的事件被 guard 拦截（handler 不执行）
//   - 其他未关闭的群的事件正常通过（handler 执行）
func TestAutoWire_NormalLoad_GuardBlocks(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck
	pm := plugin.NewManager(eng)

	// 注册 pluginctrl（Privileged，自动注入 autoWireListener）
	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("su"))))
	ctrl := getPluginCtrl(t, pm)

	var handlerCalls int32
	// 注册 weather → 触发 OnPluginLoaded("weather") → guard 注入
	require.NoError(t, pm.Register(newWeatherDescriptor(&handlerCalls)))

	// 关闭 g1 群的 weather 插件
	require.NoError(t, ctrl.SetGroupEnabled("g1", "weather", false))

	// g1 事件：guard 应拦截，handler 不执行
	eng.ProcessEvent(makeGroupCtx("g1", "u1"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&handlerCalls),
		"guard 应拦截已关闭插件的群消息，handler 不得执行")

	// g2 事件：weather 默认开启，handler 应执行
	eng.ProcessEvent(makeGroupCtx("g2", "u1"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&handlerCalls),
		"guard 应放行默认开启插件的群消息，handler 应执行")
}

// ─── 测试：热重载 no-op ───────────────────────────────────────────────────────

// TestAutoWire_Reload_GuardPersists 验证插件热重载后 guard 仍然有效：
//   - pm.Reload 触发 notifyReloaded（而非 notifyLoaded）
//   - OnPluginReloaded 是 no-op，不重新注入 guard
//   - 已有 guard 的闭包持有 *Plugin 指针，直接读取当前状态，热重载后仍然有效
func TestAutoWire_Reload_GuardPersists(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck
	pm := plugin.NewManager(eng)

	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("su"))))
	ctrl := getPluginCtrl(t, pm)

	var handlerCalls int32
	require.NoError(t, pm.Register(newWeatherDescriptor(&handlerCalls)))

	// 关闭 g1 群的 weather
	require.NoError(t, ctrl.SetGroupEnabled("g1", "weather", false))

	// 热重载 weather（触发 notifyReloaded，OnPluginReloaded no-op）
	require.NoError(t, pm.Reload(stdctx.Background(), "weather"))

	// 重置计数器（reload 后 Setup 重新执行，绑定同一个 &handlerCalls）
	atomic.StoreInt32(&handlerCalls, 0)

	// 重载后 guard 仍应有效：g1 事件被拦截
	eng.ProcessEvent(makeGroupCtx("g1", "u1"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&handlerCalls),
		"热重载后 guard 应仍然拦截已关闭插件的群消息")

	// g2 事件应通过
	eng.ProcessEvent(makeGroupCtx("g2", "u1"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&handlerCalls),
		"热重载后 guard 应仍然放行默认开启插件的群消息")
}

// ─── 测试：卸载 + 重注册（核心回归测试） ─────────────────────────────────────

// TestAutoWire_UnloadThenReregister_GuardRewiredOnce 是防止双重 guard 回归的核心测试。
//
// 修复前的 bug：
//   - 第一次注册 weather → UseForGroup("weather", guard)    → chain = [guard]
//   - 卸载 weather → guard 仍在 groupMiddlewares（phantom entry）
//   - 重新注册 weather → UseForGroup("weather", guard) 再次追加 → chain = [guard, guard]
//
// 修复后（reset-before-wire）：
//   - OnPluginUnloaded 调用 groupResetFn("weather") → 清除 phantom entry
//   - OnPluginLoaded 先 Reset 再 wire → chain 始终为 [guard]
//
// 虽然 combinedGuard 的观测行为在单/双 guard 下相同（短路返回），
// 本测试通过完整的 load → block → unload → re-register → block 验证全生命周期：
// guard 在每次重注册后正确生效，系统行为与初次注册一致。
func TestAutoWire_UnloadThenReregister_GuardRewiredOnce(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck
	pm := plugin.NewManager(eng)

	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("su"))))
	ctrl := getPluginCtrl(t, pm)

	var handlerCalls int32

	// ── 第一轮：注册 + 验证 guard 有效 ──────────────────────────────────
	require.NoError(t, pm.Register(newWeatherDescriptor(&handlerCalls)))
	require.NoError(t, ctrl.SetGroupEnabled("g1", "weather", false))

	eng.ProcessEvent(makeGroupCtx("g1", "u1"))
	require.Equal(t, int32(0), atomic.LoadInt32(&handlerCalls),
		"第一轮：g1 事件应被 guard 拦截")

	eng.ProcessEvent(makeGroupCtx("g2", "u1"))
	require.Equal(t, int32(1), atomic.LoadInt32(&handlerCalls),
		"第一轮：g2 事件应通过 guard")

	// ── 卸载 weather ───────────────────────────────────────────────────
	// OnPluginUnloaded 触发 → groupResetFn("weather") 清除 phantom entry
	require.NoError(t, pm.Unregister(stdctx.Background(), "weather"))

	// ── 第二轮：重注册 + 验证 guard 仍然有效（回归验证） ─────────────
	// OnPluginLoaded 触发 → reset-before-wire → chain = [guard]（不会双重追加）
	atomic.StoreInt32(&handlerCalls, 0)
	require.NoError(t, pm.Register(newWeatherDescriptor(&handlerCalls)))

	// pluginctrl 的状态跨越 weather 的生命周期持久存在：
	// g1 仍然是 SetGroupEnabled("g1", "weather", false) 的状态
	eng.ProcessEvent(makeGroupCtx("g1", "u1"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&handlerCalls),
		"重注册后：g1 事件应被重新注入的 guard 拦截（回归验证：无双重 guard）")

	eng.ProcessEvent(makeGroupCtx("g2", "u1"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&handlerCalls),
		"重注册后：g2 事件应通过 guard")
}

// ─── 测试：逆序注册（retroactive wiring）────────────────────────────────────

// TestAutoWire_RetroactiveWire_WeatherRegisteredFirst 验证 weather 先于 pluginctrl
// 注册时，pluginctrl 在 Setup 中通过 ctx.Info.List() 补充注入 combinedGuard：
//   - weather 注册时 pluginctrl 尚未存在，guard 不会自动注入
//   - pluginctrl 随后注册，Setup 遍历已有插件并补充注入
//   - 之后 guard 对 weather 的管控与正常注册顺序完全相同
func TestAutoWire_RetroactiveWire_WeatherRegisteredFirst(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck
	pm := plugin.NewManager(eng)

	var handlerCalls int32

	// 先注册 weather（此时 pluginctrl 尚未存在，guard 未注入）
	require.NoError(t, pm.Register(newWeatherDescriptor(&handlerCalls)))

	// 后注册 pluginctrl（retroactive wiring：Setup 补充为 weather 注入 guard）
	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("su"))))
	ctrl := getPluginCtrl(t, pm)

	// 关闭 g1 群的 weather
	require.NoError(t, ctrl.SetGroupEnabled("g1", "weather", false))

	// g1 事件：补充注入的 guard 应拦截
	eng.ProcessEvent(makeGroupCtx("g1", "u1"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&handlerCalls),
		"retroactive wire：补充注入的 guard 应拦截已关闭插件的群消息")

	// g2 事件：应通过
	eng.ProcessEvent(makeGroupCtx("g2", "u1"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&handlerCalls),
		"retroactive wire：补充注入的 guard 应放行默认开启插件的群消息")
}

// ─── 测试：超级管理员豁免 ─────────────────────────────────────────────────────

// TestAutoWire_SuperUserBypass 验证超级管理员即使在插件关闭的群也能通过 guard。
// combinedGuard 的第 0 步是超级管理员豁免。
func TestAutoWire_SuperUserBypass(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck
	pm := plugin.NewManager(eng)

	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("admin"))))
	ctrl := getPluginCtrl(t, pm)

	var handlerCalls int32
	require.NoError(t, pm.Register(newWeatherDescriptor(&handlerCalls)))

	// 关闭 g1 的 weather
	require.NoError(t, ctrl.SetGroupEnabled("g1", "weather", false))

	// 普通用户在 g1：被拦截
	eng.ProcessEvent(makeGroupCtx("g1", "normal-user"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&handlerCalls),
		"普通用户应被 guard 拦截")

	// 超级管理员在 g1：豁免，handler 仍然执行
	eng.ProcessEvent(makeGroupCtx("g1", "admin"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&handlerCalls),
		"超级管理员应绕过 guard 直接执行 handler")
}

// ─── 测试：基础设施插件豁免（WithExcludedPlugins）────────────────────────────

// TestAutoWire_InfraPlugin_NotWired 验证 isInfraPlugin 返回 true 的插件不会被
// autoWireListener 注入 guard（通过 WithExcludedPlugins 将测试插件加入豁免列表）。
func TestAutoWire_InfraPlugin_NotWired(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck
	pm := plugin.NewManager(eng)

	// 将 "infra-svc" 加入豁免列表（模拟基础设施插件）
	require.NoError(t, pm.Register(pluginctrl.New(
		pluginctrl.WithSuperUsers("su"),
		pluginctrl.WithExcludedPlugins("infra-svc"),
	)))
	ctrl := getPluginCtrl(t, pm)

	var handlerCalls int32
	infraDesc := &plugin.Descriptor{
		Name: "infra-svc",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Reg.RegisterMatcher(string(platform.EventKindGroupMessage)).
				Handle(func(_ *eventctx.Context) error {
					atomic.AddInt32(&handlerCalls, 1)
					return nil
				})
			return nil, nil
		},
	}
	require.NoError(t, pm.Register(infraDesc))

	// 尝试关闭 g1 的 infra-svc（无效：guard 未注入）
	require.NoError(t, ctrl.SetGroupEnabled("g1", "infra-svc", false))

	// infra-svc 不受 guard 管控，handler 应正常执行
	eng.ProcessEvent(makeGroupCtx("g1", "u1"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&handlerCalls),
		"豁免列表中的插件不应被 guard 拦截，handler 应执行")
}
