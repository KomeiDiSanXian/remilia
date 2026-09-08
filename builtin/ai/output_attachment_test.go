package ai

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// --- attachmentFromImageURI ---

func TestAttachmentFromImageURI(t *testing.T) {
	pngData := []byte("fake-png-bytes")
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData)

	tests := []struct {
		name     string
		uri      string
		wantOK   bool
		wantURL  string
		wantData []byte
		wantMime string
	}{
		{name: "data URI 解码为二进制", uri: dataURI, wantOK: true, wantData: pngData, wantMime: "image/png"},
		{name: "远程 URL 透传", uri: "https://img.example.com/a.png", wantOK: true, wantURL: "https://img.example.com/a.png"},
		{name: "空 URI", uri: "", wantOK: false},
		{name: "非 http 协议", uri: "ftp://img.example.com/a.png", wantOK: false},
		{name: "data URI 缺 base64 标记", uri: "data:image/png,raw", wantOK: false},
		{name: "base64 非法", uri: "data:image/png;base64,!!!not-base64!!!", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			att, ok := attachmentFromImageURI(tt.uri)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if att.Kind != platform.AttachmentKindImage {
				t.Errorf("Kind = %q, want image", att.Kind)
			}
			if att.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", att.URL, tt.wantURL)
			}
			if string(att.Data) != string(tt.wantData) {
				t.Errorf("Data mismatch")
			}
			if att.MimeType != tt.wantMime {
				t.Errorf("MimeType = %q, want %q", att.MimeType, tt.wantMime)
			}
		})
	}
}

// --- OpenAI ---

// openAIImageOutFixture 生成一个返回图片输出（content 数组携带 image_url）的
// OpenAI 兼容测试服务器，respBody 为 content 字段的 JSON 片段。
func openAIImageOutServer(t *testing.T, contentJSON string) *httptest.Server {
	t.Helper()
	body := `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":` + contentJSON + `},"finish_reason":"stop"}]}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func pngDataURI(t *testing.T) string {
	t.Helper()
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
}

func TestOpenAIChatImageOutput(t *testing.T) {
	srv := openAIImageOutServer(t, `[{"type":"text","text":"给你画好了"},{"type":"image_url","image_url":{"url":"`+pngDataURI(t)+`"}}]`)
	defer srv.Close()

	prov, err := NewOpenAIProvider(&Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := prov.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "画一个苹果"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "给你画好了" {
		t.Errorf("Content = %q", resp.Content)
	}
	if len(resp.Attachments) != 1 {
		t.Fatalf("Attachments = %d, want 1", len(resp.Attachments))
	}
	att := resp.Attachments[0]
	if att.Kind != platform.AttachmentKindImage || string(att.Data) != "png-bytes" || att.MimeType != "image/png" {
		t.Errorf("unexpected attachment: kind=%q mime=%q data=%q", att.Kind, att.MimeType, att.Data)
	}
}

func TestOpenAIChatURLOutput(t *testing.T) {
	srv := openAIImageOutServer(t, `{"type":"image_url","image_url":{"url":"https://img.example.com/apple.png"}}`)
	defer srv.Close()

	prov, _ := NewOpenAIProvider(&Config{BaseURL: srv.URL, APIKey: "k"})
	resp, err := prov.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "画一个苹果"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Attachments) != 1 || resp.Attachments[0].URL != "https://img.example.com/apple.png" {
		t.Fatalf("Attachments = %+v", resp.Attachments)
	}
}

func TestOpenAIStreamImageOutput(t *testing.T) {
	// 流式：delta.content 数组携带图片（部分网关的多模态流式格式）+
	// OpenRouter 风格的 delta.images 字段。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		lines := []string{
			`data: {"choices":[{"delta":{"content":[{"type":"text","text":"画好了 "}]}}]}`,
			`data: {"choices":[{"delta":{"content":[{"type":"image_url","image_url":{"url":"` + pngDataURI(t) + `"}}]}}]}`,
			`data: {"choices":[{"delta":{"images":[{"type":"image_url","image_url":{"url":"https://img.example.com/orange.png"}}]}}]}`,
			`data: [DONE]`,
		}
		_, _ = w.Write([]byte(strings.Join(lines, "\n\n")))
	}))
	defer srv.Close()

	prov, err := NewOpenAIProvider(&Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := prov.ChatStream(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "画一个苹果"}}})
	if err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	var atts []platform.Attachment
	for ev := range ch {
		switch ev.Type {
		case StreamEventText:
			text.WriteString(ev.Content)
		case StreamEventAttachment:
			atts = append(atts, *ev.Attachment)
		case StreamEventError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if text.String() != "画好了 " {
		t.Errorf("text = %q", text.String())
	}
	if len(atts) != 2 {
		t.Fatalf("attachments = %d, want 2", len(atts))
	}
	if string(atts[0].Data) != "png-bytes" {
		t.Errorf("att[0].Data = %q", atts[0].Data)
	}
	if atts[1].URL != "https://img.example.com/orange.png" {
		t.Errorf("att[1].URL = %q", atts[1].URL)
	}
}

// --- Anthropic ---

func TestAnthropicChatImageOutput(t *testing.T) {
	imgB64 := base64.StdEncoding.EncodeToString([]byte("claude-png"))
	body := `{"id":"m","type":"message","role":"assistant","content":[` +
		`{"type":"text","text":"给你画好了"},` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + imgB64 + `"}}],` +
		`"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	prov, err := NewAnthropicProvider(&Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := prov.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "画一个苹果"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "给你画好了" {
		t.Errorf("Content = %q", resp.Content)
	}
	if len(resp.Attachments) != 1 {
		t.Fatalf("Attachments = %d, want 1", len(resp.Attachments))
	}
	att := resp.Attachments[0]
	if att.Kind != platform.AttachmentKindImage || string(att.Data) != "claude-png" || att.MimeType != "image/png" {
		t.Errorf("unexpected attachment: kind=%q mime=%q data=%q", att.Kind, att.MimeType, att.Data)
	}
}

func TestAnthropicStreamImageOutput(t *testing.T) {
	imgB64 := base64.StdEncoding.EncodeToString([]byte("claude-stream-png"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		lines := []string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"画好了"}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + imgB64 + `"}}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
		}
		_, _ = w.Write([]byte(strings.Join(lines, "\n")))
	}))
	defer srv.Close()

	prov, err := NewAnthropicProvider(&Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := prov.ChatStream(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "画一个苹果"}}})
	if err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	var atts []platform.Attachment
	for ev := range ch {
		switch ev.Type {
		case StreamEventText:
			text.WriteString(ev.Content)
		case StreamEventAttachment:
			atts = append(atts, *ev.Attachment)
		case StreamEventError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if text.String() != "画好了" {
		t.Errorf("text = %q", text.String())
	}
	if len(atts) != 1 || string(atts[0].Data) != "claude-stream-png" || atts[0].MimeType != "image/png" {
		t.Fatalf("attachments = %+v", atts)
	}
}

// --- 编排层：附件事件并入最终结果 ---

func TestProcessWithToolsStreamAttachment(t *testing.T) {
	p := &Plugin{
		cfg:      &Config{MaxDepth: 3, APITimeout: 5 * time.Second, ToolTimeout: 3 * time.Second},
		sm:       NewSessionManager(100, 20, time.Hour, nil),
		reg:      NewToolRegistry(),
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{
			chatStreamFn: func(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
				ch := make(chan StreamEvent, 4)
				ch <- StreamEvent{Type: StreamEventText, Content: "苹果画好了"}
				ch <- StreamEvent{Type: StreamEventAttachment, Attachment: &platform.Attachment{
					Kind:     platform.AttachmentKindImage,
					Data:     []byte("apple-image"),
					MimeType: "image/png",
				}}
				ch <- StreamEvent{Type: StreamEventDone}
				close(ch)
				return ch, nil
			},
		},
	}

	session := p.sm.GetOrCreate("test:img", "user", "chat")
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: "画一个苹果给我看"})

	evt := platform.NewSyntheticEvent("c2c", "画一个苹果给我看")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	result, err := p.processWithTools(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "苹果画好了" {
		t.Errorf("Text = %q", result.Text)
	}
	if len(result.Attachments) != 1 {
		t.Fatalf("Attachments = %d, want 1", len(result.Attachments))
	}
	att := result.Attachments[0]
	if att.Kind != platform.AttachmentKindImage || string(att.Data) != "apple-image" {
		t.Errorf("unexpected attachment: kind=%q data=%q", att.Kind, att.Data)
	}
}
