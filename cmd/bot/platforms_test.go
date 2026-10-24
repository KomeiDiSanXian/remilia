package main

import (
	"fmt"
	"testing"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/platform/onebot"
)

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
