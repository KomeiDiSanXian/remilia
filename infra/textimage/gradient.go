package textimage

import (
	"image"
	"image/color"
	"math"
)

// GradientStop defines a colour at a specific position along a gradient axis.
// Pos must be in [0.0, 1.0]; stops outside that range are clamped.
type GradientStop struct {
	// Pos is the normalised position along the gradient (0 = start, 1 = end).
	Pos float64
	// Color is the colour at this stop.
	Color color.Color
}

// Stop is a convenience constructor for a [GradientStop].
func Stop(pos float64, c color.Color) GradientStop { return GradientStop{Pos: pos, Color: c} }

// LinearGradient returns a w×h image filled with a linear colour gradient.
//
// angleDeg controls the direction:
//   - 0°   → left → right
//   - 90°  → top → bottom
//   - 180° → right → left
//   - 270° → bottom → top
//   - 45°  → top-left → bottom-right
//
// The image can be used directly as [WithBgImage] input.
// At least two stops are recommended; a single stop produces a solid fill.
//
// Example:
//
//	bg := textimage.LinearGradient(640, 400, 135,
//	    textimage.Stop(0, color.RGBA{R:60,  G:20,  B:120, A:255}),
//	    textimage.Stop(1, color.RGBA{R:20,  G:80,  B:180, A:255}),
//	)
func LinearGradient(w, h int, angleDeg float64, stops ...GradientStop) image.Image {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if len(stops) == 0 {
		return img
	}
	gradSortStops(stops)

	rad := angleDeg * math.Pi / 180
	cosA := math.Cos(rad)
	sinA := math.Sin(rad)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Normalise pixel to [-0.5, 0.5] in each axis.
			nx := float64(x)/float64(w) - 0.5
			ny := float64(y)/float64(h) - 0.5
			// Project onto gradient direction vector and shift to [0, 1].
			t := nx*cosA + ny*sinA + 0.5
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
			img.SetRGBA(x, y, gradLerp(stops, t))
		}
	}
	return img
}

// RadialGradient returns a w×h image with a radial gradient emanating from
// the centre of the image.
// t=0 at the centre corresponds to stops[0]; t=1 at the farthest corner
// corresponds to stops[last].
//
// Example:
//
//	bg := textimage.RadialGradient(400, 400,
//	    textimage.Stop(0,   color.RGBA{R:255, G:255, B:255, A:255}),
//	    textimage.Stop(0.6, color.RGBA{R:100, G:150, B:220, A:255}),
//	    textimage.Stop(1,   color.RGBA{R:10,  G:20,  B:80,  A:255}),
//	)
func RadialGradient(w, h int, stops ...GradientStop) image.Image {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if len(stops) == 0 {
		return img
	}
	gradSortStops(stops)

	cx := float64(w-1) / 2
	cy := float64(h-1) / 2
	// Normalise to the distance to the farthest corner.
	maxR := math.Sqrt(cx*cx + cy*cy)
	if maxR == 0 {
		maxR = 1
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			t := math.Sqrt(dx*dx+dy*dy) / maxR
			if t > 1 {
				t = 1
			}
			img.SetRGBA(x, y, gradLerp(stops, t))
		}
	}
	return img
}

// gradSortStops sorts stops by Pos ascending (insertion sort; stop count is tiny).
func gradSortStops(stops []GradientStop) {
	for i := 1; i < len(stops); i++ {
		for j := i; j > 0 && stops[j].Pos < stops[j-1].Pos; j-- {
			stops[j], stops[j-1] = stops[j-1], stops[j]
		}
	}
}

// gradLerp interpolates a colour at position t (0–1) across the stop list.
func gradLerp(stops []GradientStop, t float64) color.RGBA {
	if t <= stops[0].Pos {
		return gradToRGBA(stops[0].Color)
	}
	last := stops[len(stops)-1]
	if t >= last.Pos {
		return gradToRGBA(last.Color)
	}
	for i := 0; i < len(stops)-1; i++ {
		a, b := stops[i], stops[i+1]
		if t >= a.Pos && t <= b.Pos {
			span := b.Pos - a.Pos
			if span == 0 {
				return gradToRGBA(b.Color)
			}
			f := (t - a.Pos) / span
			ar, ag, ab, aa := a.Color.RGBA()
			br, bg, bb, ba := b.Color.RGBA()
			lerp := func(x, y uint32) uint8 {
				return uint8(float64(x>>8)*(1-f) + float64(y>>8)*f + 0.5)
			}
			return color.RGBA{R: lerp(ar, br), G: lerp(ag, bg), B: lerp(ab, bb), A: lerp(aa, ba)}
		}
	}
	return gradToRGBA(last.Color)
}

func gradToRGBA(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}
