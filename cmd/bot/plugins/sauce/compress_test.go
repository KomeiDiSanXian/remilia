package sauce

import (
	"bytes"
	"testing"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/imagekit"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slicesShareBacking 判断两个切片是否共享底层数组（即未重编码、原样返回）。
func slicesShareBacking(a, b []byte) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

// newSauceCtxWithText 构造文本内容可自定义的最小 Context。
func newSauceCtxWithText(text string) *eventctx.Context {
	evt := &replyQuoteEvent{
		segments: []platform.Segment{{Type: platform.SegmentText, Text: text}},
	}
	return eventctx.NewContextFromEvent(evt, mock.NewSender())
}

func TestCompressThumbnailForSend_Defaults(t *testing.T) {
	// 生产参数：超过 5MB 才压缩，最长边上限 4096
	assert.Equal(t, int64(5*1024*1024), imagekit.DefaultMaxBytes)
	assert.Equal(t, 4096, imagekit.DefaultMaxDimension)
}

func TestCompressThumbnailForSend_OriginalFlag(t *testing.T) {
	p := &Plugin{}
	data := bytes.Repeat([]byte{0x01}, 6*1024*1024) // > 默认 5MB 阈值

	out, mime := p.compressThumbnailForSend(data, "image/png", true)
	assert.True(t, slicesShareBacking(data, out), "-original 应跳过压缩")
	assert.Equal(t, "image/png", mime)
}

func TestCompressThumbnailForSend_ConfigDisabled(t *testing.T) {
	// send_thumbnail_max_bytes=0：关闭压缩，原样发送
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"send_thumbnail_max_bytes": 0}}}
	data := bytes.Repeat([]byte{0x01}, 6*1024*1024) // > 默认 5MB 阈值

	out, mime := p.compressThumbnailForSend(data, "image/png", false)
	assert.True(t, slicesShareBacking(data, out), "配置关闭后不应压缩")
	assert.Equal(t, "image/png", mime)
}

func TestCompressThumbnailForSend_ConfigCustom(t *testing.T) {
	// 自定义体积阈值生效
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"send_thumbnail_max_bytes": 256 * 1024}}}
	data := makeNoisePNG(t, 600, 600)
	require.Greater(t, len(data), 256*1024)

	out, mime := p.compressThumbnailForSend(data, "image/png", false)
	require.False(t, slicesShareBacking(data, out), "超过自定义阈值应压缩")
	require.LessOrEqual(t, len(out), 256*1024)
	assert.Equal(t, "image/jpeg", mime)
}

func TestResolveOriginalFlag(t *testing.T) {
	p := &Plugin{}

	assert.False(t, p.resolveOriginalFlag(newSauceCtxWithText("/sauce")))
	assert.True(t, p.resolveOriginalFlag(newSauceCtxWithText("/sauce -original")))
	assert.True(t, p.resolveOriginalFlag(newSauceCtxWithText("/sauce -original -engine iqdb")))
	assert.False(t, p.resolveOriginalFlag(newSauceCtxWithText("/sauce -original=false")))
	assert.False(t, p.resolveOriginalFlag(newSauceCtxWithText("/sauce -engine iqdb")))
}
