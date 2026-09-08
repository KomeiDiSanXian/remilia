// Package satori sender_upload_test.go — Data 附件自动上传的回归测试。
//
// 此前 sender 直接编码发送，Data-only 附件在 EncodeOutboundMessage 中被
// 静默跳过：消息正常发出但图片丢失且无任何错误。现在 Send 前会先经
// upload.create 上传并回填 URL。
package satori

import (
	stdctx "context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSendUploadsDataAttachments Data 附件应先上传换取 URL，
// 并以 <img> 元素随消息内容同条发送。
func TestSendUploadsDataAttachments(t *testing.T) {
	var uploadedPath, msgContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/upload.create":
			uploadedPath = r.URL.Path
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(`{"file_0":"https://cdn.example.com/up/abc.png"}`))
		case "/v1/message.create":
			var req map[string]any
			body, _ := io.ReadAll(r.Body)
			require.NoError(t, json.Unmarshal(body, &req))
			if c, ok := req["content"].(string); ok {
				msgContent = c
			}
			_, _ = w.Write([]byte(`[{"id":"m1"}]`))
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	s := newSender(newClient(Config{ServerURL: srv.URL, Platform: "test", UserID: "bot", Version: "v1"}))
	res, err := s.Send(stdctx.Background(), platform.SendRequest{
		Target: platform.ChatInfo{ID: "ch_1"},
		Message: platform.TextMessage("看这张图").WithAttachments(platform.Attachment{
			Kind:     platform.AttachmentKindImage,
			Name:     "pic.png",
			MimeType: "image/png",
			Data:     []byte{0x89, 'P', 'N', 'G', 1, 2, 3},
		}),
	})
	require.NoError(t, err)
	assert.Equal(t, "m1", res.MessageID)

	assert.Equal(t, "/v1/upload.create", uploadedPath, "Data 附件应先经 upload.create 上传")
	assert.Contains(t, msgContent, `看这张图`)
	assert.Contains(t, msgContent, `<img src="https://cdn.example.com/up/abc.png"`,
		"图片应以 <img> 元素内嵌而非静默丢弃")
}

// TestSendUploadFailureSurfaces 上传失败必须报错，不允许静默丢图。
func TestSendUploadFailureSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/upload.create" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`upload unavailable`))
			return
		}
		t.Errorf("upload failed but message.create was still called: %s", r.URL.Path)
	}))
	defer srv.Close()

	s := newSender(newClient(Config{ServerURL: srv.URL, Platform: "test", UserID: "bot", Version: "v1"}))
	_, err := s.Send(stdctx.Background(), platform.SendRequest{
		Target: platform.ChatInfo{ID: "ch_1"},
		Message: platform.TextMessage("看这张图").WithAttachments(platform.Attachment{
			Kind:     platform.AttachmentKindImage,
			Name:     "pic.png",
			MimeType: "image/png",
			Data:     []byte{1, 2, 3},
		}),
	})
	require.Error(t, err, "上传失败应返回错误而非静默丢图")
}

// TestSendURLAttachmentNoUpload URL 附件不应触发上传。
func TestSendURLAttachmentNoUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/upload.create" {
			t.Error("URL 附件不应触发 upload.create")
		}
		if r.URL.Path == "/v1/message.create" {
			_, _ = w.Write([]byte(`[{"id":"m1"}]`))
			return
		}
		t.Errorf("unexpected request path: %s", r.URL.Path)
	}))
	defer srv.Close()

	s := newSender(newClient(Config{ServerURL: srv.URL, Platform: "test", UserID: "bot", Version: "v1"}))
	_, err := s.Send(stdctx.Background(), platform.SendRequest{
		Target: platform.ChatInfo{ID: "ch_1"},
		Message: platform.TextMessage("link").WithAttachments(platform.Attachment{
			Kind: platform.AttachmentKindImage, URL: "https://img.example.com/a.png",
		}),
	})
	require.NoError(t, err)
}
