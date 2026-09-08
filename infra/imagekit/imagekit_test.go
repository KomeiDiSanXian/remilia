package imagekit

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

// slicesShareBacking 判断两个切片是否共享底层数组（即未重编码、原样返回）。
func slicesShareBacking(a, b []byte) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

func TestCompress_BelowThresholdUnchanged(t *testing.T) {
	data := makePNG(t, 64, 64)
	require.Less(t, len(data), 512*1024, "前提：小图应低于阈值")

	res := Compress(data, "image/png", Options{MaxBytes: 512 * 1024, MaxDimension: 4096})
	assert.False(t, res.Reencoded)
	assert.True(t, slicesShareBacking(data, res.Data), "低于阈值不应重编码")
	assert.Equal(t, "image/png", res.Mime)
}

func TestCompress_LargeNoiseImageCompressed(t *testing.T) {
	data := makeNoisePNG(t, 600, 600)
	require.Greater(t, len(data), 512*1024, "前提：噪声 PNG 应远超阈值")

	res := Compress(data, "image/png", Options{MaxBytes: 512 * 1024, MaxDimension: 256})
	require.True(t, res.Reencoded)
	require.False(t, slicesShareBacking(data, res.Data), "超阈值应重编码")
	require.LessOrEqual(t, len(res.Data), 512*1024)
	assert.Contains(t, []string{"image/png", "image/jpeg"}, res.Mime)

	img, _, err := image.Decode(bytes.NewReader(res.Data))
	require.NoError(t, err)
	b := img.Bounds()
	assert.LessOrEqual(t, b.Dx(), 256, "边长应缩到 MaxDimension 内")
	assert.LessOrEqual(t, b.Dy(), 256)
}

func TestCompress_DimensionOnly(t *testing.T) {
	// 体积未超限但边长超限：也应触发压缩
	data := makePNG(t, 2000, 1000)
	require.Less(t, len(data), 5*1024*1024, "前提：纯色 PNG 体积应低于阈值")

	res := Compress(data, "image/png", Options{MaxBytes: 5 * 1024 * 1024, MaxDimension: 500})
	require.True(t, res.Reencoded)
	img, _, err := image.Decode(bytes.NewReader(res.Data))
	require.NoError(t, err)
	b := img.Bounds()
	assert.LessOrEqual(t, b.Dx(), 500)
	assert.LessOrEqual(t, b.Dy(), 500)
	assert.Equal(t, "image/png", res.Mime)
}

func TestCompress_GIFUnchanged(t *testing.T) {
	// 动图即使超阈值也应原样返回，避免被压成静态帧
	data := makeNoisePNG(t, 600, 600)
	require.Greater(t, len(data), 512*1024)

	res := Compress(data, "image/gif", Options{MaxBytes: 512 * 1024, MaxDimension: 256})
	assert.False(t, res.Reencoded)
	assert.True(t, slicesShareBacking(data, res.Data), "动图不应重编码")
	assert.Equal(t, "image/gif", res.Mime)
}

func TestCompress_InvalidDataUnchanged(t *testing.T) {
	data := bytes.Repeat([]byte{0x01}, 1024*1024) // 超阈值但无法解码

	res := Compress(data, "image/jpeg", Options{MaxBytes: 512 * 1024, MaxDimension: 256})
	assert.False(t, res.Reencoded)
	assert.True(t, slicesShareBacking(data, res.Data), "压缩失败应回退原图")
	assert.Equal(t, "image/jpeg", res.Mime)
}

func TestCompress_DisabledUnchanged(t *testing.T) {
	data := makeNoisePNG(t, 600, 600)

	res := Compress(data, "image/png", Options{})
	assert.False(t, res.Reencoded, "限制全为 0 时应原样返回")
	assert.True(t, slicesShareBacking(data, res.Data))
}

func TestCompress_DecodeBudgetExceededUnchanged(t *testing.T) {
	// 600×600 解码需 1.4MB，预算 1MB 时应跳过重编码，控制瞬时内存峰值
	data := makeNoisePNG(t, 600, 600)
	require.Greater(t, len(data), 512*1024, "前提：噪声 PNG 应超体积阈值")

	res := Compress(data, "image/png", Options{
		MaxBytes:       512 * 1024,
		MaxDimension:   256,
		MaxDecodeBytes: 1024 * 1024,
	})
	assert.False(t, res.Reencoded, "超出解码预算应跳过重编码")
	assert.True(t, slicesShareBacking(data, res.Data))
	assert.Equal(t, "image/png", res.Mime)
}

func TestCompress_DecodeBudgetWithinCompressed(t *testing.T) {
	// 同样的图，预算放宽到 2MB（>1.4MB）时应正常压缩
	data := makeNoisePNG(t, 600, 600)

	res := Compress(data, "image/png", Options{
		MaxBytes:       512 * 1024,
		MaxDimension:   256,
		MaxDecodeBytes: 2 * 1024 * 1024,
	})
	assert.True(t, res.Reencoded)
	assert.LessOrEqual(t, len(res.Data), 512*1024)
}

func TestCompress_DecodeBudgetUnlimited(t *testing.T) {
	// 负值 = 显式关闭预算限制，行为与引入预算前一致
	data := makeNoisePNG(t, 600, 600)

	res := Compress(data, "image/png", Options{
		MaxBytes:       512 * 1024,
		MaxDimension:   256,
		MaxDecodeBytes: -1,
	})
	assert.True(t, res.Reencoded)
}

func TestCompress_DefaultDecodeBudgetCoversMaxDimension(t *testing.T) {
	// 默认预算必须覆盖 MaxDimension=4096 所需的最大解码工作区（67MB）
	data := makeNoisePNG(t, 2048, 2048) // 解码需 16MB < 128MB 默认预算
	require.Greater(t, len(data), 1024*1024)

	res := Compress(data, "image/png", Options{MaxBytes: 1024 * 1024, MaxDimension: 1024})
	assert.True(t, res.Reencoded, "默认预算下的常规大图应正常压缩")
	assert.LessOrEqual(t, len(res.Data), 1024*1024)
}

func TestDefaults(t *testing.T) {
	// 生产参数：超过 5MB 才压缩，最长边上限 4096
	assert.Equal(t, int64(5*1024*1024), DefaultMaxBytes)
	assert.Equal(t, 4096, DefaultMaxDimension)
}
