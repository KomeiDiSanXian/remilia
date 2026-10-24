package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestQQConfig_ResolveMode 固化接入方式的解析语义：
// 留空/未知值一律回退 webhook，保证老配置行为不变。
func TestQQConfig_ResolveMode(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{"", QQModeWebhook},
		{QQModeWebhook, QQModeWebhook},
		{QQModeWS, QQModeWS},
		{"websocket", QQModeWebhook}, // 未知值由 Validate 拒绝，解析时兜底
	}
	for _, tt := range tests {
		t.Run("mode="+tt.mode, func(t *testing.T) {
			assert.Equal(t, tt.want, QQConfig{Mode: tt.mode}.ResolveMode())
		})
	}
}

// TestQQConfig_ValidateMode 校验 mode 取值与 intents 下界。
func TestQQConfig_ValidateMode(t *testing.T) {
	base := QQConfig{AppID: 1, BotID: 2, Token: "t", Secret: "s"}

	invalid := base
	invalid.Mode = "websocket"
	assert.ErrorContains(t, invalid.Validate(), "qq.mode")

	negative := base
	negative.Intents = -1
	assert.ErrorContains(t, negative.Validate(), "qq.intents")

	for _, mode := range []string{"", QQModeWebhook, QQModeWS} {
		cfg := base
		cfg.Mode = mode
		assert.NoError(t, cfg.Validate(), "mode=%q", mode)
	}
}

// TestOneBotConfig_ResolveMode 固化 OneBot 三种接入方式的推断规则：
// 老配置只写 url 时必须继续走 forward_ws。
func TestOneBotConfig_ResolveMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  OneBotConfig
		want string
	}{
		{"empty defaults to forward", OneBotConfig{}, OneBotModeForwardWS},
		{"url only", OneBotConfig{URL: "ws://127.0.0.1:6700"}, OneBotModeForwardWS},
		{"listen only", OneBotConfig{ListenAddr: ":8080"}, OneBotModeReverseWS},
		{"both prefers url", OneBotConfig{URL: "ws://h:6700", ListenAddr: ":8080"}, OneBotModeForwardWS},
		{"explicit reverse", OneBotConfig{Mode: OneBotModeReverseWS, ListenAddr: ":8080"}, OneBotModeReverseWS},
		{"explicit http post", OneBotConfig{Mode: OneBotModeHTTPPost}, OneBotModeHTTPPost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.ResolveMode())
		})
	}
}

// TestOneBotConfig_ValidateMode 按接入方式校验必填字段。
func TestOneBotConfig_ValidateMode(t *testing.T) {
	tests := []struct {
		name    string
		cfg     OneBotConfig
		wantErr string
	}{
		{"forward ws needs url", OneBotConfig{Mode: OneBotModeForwardWS}, "onebot.url"},
		{"forward ws ok", OneBotConfig{Mode: OneBotModeForwardWS, URL: "ws://h:6700"}, ""},
		{"reverse ws needs listen", OneBotConfig{Mode: OneBotModeReverseWS}, "onebot.listen_addr"},
		{"reverse ws ok", OneBotConfig{Mode: OneBotModeReverseWS, ListenAddr: ":8080"}, ""},
		{"http post needs listen", OneBotConfig{Mode: OneBotModeHTTPPost, URL: "http://h:5700"}, "onebot.listen_addr"},
		{"http post needs api url", OneBotConfig{Mode: OneBotModeHTTPPost, ListenAddr: ":8080"}, "onebot.url"},
		{"http post ok", OneBotConfig{Mode: OneBotModeHTTPPost, ListenAddr: ":8080", URL: "http://h:5700"}, ""},
		{"unknown mode", OneBotConfig{Mode: "sse", URL: "ws://h:6700"}, "onebot.mode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
