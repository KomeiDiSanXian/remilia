package gif

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	stdgif "image/gif"
	"io"
)

// Encoder builds a GIF animation frame by frame.
//
// All methods on Encoder are NOT safe for concurrent use; protect access with
// an external mutex if frames are added from multiple goroutines.
type Encoder struct {
	frames    []*image.Paletted
	delays    []int // 100ths of a second (GIF unit)
	disposals []byte
	loopCount int
	palette   color.Palette
	dithering bool
}

// New creates a new Encoder with the provided options.
// With no options it uses a 256-colour web-safe palette, Floyd-Steinberg
// dithering, and loops forever (LoopCount == 0).
func New(opts ...Option) *Encoder {
	e := &Encoder{
		loopCount: 0,
		palette:   defaultPalette(),
		dithering: true,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// ─── Options ──────────────────────────────────────────────────────────────────

// Option configures an [Encoder].
type Option func(*Encoder)

// WithPalette sets a custom colour palette for all subsequent frames.
// The palette must have at most 256 entries; excess entries are silently
// ignored during encoding.
func WithPalette(p color.Palette) Option {
	return func(e *Encoder) { e.palette = p }
}

// WithLoopCount sets the GIF loop count.
//
//   - 0  = loop forever (default)
//   - -1 = play once, do not loop
//   - n  = loop exactly n times
func WithLoopCount(n int) Option {
	return func(e *Encoder) { e.loopCount = n }
}

// WithDithering enables or disables Floyd-Steinberg error-diffusion dithering
// during colour quantisation (default: true / enabled).
// Disabling dithering is faster but may produce visible colour banding on
// images with smooth gradients.
func WithDithering(enabled bool) Option {
	return func(e *Encoder) { e.dithering = enabled }
}

// ─── Frame API ────────────────────────────────────────────────────────────────

// AddFrame converts img to a paletted image and appends it as a new animation
// frame.  delayMs is the frame delay in milliseconds; it is rounded down to
// the nearest 10 ms (the GIF time unit is 1/100 s).  A minimum of 10 ms is
// enforced even if delayMs is smaller.
func (e *Encoder) AddFrame(img image.Image, delayMs int) error {
	paletted, err := quantise(img, e.palette, e.dithering)
	if err != nil {
		return fmt.Errorf("gif: quantise frame: %w", err)
	}
	e.appendFrame(paletted, delayMs)
	return nil
}

// AddPaletted appends a pre-quantised frame directly, bypassing colour
// conversion.  Use this for maximum control over the output (e.g. when you
// have already computed the optimal palette for a frame).
func (e *Encoder) AddPaletted(img *image.Paletted, delayMs int) {
	e.appendFrame(img, delayMs)
}

func (e *Encoder) appendFrame(img *image.Paletted, delayMs int) {
	delay := delayMs / 10
	if delay < 1 {
		delay = 1
	}
	e.frames = append(e.frames, img)
	e.delays = append(e.delays, delay)
	e.disposals = append(e.disposals, stdgif.DisposalBackground)
}

// ─── Output ───────────────────────────────────────────────────────────────────

// Encode writes the GIF animation to w.
// Returns an error if no frames have been added.
func (e *Encoder) Encode(w io.Writer) error {
	if len(e.frames) == 0 {
		return fmt.Errorf("gif: no frames to encode")
	}
	return stdgif.EncodeAll(w, &stdgif.GIF{
		Image:     e.frames,
		Delay:     e.delays,
		Disposal:  e.disposals,
		LoopCount: e.loopCount,
	})
}

// Bytes encodes the GIF animation and returns the raw bytes.
// Returns an error if no frames have been added.
func (e *Encoder) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := e.Encode(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Len returns the number of frames added so far.
func (e *Encoder) Len() int { return len(e.frames) }

// Reset clears all accumulated frames and resets delays/disposals, while
// keeping the palette, dithering, and loop-count settings.
func (e *Encoder) Reset() {
	e.frames = e.frames[:0]
	e.delays = e.delays[:0]
	e.disposals = e.disposals[:0]
}

// ─── colour quantisation ──────────────────────────────────────────────────────

// quantise converts any image.Image to an *image.Paletted using the given palette.
// If dither is true, Floyd-Steinberg error diffusion is applied.
func quantise(img image.Image, p color.Palette, dither bool) (*image.Paletted, error) {
	bounds := img.Bounds()
	dst := image.NewPaletted(bounds, p)
	if dither {
		stddraw.FloydSteinberg.Draw(dst, bounds, img, bounds.Min)
	} else {
		stddraw.Draw(dst, bounds, img, bounds.Min, stddraw.Src)
	}
	return dst, nil
}

// ─── default palette ──────────────────────────────────────────────────────────

// defaultPalette returns a 256-colour web-safe palette:
// 216 colours from the 6×6×6 RGB cube plus 40 evenly-spaced grey shades.
func defaultPalette() color.Palette {
	p := make(color.Palette, 0, 256)

	// 6×6×6 RGB cube (216 colours).
	for r := range 6 {
		for g := range 6 {
			for b := range 6 {
				p = append(p, color.RGBA{
					R: uint8(r * 51),
					G: uint8(g * 51),
					B: uint8(b * 51),
					A: 255,
				})
			}
		}
	}

	// 40 grey shades to round out to 256 entries.
	for i := 1; i <= 40; i++ {
		v := uint8(i * 255 / 40)
		p = append(p, color.RGBA{R: v, G: v, B: v, A: 255})
	}

	return p
}
