package textimage

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	stdraw "image/draw"
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// ─── Image placement options ──────────────────────────────────────────────────

// ImageOption configures how an image is placed on a [Canvas].
type ImageOption func(*imageOpts)

type imageOpts struct {
	width    int       // scale to exact width (0 = auto)
	height   int       // scale to exact height (0 = auto)
	maxWidth int       // clamp to max width (0 = canvas width − 2×paddingX)
	align    Alignment // horizontal alignment; default AlignLeft
	paddingX int       // horizontal padding from canvas edges (left/right align)
	paddingY int       // vertical padding above and below the image
	circle   bool      // clip image to a circle (avatar style)
	roundR   int       // rounded-corner radius in pixels (ignored when circle=true)
}

// WithImgWidth scales the image to an exact pixel width (height is proportional
// unless WithImgHeight is also set).
func WithImgWidth(w int) ImageOption { return func(o *imageOpts) { o.width = w } }

// WithImgHeight scales the image to an exact pixel height (width is proportional
// unless WithImgWidth is also set).
func WithImgHeight(h int) ImageOption { return func(o *imageOpts) { o.height = h } }

// WithImgMaxWidth clamps the image to at most w pixels wide before scaling.
// Overrides the default of canvas-width − 2×paddingX.
func WithImgMaxWidth(w int) ImageOption { return func(o *imageOpts) { o.maxWidth = w } }

// WithImgAlign sets the horizontal alignment of the image within the canvas row.
func WithImgAlign(a Alignment) ImageOption { return func(o *imageOpts) { o.align = a } }

// WithImgPadding sets the horizontal (x) and vertical (y) padding around the image.
func WithImgPadding(x, y int) ImageOption {
	return func(o *imageOpts) { o.paddingX = x; o.paddingY = y }
}

// WithImgCircle clips the image to a circle. Useful for bot avatars.
// The circle is inscribed in the shorter image dimension.
func WithImgCircle() ImageOption { return func(o *imageOpts) { o.circle = true } }

// WithImgRoundRadius clips the image corners with the given radius in pixels.
// Ignored when [WithImgCircle] is also set.
func WithImgRoundRadius(r int) ImageOption { return func(o *imageOpts) { o.roundR = r } }

// ─── Canvas ───────────────────────────────────────────────────────────────────

// canvasBlock is an element that knows its rendered height and can paint itself
// onto a destination image at a given y-offset.
type canvasBlock interface {
	blockHeight() int
	drawAt(dst *image.RGBA, yOffset, canvasWidth int)
}

// Canvas builds a composite image by stacking content blocks vertically.
//
// Typical usage:
//
//	c, _ := textimage.NewCanvas(640, textimage.WithCJKFont(), textimage.WithFontSize(18))
//	_ = c.AddImage(avatarImg, textimage.WithImgCircle(), textimage.WithImgWidth(80), textimage.WithImgAlign(textimage.AlignCenter))
//	_ = c.AddSpacer(8)
//	_ = c.AddText("Bot Health Report\n───────────────────")
//	_ = c.AddText(reportText)
//	png, _ := c.ResultPNG()
type Canvas struct {
	width   int
	bgColor color.Color
	blocks  []canvasBlock
	opts    Options // canvas-level text defaults
}

// NewCanvas creates a new Canvas with the given pixel width.
// Canvas-level options (font, size, colors, padding …) serve as defaults for all
// [Canvas.AddText] calls; they can be overridden per-block.
// The canvas height grows automatically as blocks are added.
func NewCanvas(width int, opts ...Option) (*Canvas, error) {
	if width < 1 {
		return nil, fmt.Errorf("textimage: canvas width must be ≥ 1, got %d", width)
	}
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	return &Canvas{width: width, bgColor: o.BgColor, opts: o}, nil
}

// AddSpacer appends px pixels of transparent vertical whitespace.
func (c *Canvas) AddSpacer(px int) {
	if px > 0 {
		c.blocks = append(c.blocks, spacerBlock(px))
	}
}

// AddText renders text and appends it as a full-width block.
// The text is word-wrapped to the canvas width automatically.
// Per-block options override the canvas-level defaults for this block only.
func (c *Canvas) AddText(text string, opts ...Option) error {
	o := c.opts
	for _, fn := range opts {
		fn(&o)
	}
	o.Width = c.width
	o.Height = 0
	r, err := New(func(t *Options) { *t = o })
	if err != nil {
		return fmt.Errorf("textimage canvas AddText: %w", err)
	}
	img, err := r.Render(text)
	_ = r.Close()
	if err != nil {
		return fmt.Errorf("textimage canvas AddText: %w", err)
	}
	c.blocks = append(c.blocks, &imgBlock{img: img})
	return nil
}

// AddImage appends a pre-decoded image, optionally resized, aligned, or clipped.
// By default the image is left-aligned and scaled down to fit the canvas width.
func (c *Canvas) AddImage(src image.Image, opts ...ImageOption) error {
	if src == nil {
		return fmt.Errorf("textimage canvas AddImage: nil image")
	}
	o := imageOpts{}
	for _, fn := range opts {
		fn(&o)
	}
	processed, err := processImage(src, o, c.width)
	if err != nil {
		return fmt.Errorf("textimage canvas AddImage: %w", err)
	}
	c.blocks = append(c.blocks, &imgBlock{img: processed, opts: o})
	return nil
}

// AddImageBytes decodes src (PNG / JPEG / GIF / …) and appends the image.
// It is a convenience wrapper around [Canvas.AddImage].
func (c *Canvas) AddImageBytes(src []byte, opts ...ImageOption) error {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return fmt.Errorf("textimage canvas AddImageBytes: %w", err)
	}
	return c.AddImage(img, opts...)
}

// RowItem describes one cell in a horizontal row created by [Canvas.AddRow].
// Set either Text or Image (not both). Width=0 means the cell shares the
// remaining canvas width equally with other zero-width cells.
type RowItem struct {
	// Width is the explicit pixel width of this cell.
	// Set to 0 to share remaining canvas width equally with other zero-width cells.
	Width int

	// Text, if non-empty, is rendered as a text block filling this cell.
	Text string
	// TextOpts are per-cell text options that override the Canvas defaults.
	TextOpts []Option

	// Image, if non-nil, is placed in this cell.
	Image image.Image
	// ImageOpts configure image scaling / alignment within this cell.
	ImageOpts []ImageOption
}

// AddRow appends a single row of horizontally laid-out cells.
// Cells with Width==0 share the remaining canvas width equally.
// Each cell is vertically centred to the tallest cell in the row.
//
// Example — avatar on the left, status text on the right:
//
//	c.AddRow(
//	    textimage.RowItem{Width: 80, Image: avatarImg, ImageOpts: []textimage.ImageOption{textimage.WithImgCircle()}},
//	    textimage.RowItem{Text: "🟢 Online\nPing: 3 ms", TextOpts: []textimage.Option{textimage.WithFontSize(16)}},
//	)
func (c *Canvas) AddRow(items ...RowItem) error {
	if len(items) == 0 {
		return nil
	}

	// Resolve cell widths.
	fixedTotal, autoCount := 0, 0
	for _, it := range items {
		if it.Width > 0 {
			fixedTotal += it.Width
		} else {
			autoCount++
		}
	}
	remaining := c.width - fixedTotal
	if remaining < 0 {
		remaining = 0
	}
	autoWidth := 0
	if autoCount > 0 {
		autoWidth = remaining / autoCount
	}

	type renderedCell struct {
		img image.Image
		x   int
	}
	cells := make([]renderedCell, len(items))
	maxH := 0
	xCursor := 0

	for i, it := range items {
		cellW := it.Width
		if cellW <= 0 {
			cellW = autoWidth
		}
		if cellW < 1 {
			cellW = 1
		}

		var cellImg image.Image
		switch {
		case it.Text != "":
			o := c.opts
			for _, fn := range it.TextOpts {
				fn(&o)
			}
			o.Width = cellW
			o.Height = 0
			r, err := New(func(t *Options) { *t = o })
			if err != nil {
				return fmt.Errorf("textimage canvas AddRow cell[%d] text: %w", i, err)
			}
			rendered, err := r.Render(it.Text)
			_ = r.Close()
			if err != nil {
				return fmt.Errorf("textimage canvas AddRow cell[%d] text: %w", i, err)
			}
			cellImg = rendered

		case it.Image != nil:
			o := imageOpts{}
			for _, fn := range it.ImageOpts {
				fn(&o)
			}
			processed, err := processImage(it.Image, o, cellW)
			if err != nil {
				return fmt.Errorf("textimage canvas AddRow cell[%d] image: %w", i, err)
			}
			cellImg = processed

		default:
			// Empty spacer cell.
			cellImg = image.NewRGBA(image.Rect(0, 0, cellW, 1))
		}

		cells[i] = renderedCell{img: cellImg, x: xCursor}
		xCursor += cellW
		if h := cellImg.Bounds().Dy(); h > maxH {
			maxH = h
		}
	}

	if maxH < 1 {
		maxH = 1
	}

	// Composite cells side-by-side, each vertically centred.
	rowImg := image.NewRGBA(image.Rect(0, 0, c.width, maxH))
	stdraw.Draw(rowImg, rowImg.Bounds(), image.NewUniform(c.bgColor), image.Point{}, stdraw.Src)
	for _, cl := range cells {
		b := cl.img.Bounds()
		yOff := (maxH - b.Dy()) / 2
		destRect := b.Add(image.Pt(cl.x, yOff))
		stdraw.Draw(rowImg, destRect, cl.img, b.Min, stdraw.Over)
	}

	c.blocks = append(c.blocks, &imgBlock{img: rowImg})
	return nil
}

// Result composites all accumulated blocks top-to-bottom and returns the image.
// It can be called multiple times; subsequent AddXxx calls keep building on
// the existing block list.
func (c *Canvas) Result() image.Image {
	totalH := 0
	for _, b := range c.blocks {
		totalH += b.blockHeight()
	}
	if totalH < 1 {
		totalH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, c.width, totalH))
	stdraw.Draw(dst, dst.Bounds(), image.NewUniform(c.bgColor), image.Point{}, stdraw.Src)
	y := 0
	for _, b := range c.blocks {
		b.drawAt(dst, y, c.width)
		y += b.blockHeight()
	}
	return dst
}

// ResultPNG composites all blocks and returns the image as PNG-encoded bytes.
func (c *Canvas) ResultPNG() ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, c.Result()); err != nil {
		return nil, fmt.Errorf("textimage canvas ResultPNG: %w", err)
	}
	return buf.Bytes(), nil
}

// ResultJPEG composites all blocks and returns the image as JPEG-encoded bytes.
// quality must be in [1, 100]; values outside that range default to 85.
func (c *Canvas) ResultJPEG(quality int) ([]byte, error) {
	if quality < 1 || quality > 100 {
		quality = 85
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, c.Result(), &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("textimage canvas ResultJPEG: %w", err)
	}
	return buf.Bytes(), nil
}

// RenderToWriter composites all blocks and writes the image as PNG to w.
func (c *Canvas) RenderToWriter(w interface{ Write([]byte) (int, error) }) error {
	if err := png.Encode(w, c.Result()); err != nil {
		return fmt.Errorf("textimage canvas RenderToWriter: %w", err)
	}
	return nil
}

// ─── Block implementations ────────────────────────────────────────────────────

// spacerBlock is an invisible vertical gap of the given pixel height.
type spacerBlock int

func (s spacerBlock) blockHeight() int               { return int(s) }
func (s spacerBlock) drawAt(_ *image.RGBA, _, _ int) {}

// imgBlock holds a pre-rendered image together with alignment/padding metadata
// for positioning on the canvas.
type imgBlock struct {
	img  image.Image
	opts imageOpts
}

func (b *imgBlock) blockHeight() int {
	return b.img.Bounds().Dy() + 2*b.opts.paddingY
}

func (b *imgBlock) drawAt(dst *image.RGBA, yOffset, canvasWidth int) {
	imgW := b.img.Bounds().Dx()
	x := b.opts.paddingX
	switch b.opts.align {
	case AlignLeft:
		// x already set to paddingX above
	case AlignCenter:
		x = (canvasWidth - imgW) / 2
	case AlignRight:
		x = canvasWidth - imgW - b.opts.paddingX
	}
	srcB := b.img.Bounds()
	destRect := srcB.Add(image.Pt(x, yOffset+b.opts.paddingY))
	stdraw.Draw(dst, destRect, b.img, srcB.Min, stdraw.Over)
}

// ─── Image processing helpers ─────────────────────────────────────────────────

// processImage scales, clamps, and optionally clips src according to opts.
// canvasWidth is used to compute the default maxWidth when opts.maxWidth == 0.
func processImage(src image.Image, o imageOpts, canvasWidth int) (image.Image, error) {
	srcB := src.Bounds()
	srcW, srcH := srcB.Dx(), srcB.Dy()
	if srcW < 1 || srcH < 1 {
		return nil, fmt.Errorf("processImage: degenerate source size %dx%d", srcW, srcH)
	}

	// ── 1. Compute target size ────────────────────────────────────────────────
	dstW, dstH := srcW, srcH
	switch {
	case o.width > 0 && o.height > 0:
		dstW, dstH = o.width, o.height
	case o.width > 0:
		dstW = o.width
		dstH = srcH * dstW / srcW
	case o.height > 0:
		dstH = o.height
		dstW = srcW * dstH / srcH
	}

	// ── 2. Clamp to maxWidth ──────────────────────────────────────────────────
	maxW := o.maxWidth
	if maxW <= 0 {
		maxW = canvasWidth - 2*o.paddingX
	}
	if maxW < 1 {
		maxW = 1
	}
	if dstW > maxW {
		dstH = dstH * maxW / dstW
		dstW = maxW
	}
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}

	// ── 3. Scale (CatmullRom for high quality) ────────────────────────────────
	var scaled image.Image
	if dstW == srcW && dstH == srcH {
		scaled = src
	} else {
		out := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
		xdraw.CatmullRom.Scale(out, out.Bounds(), src, srcB, xdraw.Over, nil)
		scaled = out
	}

	// ── 4. Clip ───────────────────────────────────────────────────────────────
	if o.circle {
		return circularClip(scaled), nil
	}
	if o.roundR > 0 {
		return roundedClip(scaled, o.roundR), nil
	}
	return scaled, nil
}

// circularClip returns a copy of src clipped to the largest inscribed circle.
// Pixels outside the circle are transparent.
func circularClip(src image.Image) image.Image {
	b := src.Bounds()
	size := b.Dx()
	if b.Dy() < size {
		size = b.Dy()
	}
	r := size / 2
	r2 := r * r
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-r, y-r
			if dx*dx+dy*dy <= r2 {
				out.Set(x, y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	}
	return out
}

// roundedClip returns a copy of src with rounded corners of the given radius.
// Pixels outside the rounded rectangle are transparent.
func roundedClip(src image.Image, radius int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	r2 := radius * radius
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if inRoundedRect(x, y, w, h, radius, r2) {
				out.Set(x, y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	}
	return out
}

// inRoundedRect reports whether pixel (x, y) is inside the rounded rectangle
// described by (w, h, radius). r2 is radius*radius, passed in to avoid
// recomputing it in the inner loop.
func inRoundedRect(x, y, w, h, radius, r2 int) bool {
	// Interior (away from corners) — always inside.
	if x >= radius && x < w-radius {
		return true
	}
	if y >= radius && y < h-radius {
		return true
	}
	// Check the four corner arcs.
	var cx, cy int
	switch {
	case x < radius && y < radius:
		cx, cy = radius, radius
	case x >= w-radius && y < radius:
		cx, cy = w-radius-1, radius
	case x < radius && y >= h-radius:
		cx, cy = radius, h-radius-1
	default: // x >= w-radius && y >= h-radius
		cx, cy = w-radius-1, h-radius-1
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r2
}
