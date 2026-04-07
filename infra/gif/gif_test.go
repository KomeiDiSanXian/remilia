package gif

import (
	"bytes"
	"image"
	"image/color"
	stdgif "image/gif"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 测试辅助函数 ──────────────────────────────────────────────────────────────

// solidFrame 创建指定尺寸和颜色的纯色图片，用于测试帧。
func solidFrame(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

// decodeGIF 解析 GIF 字节并返回 *gif.GIF，用于断言编码结果。
func decodeGIF(t *testing.T, data []byte) *stdgif.GIF {
	t.Helper()
	g, err := stdgif.DecodeAll(bytes.NewReader(data))
	require.NoError(t, err, "DecodeAll 应能解析合法 GIF 字节")
	return g
}

// ─── New / 选项 ────────────────────────────────────────────────────────────────

func TestNew_Defaults(t *testing.T) {
	enc := New()
	assert.Equal(t, 0, enc.Len(), "初始帧数为 0")
	assert.True(t, enc.dithering, "默认启用抖动")
	assert.Equal(t, 0, enc.loopCount, "默认无限循环")
	assert.NotEmpty(t, enc.palette, "默认调色板不为空")
}

func TestWithLoopCount(t *testing.T) {
	enc := New(WithLoopCount(-1))
	assert.Equal(t, -1, enc.loopCount)
}

func TestWithDithering_Disabled(t *testing.T) {
	enc := New(WithDithering(false))
	assert.False(t, enc.dithering)
}

func TestWithPalette_Custom(t *testing.T) {
	p := color.Palette{color.Black, color.White}
	enc := New(WithPalette(p))
	assert.Len(t, enc.palette, 2)
}

// ─── AddFrame / Len / Reset ───────────────────────────────────────────────────

func TestAddFrame_Basic(t *testing.T) {
	enc := New()
	require.NoError(t, enc.AddFrame(solidFrame(10, 10, color.RGBA{R: 200, A: 255}), 100))
	assert.Equal(t, 1, enc.Len())
}

func TestAddFrame_MultipleFrames(t *testing.T) {
	enc := New()
	colors := []color.RGBA{
		{R: 255, A: 255},
		{G: 255, A: 255},
		{B: 255, A: 255},
	}
	for _, c := range colors {
		require.NoError(t, enc.AddFrame(solidFrame(20, 20, c), 50))
	}
	assert.Equal(t, 3, enc.Len())
}

func TestAddPaletted(t *testing.T) {
	enc := New()
	p := defaultPalette()
	img := image.NewPaletted(image.Rect(0, 0, 8, 8), p)
	enc.AddPaletted(img, 200)
	assert.Equal(t, 1, enc.Len())
}

func TestReset(t *testing.T) {
	enc := New()
	require.NoError(t, enc.AddFrame(solidFrame(4, 4, color.Black), 50))
	require.NoError(t, enc.AddFrame(solidFrame(4, 4, color.White), 50))
	assert.Equal(t, 2, enc.Len())

	enc.Reset()
	assert.Equal(t, 0, enc.Len())
	// 配置项应保持不变
	assert.True(t, enc.dithering)
	assert.Equal(t, 0, enc.loopCount)
}

// ─── Encode / Bytes ───────────────────────────────────────────────────────────

func TestBytes_EmptyReturnsError(t *testing.T) {
	enc := New()
	_, err := enc.Bytes()
	assert.Error(t, err, "空编码器调用 Bytes() 应返回错误")
}

func TestBytes_SingleFrame(t *testing.T) {
	enc := New()
	require.NoError(t, enc.AddFrame(solidFrame(16, 16, color.RGBA{R: 100, G: 200, B: 50, A: 255}), 100))

	data, err := enc.Bytes()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), 6)
	// GIF 文件头：GIF89a
	assert.Equal(t, "GIF89a", string(data[:6]))
}

func TestBytes_MultiFrameDecodeRoundtrip(t *testing.T) {
	enc := New(WithLoopCount(3))
	frames := []struct {
		c     color.RGBA
		delay int
	}{
		{color.RGBA{R: 255, A: 255}, 50},
		{color.RGBA{G: 255, A: 255}, 100},
		{color.RGBA{B: 255, A: 255}, 200},
	}
	for _, f := range frames {
		require.NoError(t, enc.AddFrame(solidFrame(32, 32, f.c), f.delay))
	}

	data, err := enc.Bytes()
	require.NoError(t, err)

	g := decodeGIF(t, data)
	assert.Len(t, g.Image, 3, "应有 3 帧")
	assert.Equal(t, 3, g.LoopCount)

	// GIF 延迟单位为 1/100 秒：delayMs / 10
	assert.Equal(t, 5, g.Delay[0], "50ms → 5 单位")
	assert.Equal(t, 10, g.Delay[1], "100ms → 10 单位")
	assert.Equal(t, 20, g.Delay[2], "200ms → 20 单位")
}

func TestDelayFloor(t *testing.T) {
	enc := New()
	// 1ms 取整后为 0 单位，应截断至最小值 1
	require.NoError(t, enc.AddFrame(solidFrame(4, 4, color.Black), 1))
	assert.Equal(t, 1, enc.delays[0])
}

// ─── 抖动开关 ─────────────────────────────────────────────────────────────────

func TestDithering_DoesNotPanic(t *testing.T) {
	img := solidFrame(64, 64, color.RGBA{R: 128, G: 64, B: 200, A: 255})
	for _, dither := range []bool{true, false} {
		enc := New(WithDithering(dither))
		assert.NotPanics(t, func() {
			_ = enc.AddFrame(img, 100)
		})
	}
}

// ─── defaultPalette ───────────────────────────────────────────────────────────

func TestDefaultPalette_Length(t *testing.T) {
	p := defaultPalette()
	assert.Equal(t, 256, len(p), "默认调色板应有 256 个颜色条目")
}

func TestDefaultPalette_ContainsBlackAndWhite(t *testing.T) {
	p := defaultPalette()
	hasBlack, hasWhite := false, false
	for _, c := range p {
		r, g, b, a := c.RGBA()
		switch {
		case r == 0 && g == 0 && b == 0 && a > 0:
			hasBlack = true
		case r == 0xffff && g == 0xffff && b == 0xffff:
			hasWhite = true
		}
	}
	assert.True(t, hasBlack, "调色板应包含黑色")
	assert.True(t, hasWhite, "调色板应包含白色")
}
