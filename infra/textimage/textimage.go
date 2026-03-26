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

	xdraw "golang.org/x/image/draw"
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
	if imgW < 1 {
		imgW = 1
	}
	if imgH < 1 {
		imgH = 1
	}

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	draw.Draw(img, img.Bounds(), image.NewUniform(r.opts.BgColor), image.Point{}, draw.Src)
	if r.opts.BgImage != nil {
		drawBgImage(img, r.opts.BgImage, r.opts.BgFit)
	}

	r.renderOntoLocked(img, 0, lines, imgW, lineH, ascent)
	return img, nil
}

// renderOnto renders text directly onto dst at yOffset without filling a background.
// It acquires r.mu internally and is intended for [Canvas] text blocks that write
// directly onto the already-background-filled canvas image.
func (r *Renderer) renderOnto(dst *image.RGBA, yOffset int, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	lines := r.wrapText(text)
	metrics := r.face.Metrics()
	naturalH := (metrics.Ascent + metrics.Descent).Ceil()
	lineH := int(float64(naturalH) * r.opts.LineHeight)
	ascent := metrics.Ascent.Ceil()

	imgW := r.opts.Width
	if imgW < 1 {
		imgW = dst.Bounds().Dx()
	}
	r.renderOntoLocked(dst, yOffset, lines, imgW, lineH, ascent)
}

// renderLine holds the computed position and pixel width of a single text line.
type renderLine struct{ x, y, w int }

// renderOntoLocked is the core text-rendering kernel.
// It draws backdrop regions and text lines onto dst at the given yOffset.
// Caller must hold r.mu.
func (r *Renderer) renderOntoLocked(dst *image.RGBA, yOffset int, lines []string, imgW, lineH, ascent int) {
	if len(lines) == 0 {
		return
	}

	// Pre-compute per-line layout (x, y, measured width).
	layouts := make([]renderLine, len(lines))
	for i, line := range lines {
		y := yOffset + r.opts.PaddingY + ascent + i*lineH
		x := r.opts.PaddingX
		lw := measureString(r.face, line)
		switch r.opts.Align {
		case AlignCenter:
			x = (imgW - lw) / 2
		case AlignRight:
			x = imgW - lw - r.opts.PaddingX
		default:
		}
		layouts[i] = renderLine{x: x, y: y, w: lw}
	}

	// BackdropModeBlock: single backdrop region covering all lines.
	if needBackdrop(r.opts) && r.opts.TextBackdropMode == BackdropModeBlock {
		bpx, bpy := r.opts.TextBackdropPadX, r.opts.TextBackdropPadY
		x0, y0 := imgW, dst.Bounds().Dy()
		x1, y1 := 0, 0
		for _, ll := range layouts {
			if ll.x-bpx < x0 {
				x0 = ll.x - bpx
			}
			if ll.y-ascent-bpy < y0 {
				y0 = ll.y - ascent - bpy
			}
			if ll.x+ll.w+bpx > x1 {
				x1 = ll.x + ll.w + bpx
			}
			if ll.y-ascent+lineH+bpy > y1 {
				y1 = ll.y - ascent + lineH + bpy
			}
		}
		blockRect := image.Rect(x0, y0, x1, y1).Intersect(dst.Bounds())
		drawBackdropRegion(dst, blockRect, r.opts)
	}

	// Text shadow — drawn before main text so it appears behind.
	if hasShadow(r.opts) {
		r.renderShadow(dst, layouts, lines, lineH, ascent)
	}

	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(r.opts.FontColor),
		Face: r.face,
	}

	for i, ll := range layouts {
		// BackdropModePerLine: per-line backdrop region.
		if needBackdrop(r.opts) && r.opts.TextBackdropMode == BackdropModePerLine {
			bpx, bpy := r.opts.TextBackdropPadX, r.opts.TextBackdropPadY
			rect := image.Rect(
				ll.x-bpx,
				ll.y-ascent-bpy,
				ll.x+ll.w+bpx,
				ll.y-ascent+lineH+bpy,
			).Intersect(dst.Bounds())
			drawBackdropRegion(dst, rect, r.opts)
		}
		d.Dot = fixed.P(ll.x, ll.y)
		d.DrawString(lines[i])
	}
}

// hasShadow reports whether a text shadow should be rendered.
func hasShadow(o Options) bool { return o.TextShadowColor != nil }

// renderShadow draws the drop shadow for all text lines onto dst.
// For blurRadius == 0 a hard-edge shadow is drawn directly; otherwise a
// temporary layer is blurred and composited to avoid leaking outside the
// shadow bounds.
func (r *Renderer) renderShadow(dst *image.RGBA, layouts []renderLine, lines []string, lineH, ascent int) {
	ox := r.opts.TextShadowOffsetX
	oy := r.opts.TextShadowOffsetY
	blur := r.opts.TextShadowBlur
	src := image.NewUniform(r.opts.TextShadowColor)

	if blur <= 0 {
		// Hard-edge shadow: draw directly onto dst at the offset position.
		sd := &font.Drawer{Dst: dst, Src: src, Face: r.face}
		for i, ll := range layouts {
			sd.Dot = fixed.P(ll.x+ox, ll.y+oy)
			sd.DrawString(lines[i])
		}
		return
	}

	// Soft shadow: render to a tight temp layer, blur it, then composite.
	// Compute the tight bounding box of all shadow glyphs (+ blur margin).
	minX := layouts[0].x + ox - blur
	maxX := layouts[0].x + layouts[0].w + ox + blur
	minY := layouts[0].y - ascent + oy - blur
	maxY := layouts[0].y - ascent + lineH + oy + blur
	for _, ll := range layouts[1:] {
		if v := ll.x + ox - blur; v < minX {
			minX = v
		}
		if v := ll.x + ll.w + ox + blur; v > maxX {
			maxX = v
		}
		if v := ll.y - ascent + oy - blur; v < minY {
			minY = v
		}
		if v := ll.y - ascent + lineH + oy + blur; v > maxY {
			maxY = v
		}
	}

	sw, sh := maxX-minX, maxY-minY
	if sw < 1 || sh < 1 {
		return
	}

	// The shadow layer uses (0,0)-origin; text positions are shifted by (-minX, -minY).
	shadowLayer := image.NewRGBA(image.Rect(0, 0, sw, sh))
	sd := &font.Drawer{Dst: shadowLayer, Src: src, Face: r.face}
	for i, ll := range layouts {
		sd.Dot = fixed.P(ll.x+ox-minX, ll.y+oy-minY)
		sd.DrawString(lines[i])
	}
	blurRGBA(shadowLayer, blur, 2)

	// Composite shadow layer onto dst, clamped to dst bounds.
	dstRect := image.Rect(minX, minY, minX+sw, minY+sh).Intersect(dst.Bounds())
	if !dstRect.Empty() {
		draw.Draw(dst, dstRect, shadowLayer, image.Pt(dstRect.Min.X-minX, dstRect.Min.Y-minY), draw.Over)
	}
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

// parsedFontCache caches the result of os.ReadFile + opentype.Parse to avoid
// redundant disk I/O and font parsing.  Creating a new font.Face from a cached
// *opentype.Font with different FaceOptions (size, DPI) is extremely cheap by
// comparison.
//
// This cache is intentionally permanent (write-once, never evicted):
//   - The key space is bounded by the number of distinct font paths used in the
//     application — typically 1–2 entries — so the map bucket array stays tiny.
//   - Parsed font objects are meant to live for the process lifetime.
var (
	parsedFontMu    sync.Mutex
	parsedFontCache = make(map[string]*opentype.Font) // key: "path\x00index"
)

// getCachedFont returns a previously parsed *opentype.Font, or nil if not cached.
func getCachedFont(key string) *opentype.Font {
	parsedFontMu.Lock()
	f := parsedFontCache[key]
	parsedFontMu.Unlock()
	return f
}

// setCachedFont stores a parsed *opentype.Font in the cache.
func setCachedFont(key string, f *opentype.Font) {
	parsedFontMu.Lock()
	parsedFontCache[key] = f
	parsedFontMu.Unlock()
}

// buildFace constructs a font.Face from the supplied options.
// It supports both plain TTF/OTF files and TrueType Collection (.ttc) files.
//
// Font file reads and parsing results are cached globally so that repeated
// calls with the same FontPath (common in Canvas multi-block rendering) only
// hit the disk once.
func buildFace(o Options) (font.Face, error) {
	dpi := o.DPI
	if dpi <= 0 {
		dpi = 72
	}
	fontSize := o.FontSize
	if fontSize <= 0 {
		fontSize = 16
	}
	faceOpts := &opentype.FaceOptions{Size: fontSize, DPI: dpi}

	// Fast path: check cache before any disk I/O.
	// For FontPath-based fonts, we can resolve the cache key without reading the file.
	if o.FontData == nil && o.FontPath != "" {
		// Try both TTC and non-TTC cache keys; one of them will match on repeat calls.
		ttcKey := fmt.Sprintf("%s\x00%d", o.FontPath, o.TTCIndex)
		plainKey := o.FontPath
		if cached := getCachedFont(ttcKey); cached != nil {
			face, err := opentype.NewFace(cached, faceOpts)
			if err != nil {
				return nil, fmt.Errorf("create face from cached collection font: %w", err)
			}
			return face, nil
		}
		if cached := getCachedFont(plainKey); cached != nil {
			face, err := opentype.NewFace(cached, faceOpts)
			if err != nil {
				return nil, fmt.Errorf("create face from cached font: %w", err)
			}
			return face, nil
		}
	}

	// Slow path: read font data and parse.
	var (
		raw      []byte
		fontPath string
	)
	switch {
	case o.FontData != nil:
		raw = o.FontData
		fontPath = o.FontPath
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

	if isTTC(fontPath, raw) {
		idx := o.TTCIndex
		cacheKey := fmt.Sprintf("%s\x00%d", fontPath, idx)
		col, err := opentype.ParseCollection(raw)
		if err != nil {
			return nil, fmt.Errorf("parse font collection %q: %w", fontPath, err)
		}
		if idx < 0 || idx >= col.NumFonts() {
			idx = 0
		}
		f, err := col.Font(idx)
		if err != nil {
			return nil, fmt.Errorf("get font[%d] from collection: %w", idx, err)
		}
		setCachedFont(cacheKey, f)
		face, err := opentype.NewFace(f, faceOpts)
		if err != nil {
			return nil, fmt.Errorf("create face from collection: %w", err)
		}
		return face, nil
	}

	cacheKey := fontPath // empty string for embedded default font
	if cached := getCachedFont(cacheKey); cached != nil {
		face, err := opentype.NewFace(cached, faceOpts)
		if err != nil {
			return nil, fmt.Errorf("create face from cached font: %w", err)
		}
		return face, nil
	}
	parsed, err := opentype.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse font: %w", err)
	}
	setCachedFont(cacheKey, parsed)
	face, err := opentype.NewFace(parsed, faceOpts)
	if err != nil {
		return nil, fmt.Errorf("create face: %w", err)
	}
	return face, nil
}

// isTTC reports whether the font data belongs to a TrueType/OpenType Collection.
func isTTC(path string, data []byte) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".ttc" || ext == ".otc" {
		return true
	}
	// Magic bytes: "ttcf"
	return len(data) >= 4 &&
		data[0] == 0x74 && data[1] == 0x74 &&
		data[2] == 0x63 && data[3] == 0x66
}

// wrapText splits text into display lines, honouring explicit newlines and
// performing word-wrap when a maximum width is known.
// For CJK text (no word boundaries), character-level wrapping is used as
// a fallback for any token that exceeds the maximum width on its own.
func (r *Renderer) wrapText(text string) []string {
	maxW := r.opts.MaxWidth
	if maxW == 0 && r.opts.Width > 0 {
		maxW = r.opts.Width - 2*r.opts.PaddingX
	}

	rawLines := strings.Split(text, "\n")
	result := make([]string, 0, len(rawLines))

	for _, para := range rawLines {
		if maxW <= 0 {
			result = append(result, para)
			continue
		}

		words := strings.Fields(para)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}

		line := ""
		for _, word := range words {
			// A single word that exceeds maxW is split character by character
			// (handles CJK text with no spaces and very long ASCII tokens).
			if measureString(r.face, word) > maxW {
				if line != "" {
					result = append(result, line)
					line = ""
				}
				result = append(result, wrapByChar(r.face, word, maxW)...)
				continue
			}
			var candidate string
			if line == "" {
				candidate = word
			} else {
				candidate = line + " " + word
			}
			if measureString(r.face, candidate) <= maxW {
				line = candidate
			} else {
				result = append(result, line)
				line = word
			}
		}
		if line != "" {
			result = append(result, line)
		}
	}

	return result
}

// wrapByChar splits text into lines at character boundaries, ensuring each
// line is at most maxW pixels wide. Used as CJK/long-token fallback.
func wrapByChar(face font.Face, text string, maxW int) []string {
	var lines []string
	var cur []rune
	for _, ch := range text {
		next := string(append(cur, ch))
		if measureString(face, next) > maxW && len(cur) > 0 {
			lines = append(lines, string(cur))
			cur = []rune{ch}
		} else {
			cur = append(cur, ch)
		}
	}
	if len(cur) > 0 {
		lines = append(lines, string(cur))
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
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

	r.mu.Lock()
	defer r.mu.Unlock()

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

// needBackdrop reports whether backdrop rendering is needed.
func needBackdrop(o Options) bool {
	if o.TextBackdropBlur > 0 {
		return true
	}
	if o.TextBackdropColor == nil {
		return false
	}
	_, _, _, a := o.TextBackdropColor.RGBA()
	return a > 0
}

// drawBackdropRegion draws a backdrop (optional blur + optional colour overlay)
// within rect on dst, clipped to the configured shape.
func drawBackdropRegion(dst *image.RGBA, rect image.Rectangle, o Options) {
	rect = rect.Intersect(dst.Bounds())
	if rect.Empty() {
		return
	}
	w, h := rect.Dx(), rect.Dy()

	mask := makeBackdropMask(w, h, o.TextBackdropShape, o.TextBackdropRoundRadius)

	if o.TextBackdropBlur > 0 {
		sub := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			srcOff := y*dst.Stride + rect.Min.X*4
			dstOff := (y - rect.Min.Y) * sub.Stride
			copy(sub.Pix[dstOff:dstOff+w*4], dst.Pix[srcOff:srcOff+w*4])
		}
		blurRGBA(sub, o.TextBackdropBlur, 3)
		draw.DrawMask(dst, rect, sub, image.Point{}, mask, image.Point{}, draw.Over)
	}

	if o.TextBackdropColor != nil {
		_, _, _, a := o.TextBackdropColor.RGBA()
		if a > 0 {
			draw.DrawMask(dst, rect, image.NewUniform(o.TextBackdropColor), image.Point{}, mask, image.Point{}, draw.Over)
		}
	}
}

// makeBackdropMask returns an Alpha mask of the given shape.
func makeBackdropMask(w, h int, shape BackdropShape, roundR int) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	switch shape {
	case BackdropShapeEllipse:
		cx, cy := w/2, h/2
		a2 := float64(cx) * float64(cx)
		b2 := float64(cy) * float64(cy)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dx, dy := float64(x-cx), float64(y-cy)
				if dx*dx/a2+dy*dy/b2 <= 1.0 {
					mask.SetAlpha(x, y, color.Alpha{A: 255})
				}
			}
		}
	case BackdropShapeRounded:
		r := roundR
		if r < 1 {
			r = 1
		}
		r2 := r * r
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if inRoundedRect(x, y, w, h, r, r2) {
					mask.SetAlpha(x, y, color.Alpha{A: 255})
				}
			}
		}
	default: // BackdropShapeRect
		for i := range mask.Pix {
			mask.Pix[i] = 255
		}
	}
	return mask
}

// drawBgImage draws src onto dst according to the fit mode.
func drawBgImage(dst *image.RGBA, src image.Image, fit BgFitMode) {
	db := dst.Bounds()
	dw, dh := db.Dx(), db.Dy()
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw < 1 || sh < 1 || dw < 1 || dh < 1 {
		return
	}

	switch fit {
	case BgFitStretch:
		xdraw.CatmullRom.Scale(dst, db, src, sb, xdraw.Over, nil)

	case BgFitFill:
		scaleW := float64(dw) / float64(sw)
		scaleH := float64(dh) / float64(sh)
		scale := scaleW
		if scaleH > scale {
			scale = scaleH
		}
		scaledW := int(float64(sw)*scale + 0.5)
		scaledH := int(float64(sh)*scale + 0.5)
		tmp := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
		xdraw.CatmullRom.Scale(tmp, tmp.Bounds(), src, sb, xdraw.Over, nil)
		offX := (scaledW - dw) / 2
		offY := (scaledH - dh) / 2
		draw.Draw(dst, db, tmp, image.Pt(offX, offY), draw.Over)

	case BgFitFit:
		scaleW := float64(dw) / float64(sw)
		scaleH := float64(dh) / float64(sh)
		scale := scaleW
		if scaleH < scale {
			scale = scaleH
		}
		scaledW := int(float64(sw)*scale + 0.5)
		scaledH := int(float64(sh)*scale + 0.5)
		if scaledW < 1 {
			scaledW = 1
		}
		if scaledH < 1 {
			scaledH = 1
		}
		tmp := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
		xdraw.CatmullRom.Scale(tmp, tmp.Bounds(), src, sb, xdraw.Over, nil)
		offX := (dw - scaledW) / 2
		offY := (dh - scaledH) / 2
		draw.Draw(dst, tmp.Bounds().Add(image.Pt(offX, offY)), tmp, image.Point{}, draw.Over)

	case BgFitCenter:
		offX := (dw - sw) / 2
		offY := (dh - sh) / 2
		draw.Draw(dst, sb.Add(image.Pt(offX, offY)), src, sb.Min, draw.Over)

	case BgFitTile:
		for y := 0; y < dh; y += sh {
			for x := 0; x < dw; x += sw {
				draw.Draw(dst, image.Rect(x, y, x+sw, y+sh).Intersect(db), src, sb.Min, draw.Over)
			}
		}
	}
}
