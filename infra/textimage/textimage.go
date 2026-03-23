package textimage

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Renderer converts text strings to raster images.
// Create one with [New]; a single Renderer is safe for concurrent use —
// a mutex serialises access to the underlying font face, whose internal
// sfnt.Buffer is not goroutine-safe.
// For maximum throughput under high concurrency, keep one Renderer per
// goroutine or use [Canvas], which constructs its own temporary Renderer
// per AddText call.
type Renderer struct {
	mu   sync.Mutex
	face font.Face
	opts Options
}

// New creates a [Renderer] with the given options.
// Without any options it uses the built-in Go Regular font at 16 pt / 72 DPI.
func New(opts ...Option) (*Renderer, error) {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}

	face, err := buildFace(o)
	if err != nil {
		return nil, fmt.Errorf("textimage: build font face: %w", err)
	}
	return &Renderer{face: face, opts: o}, nil
}

// Close releases resources held by the underlying font face.
// The Renderer must not be used after Close returns.
func (r *Renderer) Close() error {
	return r.face.Close()
}

// Render converts text to an [image.Image].
// Newlines in text produce explicit line breaks; long lines are word-wrapped
// when a finite maximum width is known (controlled by [WithSize] / [WithMaxWidth]).
// Safe for concurrent use from multiple goroutines.
func (r *Renderer) Render(text string) (image.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	lines := r.wrapText(text)

	metrics := r.face.Metrics()
	// Natural line height (ascent + descent) scaled by the LineHeight multiplier.
	naturalH := (metrics.Ascent + metrics.Descent).Ceil()
	lineH := int(float64(naturalH) * r.opts.LineHeight)
	ascent := metrics.Ascent.Ceil()

	// Resolve image dimensions.
	imgW, imgH := r.opts.Width, r.opts.Height
	if imgW == 0 || imgH == 0 {
		maxLW := 0
		for _, l := range lines {
			if w := measureString(r.face, l); w > maxLW {
				maxLW = w
			}
		}
		if imgW == 0 {
			imgW = maxLW + 2*r.opts.PaddingX
		}
		if imgH == 0 {
			imgH = lineH*len(lines) + 2*r.opts.PaddingY
		}
	}

	// Guard against degenerate sizes.
	if imgW < 1 {
		imgW = 1
	}
	if imgH < 1 {
		imgH = 1
	}

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	draw.Draw(img, img.Bounds(), image.NewUniform(r.opts.BgColor), image.Point{}, draw.Src)

	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(r.opts.FontColor),
		Face: r.face,
	}

	for i, line := range lines {
		y := r.opts.PaddingY + ascent + i*lineH
		x := r.opts.PaddingX

		switch r.opts.Align {
		case AlignCenter:
			x = (imgW - measureString(r.face, line)) / 2
		case AlignRight:
			x = imgW - measureString(r.face, line) - r.opts.PaddingX
		}

		d.Dot = fixed.P(x, y)
		d.DrawString(line)
	}

	return img, nil
}

// RenderToPNG renders text and returns the result as PNG-encoded bytes.
func (r *Renderer) RenderToPNG(text string) ([]byte, error) {
	img, err := r.Render(text)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("textimage: png encode: %w", err)
	}
	return buf.Bytes(), nil
}

// RenderToJPEG renders text and returns the result as JPEG-encoded bytes.
// quality should be in [1, 100]; values outside that range default to 85.
func (r *Renderer) RenderToJPEG(text string, quality int) ([]byte, error) {
	img, err := r.Render(text)
	if err != nil {
		return nil, err
	}
	if quality < 1 || quality > 100 {
		quality = 85
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("textimage: jpeg encode: %w", err)
	}
	return buf.Bytes(), nil
}

// RenderToFile renders text and writes the image to filename.
// The output format is inferred from the file extension:
//   - .jpg / .jpeg → JPEG (quality 85)
//   - anything else → PNG
func (r *Renderer) RenderToFile(filename, text string) error {
	img, err := r.Render(text)
	if err != nil {
		return err
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("textimage: create file %q: %w", filename, err)
	}
	defer f.Close()

	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg":
		if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
			return fmt.Errorf("textimage: jpeg encode: %w", err)
		}
	default:
		if err := png.Encode(f, img); err != nil {
			return fmt.Errorf("textimage: png encode: %w", err)
		}
	}
	return nil
}

// RenderToWriter renders text and writes the PNG image to w.
func (r *Renderer) RenderToWriter(w interface{ Write([]byte) (int, error) }, text string) error {
	img, err := r.Render(text)
	if err != nil {
		return err
	}
	if err := png.Encode(w, img); err != nil {
		return fmt.Errorf("textimage: png encode: %w", err)
	}
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildFace constructs a font.Face from the supplied options.
// It supports both plain TTF/OTF files and TrueType Collection (.ttc) files.
func buildFace(o Options) (font.Face, error) {
	// Resolve raw font bytes and the file path (used for extension sniffing).
	var (
		raw      []byte
		fontPath string
	)
	switch {
	case o.FontData != nil:
		raw = o.FontData
		fontPath = o.FontPath // may be empty; used only for .ttc detection
	case o.FontPath != "":
		data, err := os.ReadFile(o.FontPath)
		if err != nil {
			return nil, fmt.Errorf("read font %q: %w", o.FontPath, err)
		}
		raw = data
		fontPath = o.FontPath
	default:
		raw = goregular.TTF
	}

	dpi := o.DPI
	if dpi <= 0 {
		dpi = 72
	}
	fontSize := o.FontSize
	if fontSize <= 0 {
		fontSize = 16
	}
	faceOpts := &opentype.FaceOptions{Size: fontSize, DPI: dpi}

	// TrueType Collection files (.ttc / .otc) bundle multiple fonts; use the
	// first one (index 0) which is typically the Regular weight.
	if isTTC(fontPath, raw) {
		col, err := opentype.ParseCollection(raw)
		if err != nil {
			return nil, fmt.Errorf("parse font collection %q: %w", fontPath, err)
		}
		idx := o.TTCIndex
		if idx < 0 || idx >= col.NumFonts() {
			idx = 0
		}
		f, err := col.Font(idx)
		if err != nil {
			return nil, fmt.Errorf("get font[%d] from collection: %w", idx, err)
		}
		face, err := opentype.NewFace(f, faceOpts)
		if err != nil {
			return nil, fmt.Errorf("create face from collection: %w", err)
		}
		return face, nil
	}

	// Plain TTF / OTF.
	parsed, err := opentype.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse font: %w", err)
	}
	face, err := opentype.NewFace(parsed, faceOpts)
	if err != nil {
		return nil, fmt.Errorf("create face: %w", err)
	}
	return face, nil
}

// isTTC reports whether the font data belongs to a TrueType/OpenType Collection.
// It checks the file extension first, then falls back to magic-byte detection.
func isTTC(path string, data []byte) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".ttc" || ext == ".otc" {
		return true
	}
	// Magic bytes: "ttcf" (0x74 0x74 0x63 0x66)
	return len(data) >= 4 &&
		data[0] == 0x74 && data[1] == 0x74 &&
		data[2] == 0x63 && data[3] == 0x66
}

// wrapText splits text into display lines, honouring explicit newlines and
// performing word-wrap when a maximum width is known.
func (r *Renderer) wrapText(text string) []string {
	maxW := r.opts.MaxWidth
	if maxW == 0 && r.opts.Width > 0 {
		maxW = r.opts.Width - 2*r.opts.PaddingX
	}

	rawLines := strings.Split(text, "\n")
	result := make([]string, 0, len(rawLines))

	for _, para := range rawLines {
		if maxW <= 0 {
			// No wrapping; preserve the paragraph as-is.
			result = append(result, para)
			continue
		}

		words := strings.Fields(para)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}

		line := words[0]
		for _, word := range words[1:] {
			candidate := line + " " + word
			if measureString(r.face, candidate) <= maxW {
				line = candidate
			} else {
				result = append(result, line)
				line = word
			}
		}
		result = append(result, line)
	}

	return result
}

// measureString returns the advance width of s in pixels.
func measureString(face font.Face, s string) int {
	return font.MeasureString(face, s).Ceil()
}

// DefaultFontTTF returns the raw TTF bytes of the built-in Go Regular font,
// allowing callers to reuse them (e.g. with [WithFontData]).
func DefaultFontTTF() []byte {
	cp := make([]byte, len(goregular.TTF))
	copy(cp, goregular.TTF)
	return cp
}

// MustNew is like [New] but panics on error. Useful in package-level var blocks.
func MustNew(opts ...Option) *Renderer {
	r, err := New(opts...)
	if err != nil {
		panic("textimage.MustNew: " + err.Error())
	}
	return r
}

// RenderText is a convenience wrapper that creates a one-shot renderer,
// renders text, and returns PNG bytes. For repeated rendering, prefer [New].
func RenderText(text string, opts ...Option) ([]byte, error) {
	r, err := New(opts...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.RenderToPNG(text)
}

// TextSize returns the pixel dimensions (width, height) that the text would
// occupy using the given options, without actually rendering an image.
func TextSize(text string, opts ...Option) (width, height int, err error) {
	r, err := New(opts...)
	if err != nil {
		return 0, 0, err
	}
	defer r.Close()

	lines := r.wrapText(text)
	metrics := r.face.Metrics()
	naturalH := (metrics.Ascent + metrics.Descent).Ceil()
	lineH := int(float64(naturalH) * r.opts.LineHeight)

	maxLW := 0
	for _, l := range lines {
		if w := measureString(r.face, l); w > maxLW {
			maxLW = w
		}
	}
	return maxLW + 2*r.opts.PaddingX, lineH*len(lines) + 2*r.opts.PaddingY, nil
}

// highlightColor is a helper used only by the package itself to ensure the
// background is never fully transparent when the user supplies color.Transparent.
func opaqueOrDefault(c color.Color, def color.Color) color.Color {
	if c == nil {
		return def
	}
	_, _, _, a := c.RGBA()
	if a == 0 {
		return def
	}
	return c
}
