// Package qq sender_markdown_image_test.go — Markdown 图文同条发送的测试。
package qq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// fakeMDImageAPI 记录 Markdown 图文发送各阶段请求。
type fakeMDImageAPI struct {
	openapi.OpenAPI
	prepareResp  gjson.Result
	mergeResp    gjson.Result
	failMarkdown bool // 模拟未申请 markdown 权限（msg_type=2 被拒）

	prepareParts int
	mergeMedia   *dto.Media
	chatMsg      *dto.Message
}

func (f *fakeMDImageAPI) GroupUploadPrepare(_ context.Context, _ string, _ *dto.UploadPrepareRequest) (gjson.Result, error) {
	f.prepareParts++
	return f.prepareResp, nil
}

func (f *fakeMDImageAPI) GroupUploadPartFinish(_ context.Context, _ string, _ *dto.UploadPartFinishRequest) (gjson.Result, error) {
	return gjson.Result{}, nil
}

func (f *fakeMDImageAPI) GroupRichMedia(_ context.Context, _ string, media *dto.Media) (gjson.Result, error) {
	f.mergeMedia = media
	return f.mergeResp, nil
}

func (f *fakeMDImageAPI) GroupChat(_ context.Context, _ string, msg *dto.Message) (gjson.Result, error) {
	if f.failMarkdown && msg.Type == dto.MarkdownMessage {
		return gjson.Result{}, errors.New("304036 无 Markdown 模板权限")
	}
	f.chatMsg = msg
	return gjson.Parse(`{"id":"msg_1"}`), nil
}

// mdImageTestPNG 生成 64×64 的真实 PNG（供 DecodeConfig 解出显示尺寸）。
func mdImageTestPNG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// TestSendMarkdownWithImage_DataAttachment 本地图片附件 + Markdown：
// 强制分片上传换取 raw_url，嵌入 markdown 以 msg_type=2 发送。
func TestSendMarkdownWithImage_DataAttachment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fake := &fakeMDImageAPI{
		prepareResp: gjson.Parse(fmt.Sprintf(
			`{"upload_id":"up_1","block_size":"1048576","parts":[{"index":1,"presigned_url":%q}]}`,
			srv.URL+"/p1")),
		mergeResp: gjson.Parse(`{"file_info":"fi_1","raw_url":"https://cos.example.com/img.png"}`),
	}
	s := &qqSender{api: fake}

	res, err := s.Send(context.Background(), platform.SendRequest{
		Target: newTestChat(),
		Message: platform.OutboundMessage{Markdown: "**hello** world"}.WithAttachments(platform.Attachment{
			Kind: platform.AttachmentKindImage, Name: "pic.png", Data: mdImageTestPNG(t),
		}),
	})
	require.NoError(t, err)
	assert.Equal(t, "msg_1", res.MessageID)

	// 走了分片上传
	assert.Equal(t, 1, fake.prepareParts)
	require.NotNil(t, fake.mergeMedia)
	assert.Equal(t, "up_1", fake.mergeMedia.UploadID)

	// msg_type=2 markdown，图片以原生语法内嵌（含显式尺寸参数、置于正文前），正文保留
	require.NotNil(t, fake.chatMsg)
	assert.EqualValues(t, dto.MarkdownMessage, fake.chatMsg.Type)
	require.NotNil(t, fake.chatMsg.Markdown)
	assert.Contains(t, fake.chatMsg.Markdown.Content, "**hello** world")
	assert.Contains(t, fake.chatMsg.Markdown.Content, "![image #64px #64px](https://cos.example.com/img.png)")
	assert.Greater(t, strings.Index(fake.chatMsg.Markdown.Content, "![image"),
		-1, "图片片段应存在")
	assert.Less(t, strings.Index(fake.chatMsg.Markdown.Content, "![image"),
		strings.Index(fake.chatMsg.Markdown.Content, "**hello**"),
		"图片应前置，信息在下")
	assert.Nil(t, fake.chatMsg.Media, "markdown 与富媒体互斥，不应携带 media")
}

// TestSendMarkdownWithImage_URLAttachment URL 附件直接嵌入，无需上传。
func TestSendMarkdownWithImage_URLAttachment(t *testing.T) {
	fake := &fakeMDImageAPI{}
	s := &qqSender{api: fake}

	_, err := s.Send(context.Background(), platform.SendRequest{
		Target: newTestChat(),
		Message: platform.OutboundMessage{Markdown: "info"}.WithAttachments(platform.Attachment{
			Kind: platform.AttachmentKindImage, URL: "https://img.example.com/a.png",
		}),
	})
	require.NoError(t, err)
	assert.Zero(t, fake.prepareParts, "URL 附件不应触发分片上传")
	require.NotNil(t, fake.chatMsg)
	assert.EqualValues(t, dto.MarkdownMessage, fake.chatMsg.Type)
	assert.Contains(t, fake.chatMsg.Markdown.Content, "![image](https://img.example.com/a.png)")
}

// TestSendMarkdownWithImage_FallbackToMedia Markdown 发送失败时回退
// msg_type=7 富媒体图文混排，正文降级为纯文本。
func TestSendMarkdownWithImage_FallbackToMedia(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fake := &fakeMDImageAPI{
		failMarkdown: true,
		prepareResp: gjson.Parse(fmt.Sprintf(
			`{"upload_id":"up_1","block_size":"1048576","parts":[{"index":1,"presigned_url":%q}]}`,
			srv.URL+"/p1")),
		mergeResp: gjson.Parse(`{"file_info":"fi_1","raw_url":"https://cos.example.com/img.png"}`),
	}
	s := &qqSender{api: fake}

	_, err := s.Send(context.Background(), platform.SendRequest{
		Target: newTestChat(),
		Message: platform.OutboundMessage{Markdown: "**hello** [来源](https://src.example.com)"}.WithAttachments(platform.Attachment{
			Kind: platform.AttachmentKindImage, Name: "pic.png", Data: mdImageTestPNG(t),
		}),
	})
	require.NoError(t, err, "回退后应发送成功")

	// 回退为 msg_type=7 富媒体图文混排，content 为降级纯文本
	require.NotNil(t, fake.chatMsg)
	assert.EqualValues(t, dto.MediaMessage, fake.chatMsg.Type)
	require.NotNil(t, fake.chatMsg.Media)
	assert.Equal(t, "fi_1", fake.chatMsg.Media.FileInfo)
	assert.Contains(t, fake.chatMsg.Content, "hello")
	assert.Contains(t, fake.chatMsg.Content, "来源 (https://src.example.com)", "链接应转为可读纯文本")
	assert.NotContains(t, fake.chatMsg.Content, "**", "不应残留 Markdown 标记")
}

// TestQQImageDisplaySize 显示尺寸解算：真实比例缩放、上限钳制、坏数据兜底。
func TestQQImageDisplaySize(t *testing.T) {
	// 64×64 原样
	att := platform.Attachment{Data: mdImageTestPNG(t)}
	w, h := qqImageDisplaySize(att)
	assert.Equal(t, 64, w)
	assert.Equal(t, 64, h)

	// 2000×1000 → 缩到最长边 600 → 600×300
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	w, h = qqImageDisplaySize(platform.Attachment{Data: buf.Bytes()})
	assert.Equal(t, 600, w)
	assert.Equal(t, 300, h)

	// 非图片数据 → (0,0) → 无尺寸参数写法
	w, h = qqImageDisplaySize(platform.Attachment{Data: []byte("not an image")})
	assert.Zero(t, w)
	assert.Zero(t, h)

	// URL 附件（无本地字节）→ (0,0)
	w, h = qqImageDisplaySize(platform.Attachment{URL: "https://x/a.png"})
	assert.Zero(t, w)
	assert.Zero(t, h)
}

// TestChunkContentType 分片上传对象 Content-Type 的判定优先级：
// 魔数嗅探 → 附件 MIME → 按类型兜底。
func TestChunkContentType(t *testing.T) {
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}
	pngBytes := mdImageTestPNG(t)
	gifBytes := append([]byte("GIF89a"), 1, 2, 3)
	webpBytes := append([]byte("RIFF"), append([]byte{4, 0, 0, 0}, []byte("WEBPWEBP")...)...)

	// 魔数优先：声明为 png 但实际是 JPEG → 以真实字节为准
	assert.Equal(t, "image/jpeg", chunkContentType(platform.Attachment{
		Kind: platform.AttachmentKindImage, MimeType: "image/png", Data: jpegBytes,
	}))
	// 各真实格式
	assert.Equal(t, "image/png", chunkContentType(platform.Attachment{Kind: platform.AttachmentKindImage, Data: pngBytes}))
	assert.Equal(t, "image/gif", chunkContentType(platform.Attachment{Kind: platform.AttachmentKindImage, Data: gifBytes}))
	assert.Equal(t, "image/webp", chunkContentType(platform.Attachment{Kind: platform.AttachmentKindImage, Data: webpBytes}))

	// 无法嗅探（文本附件）→ 回退附件 MIME
	assert.Equal(t, "text/plain", chunkContentType(platform.Attachment{
		Kind: platform.AttachmentKindFile, MimeType: "text/plain", Data: []byte("hello"),
	}))

	// 无字节、无 MIME → 按类型兜底
	assert.Equal(t, "image/png", chunkContentType(platform.Attachment{Kind: platform.AttachmentKindImage}))
	assert.Equal(t, "video/mp4", chunkContentType(platform.Attachment{Kind: platform.AttachmentKindVideo}))
	assert.Equal(t, "audio/silk", chunkContentType(platform.Attachment{Kind: platform.AttachmentKindAudio}))
	assert.Equal(t, "application/octet-stream", chunkContentType(platform.Attachment{Kind: platform.AttachmentKindFile}))
}

// TestPlainTextFromMarkdown 纯文本降级的转换规则。
// （pic 插件生成的链接 href 已将裸括号 percent-encode，见 pic.mdLinkHref）
func TestPlainTextFromMarkdown(t *testing.T) {
	in := "**1.** [Konachan] 评分: `9`\n[来源](https://a.com/x)  |  [原始图](https://b.com/y.png)"
	out := plainTextFromMarkdown(in)
	assert.Equal(t, "1. [Konachan] 评分: 9\n来源 (https://a.com/x)  |  原始图 (https://b.com/y.png)", out)
}
