package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWatcherStopCleanup 测试 Watcher 停止时的资源清理
func TestWatcherStopCleanup(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	// 写入初始配置（包含必需的 bot 字段）
	initialConfig := `bot:
  app_id: 123456789
  bot_id: 987654321
  token: "test-token"
  secret: "test-secret"
server:
  host: "0.0.0.0"
  port: 8080
log:
  level: "info"
  format: "json"
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// 创建 Watcher
	watcher, err := NewWatcher(configPath)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// 注册回调以检测意外的 reload
	reloaded := make(chan struct{}, 1)
	watcher.AddCallback(func(_, _ *Config) error {
		select {
		case reloaded <- struct{}{}:
		default:
		}
		return nil
	})

	// 启动 Watcher（goroutine 通过 WaitGroup 与 Stop 同步）
	watcher.Start()

	// 停止 Watcher
	if err := watcher.Stop(); err != nil {
		t.Fatalf("Failed to stop watcher: %v", err)
	}

	// 修改文件触发事件（在 watcher 停止后）
	updatedConfig := `bot:
  app_id: 999888777
  bot_id: 777888999
  token: "test-token-updated"
  secret: "test-secret-updated"
server:
  host: "0.0.0.0"
  port: 8081
log:
  level: "debug"
  format: "text"
`
	if err := os.WriteFile(configPath, []byte(updatedConfig), 0644); err != nil {
		t.Fatalf("Failed to update test config: %v", err)
	}

	// 验证停止后不会触发 reload
	select {
	case <-reloaded:
		t.Error("reload was triggered after Stop")
	case <-time.After(200 * time.Millisecond):
	}

	t.Log("Watcher stopped and cleaned up successfully")
}

// TestWatcherTimerCleanup 测试 debounce timer 在 watcher 停止后不会执行
func TestWatcherTimerCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	initialConfig := `bot:
  app_id: 123456789
  bot_id: 987654321
  token: "test-token"
  secret: "test-secret"
server:
  host: "0.0.0.0"
  port: 8080
log:
  level: "info"
  format: "json"
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	watcher, err := NewWatcher(configPath, WithDebounceDelay(500*time.Millisecond))
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// 注册回调以检测意外的 reload
	reloaded := make(chan struct{}, 1)
	watcher.AddCallback(func(_, _ *Config) error {
		select {
		case reloaded <- struct{}{}:
		default:
		}
		return nil
	})

	watcher.Start()

	// 修改文件触发 debounce timer
	updatedConfig := `bot:
  app_id: 123456789
  bot_id: 987654321
  token: "test-token-modified"
  secret: "test-secret-modified"
server:
  host: "0.0.0.0"
  port: 8081
log:
  level: "debug"
  format: "text"
`
	if err := os.WriteFile(configPath, []byte(updatedConfig), 0644); err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	// 等待 fsnotify 传递事件（仍在 debounce 窗口内，debounce=500ms）
	time.Sleep(50 * time.Millisecond)

	// 确保 debounce 尚未完成（测试条件有效）
	select {
	case <-reloaded:
		t.Fatal("debounce completed before Stop, test conditions invalid")
	default:
	}

	// 在 timer 触发前停止 watcher
	if err := watcher.Stop(); err != nil {
		t.Fatalf("Failed to stop watcher: %v", err)
	}

	// 等待超过 debounce delay 的时间，验证 timer 未执行回调
	select {
	case <-reloaded:
		t.Error("reload was triggered after Stop (timer cleanup failed)")
	case <-time.After(600 * time.Millisecond):
	}

	t.Log("Timer cleanup handled correctly")
}

// TestWatcherMultipleStop 测试多次调用 Stop 不会出错
func TestWatcherMultipleStop(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	initialConfig := `bot:
  app_id: 123456789
  bot_id: 987654321
  token: "test-token"
  secret: "test-secret"
server:
  host: "0.0.0.0"
  port: 8080
log:
  level: "info"
  format: "json"
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	watcher, err := NewWatcher(configPath)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	watcher.Start()

	// 多次调用 Stop
	if err := watcher.Stop(); err != nil {
		t.Fatalf("First stop failed: %v", err)
	}

	// 第二次调用应该也不会 panic（虽然可能返回错误）
	_ = watcher.Stop()

	t.Log("Multiple Stop calls handled correctly")
}
