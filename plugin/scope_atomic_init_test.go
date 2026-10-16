package plugin

// scope_atomic_init_test.go — 根 Scope 并发首次创建的回归测试
//
// 回归背景：SetupContext.Scope() 裸读裸写 rootScope。并发首调（插件后台
// goroutine 与 Subscribe/SubscribeAll）会各建一个根 Scope：落败方的订阅 /
// OnDispose 钩子不在根上，卸载时泄漏；同时该字段的读写会被 -race 报为数据竞争。

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupContext_ScopeConcurrentInit(t *testing.T) {
	ctx := NewTestSetupContext("race-probe", nil)
	t.Cleanup(func() { StopTestSetupContext(ctx) })

	const callers = 64
	scopes := make([]*Scope, callers)

	var start sync.WaitGroup
	start.Add(1)

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait() // 对齐起跑线，让首次创建真正并发
			scopes[i] = ctx.Scope()
		}(i)
	}
	start.Done()
	wg.Wait()

	first := scopes[0]
	require.NotNil(t, first)
	for i := 1; i < callers; i++ {
		assert.Same(t, first, scopes[i], "并发首次调用必须拿到同一个根 Scope")
	}
	assert.Same(t, first, ctx.Scope(), "后续调用返回同一实例")
}
