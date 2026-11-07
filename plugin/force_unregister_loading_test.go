package plugin

// force_unregister_loading_test.go — 强制注销与在飞 Setup 交叠的回归测试
//
// 回归背景：ForceUnregister 直接删除表项，但慢 Setup 的收尾（load 末尾）仍会
// 把 API 写回容器、把状态改成 Loaded、并把命令留在引擎里；registerPostSetupSingle
// 随后又把名字写回 loadOrder。管理端看到的是"已卸载但命令还在响应"的幽灵插件，
// 而 StartAll/StopAll/配置广播还会反复处理这个不存在的名字。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceUnregister_DuringLoadingDoesNotResurrect(t *testing.T) {
	eng := newTestPluginEngine(t)
	pm := NewManager(eng)

	started := make(chan struct{})
	release := make(chan struct{})
	regDone := make(chan error, 1)
	go func() {
		regDone <- pm.Register(&Descriptor{
			Name: "slow-setup",
			Setup: func(ctx *SetupContext) (any, error) {
				close(started)
				<-release
				ctx.Reg.RegisterCommand("", "/slow")
				return &struct{}{}, nil
			},
		})
	}()
	<-started

	require.NoError(t, pm.ForceUnregister("slow-setup"))
	close(release)

	// 注册返回"结果已作废"的错误；无论哪种，最终状态都必须是"不存在"
	assert.ErrorIs(t, <-regDone, errInstanceDiscarded)

	_, inMap := pm.Get("slow-setup")
	_, inContainer := pm.GetContainer().Get("slow-setup")
	assert.False(t, inMap, "强制卸载后不得重新出现在插件表中")
	assert.False(t, inContainer, "强制卸载后不得重新导出 API")
	assert.NotContains(t, pm.GetLoadOrder(), "slow-setup", "强制卸载后不得残留 loadOrder")
	assert.Equal(t, 0, eng.GetMatcherCount(), "强制卸载后不得残留 matcher")
}

// TestUnregister_DuringLoadingDoesNotResurrect 普通注销走 waitForLoading，
// 同样不得让 Setup 的收尾把插件写回管理状态。
func TestUnregister_DuringLoadingDoesNotResurrect(t *testing.T) {
	eng := newTestPluginEngine(t)
	pm := NewManager(eng)

	started := make(chan struct{})
	release := make(chan struct{})
	regDone := make(chan error, 1)
	go func() {
		regDone <- pm.Register(&Descriptor{
			Name: "slow-unregister",
			Setup: func(ctx *SetupContext) (any, error) {
				close(started)
				<-release
				ctx.Reg.RegisterCommand("", "/slow-unregister")
				return &struct{}{}, nil
			},
		})
	}()
	<-started

	unregisterDone := make(chan error, 1)
	go func() { unregisterDone <- pm.Unregister(context.Background(), "slow-unregister") }()
	close(release)

	// 两条路径之一必然先落定：Setup 结束后注销完成，或注册先完成再由注销清理。
	<-regDone
	<-unregisterDone

	_, inMap := pm.Get("slow-unregister")
	_, inContainer := pm.GetContainer().Get("slow-unregister")
	assert.False(t, inContainer, "注销后不得残留容器 key")
	if !inMap {
		assert.NotContains(t, pm.GetLoadOrder(), "slow-unregister")
		assert.Equal(t, 0, eng.GetMatcherCount(), "注销后不得残留 matcher")
	}
}
