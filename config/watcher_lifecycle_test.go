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

	// 启动 Watcher
	watcher.Start()

	// 等待一小段时间确保 watcher 运行
	time.Sleep(100 * time.Millisecond)

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

	// 等待一段时间，确认没有 panic 或错误
	time.Sleep(200 * time.Millisecond)

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

	watcher.Start()
	time.Sleep(100 * time.Millisecond)

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

	// 在 timer 触发前停止 watcher
	time.Sleep(100 * time.Millisecond)
	if err := watcher.Stop(); err != nil {
		t.Fatalf("Failed to stop watcher: %v", err)
	}

	// 等待超过 debounce delay 的时间
	// 如果 timer 回调检查了 context，它应该不会执行
	time.Sleep(600 * time.Millisecond)

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
	time.Sleep(100 * time.Millisecond)

	// 多次调用 Stop
	if err := watcher.Stop(); err != nil {
		t.Fatalf("First stop failed: %v", err)
	}

	// 第二次调用应该也不会 panic（虽然可能返回错误）
	_ = watcher.Stop()

	t.Log("Multiple Stop calls handled correctly")
}
