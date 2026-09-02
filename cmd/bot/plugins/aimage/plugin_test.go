// Package aimage plugin_test.go — 插件配置、工具注册与执行逻辑测试。
package aimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand"
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

func TestSendMaxConfig(t *testing.T) {
	p := &Plugin{} // cfg nil
	if got := p.sendMaxBytes(); got != 5*1024*1024 {
		t.Errorf("send_max_bytes default = %d", got)
	}
	if got := p.sendMaxDimension(); got != 4096 {
		t.Errorf("send_max_dimension default = %d", got)
	}

	p2 := &Plugin{cfg: &fakeConfig{vals: map[string]any{
		"send_max_bytes":     1024 * 1024,
		"send_max_dimension": 2000,
	}}}
	if got := p2.sendMaxBytes(); got != 1024*1024 {
		t.Errorf("send_max_bytes override = %d", got)
	}
	if got := p2.sendMaxDimension(); got != 2000 {
		t.Errorf("send_max_dimension override = %d", got)
	}

	p3 := &Plugin{cfg: &fakeConfig{vals: map[string]any{"send_max_bytes": 0}}}
	if got := p3.sendMaxBytes(); got != 0 {
		t.Errorf("send_max_bytes=0 应关闭体积压缩，got %d", got)
	}
}

func TestAttachmentForUnderLimitPassthrough(t *testing.T) {
	p := &Plugin{}
	data := solidPNG(t, 100, 100)
	att := p.attachmentFor(imageResult{Data: data, MimeType: "image/png"})
	if att.Kind != platform.AttachmentKindImage {
		t.Errorf("unexpected kind %v", att.Kind)
	}
	if !bytes.Equal(att.Data, data) {
		t.Error("未超限时应原样返回图片数据")
	}
	if att.MimeType != "image/png" || att.Name != "aimage.png" {
		t.Errorf("unexpected mime/name: %q / %q", att.MimeType, att.Name)
	}
}

func TestAttachmentForCompressOverBytes(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"send_max_bytes": 256 * 1024}}}
	data := noisePNG(t, 600, 600)
	if len(data) <= 256*1024 {
		t.Fatalf("test noise PNG too small: %d bytes", len(data))
	}

	att := p.attachmentFor(imageResult{Data: data, MimeType: "image/png"})
	if len(att.Data) > 256*1024 {
		t.Errorf("压缩后仍超过体积上限: %d bytes", len(att.Data))
	}
	if att.MimeType != "image/jpeg" || att.Name != "aimage.jpg" {
		t.Errorf("unexpected mime/name after re-encode: %q / %q", att.MimeType, att.Name)
	}
}

func TestAttachmentForCompressOverDimension(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"send_max_dimension": 64}}}
	data := solidPNG(t, 512, 512)

	att := p.attachmentFor(imageResult{Data: data, MimeType: "image/png"})
	cfg, _, err := image.DecodeConfig(bytes.NewReader(att.Data))
	if err != nil {
		t.Fatalf("decode compressed image: %v", err)
	}
	if cfg.Width > 64 || cfg.Height > 64 {
		t.Errorf("超边长图片应等比缩小，got %dx%d", cfg.Width, cfg.Height)
	}
	if att.MimeType != "image/png" {
		t.Errorf("PNG 缩小后应保持 PNG，got %q", att.MimeType)
	}
}

func TestAttachmentForConfigDisabled(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{
		"send_max_bytes":     0,
		"send_max_dimension": 0,
	}}}
	data := noisePNG(t, 300, 300)
	att := p.attachmentFor(imageResult{Data: data, MimeType: "image/png"})
	if !bytes.Equal(att.Data, data) {
		t.Error("send_max_bytes/dimension=0 时应跳过压缩")
	}
	if att.MimeType != "image/png" {
		t.Errorf("unexpected mime %q", att.MimeType)
	}
}

func TestVariantKeyboardEnabled(t *testing.T) {
	p := &Plugin{} // cfg nil → 默认开启
	if !p.variantKeyboardEnabled() {
		t.Error("cfg nil 时应默认开启变体键盘")
	}
	p2 := &Plugin{cfg: &fakeConfig{vals: map[string]any{}}}
	if !p2.variantKeyboardEnabled() {
		t.Error("未配置时应默认开启变体键盘")
	}
	p3 := &Plugin{cfg: &fakeConfig{vals: map[string]any{"qq_variant_keyboard": false}}}
	if p3.variantKeyboardEnabled() {
		t.Error("qq_variant_keyboard=false 时应关闭变体键盘")
	}
}

func TestVariantKeyboard(t *testing.T) {
	p := &Plugin{}
	kb := p.variantKeyboard("一只戴帽子的橘猫")
	if kb == nil {
		t.Fatal("variantKeyboard 不应返回 nil")
	}
	rows := kb.Keyboard.Content.Rows
	if len(rows) != 2 {
		t.Fatalf("应为 2 行按钮，got %d", len(rows))
	}
	if len(rows[0].Buttons) != 3 || len(rows[1].Buttons) != 3 {
		t.Fatalf("每行应为 3 个按钮，got %d / %d", len(rows[0].Buttons), len(rows[1].Buttons))
	}

	want := []string{
		"/aimage 一只戴帽子的橘猫",
		"/aimage 一只戴帽子的橘猫，水彩风格",
		"/aimage 一只戴帽子的橘猫，赛博朋克风",
		"/aimage 一只戴帽子的橘猫 -size 1344x768",
		"/aimage 一只戴帽子的橘猫 -size 768x1344",
		"/aimage 一只戴帽子的橘猫 -size 1024x1024",
	}
	got := make([]string, 0, 6)
	for _, row := range rows {
		for _, b := range row.Buttons {
			got = append(got, b.RenderData.Label)
			if b.Action.Type != 2 {
				t.Errorf("按钮 %q 应为 type=2（填入输入框），got %d", b.RenderData.Label, b.Action.Type)
			}
		}
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("按钮[%d] = %q，want %q", i, got[i], w)
		}
	}

	// 空提示词不生成键盘。
	if kb2 := p.variantKeyboard("   "); kb2 != nil {
		t.Error("空提示词应返回 nil 键盘")
	}
}

func TestVariantKeyboardTruncatesLongPrompt(t *testing.T) {
	p := &Plugin{}
	long := "一只戴帽子的橘猫坐在窗台上晒太阳，背景是樱花树与远山"
	kb := p.variantKeyboard(long)
	if kb == nil {
		t.Fatal("variantKeyboard 不应返回 nil")
	}
	label := kb.Keyboard.Content.Rows[0].Buttons[0].RenderData.Label
	if got := []rune(label); len(got) > 24+len([]rune("/aimage ")) {
		t.Errorf("超长提示词应被截断，label %q 长度 %d", label, len(got))
	}
	if strings.Contains(label, "…") {
		t.Errorf("命令截断不应包含省略号，label %q", label)
	}
}

func TestVariantKeyboardMessageCarriesExtra(t *testing.T) {
	p := &Plugin{}
	msg := p.variantKeyboardMessage("一只猫")
	if msg.Text == "" {
		t.Error("键盘消息应带说明文本")
	}
	if len(msg.Extra) != 1 {
		t.Fatalf("应注入 QQ MessageExtra，got %d extra keys", len(msg.Extra))
	}
}

// solidPNG 生成纯色 PNG（体积小，用于边长/透传场景）。
func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 200, G: 120, B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

// noisePNG 生成随机噪点 PNG（压缩率极低，用于触发体积限制）。
func noisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(42))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{
				R: uint8(rng.Intn(256)),
				G: uint8(rng.Intn(256)),
				B: uint8(rng.Intn(256)),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}
