package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KomeiDiSanXian/remilia/config"
)

func writeTempConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestConfigHotReloadChain 验证热重载链路：
//
//	config.Load(path) → config.Get() 更新 → 监听器收到通知
func TestConfigHotReloadChain(t *testing.T) {
	dir := t.TempDir()

	cfgPath := writeTempConfig(t, dir, `
bot:
  qq:
    webhook:
      host: "0.0.0.0"
      port: 8080
log:
  level: "info"
  format: "text"
`)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("initial Load: %v", err)
	}
	if cfg.Bot.QQ.Webhook.Port != 8080 {
		t.Fatalf("expected port 8080, got %d", cfg.Bot.QQ.Webhook.Port)
	}

	// 注册监听器
	listenerCalled := false
	var received *config.Config
	token := config.Subscribe(func(c *config.Config) {
		listenerCalled = true
		received = c
	})
	defer token.Cancel()

	// 更新配置文件并重载
	writeTempConfig(t, dir, `
bot:
  qq:
    webhook:
      host: "0.0.0.0"
      port: 9090
log:
  level: "debug"
  format: "json"
`)

	cfg2, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload Load: %v", err)
	}

	// config.Get() 返回新值
	if cfg2.Bot.QQ.Webhook.Port != 9090 {
		t.Errorf("port expected 9090, got %d", cfg2.Bot.QQ.Webhook.Port)
	}
	if cfg2.Log.Level != "debug" {
		t.Errorf("log.level expected debug, got %s", cfg2.Log.Level)
	}

	// 监听器被通知
	if !listenerCalled {
		t.Error("listener was not called")
	}
	if received == nil || received.Bot.QQ.Webhook.Port != 9090 {
		t.Errorf("listener received wrong config")
	}
}

// TestConfigHotReload_Invalid 验证无效配置不会覆盖当前运行中的配置。
func TestConfigHotReload_Invalid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTempConfig(t, dir, `
bot:
  qq:
    webhook:
      host: "0.0.0.0"
      port: 8080
log:
  level: "info"
  format: "text"
`)

	_, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("initial Load: %v", err)
	}

	// 写入无效配置（端口越界）
	writeTempConfig(t, dir, `
bot:
  qq:
    webhook:
      host: "0.0.0.0"
      port: 99999
log:
  level: "info"
  format: "text"
`)

	_, err = config.Load(cfgPath)
	if err == nil {
		t.Error("expected error for invalid port, got nil")
	}

	// config.Get() 仍返回旧的有效值
	after, ok := config.Get()
	if !ok {
		t.Fatal("Get() returned false after failed Load")
	}
	if after.Bot.QQ.Webhook.Port != 8080 {
		t.Errorf("port should remain 8080, got %d", after.Bot.QQ.Webhook.Port)
	}
}
