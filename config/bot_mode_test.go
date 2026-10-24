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

// TestSatoriConfig_ResolveMode 固化 Satori 接入方式的推断语义：
// 配置了 webhook 块即视为 webhook，否则维持 ws。
func TestSatoriConfig_ResolveMode(t *testing.T) {
	hook := &SatoriWebhookConfig{ListenAddr: ":5140"}

	tests := []struct {
		name string
		cfg  SatoriConfig
		want string
	}{
		{"empty defaults to ws", SatoriConfig{}, SatoriModeWS},
		{"webhook block infers webhook", SatoriConfig{Webhook: hook}, SatoriModeWebhook},
		{"explicit ws wins over block", SatoriConfig{Mode: SatoriModeWS, Webhook: hook}, SatoriModeWS},
		{"unknown kept as is", SatoriConfig{Mode: "sse"}, "sse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.ResolveMode())
		})
	}
}

// TestSatoriConfig_ValidateMode webhook 模式必须有监听地址，server_url 始终必填。
func TestSatoriConfig_ValidateMode(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SatoriConfig
		wantErr string
	}{
		{"missing server url", SatoriConfig{}, "satori.server_url"},
		{"ws ok", SatoriConfig{ServerURL: "http://localhost:5140"}, ""},
		{"webhook needs block", SatoriConfig{Mode: SatoriModeWebhook, ServerURL: "http://h"}, "satori.webhook"},
		{"webhook needs listen", SatoriConfig{Mode: SatoriModeWebhook, ServerURL: "http://h", Webhook: &SatoriWebhookConfig{}}, "satori.webhook.listen_addr"},
		{"webhook ok", SatoriConfig{Mode: SatoriModeWebhook, ServerURL: "http://h", Webhook: &SatoriWebhookConfig{ListenAddr: ":5140"}}, ""},
		{"unknown mode", SatoriConfig{Mode: "sse", ServerURL: "http://h"}, "satori.mode"},
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

// TestDiscordConfig_ResolveMode 固化 Discord 接入方式的推断语义：
// 配置了 interactions 块即视为 interactions，否则仍是 gateway。
func TestDiscordConfig_ResolveMode(t *testing.T) {
	ic := &DiscordInteractionsConfig{ListenAddr: ":8080", PublicKey: "ab"}

	tests := []struct {
		name string
		cfg  DiscordConfig
		want string
	}{
		{"empty defaults to gateway", DiscordConfig{}, DiscordModeGateway},
		{"interactions block infers interactions", DiscordConfig{Interactions: ic}, DiscordModeInteractions},
		{"explicit gateway wins over block", DiscordConfig{Mode: DiscordModeGateway, Interactions: ic}, DiscordModeGateway},
		{"unknown kept as is", DiscordConfig{Mode: "shard"}, "shard"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.ResolveMode())
		})
	}
}

// TestDiscordConfig_ValidateMode interactions 模式要求监听地址与公钥。
func TestDiscordConfig_ValidateMode(t *testing.T) {
	valid := DiscordInteractionsConfig{ListenAddr: ":8080", PublicKey: "ab"}
	badAck := valid
	badAck.AckTimeout = "soon"

	tests := []struct {
		name    string
		cfg     DiscordConfig
		wantErr string
	}{
		{"missing token", DiscordConfig{}, "discord.token"},
		{"gateway ok", DiscordConfig{Token: "t"}, ""},
		{"interactions needs block", DiscordConfig{Token: "t", Mode: DiscordModeInteractions}, "discord.interactions"},
		{"interactions needs listen", DiscordConfig{Token: "t", Mode: DiscordModeInteractions, Interactions: &DiscordInteractionsConfig{PublicKey: "ab"}}, "discord.interactions.listen_addr"},
		{"interactions needs public key", DiscordConfig{Token: "t", Mode: DiscordModeInteractions, Interactions: &DiscordInteractionsConfig{ListenAddr: ":8080"}}, "discord.interactions.public_key"},
		{"interactions bad ack timeout", DiscordConfig{Token: "t", Mode: DiscordModeInteractions, Interactions: &badAck}, "discord.interactions.ack_timeout"},
		{"interactions ok", DiscordConfig{Token: "t", Mode: DiscordModeInteractions, Interactions: &valid}, ""},
		{"interactions inferred from block", DiscordConfig{Token: "t", Interactions: &valid}, ""},
		{"unknown mode", DiscordConfig{Token: "t", Mode: "shard"}, "discord.mode"},
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

// TestTelegramConfig_ResolveMode 固化 Telegram 接入方式的推断语义：
// 配置了 webhook 块即视为 webhook，否则维持长轮询。
func TestTelegramConfig_ResolveMode(t *testing.T) {
	hook := &TelegramWebhookConfig{PublicURL: "https://bot.example.com/telegram"}

	tests := []struct {
		name string
		cfg  TelegramConfig
		want string
	}{
		{"empty defaults to polling", TelegramConfig{}, TelegramModePolling},
		{"webhook block infers webhook", TelegramConfig{Webhook: hook}, TelegramModeWebhook},
		{"explicit polling wins over block", TelegramConfig{Mode: TelegramModePolling, Webhook: hook}, TelegramModePolling},
		{"unknown kept as is", TelegramConfig{Mode: "websocket"}, "websocket"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.ResolveMode())
		})
	}
}

// TestTelegramConfig_ValidateMode webhook 模式要求 https 的 public_url。
func TestTelegramConfig_ValidateMode(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TelegramConfig
		wantErr string
	}{
		{"missing token", TelegramConfig{}, "telegram.token"},
		{"polling ok", TelegramConfig{Token: "t"}, ""},
		{"webhook needs block", TelegramConfig{Token: "t", Mode: TelegramModeWebhook}, "telegram.webhook"},
		{"webhook needs public url", TelegramConfig{Token: "t", Mode: TelegramModeWebhook, Webhook: &TelegramWebhookConfig{}}, "telegram.webhook.public_url"},
		{"webhook rejects http", TelegramConfig{Token: "t", Mode: TelegramModeWebhook, Webhook: &TelegramWebhookConfig{PublicURL: "http://bot.example.com"}}, "https://"},
		{"webhook bad timeout", TelegramConfig{Token: "t", Mode: TelegramModeWebhook, Webhook: &TelegramWebhookConfig{PublicURL: "https://bot.example.com", ShutdownTimeout: "soon"}}, "shutdown_timeout"},
		{"webhook bad port", TelegramConfig{Token: "t", Mode: TelegramModeWebhook, Webhook: &TelegramWebhookConfig{PublicURL: "https://bot.example.com", Port: 70000}}, "port"},
		{"webhook ok", TelegramConfig{Token: "t", Mode: TelegramModeWebhook, Webhook: &TelegramWebhookConfig{PublicURL: "https://bot.example.com/telegram", Port: 8443}}, ""},
		{"webhook inferred from block", TelegramConfig{Token: "t", Webhook: &TelegramWebhookConfig{PublicURL: "https://bot.example.com/telegram"}}, ""},
		{"unknown mode", TelegramConfig{Token: "t", Mode: "websocket"}, "telegram.mode"},
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
