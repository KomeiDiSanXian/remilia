package plugin

// failed_load_cleanup_test.go — 加载失败的资源回收回归测试
//
// 回归背景：Setup 失败或超时只停 goroutine 管理器：rootScope 上的 EventBus
// 订阅、中间件、OnDispose 钩子在插件被判失败后仍然活着——订阅继续响应事件、
// 清理逻辑永不执行。超时路径更隐蔽：Setup 仍在后台运行，清理之后它继续注册
// 的资源也要在它结束后再清一次。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_SetupFailureCleansScopeResources(t *testing.T) {
	pm := NewManager(nil)

	var disposed, subscribed atomic.Bool

	desc := &Descriptor{
		Name: "failing-setup",
		Setup: func(ctx *SetupContext) (any, error) {
			// Setup 中途失败：已建立的订阅与清理钩子必须被回收
			root := ctx.Scope()
			root.OnDispose(func() error {
				disposed.Store(true)
				return nil
			})
			root.Subscribe("plugin.loaded", func(context.Context, any) error {
				subscribed.Store(true)
				return nil
			})
			return nil, assert.AnError
		},
	}

	err := pm.Register(desc)
	require.Error(t, err)

	require.Eventually(t, disposed.Load, 2*time.Second, 5*time.Millisecond,
		"加载失败后 rootScope 的 OnDispose 钩子必须执行（此前永久泄漏）")
	assert.Equal(t, 0, pm.Count(), "失败的插件不应留在表中")

	// loadOrder 必须清理：重试注册不应累积重复名字
	for _, n := range pm.GetLoadOrder() {
		assert.NotEqual(t, "failing-setup", n, "失败插件的名字不应残留在 loadOrder")
	}
}
