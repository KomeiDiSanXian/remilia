package plugin

// bluegreen_rebind_test.go — 蓝绿重载切换后的实例绑定回归测试
//
// 回归背景：reloadBlueGreen 用一个临时 Instance 跑新 Setup 以隔离过程状态，
// 切换时只回拷了 matchers/goroutineMgr/exportedAPI，没有把 setupContext 与
// liveRegistryWriter 的 instance 指回长期存活的 pi。后果：
//   - writer 的活动性检查读的是那个永远 Loaded 的临时实例，卸载后插件后台
//     goroutine 的迟到注册不再被拦截，留下继续派发事件的幽灵 matcher；
//   - exportAs 的归属记录拿不到 Manager（临时实例 manager 为 nil），重载后
//     导出的 key 失去归属，卸载时可能误删后来者的同名服务。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlueGreen_RegistryWriterRebindsToInstance(t *testing.T) {
	eng := newTestPluginEngine(t)
	pm := NewManager(eng)

	require.NoError(t, pm.Register(&Descriptor{
		Name:     "bg-rebind",
		Advanced: &Advanced{Strategy: ReloadBlueGreen},
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.Reg.RegisterCommand("", "/bg-rebind")
			return &struct{ N int }{1}, nil
		},
	}))
	require.NoError(t, pm.StartAll(context.Background()))

	inst, ok := pm.Get("bg-rebind")
	require.True(t, ok)
	require.NoError(t, pm.Reload(context.Background(), "bg-rebind"))

	inst.mu.RLock()
	sc := inst.setupContext
	inst.mu.RUnlock()
	require.NotNil(t, sc)
	assert.Same(t, inst, sc.instance, "切换后 SetupContext 必须绑回长期实例")
	assert.NotNil(t, sc.instance.managerRef(), "绑定的实例必须能拿回 Manager（导出归属依赖它）")

	rw, ok := sc.Reg.(*liveRegistryWriter)
	require.True(t, ok)
	assert.Same(t, inst, rw.boundInstance(), "注册写入器必须重新绑定长期实例")

	before := eng.GetMatcherCount()
	require.NoError(t, pm.Unregister(context.Background(), "bg-rebind"))
	require.Equal(t, 0, eng.GetMatcherCount())

	// 卸载后插件后台 goroutine 的迟到注册必须被拒绝（旧实现会留下被派发的 matcher）
	assert.Nil(t, sc.Reg.RegisterCommand("", "/bg-ghost"))
	assert.Equal(t, before-1, eng.GetMatcherCount(), "卸载后的迟到注册不得产生幽灵 matcher")
}
