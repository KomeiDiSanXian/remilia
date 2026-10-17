package plugin

// lifecycle_publish_test.go — 框架生命周期通知必须受发布预算约束的回归测试
//
// 回归背景：notifyLoaded/Unloaded/Reloaded 在业务调用链内同步调用
// bus.Publish(topic, data)，而 Publish 在 worker 池满时会一直等待空闲槽位。
// 订阅者互相等待（例如 handler 内又触发插件管理操作）会把 /plugin 之类的
// 管理命令整体挂死。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycleNotify_DoesNotBlockOnSaturatedBus(t *testing.T) {
	// 池大小 1：一个卡住的 handler 即可占满
	pm := NewManagerWithEventBusOptions(nil, EventBusOptions{WorkerPoolSize: 1})

	release := make(chan struct{})
	entered := make(chan struct{})
	var once atomic.Bool
	_, err := pm.eventBus.Subscribe("plugin.loaded", func(context.Context, any) error {
		if once.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		return nil
	})
	require.NoError(t, err)

	// 占满唯一的 worker 槽位
	require.NoError(t, pm.eventBus.Publish("plugin.loaded", "warm"))
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("blocking subscriber did not start")
	}

	// 生命周期通知必须受限返回（超时丢弃并告警），而不是无限等待
	done := make(chan struct{})
	go func() {
		pm.notifyLoaded("demo")
		close(done)
	}()

	select {
	case <-done:
		// 期望路径
	case <-time.After(pluginLifecyclePublishTimeout + 5*time.Second):
		close(release)
		t.Fatal("lifecycle notification blocked on a saturated event bus")
	}

	close(release)
}

func TestLifecycleNotify_DeliversWhenBusHasCapacity(t *testing.T) {
	pm := NewManager(nil)

	loaded := make(chan string, 1)
	_, err := pm.eventBus.Subscribe("plugin.loaded", func(_ context.Context, data any) error {
		if name, ok := data.(string); ok {
			loaded <- name
		}
		return nil
	})
	require.NoError(t, err)

	pm.notifyLoaded("delivered")

	select {
	case name := <-loaded:
		assert.Equal(t, "delivered", name)
	case <-time.After(2 * time.Second):
		t.Fatal("notification should still be delivered when the bus has capacity")
	}
}
