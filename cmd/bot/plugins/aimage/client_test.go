// Package aimage client_test.go — 客户端与工具函数的单元测试。
package aimage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// jsonDecode 解析请求体为 JSON，供测试断言请求参数。
func jsonDecode(r *http.Request, v any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// testPNG 一个最小合法 PNG 头（8 字节 magic），供 DetectContentType 识别。
func testPNG() []byte {
	return []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
}

func TestNewImageClientValidation(t *testing.T) {
	if _, err := newImageClient(providerOpenAI, "", "", "", "1024x1024", 20, 7, "", time.Second, ""); err == nil {
		t.Error("expected error for empty base_url")
	}
	if _, err := newImageClient("unknown", "http://127.0.0.1:8080", "", "", "1024x1024", 20, 7, "", time.Second, ""); err == nil {
		t.Error("expected error for unknown provider")
	}
	c, err := newImageClient(providerOpenAI, "http://127.0.0.1:8080/v1/", "", "dall-e-3", "1024x1024", 20, 7, "", time.Second, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.baseURL != "http://127.0.0.1:8080/v1" {
		t.Errorf("baseURL should trim trailing slash, got %q", c.baseURL)
	}
}

func TestGenerateOpenAIB64(t *testing.T) {
	png := testPNG()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("unexpected Authorization %q", got)
		}
		var req openAIGenRequest
		if err := jsonDecode(r, &req); err != nil {
			t.Errorf("bad request body: %v", err)
			return
		}
		if req.Prompt != "一只猫" || req.N != 2 || req.Size != "512x512" || req.Model != "dall-e-3" {
			t.Errorf("unexpected request: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"},{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	}))
	defer srv.Close()

	c, err := newImageClient(providerOpenAI, srv.URL+"/v1", "test-key", "dall-e-3", "512x512", 20, 7, "", 5*time.Second, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	imgs, err := c.generate(context.Background(), "一只猫", 2)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("expected 2 images, got %d", len(imgs))
	}
	if imgs[0].MimeType != "image/png" {
		t.Errorf("unexpected mime %q", imgs[0].MimeType)
	}
	if len(imgs[0].Data) == 0 {
		t.Error("expected non-empty image data")
	}
}

func TestGenerateOpenAIURL(t *testing.T) {
	png := testPNG()
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gen" {
			_, _ = w.Write(png)
			return
		}
		http.NotFound(w, r)
	}))
	defer imgSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"url":"` + imgSrv.URL + `/gen"}]}`))
	}))
	defer apiSrv.Close()

	c, err := newImageClient(providerOpenAI, apiSrv.URL, "", "", "1024x1024", 20, 7, "", 5*time.Second, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 测试环境放行 http://127.0.0.1（生产默认 netguard.AllowURL 仅公网 https）
	c.allowURL = func(raw string) bool { return strings.HasPrefix(raw, "http://") }
	// 下载客户端默认带 netguard 公网拨号守卫，测试环境换成普通客户端
	c.download = &http.Client{Timeout: 5 * time.Second}

	imgs, err := c.generate(context.Background(), "一只猫", 1)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(imgs) != 1 || len(imgs[0].Data) == 0 {
		t.Fatalf("expected 1 downloaded image, got %d", len(imgs))
	}
}

func TestGenerateOpenAIAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"message":"insufficient quota"}}`))
	}))
	defer srv.Close()

	c, err := newImageClient(providerOpenAI, srv.URL, "", "", "1024x1024", 20, 7, "", 5*time.Second, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := c.generate(context.Background(), "一只猫", 1); err == nil || !strings.Contains(err.Error(), "insufficient quota") {
		t.Errorf("expected quota error, got %v", err)
	}
}

func TestGenerateOpenAIHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	c, err := newImageClient(providerOpenAI, srv.URL, "", "", "1024x1024", 20, 7, "", 5*time.Second, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := c.generate(context.Background(), "一只猫", 1); err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("expected 502 error, got %v", err)
	}
}

func TestGenerateSDWebUI(t *testing.T) {
	png := testPNG()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sdapi/v1/txt2img" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var req sdWebUIRequest
		if err := jsonDecode(r, &req); err != nil {
			t.Errorf("bad request body: %v", err)
			return
		}
		if req.Prompt != "一只猫" || req.Width != 512 || req.Height != 768 || req.Steps != 20 || req.CfgScale != 7.5 {
			t.Errorf("unexpected request: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":["` + base64.StdEncoding.EncodeToString(png) + `"]}`))
	}))
	defer srv.Close()

	c, err := newImageClient(providerSDWebUI, srv.URL, "", "", "512x768", 20, 7.5, "低质量,模糊", 5*time.Second, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	imgs, err := c.generate(context.Background(), "一只猫", 2)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("expected 2 images (loop per n), got %d", len(imgs))
	}
}

func TestGenerateSDWebUIEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[]}`))
	}))
	defer srv.Close()

	c, err := newImageClient(providerSDWebUI, srv.URL, "", "", "1024x1024", 20, 7, "", 5*time.Second, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := c.generate(context.Background(), "一只猫", 1); err == nil {
		t.Error("expected error for empty images")
	}
}

func TestGenerateEmptyPrompt(t *testing.T) {
	c, err := newImageClient(providerOpenAI, "http://127.0.0.1:8080", "", "", "1024x1024", 20, 7, "", time.Second, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := c.generate(context.Background(), "  ", 1); err == nil {
		t.Error("expected error for empty prompt")
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		in           string
		wantW, wantH int
		wantErr      bool
	}{
		{"1024x1024", 1024, 1024, false},
		{" 512 x 768 ", 512, 768, false},
		{"1024X768", 1024, 768, false},
		{"1024", 0, 0, true},
		{"axb", 0, 0, true},
		{"-1x100", 0, 0, true},
		{"", 0, 0, true},
	}
	for _, tc := range cases {
		w, h, err := parseSize(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseSize(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSize(%q): unexpected error %v", tc.in, err)
			continue
		}
		if w != tc.wantW || h != tc.wantH {
			t.Errorf("parseSize(%q) = %dx%d, want %dx%d", tc.in, w, h, tc.wantW, tc.wantH)
		}
	}
}

func TestDetectImageMime(t *testing.T) {
	if got := detectImageMime(testPNG()); got != "image/png" {
		t.Errorf("png magic: got %q", got)
	}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}
	if got := detectImageMime(jpeg); got != "image/jpeg" {
		t.Errorf("jpeg magic: got %q", got)
	}
	if got := detectImageMime([]byte("hello")); got != "image/png" {
		t.Errorf("unknown: got %q", got)
	}
}
