package ai

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/infra/netguard"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestIsCommandMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"/ping", true},
		{"!cmd", true},
		{"!!cmd", true},
		{"hello", false},
		{"", false},
		{"   ", false},
		{"/", true},
	}
	for _, tt := range tests {
		got := runtime.IsCommandMessage(tt.msg)
		if got != tt.want {
			t.Errorf("runtime.IsCommandMessage(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestFormatAIError(t *testing.T) {
	tests := []struct {
		err  string
		want string
	}{
		{"API returned 401", "API 认证失败，请检查 api_key 配置"},
		{"error 401", "API 认证失败，请检查 api_key 配置"},
		{"API returned 404", "API 地址或模型名称错误，请检查 base_url 和 model 配置"},
		{"error 429", "请求过于频繁，请稍后再试"},
		{"context deadline exceeded", "请求超时，请检查网络连接或增大超时配置"},
		{"connection refused", "无法连接 API 服务器，请检查 base_url 配置"},
		{"no such host", "API 域名解析失败，请检查 base_url 配置"},
		{"unknown error", "AI 处理出错，请稍后再试"},
	}
	for _, tt := range tests {
		err := runtime.FormatAIError(errFromString(tt.err))
		if err != tt.want {
			t.Errorf("runtime.FormatAIError(%q) = %q, want %q", tt.err, err, tt.want)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"127.0.0.1", false},
		{"192.168.1.1", false},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"169.254.1.1", false},
		{"224.0.0.1", false},
		{"::1", false},
		{"fe80::1", false},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		got := netguard.IsPublicIP(ip)
		if got != tt.want {
			t.Errorf("netguard.IsPublicIP(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestIsPublicIPNil(t *testing.T) {
	if netguard.IsPublicIP(nil) {
		t.Error("nil IP should not be public")
	}
}

func TestMakeSessionID(t *testing.T) {
	id := runtime.MakeSessionID("discord", "123", "456")
	if id != "discord:123:456" {
		t.Errorf("expected %q, got %q", "discord:123:456", id)
	}
}

func TestFormatDuration(t *testing.T) {
	admin := &adminState{}
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{90 * time.Minute, "1h 30m"},
		{2*time.Hour + 15*time.Minute, "2h 15m"},
		{0, "0m"},
	}
	for _, tt := range tests {
		got := admin.formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestExtractSkillDescription(t *testing.T) {
	catalog := &catalogState{}
	tests := []struct {
		prompt string
		want   string
	}{
		{"First line\nSecond line", "First line"},
		{"Single line", "Single line"},
		{"", ""},
		{"   Trimmed   ", "Trimmed"},
	}
	for _, tt := range tests {
		got := catalog.extractSkillDescription(tt.prompt)
		if got != tt.want {
			t.Errorf("extractSkillDescription(%q) = %q, want %q", tt.prompt, got, tt.want)
		}
	}
}

func TestExtractSkillDescriptionLong(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := (&catalogState{}).extractSkillDescription(long)
	if len(got) > 203 {
		t.Errorf("description too long: %d chars", len(got))
	}
}

func TestCaptureSender(t *testing.T) {
	cs := &execution.CaptureSender{}
	req := platform.SendRequest{
		Message: platform.OutboundMessage{
			Text: "hello world",
		},
	}
	_, err := cs.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if cs.CapturedText != "hello world" {
		t.Errorf("expected captured text %q, got %q", "hello world", cs.CapturedText)
	}
}

func TestCaptureSenderAttachments(t *testing.T) {
	cs := &execution.CaptureSender{}
	req := platform.SendRequest{
		Message: platform.OutboundMessage{
			Text: "with attachment",
			Attachments: []platform.Attachment{
				{URL: "http://example.com/img.png"},
			},
		},
	}
	cs.Send(context.Background(), req)
	if len(cs.CapturedAttachments) != 1 {
		t.Errorf("expected 1 captured attachment, got %d", len(cs.CapturedAttachments))
	}
}

func TestCaptureSenderMarkdownFallback(t *testing.T) {
	cs := &execution.CaptureSender{}
	req := platform.SendRequest{
		Message: platform.OutboundMessage{
			Markdown: "**markdown**",
		},
	}
	cs.Send(context.Background(), req)
	if cs.CapturedText != "**markdown**" {
		t.Errorf("expected captured markdown %q, got %q", "**markdown**", cs.CapturedText)
	}
}

func TestInferPartType(t *testing.T) {
	tests := []struct {
		mime string
		want protocol.ContentPartType
	}{
		{"image/jpeg", protocol.ContentPartImage},
		{"image/png", protocol.ContentPartImage},
		{"audio/wav", protocol.ContentPartAudio},
		{"audio/mpeg", protocol.ContentPartAudio},
		{"text/plain", ""},
		{"application/json", ""},
	}
	for _, tt := range tests {
		got := runtime.InferPartType(tt.mime)
		if got != tt.want {
			t.Errorf("runtime.InferPartType(%q) = %q, want %q", tt.mime, got, tt.want)
		}
	}
}

func TestInferAudioFormat(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"audio/wav", "wav"},
		{"audio/wave", "wav"},
		{"audio/x-wav", "wav"},
		{"audio/mpeg", "mp3"},
		{"audio/mp3", "mp3"},
		{"audio/L16", "pcm"},
		{"audio/l16", "pcm"},
		{"audio/ogg", ""},
		{"audio/flac", ""},
	}
	for _, tt := range tests {
		got := runtime.InferAudioFormat(tt.mime)
		if got != tt.want {
			t.Errorf("runtime.InferAudioFormat(%q) = %q, want %q", tt.mime, got, tt.want)
		}
	}
}

func TestBuildHealthProbeURL(t *testing.T) {
	tests := []struct {
		cfg  config.Config
		want string
	}{
		{config.Config{Provider: "openai", BaseURL: "https://api.openai.com/v1"}, "https://api.openai.com/v1/models"},
		{config.Config{Provider: "anthropic", BaseURL: "https://api.anthropic.com"}, "https://api.anthropic.com/v1/messages"},
		{config.Config{Provider: "anthropic", BaseURL: "https://api.anthropic.com/v1"}, "https://api.anthropic.com/v1/messages"},
	}
	for _, tt := range tests {
		got := buildHealthProbeURL(&tt.cfg)
		if got != tt.want {
			t.Errorf("buildHealthProbeURL(%+v) = %q, want %q", tt.cfg, got, tt.want)
		}
	}
}

// Helpers

type errString string

func (e errString) Error() string { return string(e) }

func errFromString(s string) error { return errString(s) }
