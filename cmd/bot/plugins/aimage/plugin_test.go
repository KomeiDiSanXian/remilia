// Package aimage plugin_test.go — 插件配置、工具注册与执行逻辑测试。
package aimage

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// fakeConfig 最小化的 plugin.ConfigReader 实现（与其它插件测试一致）。
type fakeConfig struct {
	vals map[string]any
}

func (f *fakeConfig) Get(k string) any       { return f.vals[k] }
func (f *fakeConfig) GetAll() map[string]any { return f.vals }
func (f *fakeConfig) GetString(k, d string) string {
	if v, ok := f.vals[k].(string); ok {
		return v
	}
	return d
}
func (f *fakeConfig) GetInt(k string, d int) int {
	if v, ok := f.vals[k].(int); ok {
		return v
	}
	return d
}
func (f *fakeConfig) GetBool(k string, d bool) bool {
	if v, ok := f.vals[k].(bool); ok {
		return v
	}
	return d
}
func (f *fakeConfig) GetDuration(k string, d time.Duration) time.Duration {
	if v, ok := f.vals[k].(time.Duration); ok {
		return v
	}
	return d
}
func (f *fakeConfig) GetFloat64(k string, d float64) float64 {
	if v, ok := f.vals[k].(float64); ok {
		return v
	}
	return d
}
func (f *fakeConfig) GetStringSlice(k string, d []string) []string {
	if v, ok := f.vals[k].([]string); ok {
		return v
	}
	return d
}
func (f *fakeConfig) GetStringMap(k string, d map[string]any) map[string]any {
	if v, ok := f.vals[k].(map[string]any); ok {
		return v
	}
	return d
}

var _ plugin.ConfigReader = (*fakeConfig)(nil)

// fakeSender 记录通过 ToolSender 发送的消息。
type fakeSender struct {
	sent []platform.OutboundMessage
}

func (f *fakeSender) ReplyToChat(_ context.Context, msg platform.OutboundMessage) (platform.SendResult, error) {
	f.sent = append(f.sent, msg)
	return platform.SendResult{}, nil
}
func (f *fakeSender) SendTo(_ context.Context, _ ai.ChatTarget, _ platform.OutboundMessage) (platform.SendResult, error) {
	return platform.SendResult{}, nil
}
func (f *fakeSender) ResolveTarget(_ context.Context, _ string, _ bool) (ai.ChatTarget, string, error) {
	return ai.ChatTarget{}, "", nil
}

var _ ai.ToolSender = (*fakeSender)(nil)

func testPluginWithServer(t *testing.T, handler http.HandlerFunc) (*Plugin, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{
		"enabled":  true,
		"base_url": srv.URL,
	}}}
	c, err := newImageClient(p.provider(), p.baseURL(), p.apiKey(), p.model(), p.size(), p.steps(), p.cfgScale(), p.negativePrompt(), p.timeout(), p.proxy())
	if err != nil {
		srv.Close()
		t.Fatalf("newImageClient: %v", err)
	}
	p.client = c
	return p, srv
}

func TestListToolsDisabled(t *testing.T) {
	p := &Plugin{} // client nil = 未启用
	if tools := p.ListTools(); tools != nil {
		t.Fatalf("expected no tools when disabled, got %d", len(tools))
	}
}

func TestListToolsEnabled(t *testing.T) {
	c, err := newImageClient(providerOpenAI, "http://127.0.0.1:8080", "", "", "1024x1024", 20, 7, "", time.Second, "")
	if err != nil {
		t.Fatalf("newImageClient: %v", err)
	}
	p := &Plugin{client: c}
	tools := p.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]
	if tool.Name != "generate_image" {
		t.Errorf("unexpected name %q", tool.Name)
	}
	if !tool.RequiresApproval {
		t.Error("expected RequiresApproval=true")
	}
	if len(tool.Categories) != 1 || tool.Categories[0] != ai.CategoryGeneral {
		t.Errorf("unexpected categories %v", tool.Categories)
	}
	if len(tool.Parameters.Required) != 1 || tool.Parameters.Required[0] != "prompt" {
		t.Errorf("unexpected required params %v", tool.Parameters.Required)
	}
}

func TestExecuteGenerateImageSends(t *testing.T) {
	png := testPNG()
	p, srv := testPluginWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	})
	defer srv.Close()

	sender := &fakeSender{}
	ctx := ai.WithToolSender(context.Background(), sender)
	out, err := p.executeGenerateImage(ctx, map[string]any{"prompt": "一只猫"})
	if err != nil {
		t.Fatalf("executeGenerateImage: %v", err)
	}
	if !strings.Contains(out, "已生成并发送 1 张图片") {
		t.Errorf("unexpected output %q", out)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sender.sent))
	}
	atts := sender.sent[0].Attachments
	if len(atts) != 1 || atts[0].Kind != platform.AttachmentKindImage || len(atts[0].Data) == 0 {
		t.Errorf("unexpected attachments: %+v", atts)
	}
}

func TestExecuteGenerateImageClampsN(t *testing.T) {
	png := testPNG()
	var gotN int
	p, srv := testPluginWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req openAIGenRequest
		if err := jsonDecode(r, &req); err != nil {
			t.Errorf("bad body: %v", err)
			return
		}
		gotN = req.N
		w.Header().Set("Content-Type", "application/json")
		var sb strings.Builder
		sb.WriteString(`{"data":[`)
		for i := 0; i < req.N; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(`{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}`)
		}
		sb.WriteString(`]}`)
		_, _ = w.Write([]byte(sb.String()))
	})
	defer srv.Close()
	p.cfg = &fakeConfig{vals: map[string]any{"enabled": true, "base_url": srv.URL, "max_n": 2}}

	sender := &fakeSender{}
	ctx := ai.WithToolSender(context.Background(), sender)
	out, err := p.executeGenerateImage(ctx, map[string]any{"prompt": "一只猫", "n": float64(10)})
	if err != nil {
		t.Fatalf("executeGenerateImage: %v", err)
	}
	if gotN != 2 {
		t.Errorf("expected n clamped to 2, got %d", gotN)
	}
	if !strings.Contains(out, "已生成并发送 2 张图片") {
		t.Errorf("unexpected output %q", out)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sender.sent))
	}
	if got := len(sender.sent[0].Attachments); got != 2 {
		t.Errorf("expected 2 attachments in one message, got %d", got)
	}
}

func TestExecuteGenerateImageMissingPrompt(t *testing.T) {
	p, srv := testPluginWithServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()
	if _, err := p.executeGenerateImage(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing prompt")
	}
}

func TestExecuteGenerateImageInvalidSize(t *testing.T) {
	p, srv := testPluginWithServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()
	if _, err := p.executeGenerateImage(context.Background(), map[string]any{"prompt": "猫", "size": "1024"}); err == nil {
		t.Error("expected error for invalid size")
	}
}

func TestExecuteGenerateImageNoSender(t *testing.T) {
	png := testPNG()
	p, srv := testPluginWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	})
	defer srv.Close()

	out, err := p.executeGenerateImage(context.Background(), map[string]any{"prompt": "一只猫"})
	if err != nil {
		t.Fatalf("executeGenerateImage: %v", err)
	}
	if !strings.Contains(out, "已生成 1 张图片") {
		t.Errorf("expected fallback text, got %q", out)
	}
}

func TestConfigDefaults(t *testing.T) {
	p := &Plugin{} // cfg nil
	if got := p.provider(); got != providerOpenAI {
		t.Errorf("provider default = %q", got)
	}
	if got := p.size(); got != "1024x1024" {
		t.Errorf("size default = %q", got)
	}
	if got := p.maxN(); got != 3 {
		t.Errorf("max_n default = %d", got)
	}
	if got := p.timeout(); got != 120*time.Second {
		t.Errorf("timeout default = %v", got)
	}
	if got := p.steps(); got != 20 {
		t.Errorf("steps default = %d", got)
	}
	if got := p.cfgScale(); got != 7 {
		t.Errorf("cfg_scale default = %v", got)
	}
	if p.enabled() {
		t.Error("enabled default should be false")
	}
}

func TestConfigOverrides(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{
		"enabled":         true,
		"provider":        providerSDWebUI,
		"base_url":        "http://127.0.0.1:7860",
		"api_key":         "k",
		"model":           "m",
		"size":            "768x512",
		"max_n":           5,
		"timeout":         30 * time.Second,
		"steps":           30,
		"cfg_scale":       6.5,
		"negative_prompt": "低质量",
		"proxy":           "http://127.0.0.1:7890",
	}}}
	if !p.enabled() {
		t.Error("enabled should be true")
	}
	if p.provider() != providerSDWebUI || p.baseURL() != "http://127.0.0.1:7860" || p.apiKey() != "k" || p.model() != "m" {
		t.Error("provider/base_url/api_key/model overrides not applied")
	}
	if p.size() != "768x512" || p.maxN() != 5 || p.timeout() != 30*time.Second || p.steps() != 30 || p.cfgScale() != 6.5 || p.negativePrompt() != "低质量" || p.proxy() != "http://127.0.0.1:7890" {
		t.Error("size/max_n/timeout/steps/cfg_scale/negative_prompt/proxy overrides not applied")
	}
}
