package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/middleware/degradation"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/onebot"
)

// TestPlatformFactories_SatoriAndDiscord 固化 satori / discord 的接入方式映射。
//
// 两种 webhook 形态此前都只在包里实现、没有装配入口：
// satori 的 WebhookAdapter 与 discord 的 InteractionsAdapter 在 cmd/bot 里不可达。
func TestPlatformFactories_SatoriAndDiscord(t *testing.T) {
	t.Run("satori webhook", func(t *testing.T) {
		cfg := &config.Config{Bot: config.BotConfig{Satori: &config.SatoriConfig{
			ServerURL: "http://localhost:5140",
			Platform:  "chronocat",
			UserID:    "1234567890",
			Webhook:   &config.SatoriWebhookConfig{ListenAddr: ":5140"},
		}}}
		a := mustBuildAdapter(t, cfg, "satori")
		if got, want := fmt.Sprintf("%T", a), "*satori.WebhookAdapter"; got != want {
			t.Fatalf("adapter type = %s, want %s", got, want)
		}
		if a.Sender() == nil {
			t.Fatal("webhook adapter must have a sender bound via WithSendConfig")
		}
	})

	t.Run("satori ws", func(t *testing.T) {
		cfg := &config.Config{Bot: config.BotConfig{Satori: &config.SatoriConfig{
			ServerURL: "http://localhost:5140",
			Platform:  "chronocat",
			UserID:    "1234567890",
		}}}
		a := mustBuildAdapter(t, cfg, "satori")
		if got, want := fmt.Sprintf("%T", a), "*satori.Adapter"; got != want {
			t.Fatalf("adapter type = %s, want %s", got, want)
		}
	})

	t.Run("discord interactions", func(t *testing.T) {
		cfg := &config.Config{Bot: config.BotConfig{Discord: &config.DiscordConfig{
			Token: "token",
			Interactions: &config.DiscordInteractionsConfig{
				ListenAddr: ":8080",
				PublicKey:  strings.Repeat("ab", 32),
				AckTimeout: "2s",
			},
		}}}
		a := mustBuildAdapter(t, cfg, "discord")
		if got, want := fmt.Sprintf("%T", a), "*discord.InteractionsAdapter"; got != want {
			t.Fatalf("adapter type = %s, want %s", got, want)
		}
	})

	t.Run("satori webhook without block", func(t *testing.T) {
		cfg := &config.Config{Bot: config.BotConfig{Satori: &config.SatoriConfig{
			Mode:      config.SatoriModeWebhook,
			ServerURL: "http://localhost:5140",
		}}}
		_, err := platformFactories(cfg)["satori"]()
		if err == nil {
			t.Fatal("expected error when mode=webhook without webhook block")
		}
	})
}

func mustBuildAdapter(t *testing.T, cfg *config.Config, name string) platform.Adapter {
	t.Helper()
	factory, ok := platformFactories(cfg)[name]
	if !ok {
		t.Fatalf("%s factory not registered", name)
	}
	a, err := factory()
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	return a
}

// TestPlatformFactories_OneBotModes 固化 onebot.mode → 适配器实现的映射。
//
// 三种接入方式共用同一份配置，切换方式只应改变传输层：
// 反向 WS 与 HTTP POST 此前在装配层被写死为正向 WS，导致
// config.Bot.OneBot.ListenAddr 完全不生效（见 platforms.go）。
func TestPlatformFactories_OneBotModes(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.OneBotConfig
		want string
	}{
		{
			name: "forward ws by url",
			cfg:  config.OneBotConfig{URL: "ws://127.0.0.1:6700"},
			want: "*onebot.ForwardWSAdapter",
		},
		{
			name: "explicit reverse ws",
			cfg:  config.OneBotConfig{Mode: config.OneBotModeReverseWS, ListenAddr: ":8080"},
			want: "*onebot.ReverseWSAdapter",
		},
		{
			name: "http post",
			cfg: config.OneBotConfig{
				Mode:       config.OneBotModeHTTPPost,
				ListenAddr: ":8080",
				URL:        "http://127.0.0.1:5700",
			},
			want: "*onebot.HTTPPostAdapter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Bot: config.BotConfig{OneBot: &tt.cfg}}
			factory, ok := platformFactories(cfg)["onebot"]
			if !ok {
				t.Fatal("onebot factory not registered")
			}
			a, err := factory()
			if err != nil {
				t.Fatalf("factory: %v", err)
			}
			if got := fmt.Sprintf("%T", a); got != tt.want {
				t.Fatalf("adapter type = %s, want %s", got, tt.want)
			}
			if got := a.Platform(); got != onebot.PlatformID {
				t.Fatalf("platform id = %q, want %q", got, onebot.PlatformID)
			}
		})
	}
}

// TestTelegramWebhookConfigMapping 固化 telegram.webhook → telegram.WebhookConfig
// 的字段映射与默认值（fail-fast 分支不触发网络）。
func TestTelegramWebhookConfigMapping(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		hook, err := telegramWebhookConfig(&config.TelegramWebhookConfig{
			PublicURL: "https://bot.example.com/tg",
		})
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		if hook.ListenAddr != "0.0.0.0:8443" {
			t.Fatalf("ListenAddr = %q, want 0.0.0.0:8443", hook.ListenAddr)
		}
		if hook.PublicURL != "https://bot.example.com/tg" {
			t.Fatalf("PublicURL = %q", hook.PublicURL)
		}
		if hook.ShutdownTimeout != 0 {
			t.Fatalf("ShutdownTimeout = %v, want zero (adapter default)", hook.ShutdownTimeout)
		}
	})

	t.Run("explicit values", func(t *testing.T) {
		hook, err := telegramWebhookConfig(&config.TelegramWebhookConfig{
			Host:            "127.0.0.1",
			Port:            9000,
			Path:            "/hook",
			PublicURL:       "https://bot.example.com/hook",
			SecretToken:     "s3cret",
			ShutdownTimeout: "5s",
			EventBuffer:     16,
			WorkerCount:     2,
		})
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		if hook.ListenAddr != "127.0.0.1:9000" {
			t.Fatalf("ListenAddr = %q", hook.ListenAddr)
		}
		if hook.Path != "/hook" || hook.SecretToken != "s3cret" {
			t.Fatalf("path/secret = %q/%q", hook.Path, hook.SecretToken)
		}
		if hook.ShutdownTimeout != 5*time.Second {
			t.Fatalf("ShutdownTimeout = %v, want 5s", hook.ShutdownTimeout)
		}
		if hook.EventBuffer != 16 || hook.WorkerCount != 2 {
			t.Fatalf("buffer/workers = %d/%d", hook.EventBuffer, hook.WorkerCount)
		}
	})

	t.Run("bad timeout", func(t *testing.T) {
		if _, err := telegramWebhookConfig(&config.TelegramWebhookConfig{
			PublicURL:       "https://bot.example.com",
			ShutdownTimeout: "soon",
		}); err == nil {
			t.Fatal("expected error for invalid shutdown_timeout")
		}
	})
}

// TestPlatformFactories_TelegramModeGuard mode=webhook 但缺少 webhook 块时，
// 工厂必须在触发网络之前返回错误。
func TestPlatformFactories_TelegramModeGuard(t *testing.T) {
	cfg := &config.Config{Bot: config.BotConfig{Telegram: &config.TelegramConfig{
		Mode:  config.TelegramModeWebhook,
		Token: "t",
	}}}
	if _, err := platformFactories(cfg)["telegram"](); err == nil {
		t.Fatal("expected error when mode=webhook without webhook block")
	}
}

// TestPlatformFactories_WeChat 固化 wechat.mode → 适配器构造的映射。
//
// 两种接入方式共用 platform/wechat 的同一个 Adapter 类型（webhook 只是
// Start 内部换传输层），因此这里断言构造成功与能力差异，而不是类型名。
func TestPlatformFactories_WeChat(t *testing.T) {
	t.Run("ws 默认", func(t *testing.T) {
		cfg := &config.Config{Bot: config.BotConfig{WeChat: &config.WeChatConfig{
			BotID:  "bot-1",
			Secret: "secret",
		}}}
		a := mustBuildAdapter(t, cfg, "wechat")
		if got := a.Platform(); got != "wechat" {
			t.Fatalf("platform = %q", got)
		}
		if !a.Capabilities().FileUpload {
			t.Fatal("长连接模式应支持素材上传")
		}
	})

	t.Run("webhook", func(t *testing.T) {
		cfg := &config.Config{Bot: config.BotConfig{WeChat: &config.WeChatConfig{
			Mode:           config.WeChatModeWebhook,
			BotID:          "bot-1",
			Token:          "token",
			EncodingAESKey: strings.Repeat("a", 43),
			Webhook:        &config.WeChatWebhookConfig{ListenAddr: ":0", ShutdownTimeout: "5s"},
		}}}
		a := mustBuildAdapter(t, cfg, "wechat")
		if a.Capabilities().FileUpload {
			t.Fatal("URL 回调模式不应声明素材上传")
		}
		detail := a.(interface{ HealthDetail() map[string]any }).HealthDetail()
		if detail["connection"] != config.WeChatModeWebhook {
			t.Fatalf("connection = %v", detail["connection"])
		}
		if detail["listen_addr"] != ":0" {
			t.Fatalf("listen_addr = %v", detail["listen_addr"])
		}
	})

	t.Run("webhook 缺少凭证", func(t *testing.T) {
		cfg := &config.Config{Bot: config.BotConfig{WeChat: &config.WeChatConfig{
			Mode:  config.WeChatModeWebhook,
			BotID: "bot-1",
		}}}
		if _, err := platformFactories(cfg)["wechat"](); err == nil {
			t.Fatal("缺少 token/encoding_aes_key 应报错")
		}
	})

	t.Run("ws 缺少 secret", func(t *testing.T) {
		cfg := &config.Config{Bot: config.BotConfig{WeChat: &config.WeChatConfig{
			BotID: "bot-1",
		}}}
		if _, err := platformFactories(cfg)["wechat"](); err == nil {
			t.Fatal("ws 模式缺少 secret 应报错")
		}
	})
}

// TestWeChatConfig_Validate 配置层校验与适配器构造的口径一致。
func TestWeChatConfig_Validate(t *testing.T) {
	if err := (&config.WeChatConfig{}).Validate(); err == nil {
		t.Fatal("缺少 bot_id 应报错")
	}
	if err := (&config.WeChatConfig{BotID: "b", Secret: "s"}).Validate(); err != nil {
		t.Fatalf("合法 ws 配置不应报错: %v", err)
	}
	if err := (&config.WeChatConfig{BotID: "b", Mode: config.WeChatModeWebhook,
		Token: "t", EncodingAESKey: strings.Repeat("a", 42),
		Webhook: &config.WeChatWebhookConfig{ListenAddr: ":0"}}).Validate(); err == nil {
		t.Fatal("encoding_aes_key 非 43 位应报错")
	}
}

// TestWeChatConfig_HasChanged wechat 凭证变化必须触发平台热更新。
func TestWeChatConfig_HasChanged(t *testing.T) {
	base := config.BotConfig{WeChat: &config.WeChatConfig{BotID: "b", Secret: "s"}}
	changed := config.BotConfig{WeChat: &config.WeChatConfig{BotID: "b", Secret: "s2"}}
	if !base.HasChanged(&changed) {
		t.Fatal("wechat 配置变化应被检测到")
	}
	same := config.BotConfig{WeChat: &config.WeChatConfig{BotID: "b", Secret: "s"}}
	if base.HasChanged(&same) {
		t.Fatal("相同配置不应报告变化")
	}
}

// TestParseDegradationStrategy 固化配置字符串 → 降级策略的映射（未知值按 drop）。
func TestParseDegradationStrategy(t *testing.T) {
	cases := map[string]degradation.DegradationStrategy{
		"drop":     degradation.DegradationDrop,
		"delay":    degradation.DegradationDelay,
		"simplify": degradation.DegradationSimplify,
		"":         degradation.DegradationDrop,
		"unknown":  degradation.DegradationDrop,
	}
	for in, want := range cases {
		if got := parseDegradationStrategy(in); got != want {
			t.Errorf("parseDegradationStrategy(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestBuildAdmission 配置映射：未配置 io 段时用默认预算（允许排队），
// 显式配置时按配置生效。类别解析不再依赖插件名清单。
func TestBuildAdmission(t *testing.T) {
	a := &app{}

	got := a.buildAdmission(config.AdmissionConfig{})
	if got == nil {
		t.Fatal("buildAdmission must return an instance")
	}

	explicit := a.buildAdmission(config.AdmissionConfig{
		SlowP50: "800ms",
		Local:   config.AdmissionClassConfig{Limit: 7},
		IO:      config.AdmissionClassConfig{Limit: 3, Queue: 5, WaitTimeout: "2s"},
	})
	if explicit == nil {
		t.Fatal("buildAdmission must return an instance for explicit config")
	}
}
