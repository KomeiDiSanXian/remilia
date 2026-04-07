package textimage

// vector_test.go — VectorCanvas 单元测试

import (
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 构造函数 ──────────────────────────────────────────────────────────────────

func TestNewVectorCanvas_Dimensions(t *testing.T) {
	c := NewVectorCanvas(600, 400)
	require.NotNil(t, c)
	b := c.Image().Bounds()
	assert.Equal(t, 600, b.Dx())
	assert.Equal(t, 400, b.Dy())
}

func TestNewVectorCard_GoldenRatio(t *testing.T) {
	c := NewVectorCard(600)
	b := c.Image().Bounds()
	assert.Equal(t, 600, b.Dx())
	// 高度 ≈ 600/1.618 ≈ 371
	assert.InDelta(t, 371.0, float64(b.Dy()), 2)
}

// ─── 输出方法 ──────────────────────────────────────────────────────────────────

func TestVectorCanvas_ToPNG(t *testing.T) {
	c := NewVectorCanvas(100, 100)
	c.SetRGB(1, 0, 0)
	c.Clear()

	data, err := c.ToPNG()
	require.NoError(t, err)
	assert.Greater(t, len(data), 8, "PNG 至少需要包含文件头")
	// PNG 魔数：\x89PNG
	assert.Equal(t, []byte{0x89, 0x50, 0x4E, 0x47}, data[:4])
}

func TestVectorCanvas_ToJPEG(t *testing.T) {
	c := NewVectorCanvas(100, 100)
	c.SetRGB(0, 0, 1)
	c.Clear()

	data, err := c.ToJPEG(85)
	require.NoError(t, err)
	assert.Greater(t, len(data), 4)
	// JPEG SOI 标记：0xFFD8FF
	assert.Equal(t, []byte{0xFF, 0xD8, 0xFF}, data[:3])
}

func TestVectorCanvas_ToJPEG_QualityClamp(t *testing.T) {
	c := NewVectorCanvas(50, 50)
	// quality=0 超出范围，应自动修正为 85
	d1, err := c.ToJPEG(0)
	require.NoError(t, err)
	d2, err := c.ToJPEG(85)
	require.NoError(t, err)
	assert.Equal(t, len(d1), len(d2))
}

func TestVectorCanvas_SavePNG(t *testing.T) {
	c := NewVectorCanvas(50, 50)
	c.SetRGB(0, 1, 0)
	c.Clear()
	require.NoError(t, c.SavePNG(t.TempDir()+"/out.png"))
}

func TestVectorCanvas_SaveJPEG(t *testing.T) {
	c := NewVectorCanvas(50, 50)
	c.SetRGB(1, 1, 0)
	c.Clear()
	require.NoError(t, c.SaveJPEG(t.TempDir()+"/out.jpg", 80))
}

// ─── DrawAvatar ────────────────────────────────────────────────────────────────

func TestVectorCanvas_DrawAvatar(t *testing.T) {
	c := NewVectorCanvas(200, 200)
	c.SetRGB(1, 1, 1)
	c.Clear()

	// 构造一张纯红色测试头像
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			src.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	assert.NotPanics(t, func() { c.DrawAvatar(src, 100, 100, 40) })

	// 绘制完成后画布仍可正常编码
	_, err := c.ToPNG()
	require.NoError(t, err)
}

func TestVectorCanvas_DrawAvatar_TinyRadius(t *testing.T) {
	c := NewVectorCanvas(100, 100)
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	assert.NotPanics(t, func() { c.DrawAvatar(src, 50, 50, 0.4) })
}

// ─── DrawProgressBar ──────────────────────────────────────────────────────────

func TestVectorCanvas_DrawProgressBar_FullAndEmpty(t *testing.T) {
	fg := color.RGBA{R: 80, G: 160, B: 240, A: 255}
	bg := color.RGBA{R: 50, G: 50, B: 60, A: 255}
	c := NewVectorCanvas(400, 50)
	assert.NotPanics(t, func() {
		c.DrawProgressBar(10, 15, 380, 20, 1.0, fg, bg) // 满
		c.DrawProgressBar(10, 40, 380, 20, 0.0, fg, bg) // 空
	})
	_, err := c.ToPNG()
	require.NoError(t, err)
}

func TestVectorCanvas_DrawProgressBar_OutOfRange(t *testing.T) {
	fg := color.RGBA{R: 255, A: 255}
	bg := color.RGBA{R: 30, A: 255}
	c := NewVectorCanvas(300, 30)
	// 超出 [0,1] 范围的值应被截断，不应 panic
	assert.NotPanics(t, func() {
		c.DrawProgressBar(0, 5, 300, 20, -0.5, fg, bg)
		c.DrawProgressBar(0, 5, 300, 20, 1.5, fg, bg)
	})
}

// ─── DrawLineChart ────────────────────────────────────────────────────────────

func TestVectorCanvas_DrawLineChart(t *testing.T) {
	c := NewVectorCanvas(400, 200)
	c.SetRGB(1, 1, 1)
	c.Clear()
	pts := []float64{0.2, 0.5, 0.8, 0.3, 0.6}
	assert.NotPanics(t, func() {
		c.DrawLineChart(20, 20, 360, 160, pts, color.RGBA{R: 100, G: 200, B: 255, A: 255}, 2)
	})
	_, err := c.ToPNG()
	require.NoError(t, err)
}

func TestVectorCanvas_DrawLineChart_TooFewPoints(t *testing.T) {
	c := NewVectorCanvas(200, 100)
	// 点数不足时静默忽略，不 panic
	assert.NotPanics(t, func() {
		c.DrawLineChart(0, 0, 200, 100, nil, color.Black, 1)            // 0 个点
		c.DrawLineChart(0, 0, 200, 100, []float64{0.5}, color.Black, 1) // 1 个点
	})
}

func TestVectorCanvas_DrawLineChartFilled(t *testing.T) {
	c := NewVectorCanvas(400, 200)
	c.SetRGB(0.1, 0.1, 0.1)
	c.Clear()
	pts := []float64{0.1, 0.9, 0.4, 0.7, 0.2}
	fill := color.RGBA{R: 80, G: 160, B: 240, A: 80}
	line := color.RGBA{R: 80, G: 160, B: 240, A: 255}
	assert.NotPanics(t, func() {
		c.DrawLineChartFilled(20, 20, 360, 160, pts, line, fill, 2)
	})
	_, err := c.ToPNG()
	require.NoError(t, err)
}

// ─── DrawRadarChart ───────────────────────────────────────────────────────────

func TestVectorCanvas_DrawRadarChart_Pentagon(t *testing.T) {
	c := NewVectorCanvas(300, 300)
	c.SetRGB(0.1, 0.1, 0.1)
	c.Clear()
	vals := []float64{0.9, 0.6, 0.75, 0.5, 0.8}
	fill := color.RGBA{R: 100, G: 180, B: 255, A: 80}
	stroke := color.RGBA{R: 100, G: 180, B: 255, A: 255}
	assert.NotPanics(t, func() {
		c.DrawRadarGrid(150, 150, 120, len(vals), 4, color.RGBA{R: 60, G: 60, B: 80, A: 200})
		c.DrawRadarChart(150, 150, 120, vals, fill, stroke)
	})
	_, err := c.ToPNG()
	require.NoError(t, err)
}

func TestVectorCanvas_DrawRadarChart_TooFewAxes(t *testing.T) {
	c := NewVectorCanvas(200, 200)
	// 轴数不足 3 时静默忽略，不 panic
	assert.NotPanics(t, func() {
		c.DrawRadarChart(100, 100, 80, []float64{0.5, 0.5}, color.Black, color.White)
	})
}

// ─── DrawRadarGrid ─────────────────────────────────────────────────────────────

func TestVectorCanvas_DrawRadarGrid_NoOp(t *testing.T) {
	c := NewVectorCanvas(200, 200)
	// 参数不满足最低要求时静默忽略，不 panic
	assert.NotPanics(t, func() {
		c.DrawRadarGrid(100, 100, 80, 2, 3, color.Gray{Y: 100}) // n<3
		c.DrawRadarGrid(100, 100, 80, 5, 0, color.Gray{Y: 100}) // rings<1
	})
}

// ─── vcClamp01（通过公开 API 间接测试）──────────────────────────────────────────

func TestVectorCanvas_ProgressBarClamp(t *testing.T) {
	c := NewVectorCanvas(300, 20)
	fg := color.RGBA{R: 200, A: 255}
	bg := color.RGBA{R: 60, A: 255}
	// 各边界值均不应 panic
	for _, pct := range []float64{-1, -0.001, 0, 0.5, 1, 1.001, 2} {
		assert.NotPanics(t, func() {
			c.DrawProgressBar(0, 0, 300, 20, pct, fg, bg)
		})
	}
}
