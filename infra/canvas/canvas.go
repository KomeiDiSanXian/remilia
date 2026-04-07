package canvas

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"

	"github.com/fogleman/gg"
	xdraw "golang.org/x/image/draw"
)

// Canvas wraps [gg.Context] and adds Bot-friendly drawing helpers.
// All upstream [gg.Context] methods are available through embedding; this
// package supplements them with higher-level primitives for notification
// cards and data visualisation.
//
// Create a canvas with [New] or [NewCard], draw onto it using any combination
// of gg primitives and the helpers below, then export via [Canvas.ToPNG],
// [Canvas.ToJPEG], [Canvas.SavePNG], or [Canvas.SaveJPEG].
type Canvas struct {
	*gg.Context
}

// New creates a blank Canvas of the given pixel dimensions.
func New(width, height int) *Canvas {
	return &Canvas{Context: gg.NewContext(width, height)}
}

// NewCard creates a Canvas with a "notification-card" aspect ratio.
// height = ⌊width / φ⌋  (Golden Ratio ≈ 1.618).
// Typical width values: 600, 800, 1080.
func NewCard(width int) *Canvas {
	return New(width, int(float64(width)/1.618))
}

// ─── Bot-friendly helpers ──────────────────────────────────────────────────────

// DrawAvatar renders img as a circle of the given radius centred at (cx, cy).
// The image is scaled to fit tightly inside the circle before clipping —
// equivalent to CSS background-size:cover.
// Pixels outside the circle are not painted (the clip is restored via Push/Pop).
func (c *Canvas) DrawAvatar(img image.Image, cx, cy, radius float64) {
	diam := int(math.Round(2 * radius))
	if diam < 1 {
		diam = 1
	}
	// Scale the source image to fill the circle bounding box.
	scaled := image.NewRGBA(image.Rect(0, 0, diam, diam))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), img, img.Bounds(), xdraw.Over, nil)

	c.Push()
	c.DrawCircle(cx, cy, radius)
	c.Clip()
	// DrawImageAnchored(img, x, y, ax=0.5, ay=0.5) centres the image at (x,y).
	c.Context.DrawImageAnchored(scaled, int(math.Round(cx)), int(math.Round(cy)), 0.5, 0.5)
	c.Pop()
}

// DrawProgressBar draws a rounded horizontal progress bar.
//
//   - x, y:    top-left corner of the bar
//   - w, h:    total width × height (including the unfilled track)
//   - percent: fill level in [0.0, 1.0]; values are clamped automatically
//   - fg:      filled-portion colour
//   - bg:      track (unfilled) colour
func (c *Canvas) DrawProgressBar(x, y, w, h, percent float64, fg, bg color.Color) {
	r := h / 2

	// Draw track (full width).
	c.SetColor(bg)
	c.DrawRoundedRectangle(x, y, w, h, r)
	c.Fill()

	// Draw fill.
	pct := clamp01(percent)
	if pct <= 0 {
		return
	}
	fillW := w * pct
	if fillW < h { // keep radius from exceeding half the fill width
		fillW = h
	}
	c.SetColor(fg)
	c.DrawRoundedRectangle(x, y, fillW, h, r)
	c.Fill()
}

// DrawLineChart draws a polyline chart inside the bounding box
// [x, x+w] × [y, y+h].
//
//   - points:    normalised y-values in [0.0, 1.0] (bottom=0, top=1); must have
//     at least 2 elements
//   - lineColor: stroke colour
//   - lineWidth: stroke width in pixels
func (c *Canvas) DrawLineChart(x, y, w, h float64, points []float64, lineColor color.Color, lineWidth float64) {
	n := len(points)
	if n < 2 {
		return
	}
	step := w / float64(n-1)

	c.Push()
	c.SetColor(lineColor)
	c.SetLineWidth(lineWidth)
	c.MoveTo(x, y+h*(1-clamp01(points[0])))
	for i := 1; i < n; i++ {
		c.LineTo(x+float64(i)*step, y+h*(1-clamp01(points[i])))
	}
	c.Stroke()
	c.Pop()
}

// DrawLineChartFilled draws a filled area chart (polyline + area fill).
// fillColor may be semi-transparent (alpha < 255) for a translucent overlay.
func (c *Canvas) DrawLineChartFilled(x, y, w, h float64, points []float64, lineColor, fillColor color.Color, lineWidth float64) {
	n := len(points)
	if n < 2 {
		return
	}
	step := w / float64(n-1)
	bottom := y + h

	c.Push()

	// Build the fill path (close back to the bottom edge).
	c.MoveTo(x, bottom)
	for i, v := range points {
		c.LineTo(x+float64(i)*step, y+h*(1-clamp01(v)))
	}
	c.LineTo(x+w, bottom)
	c.ClosePath()
	c.SetColor(fillColor)
	c.Fill()

	// Draw the line on top.
	c.SetColor(lineColor)
	c.SetLineWidth(lineWidth)
	c.MoveTo(x, y+h*(1-clamp01(points[0])))
	for i := 1; i < n; i++ {
		c.LineTo(x+float64(i)*step, y+h*(1-clamp01(points[i])))
	}
	c.Stroke()

	c.Pop()
}

// DrawRadarChart draws a radar (spider) chart with len(values) axes inside the
// circle of the given radius centred at (cx, cy).
//
//   - values:      normalised vertex distances from the centre, one per axis, in
//     [0.0, 1.0]; must have at least 3 elements
//   - fillColor:   interior polygon fill (may be semi-transparent)
//   - strokeColor: polygon border colour
//
// The first axis points straight up (angle = −π/2).
func (c *Canvas) DrawRadarChart(cx, cy, radius float64, values []float64, fillColor, strokeColor color.Color) {
	n := len(values)
	if n < 3 {
		return
	}
	const startAngle = -math.Pi / 2
	pts := make([][2]float64, n)
	for i, v := range values {
		angle := startAngle + float64(i)*2*math.Pi/float64(n)
		r := radius * clamp01(v)
		pts[i] = [2]float64{cx + r*math.Cos(angle), cy + r*math.Sin(angle)}
	}

	c.Push()
	c.MoveTo(pts[0][0], pts[0][1])
	for i := 1; i < n; i++ {
		c.LineTo(pts[i][0], pts[i][1])
	}
	c.ClosePath()
	c.SetColor(fillColor)
	c.FillPreserve()
	c.SetColor(strokeColor)
	c.Stroke()
	c.Pop()
}

// DrawRadarGrid draws the background grid (axes + concentric rings) for a radar
// chart. Call this before [Canvas.DrawRadarChart] so the data polygon appears on top.
//
//   - n:         number of axes (should match the data's len(values))
//   - rings:     number of concentric reference rings (e.g. 4 for 25 %/50 %/75 %/100 %)
//   - gridColor: colour for axes and rings (often a semi-transparent grey)
func (c *Canvas) DrawRadarGrid(cx, cy, radius float64, n, rings int, gridColor color.Color) {
	if n < 3 || rings < 1 {
		return
	}
	const startAngle = -math.Pi / 2

	c.Push()
	c.SetColor(gridColor)
	c.SetLineWidth(1)

	// Concentric rings.
	for ring := 1; ring <= rings; ring++ {
		r := radius * float64(ring) / float64(rings)
		c.DrawCircle(cx, cy, r)
		c.Stroke()
	}

	// Axis lines from centre to perimeter.
	for i := range n {
		angle := startAngle + float64(i)*2*math.Pi/float64(n)
		c.MoveTo(cx, cy)
		c.LineTo(cx+radius*math.Cos(angle), cy+radius*math.Sin(angle))
		c.Stroke()
	}

	c.Pop()
}

// ─── Output ───────────────────────────────────────────────────────────────────

// ToPNG encodes the current canvas state as PNG-compressed bytes.
func (c *Canvas) ToPNG() ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, c.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ToJPEG encodes the current canvas state as JPEG-compressed bytes.
// quality must be in [1, 100]; values outside that range are clamped to 85.
func (c *Canvas) ToJPEG(quality int) ([]byte, error) {
	if quality < 1 || quality > 100 {
		quality = 85
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, c.Image(), &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SavePNG writes a PNG file to path.
// It delegates to [gg.Context.SavePNG].
func (c *Canvas) SavePNG(path string) error {
	return c.Context.SavePNG(path)
}

// SaveJPEG encodes the canvas as JPEG and writes it to path.
// quality must be in [1, 100]; values outside that range are clamped to 85.
func (c *Canvas) SaveJPEG(path string, quality int) error {
	data, err := c.ToJPEG(quality)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
