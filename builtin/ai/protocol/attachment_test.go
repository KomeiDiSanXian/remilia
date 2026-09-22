package protocol

import (
	"encoding/base64"
	"testing"

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
