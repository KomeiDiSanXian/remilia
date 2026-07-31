package sauce

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makePNG 生成指定尺寸的纯色 PNG。
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 200, G: 120, B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// makeNoisePNG 生成随机噪点 PNG，压缩率极低，用于触发体积限制。
func makeNoisePNG(t *testing.T, w, h int) []byte {
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
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestPreprocessUpscaleSmall(t *testing.T) {
	data := makePNG(t, 200, 150)
	proc, err := preprocessImage(data, PreprocessOptions{UpscaleSmall: true})
	require.NoError(t, err)
	assert.Equal(t, 400, proc.Width)
	assert.Equal(t, 300, proc.Height)
	assert.Equal(t, "image/png", proc.Mime)
	assert.NotEmpty(t, proc.Data)
}

func TestPreprocessNoUpscaleWhenLargeEnough(t *testing.T) {
	data := makePNG(t, 800, 600)
	proc, err := preprocessImage(data, PreprocessOptions{UpscaleSmall: true})
	require.NoError(t, err)
	assert.Equal(t, 800, proc.Width)
	assert.Equal(t, 600, proc.Height)
}

func TestPreprocessShrinkOverMaxDimension(t *testing.T) {
	data := makePNG(t, 2000, 1000)
	proc, err := preprocessImage(data, PreprocessOptions{
		UpscaleSmall: true,
		MaxDimension: 500,
		MaxBytes:     defaultMaxBytes,
	})
	require.NoError(t, err)
	assert.Equal(t, 500, proc.Width)
	assert.Equal(t, 250, proc.Height)
}

func TestPreprocessMaxBytesFallback(t *testing.T) {
	data := makeNoisePNG(t, 900, 600)
	proc, err := preprocessImage(data, PreprocessOptions{UpscaleSmall: false, MaxBytes: 512 * 1024})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(proc.Data), 512*1024)
	assert.Equal(t, "image/jpeg", proc.Mime) // PNG 超限后降级为 JPEG
}

func TestPreprocessInvalidImage(t *testing.T) {
	_, err := preprocessImage([]byte("not an image"), PreprocessOptions{})
	assert.Error(t, err)
}

func TestSniffMime(t *testing.T) {
	assert.Equal(t, "image/png", sniffMime([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}))
	assert.Equal(t, "image/jpeg", sniffMime([]byte{0xFF, 0xD8, 0xFF, 0xE0}))
	assert.Equal(t, "image/jpeg", sniffMime(nil))
}
