package plugin

// registry_writer_gate_test.go — 失效实例注册与 writer 字段竞争的回归测试
//
// 回归背景：
//   - 蓝绿重载会 RemoveGroup 旧分组，旧实例的后台 goroutine 若继续经
//     ctx.Reg 注册，Matcher 会落进已清空的临时组：此后无人管理（不再被任何
//     RemoveGroup 覆盖），事件继续被它处理；
//   - 迁移时 rw.name/postRegister 被无锁改写，与插件后台 goroutine 的注册
//     调用并发（数据竞争）。

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveRegistryWriter_RefusesRegistrationForInactiveInstance(t *testing.T) {
	eng := newTestPluginEngine(t)
	pm := NewManager(eng)
	require.NoError(t, pm.Register(&Descriptor{
		Name:  "gate",
		Setup: func(*SetupContext) (any, error) { return nil, nil },
	}))

	pm.mu.RLock()
	inst := pm.plugins["gate"]
	pm.mu.RUnlock()
	require.NotNil(t, inst)
	require.NotNil(t, inst.setupContext)
	rw, ok := inst.setupContext.Reg.(*liveRegistryWriter)
	require.True(t, ok)

	// 活动实例：正常注册
	require.NotNil(t, rw.RegisterCommand("", "/alive"))

	// 失效实例：拒绝注册（返回 nil 或撤销已创建的 Matcher），
	// 不得把 Matcher 落进已清空的分组
	inst.SetState(Unloading)
	assert.Nil(t, rw.RegisterCommand("", "/late"), "卸载中的实例不得再注册")

	before := eng.GetMatcherCount()
	late := rw.RegisterMatcher("")
	if late != nil {
		// 该路径先创建再在收尾撤销：Matcher 不得留在引擎里
		require.Eventually(t, func() bool { return eng.GetMatcherCount() <= before },
			2*time.Second, 10*time.Millisecond, "迟到的 Matcher 必须被撤销")
	}

	inst.SetState(Error)
	assert.Nil(t, rw.RegisterCommand("", "/late2"))

	// 恢复活动（重载完成后）后可以继续注册
	inst.SetState(Loaded)
	assert.NotNil(t, rw.RegisterCommand("", "/again"))
}

func TestLiveRegistryWriter_SetNameIsRaceFree(t *testing.T) {
	eng := newTestPluginEngine(t)
	pm := NewManager(eng)
	require.NoError(t, pm.Register(&Descriptor{
		Name:  "rw-race",
		Setup: func(*SetupContext) (any, error) { return nil, nil },
	}))

	pm.mu.RLock()
	inst := pm.plugins["rw-race"]
	pm.mu.RUnlock()
	rw, ok := inst.setupContext.Reg.(*liveRegistryWriter)
	require.True(t, ok)

	var wg sync.WaitGroup
	wg.Add(2)
	// 迁移侧持续切换分组与回调
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			rw.setName("rw-race", nil)
		}
	}()
	// 插件后台 goroutine 持续注册
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = rw.RegisterCommand("", "/race")
		}
	}()
	wg.Wait()
}
