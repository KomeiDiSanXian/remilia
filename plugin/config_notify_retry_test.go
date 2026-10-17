package plugin

// config_notify_retry_test.go — 配置变更通知在写锁长期占用下不得丢弃的回归测试
//
// 回归背景：propagateConfigChangeAttempt 用 TryRLock + 有限次重试（200ms × N）
// 避开写锁，重试耗尽即丢弃通知：插件配置永久错过这次变更（配置文件已更新、
// 运行时行为不变），调用方只看到一条 Warn 日志。

import (
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigChangeNotification_SurvivesLongLockHold(t *testing.T) {
	var mu sync.Mutex
	values := map[string]map[string]any{"cfg-plugin": {"k": "v1"}}

	provider := &mockConfigProvider{}
	provider.subFn = func(name string) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		cur := values[name]
		if cur == nil {
			return nil
		}
		cp := make(map[string]any, len(cur))
		maps.Copy(cp, cur)
		return cp
	}

	pm := NewManager(nil, WithConfigProvider(provider))
	require.NoError(t, pm.Register(&Descriptor{
		Name:  "cfg-plugin",
		Setup: func(*SetupContext) (any, error) { return nil, nil },
	}))

	// 配置源更新（模拟配置文件被改写）
	mu.Lock()
	values["cfg-plugin"]["k"] = "v2"
	mu.Unlock()

	// 长时间占用 Manager 写锁（超过全部重试窗口）
	pm.mu.Lock()
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(propagateMaxRetries*200*time.Millisecond + 300*time.Millisecond)
		pm.mu.Unlock()
	}()

	// 触发广播：重试耗尽后应由元数据 goroutine 等待写锁完成
	pm.config.propagateConfigChange()
	<-released

	// 写锁释放后必须完成广播（插件视角看到新值）
	require.Eventually(t, func() bool {
		pm.mu.RLock()
		inst := pm.plugins["cfg-plugin"]
		pm.mu.RUnlock()
		if inst == nil {
			return false
		}
		cfg := inst.GetConfig()
		return cfg != nil && cfg.GetString("k", "") == "v2"
	}, 5*time.Second, 20*time.Millisecond,
		"写锁释放后必须完成配置广播，而不是丢弃通知")

	// 停机回收元数据 goroutine
	pm.lifecycle.Shutdown()
	assert.NoError(t, pm.StopAll(nil))
}
