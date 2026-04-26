package config

// watcher_debounce_race_test.go — debounce timer 与 Stop() 竞态测试
//
// 测试目标（T-4）：验证 L-4 修复：
// 当 debounce timer 在 Stop() 之后触发时，reload() 检查 ctx.Err() 并安全返回，
// 不会访问已关闭的 watcher 资源，也不会触发回调。
//
// 推荐运行方式（开启 -race 探测器）：
//
//	go test -race ./config/ -run TestWatcher.*Debounce -count=3

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWatcherDebounceRace_StopBeforeTimerFires 验证 L-4 修复的核心场景：
// 文件变更触发 debounce timer 后，在 timer 到期前调用 Stop()，
// timer 最终触发时 reload() 因 ctx 已取消而安全提前返回，回调不被执行。
func TestWatcherDebounceRace_StopBeforeTimerFires(t *testing.T) {
	// createTempConfigFile 内部使用 t.TempDir()，无需手动 Remove
	tmpFile := createTempConfigFile(t, validConfig)

	var reloadCount atomic.Int32
	reloaded := make(chan struct{}, 1)

	// debounce 设为 300ms，给 Stop() 充足的时间在 timer 前调用
	watcher, err := NewWatcher(tmpFile, WithDebounceDelay(300*time.Millisecond))
	require.NoError(t, err)

	watcher.AddCallback(func(_, _ *Config) error {
		reloadCount.Add(1)
		select {
		case reloaded <- struct{}{}:
		default:
		}
		return nil
	})
	watcher.Start()

	// 修改配置文件，触发 debounce timer
	newContent := `
bot:
  app_id: 999999
  bot_id: 888888
  token: "new-token"
  secret: "new-secret"
server:
  host: "0.0.0.0"
  port: 9090
log:
  level: "debug"
  format: "json"
`
	err = os.WriteFile(tmpFile, []byte(newContent), 0644)
	require.NoError(t, err)

	// 在 debounce timer 到期前（50ms < 300ms）停止 watcher
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, watcher.Stop())

	// 等待超过 debounce delay，确认 timer 已触发并被 ctx.Err() 拦截
	select {
	case <-reloaded:
		t.Error("callback invoked after Stop (ctx.Err() check failed)")
	case <-time.After(350 * time.Millisecond):
	}

	// L-4 修复验证：timer 触发后 reload() 检测到 ctx 已取消，不执行回调
	assert.Equal(t, int32(0), reloadCount.Load(),
		"L-4 fix: Stop() 后 debounce timer 触发时 reload() 应提前返回，不调用回调")
}

// TestWatcherDebounceRace_MultipleFileChanges 验证多次文件变更时 Stop() 的安全性：
// 快速写入多次变更后立即停止，无论哪个 debounce timer 最终触发，
// 都应安全地被 ctx 取消检查拦截。
func TestWatcherDebounceRace_MultipleFileChanges(t *testing.T) {
	// createTempConfigFile 内部使用 t.TempDir()，无需手动 Remove
	tmpFile := createTempConfigFile(t, validConfig)

	var reloadCount atomic.Int32
	reloaded := make(chan struct{}, 1)

	watcher, err := NewWatcher(tmpFile, WithDebounceDelay(200*time.Millisecond))
	require.NoError(t, err)

	watcher.AddCallback(func(_, _ *Config) error {
		reloadCount.Add(1)
		select {
		case reloaded <- struct{}{}:
		default:
		}
		return nil
	})
	watcher.Start()

	// 快速写入多次，每次重置 debounce timer
	for i := range 5 {
		content := validConfig
		_ = i
		err := os.WriteFile(tmpFile, []byte(content), 0644)
		if err != nil {
			t.Logf("file write %d: %v (ignored)", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 等待 fsnotify 事件传递，仍在 debounce 窗口内
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, watcher.Stop())

	// 等待超过 debounce delay，验证 Stop 后无回调
	select {
	case <-reloaded:
		// 可能发生在 Stop 前的最后一次 debounce 完成
		// 这是允许的（reloadCount <= 1）
	default:
	}
	select {
	case <-reloaded:
		t.Error("callback invoked after Stop (ctx.Err() check failed)")
	case <-time.After(300 * time.Millisecond):
	}

	// 回调次数应为 0（所有 timer 均在 Stop 后触发，被 ctx.Err() 拦截）
	// 或者 <= 1（极端情况下第一次写入刚好在 Stop 前完成 debounce）
	assert.LessOrEqual(t, reloadCount.Load(), int32(1),
		"多次文件变更后 Stop()：回调执行次数应 ≤ 1（Stop 之后的 timer 不触发回调）")
}

// TestWatcherDebounceRace_ReloadAfterStop_NoAccess 验证 reload() 在 Stop() 后被调用时
// 不会 panic 或产生 data race（直接通过 ForceReload 触发）。
func TestWatcherDebounceRace_ReloadAfterStop_NoAccess(t *testing.T) {
	// createTempConfigFile 内部使用 t.TempDir()，无需手动 Remove
	tmpFile := createTempConfigFile(t, validConfig)

	watcher, err := NewWatcher(tmpFile)
	require.NoError(t, err)
	watcher.Start()

	// 停止 watcher
	require.NoError(t, watcher.Stop())

	// 停止后直接调用 ForceReload（内部调用 reload()），
	// 应触发 ctx.Err() 检查并返回 nil，不 panic
	assert.NotPanics(t, func() {
		err := watcher.ForceReload()
		// reload 应返回 nil（ctx 已取消，提前返回）
		assert.NoError(t, err, "Stop() 后 ForceReload 应返回 nil（ctx 已取消）")
	})
}

// TestWatcherDebounceRace_ConcurrentStopAndTimer 使用多个 goroutine 并发触发 Stop，
// 同时让 debounce timer 以高频触发，用 -race 检测器验证无竞争。
func TestWatcherDebounceRace_ConcurrentStopAndTimer(t *testing.T) {
	// createTempConfigFile 内部使用 t.TempDir()，无需手动 Remove
	tmpFile := createTempConfigFile(t, validConfig)

	reloaded := make(chan struct{}, 1)
	watcher, err := NewWatcher(tmpFile, WithDebounceDelay(5*time.Millisecond))
	require.NoError(t, err)

	watcher.AddCallback(func(_, _ *Config) error {
		select {
		case reloaded <- struct{}{}:
		default:
		}
		return nil
	})

	watcher.Start()

	// 写入变更（触发 debounce timer，5ms 后触发）
	_ = os.WriteFile(tmpFile, []byte(validConfig), 0644)
	time.Sleep(2 * time.Millisecond)

	// 立即 Stop（与 timer 竞争）
	_ = watcher.Stop()

	// 等待确认 timer 已处理完毕（不应触发回调）
	select {
	case <-reloaded:
		// 允许 Stop 前 timer 已触发（race 本身），用 -race 检测 data race
	default:
	}
	select {
	case <-reloaded:
		t.Error("callback invoked after Stop (race condition in timer cleanup)")
	case <-time.After(20 * time.Millisecond):
	}

	// 若无 panic 且 -race 无报告，则测试通过
}
