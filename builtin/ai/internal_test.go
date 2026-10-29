package ai

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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
		err  error
		want string
	}{
		// 结构化 APIError：精确按状态码映射。
		{&protocol.APIError{StatusCode: 401, Body: "invalid key"}, "API 认证失败，请检查 api_key 配置"},
		{&protocol.APIError{StatusCode: 403, Body: "forbidden"}, "API 认证失败，请检查 api_key 配置"},
		{&protocol.APIError{StatusCode: 404, Body: "model not found"}, "API 地址或模型名称错误，请检查 base_url 和 model 配置"},
		{&protocol.APIError{StatusCode: 429, Body: "rate limited"}, "请求过于频繁，请稍后再试"},
		// 500 也会被重试后上抛，落通用文案（避免对原始错误体 Leaky 展示）。
		{&protocol.APIError{StatusCode: 500, Body: "internal"}, "AI 处理出错，请稍后再试"},
		{errors.New("context deadline exceeded"), "请求超时，请检查网络连接或增大超时配置"},
		{errors.New("connection refused"), "无法连接 API 服务器，请检查 base_url 配置"},
		{errors.New("no such host"), "API 域名解析失败，请检查 base_url 配置"},
		// 子串误匹配防护：错误文本里的"1404"/时间戳不再触发状态码映射。
		{errors.New("failed at 1404 units in 401ms"), "AI 处理出错，请稍后再试"},
		// 工具深度超限的专门文案。
		{errors.New("超过最大工具调用深度 (5)"), "任务过于复杂，未能完成：请把任务拆成小步，或直接告诉我一步要做什么"},
		{errors.New("unknown error"), "AI 处理出错，请稍后再试"},
	}
	for i, tt := range tests {
		err := runtime.FormatAIError(tt.err)
		if err != tt.want {
			t.Errorf("case %d: runtime.FormatAIError(%v) = %q, want %q", i, tt.err, err, tt.want)
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

// TestAttachmentClientBlocksDirectLoopback 默认附件客户端必须阻断直连回环目标
// 与拨号层（解析结果）的内网地址。
//
// 回归背景：附件下载曾用裸 netguard.DialContext 且未设置 Proxy，既绕过代理，
// 又在真配上代理后把回环/内网代理本身判成 SSRF。代理放行现在由
// infra/outbound 统一处理（见 outbound.TestDialBypassesProxyAddress），
// 本用例只守住"默认策略仍然严格"。
func TestAttachmentClientBlocksDirectLoopback(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
		t.Setenv(key, "")
	}

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()

	if _, err := defaultAttachmentClient.Get(context.Background(), srv.URL+"/a.png"); err == nil {
		t.Fatal("direct loopback fetch must stay blocked")
	}
	if hits != 0 {
		t.Fatalf("blocked request must not reach the server, got %d hits", hits)
	}
}

func TestMakeSessionID(t *testing.T) {
	id := runtime.MakeSessionID("discord", "123", "456")
	if id != "discord:123:456" {
		t.Errorf("expected %q, got %q", "discord:123:456", id)
	}
}

func TestFormatDuration(t *testing.T) {
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
		got := formatDuration(tt.d)
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
		{config.Config{Provider: config.ProviderOpenAI, BaseURL: "https://api.openai.com/v1"}, "https://api.openai.com/v1/models"},
		{config.Config{Provider: config.ProviderAnthropic, BaseURL: "https://api.anthropic.com"}, "https://api.anthropic.com/v1/messages"},
		{config.Config{Provider: config.ProviderAnthropic, BaseURL: "https://api.anthropic.com/v1"}, "https://api.anthropic.com/v1/messages"},
	}
	for _, tt := range tests {
		got := buildHealthProbeURL(&tt.cfg)
		if got != tt.want {
			t.Errorf("buildHealthProbeURL(%+v) = %q, want %q", tt.cfg, got, tt.want)
		}
	}
}

// TestHealthProbeAuthPerProvider 守卫探针的认证协议：Anthropic 只认 x-api-key +
// anthropic-version，OpenAI 兼容端点用 Authorization: Bearer。用错 scheme 时探针
// 会恒定 401——而探针若又接受 4xx，就等于永远报健康。
func TestHealthProbeAuthPerProvider(t *testing.T) {
	cases := []struct {
		provider string
		want     map[string]string
	}{
		{config.ProviderAnthropic, map[string]string{"x-api-key": "k", "anthropic-version": protocol.AnthropicAPIVersion}},
		{config.ProviderOpenAI, map[string]string{"Authorization": "Bearer k"}},
	}
	for _, c := range cases {
		got := healthProbeHeaders(&config.Config{Provider: c.provider, APIKey: "k"})
		if len(got) != len(c.want) {
			t.Errorf("provider %q: headers = %v, want %v", c.provider, got, c.want)
			continue
		}
		for name, want := range c.want {
			if got[name] != want {
				t.Errorf("provider %q: header %q = %q, want %q", c.provider, name, got[name], want)
			}
		}
	}
	// Anthropic 必须不发 Bearer：那不是它的鉴权方式。
	if _, ok := healthProbeHeaders(&config.Config{Provider: config.ProviderAnthropic})["Authorization"]; ok {
		t.Error("anthropic probe must not send Authorization: Bearer")
	}
}

// TestHealthProbeRejectsClientErrors 守卫状态码接受范围：401/403 是"Key 失效"，
// 必须判不健康；429 表示鉴权通过但被限流，仍算健康。
func TestHealthProbeRejectsClientErrors(t *testing.T) {
	got := map[int]bool{}
	for _, code := range []int{200, 201, 204, 400, 401, 403, 404, 429, 500} {
		got[code] = healthAcceptsStatus(code)
	}
	want := map[int]bool{200: true, 201: true, 204: true, 400: false, 401: false, 403: false, 404: false, 429: true, 500: false}
	for code, w := range want {
		if got[code] != w {
			t.Errorf("accept status %d = %v, want %v", code, got[code], w)
		}
	}
}

// TestAttachmentTrustedMediaHost 回归：声明受信媒体来源（allow_private_media_hosts
// 或平台适配器自声明）后，回环/内网的附件地址（含明文 http）可下载；未配置时
// 仍按严格公网准入拒绝。
func TestAttachmentTrustedMediaHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	url := srv.URL + "/a.png"

	// 未初始化插件的默认客户端拒绝同一地址（降级路径不放大权限）。
	if _, err := (&Plugin{}).attachment().Get(context.Background(), url); err == nil {
		t.Fatal("unconfigured media host must stay blocked")
	}

	client, err := newAttachmentClient([]string{srv.URL})
	if err != nil {
		t.Fatalf("newAttachmentClient: %v", err)
	}
	p := &Plugin{attachmentClient: client}
	if err := p.attachment().Allow(url); err != nil {
		t.Fatalf("configured media host must be allowed: %v", err)
	}
	resp, err := p.attachment().Get(context.Background(), url)
	if err != nil {
		t.Fatalf("trusted media host download failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
