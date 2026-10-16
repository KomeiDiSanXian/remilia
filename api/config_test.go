package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// validQQConfig 是可验证通过的最小 QQ 配置，含真实密钥用于脱敏/写回测试。
const validQQConfig = `
bot:
  qq:
    app_id: 123
    bot_id: 456
    token: "real-token"
    secret: "real-secret"
    webhook:
      host: "0.0.0.0"
      port: 8080
log:
  level: "info"
  format: "text"
`

// TestWriteConfigUpdate_CanonicalizesGoStyleKeys 回归：GET /config 返回 Go 字段名
// （Log / Bot），原样回传时必须命中配置文件里的小写 YAML 键（log / bot），
// 既不能写出并存的大小写重复段落，也不能让修改被静默忽略。
func TestWriteConfigUpdate_CanonicalizesGoStyleKeys(t *testing.T) {
	dir := t.TempDir()
	path := writeTempConfig(t, dir, validQQConfig)
	if _, err := config.Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	srv := &Server{configPath: path}
	// JSON 解码后数字为 float64，与 HTTP 请求路径一致。
	err := srv.writeConfigUpdate(map[string]any{
		"Log": map[string]any{"Level": "debug"},
		"Bot": map[string]any{"QQ": map[string]any{"Webhook": map[string]any{"Port": float64(9090)}}},
	})
	require.NoError(t, err)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "\nLog:")
	assert.NotContains(t, string(raw), "\nBot:")

	cfg, ok := config.Get()
	require.True(t, ok)
	assert.Equal(t, 9090, cfg.Bot.QQ.Webhook.Port)
	assert.Equal(t, "debug", cfg.Log.Level)
}

// TestWriteConfigUpdate_DropsMaskedSecrets 回归：原始 JSON 编辑器回传的脱敏占位符
// 不能覆盖磁盘上的真实密钥，同时同一请求里的真实修改必须正常写入。
func TestWriteConfigUpdate_DropsMaskedSecrets(t *testing.T) {
	dir := t.TempDir()
	path := writeTempConfig(t, dir, validQQConfig)
	if _, err := config.Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	cfg, ok := config.Get()
	require.True(t, ok)
	updates, ok := maskSensitive(cfg).(map[string]any)
	require.True(t, ok)

	bot := updates["Bot"].(map[string]any)
	qq := bot["QQ"].(map[string]any)
	placeholder, ok := qq["Token"].(string)
	require.True(t, ok)
	require.Contains(t, placeholder, "*")
	qq["Webhook"].(map[string]any)["Port"] = float64(9090)
	qq["Secret"] = "brand-new-secret"

	srv := &Server{configPath: path}
	require.NoError(t, srv.writeConfigUpdate(updates))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "***")

	reloaded, ok := config.Get()
	require.True(t, ok)
	assert.Equal(t, "real-token", reloaded.Bot.QQ.Token)
	assert.Equal(t, "brand-new-secret", reloaded.Bot.QQ.Secret)
	assert.Equal(t, 9090, reloaded.Bot.QQ.Webhook.Port)
}

// TestDropMaskedSecrets_ShortSecret 覆盖 5~6 位短密钥：占位符没有连续三个星号，
// 只能靠与当前值的精确比对识别。
func TestDropMaskedSecrets_ShortSecret(t *testing.T) {
	current := map[string]any{"bot": map[string]any{"qq": map[string]any{"token": "abcde"}}}
	updates := map[string]any{
		"Bot": map[string]any{"QQ": map[string]any{
			"Token":  maskSecret("abcde"),
			"Secret": "new-secret",
		}},
	}
	require.NotContains(t, maskSecret("abcde"), "***")

	dropMaskedSecrets(current, updates)

	qq := updates["Bot"].(map[string]any)["QQ"].(map[string]any)
	assert.NotContains(t, qq, "Token")
	assert.Equal(t, "new-secret", qq["Secret"])
}

// TestDropMaskedSecrets_KeepsPlainValues 不含星号的真实值必须原样保留。
func TestDropMaskedSecrets_KeepsPlainValues(t *testing.T) {
	current := map[string]any{"bot": map[string]any{"qq": map[string]any{"token": "old"}}}
	updates := map[string]any{"Bot": map[string]any{"QQ": map[string]any{"Token": "real-new-token"}}}

	dropMaskedSecrets(current, updates)

	assert.Equal(t, "real-new-token", updates["Bot"].(map[string]any)["QQ"].(map[string]any)["Token"])
}

// TestNormalizeNumericStrings 数字字符串只在目标字段本身是数字时还原，
// 普通字符串字段不受影响。
func TestNormalizeNumericStrings(t *testing.T) {
	current := map[string]any{
		"bot": map[string]any{
			"qq": map[string]any{"app_id": uint64(1), "name": "123"},
		},
	}
	updates := map[string]any{
		"Bot": map[string]any{
			"QQ": map[string]any{"AppID": "9007199254740993", "Name": "456", "Token": "abc"},
		},
	}

	normalizeNumericStrings(current, updates)

	qq := updates["Bot"].(map[string]any)["QQ"].(map[string]any)
	assert.Equal(t, uint64(9007199254740993), qq["AppID"])
	assert.Equal(t, "456", qq["Name"], "字符串字段不应被改成数字")
	assert.Equal(t, "abc", qq["Token"])
}

// TestConfigAPI_NumericIDRoundTrip 端到端回归：GET /config 的 QQ AppID/BotID 必须是
// 字符串（避免浏览器 Number 精度上限），原样 PUT 回传后配置文件里仍是数字、值不丢精度。
func TestConfigAPI_NumericIDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := writeTempConfig(t, dir, `
bot:
  qq:
    app_id: 9007199254740993
    bot_id: 456
    token: "real-token"
    secret: "real-secret"
    webhook:
      host: "0.0.0.0"
      port: 8080
log:
  level: "info"
  format: "text"
`)
	if _, err := config.Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	srv := NewServer(":0", "test-api-key", Deps{ConfigPath: path})
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	r.Header.Set(authHeader("test-api-key"))
	mux.ServeHTTP(w, r)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	qq := resp.Data["Bot"].(map[string]any)["QQ"].(map[string]any)
	assert.Equal(t, "9007199254740993", qq["AppID"])
	assert.Equal(t, "456", qq["BotID"])

	// 模拟仪表盘：改一个无关字段后把整棵（含字符串 ID 的）配置回传
	qq["Webhook"].(map[string]any)["Port"] = float64(9090)
	body, err := json.Marshal(map[string]any{"Bot": resp.Data["Bot"]})
	require.NoError(t, err)

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPut, "/api/v1/config", bytes.NewReader(body))
	r.Header.Set(authHeader("test-api-key"))
	r.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(w, r)
	require.Equal(t, 200, w.Code, w.Body.String())

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "app_id: 9007199254740993", "ID 应以数字落盘")
	assert.NotContains(t, string(raw), `"9007199254740993"`)

	reloaded, ok := config.Get()
	require.True(t, ok)
	assert.Equal(t, uint64(9007199254740993), reloaded.Bot.QQ.AppID)
	assert.Equal(t, uint64(456), reloaded.Bot.QQ.BotID)
	assert.Equal(t, 9090, reloaded.Bot.QQ.Webhook.Port)
}
