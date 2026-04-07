package canvas

import (
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── New / NewCard ─────────────────────────────────────────────────────────────

func TestNew_Dimensions(t *testing.T) {
	c := New(600, 400)
	require.NotNil(t, c)
	img := c.Image()
	b := img.Bounds()
	assert.Equal(t, 600, b.Dx())
	assert.Equal(t, 400, b.Dy())
}

func TestNewCard_GoldenRatio(t *testing.T) {
	c := NewCard(600)
	b := c.Image().Bounds()
	assert.Equal(t, 600, b.Dx())
	assert.InDelta(t, 371.0, float64(b.Dy()), 2, "height ≈ 600/φ")
}

// ─── ToPNG / ToJPEG ───────────────────────────────────────────────────────────

func TestToPNG_ReturnsBytes(t *testing.T) {
	c := New(100, 100)
	c.SetRGB(1, 0, 0)
	c.Clear()

	data, err := c.ToPNG()
	require.NoError(t, err)
	assert.Greater(t, len(data), 8, "PNG must have at least a header")
	assert.Equal(t, []byte{0x89, 0x50, 0x4E, 0x47}, data[:4], "PNG magic bytes")
}

func TestToJPEG_ReturnsBytes(t *testing.T) {
	c := New(100, 100)
	c.SetRGB(0, 0, 1)
	c.Clear()

	data, err := c.ToJPEG(85)
	require.NoError(t, err)
	assert.Greater(t, len(data), 4)
	assert.Equal(t, []byte{0xFF, 0xD8, 0xFF}, data[:3], "JPEG SOI marker")
}

func TestToJPEG_QualityClamp(t *testing.T) {
	c := New(50, 50)
	// out-of-range quality → clamped to 85
	d1, err := c.ToJPEG(0)
	require.NoError(t, err)
	d2, err := c.ToJPEG(85)
	require.NoError(t, err)
	assert.Equal(t, len(d1), len(d2), "clamped quality should equal explicit q=85")
}

// ─── SavePNG / SaveJPEG ───────────────────────────────────────────────────────

func TestSavePNG(t *testing.T) {
	c := New(50, 50)
	c.SetRGB(0, 1, 0)
	c.Clear()
	path := t.TempDir() + "/out.png"
	require.NoError(t, c.SavePNG(path))
}

func TestSaveJPEG(t *testing.T) {
	c := New(50, 50)
	c.SetRGB(1, 1, 0)
	c.Clear()
	path := t.TempDir() + "/out.jpg"
	require.NoError(t, c.SaveJPEG(path, 80))
}

// ─── DrawAvatar ────────────────────────────────────────────────────────────────

func TestDrawAvatar_DoesNotPanic(t *testing.T) {
	c := New(200, 200)
	c.SetRGB(1, 1, 1)
	c.Clear()

	// Create a small solid red image as a fake avatar.
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			src.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}

	assert.NotPanics(t, func() {
		c.DrawAvatar(src, 100, 100, 40)
	})

	// The canvas must still be encodable after the operation.
	_, err := c.ToPNG()
	require.NoError(t, err)
}

func TestDrawAvatar_TinyRadius(t *testing.T) {
	c := New(100, 100)
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	assert.NotPanics(t, func() {
		c.DrawAvatar(src, 50, 50, 0.4)
	})
}

// ─── DrawProgressBar ──────────────────────────────────────────────────────────

func TestDrawProgressBar_FullAndEmpty(t *testing.T) {
	fg := color.RGBA{R: 80, G: 160, B: 240, A: 255}
	bg := color.RGBA{R: 50, G: 50, B: 60, A: 255}

	c := New(400, 50)
	assert.NotPanics(t, func() {
		c.DrawProgressBar(10, 15, 380, 20, 1.0, fg, bg) // full
		c.DrawProgressBar(10, 40, 380, 20, 0.0, fg, bg) // empty
	})

	_, err := c.ToPNG()
	require.NoError(t, err)
}

func TestDrawProgressBar_OutOfRange(t *testing.T) {
	fg := color.RGBA{R: 255, A: 255}
	bg := color.RGBA{R: 30, A: 255}
	c := New(300, 30)

	// Values outside [0,1] should be clamped without panicking.
	assert.NotPanics(t, func() {
		c.DrawProgressBar(0, 5, 300, 20, -0.5, fg, bg)
		c.DrawProgressBar(0, 5, 300, 20, 1.5, fg, bg)
	})
}

// ─── DrawLineChart ────────────────────────────────────────────────────────────

func TestDrawLineChart_NormalPath(t *testing.T) {
	c := New(400, 200)
	c.SetRGB(1, 1, 1)
	c.Clear()

	pts := []float64{0.2, 0.5, 0.8, 0.3, 0.6}
	assert.NotPanics(t, func() {
		c.DrawLineChart(20, 20, 360, 160, pts, color.RGBA{R: 100, G: 200, B: 255, A: 255}, 2)
	})
	_, err := c.ToPNG()
	require.NoError(t, err)
}

func TestDrawLineChart_TooFewPoints(t *testing.T) {
	c := New(200, 100)
	assert.NotPanics(t, func() {
		// 0 points — no-op
		c.DrawLineChart(0, 0, 200, 100, nil, color.Black, 1)
		// 1 point — no-op
		c.DrawLineChart(0, 0, 200, 100, []float64{0.5}, color.Black, 1)
	})
}

func TestDrawLineChartFilled_DoesNotPanic(t *testing.T) {
	c := New(400, 200)
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

// ─── DrawRadarChart ────────────────────────────────────────────────────────────

func TestDrawRadarChart_Pentagon(t *testing.T) {
	c := New(300, 300)
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

func TestDrawRadarChart_TooFewAxes(t *testing.T) {
	c := New(200, 200)
	// Fewer than 3 values → no-op, no panic
	assert.NotPanics(t, func() {
		c.DrawRadarChart(100, 100, 80, []float64{0.5, 0.5}, color.Black, color.White)
	})
}

// ─── DrawRadarGrid ─────────────────────────────────────────────────────────────

func TestDrawRadarGrid_NoOp(t *testing.T) {
	c := New(200, 200)
	assert.NotPanics(t, func() {
		c.DrawRadarGrid(100, 100, 80, 2, 3, color.Gray{Y: 100}) // n<3 → no-op
		c.DrawRadarGrid(100, 100, 80, 5, 0, color.Gray{Y: 100}) // rings<1 → no-op
	})
}

// ─── clamp01 (internal, tested via helpers) ────────────────────────────────────

func TestProgressBarClamp(t *testing.T) {
	// Exercise negative and >1 values through the public API.
	c := New(300, 20)
	fg := color.RGBA{R: 200, A: 255}
	bg := color.RGBA{R: 60, A: 255}

	for _, pct := range []float64{-1, -0.001, 0, 0.5, 1, 1.001, 2} {
		assert.NotPanics(t, func() {
			c.DrawProgressBar(0, 0, 300, 20, pct, fg, bg)
		})
	}
}
