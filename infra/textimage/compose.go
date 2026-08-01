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

// ─── 图片放置选项 ───────────────────────────────────────────────────────────────

// ImageOption 配置图片在 [Canvas] 上的放置方式。
type ImageOption func(*imageOpts)

type imageOpts struct {
	width      int       // 缩放到精确宽度（0 = 自动）
	height     int       // 缩放到精确高度（0 = 自动）
	maxWidth   int       // 限制最大宽度（0 = 画布宽度 − 2×paddingX）
	align      Alignment // 水平对齐方式，默认 AlignLeft
	paddingX   int       // 距画布左右边缘的水平内边距
	paddingY   int       // 图片上下的垂直内边距
	roundR     int       // 圆角半径（像素），circle=true 时忽略
	opacity    float64   // 不透明度 [0.0, 1.0]，仅当 opacitySet=true 时有效
	circle     bool      // 将图片裁剪为圆形（头像样式）
	opacitySet bool      // true 表示调用方显式设置了 opacity
}

// WithImgWidth 将图片缩放到精确的像素宽度（若未同时设置 WithImgHeight，则高度等比缩放）。
func WithImgWidth(w int) ImageOption { return func(o *imageOpts) { o.width = w } }

// WithImgHeight 将图片缩放到精确的像素高度（若未同时设置 WithImgWidth，则宽度等比缩放）。
func WithImgHeight(h int) ImageOption { return func(o *imageOpts) { o.height = h } }

// WithImgMaxWidth 在缩放前将图片限制为最多 w 像素宽。
// 覆盖默认值（画布宽度 − 2×paddingX）。
func WithImgMaxWidth(w int) ImageOption { return func(o *imageOpts) { o.maxWidth = w } }

// WithImgAlign 设置图片在画布行中的水平对齐方式。
func WithImgAlign(a Alignment) ImageOption { return func(o *imageOpts) { o.align = a } }

// WithImgPadding 设置图片周围的水平（x）和垂直（y）内边距。
func WithImgPadding(x, y int) ImageOption {
	return func(o *imageOpts) { o.paddingX = x; o.paddingY = y }
}

// WithImgCircle 将图片裁剪为圆形，适合机器人头像。
// 圆形内切于图片较短的那一边。
func WithImgCircle() ImageOption { return func(o *imageOpts) { o.circle = true } }

// WithImgRoundRadius 以给定像素半径对图片进行圆角裁剪。
// 当同时设置了 [WithImgCircle] 时，此选项被忽略。
func WithImgRoundRadius(r int) ImageOption { return func(o *imageOpts) { o.roundR = r } }

// WithImgOpacity 设置图片的整体不透明度（0.0 = 完全透明，1.0 = 完全不透明）。
// 未调用此选项时默认为完全不透明。
// 适合制作水印、叠加半透明装饰图等效果。
func WithImgOpacity(alpha float64) ImageOption {
	return func(o *imageOpts) { o.opacity = alpha; o.opacitySet = true }
}

// ─── Canvas ───────────────────────────────────────────────────────────────────

// canvasBlock 是一个知道自身渲染高度并能在指定 y 偏移处绘制自身的元素。
type canvasBlock interface {
	blockHeight() int
	drawAt(dst *image.RGBA, yOffset, canvasWidth int)
}

// Canvas 通过垂直堆叠内容块来构建合成图片。
//
// 典型用法：
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
	bgImage image.Image
	bgFit   BgFitMode
	blocks  []canvasBlock
	opts    Options // 画布级文本默认选项
}

// NewCanvas 以指定像素宽度创建新的 Canvas。
// 画布级选项（字体、字号、颜色、内边距等）作为所有 [Canvas.AddText] 调用的默认值；
// 可在每个块级别进行覆盖。
// 画布高度随着块的添加而自动增长。
func NewCanvas(width int, opts ...Option) (*Canvas, error) {
	if width < 1 {
		return nil, fmt.Errorf("textimage: canvas width must be ≥ 1, got %d", width)
	}
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	return &Canvas{width: width, bgColor: o.BgColor, bgImage: o.BgImage, bgFit: o.BgFit, opts: o}, nil
}

// AddSpacer 在末尾追加 px 像素高度的透明垂直空白。
func (c *Canvas) AddSpacer(px int) {
	if px > 0 {
		c.blocks = append(c.blocks, spacerBlock(px))
	}
}

// AddText appends a text block to the canvas.
// Text is NOT rendered immediately; rendering happens in [Canvas.Result] /
// [Canvas.ResultPNG] / [Canvas.ResultJPEG], at which point backdrop blur
// operates on the actual canvas pixels (including any BgImage).
// Block-level options override the canvas defaults for this block only.
func (c *Canvas) AddText(text string, opts ...Option) error {
	o := c.opts
	o.BgImage = nil // canvas owns the background; strip to avoid y=0 re-render
	for _, fn := range opts {
		fn(&o)
	}
	block, err := newTextBlock(text, o, c.width)
	if err != nil {
		return fmt.Errorf("textimage canvas AddText: %w", err)
	}
	c.blocks = append(c.blocks, block)
	return nil
}

// AddImage 追加一张已解码的图片，可选择调整大小、对齐或裁剪。
// 默认情况下，图片左对齐，并缩小以适应画布宽度。
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
	remaining := max(c.width-fixedTotal, 0)
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
			o.BgImage = nil // row cells are pre-rendered; canvas owns the background
			// 使用透明背景：Canvas.Result() 已在调用 drawAt 前绘制好背景（含 BgImage），
			// 文字单元格内容将以 Over 合成叠加到画布上，保留渐变/图片背景。
			// 若调用方在 TextOpts 中显式设置了 WithBgColor，仍会生效（覆盖此默认值）。
			o.BgColor = color.RGBA{}
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
	// 行容器使用透明背景：Canvas.Result() 已提前绘制 BgColor + BgImage，
	// 此处不再填充纯色，避免遮盖渐变背景图。
	rowImg := image.NewRGBA(image.Rect(0, 0, c.width, maxH))
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
	if c.bgImage != nil {
		drawBgImage(dst, c.bgImage, c.bgFit)
	}
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

// textBlock stores raw text and rendering options for deferred rendering.
// Unlike imgBlock, it renders directly onto the canvas image at drawAt time,
// so backdrop blur operations see the actual canvas background pixels
// (including any BgImage drawn by Canvas.Result).
type textBlock struct {
	text string
	opts Options
	h    int // pre-computed block height
}

// newTextBlock creates a textBlock, pre-computing its pixel height.
// BgImage is stripped from opts because the canvas handles the background;
// individual text blocks should not re-render it from y=0.
func newTextBlock(text string, opts Options, canvasWidth int) (*textBlock, error) {
	opts.Width = canvasWidth
	opts.BgImage = nil // canvas owns the background
	_, h, err := TextSize(text, func(o *Options) { *o = opts })
	if err != nil {
		return nil, err
	}
	if h < 1 {
		h = 1
	}
	return &textBlock{text: text, opts: opts, h: h}, nil
}

func (b *textBlock) blockHeight() int { return b.h }

// drawAt renders b.text directly onto dst at yOffset using the actual dst
// pixels as the backdrop source. This ensures blur operates on the real
// canvas background (BgColor + BgImage) rather than a stale per-block copy.
func (b *textBlock) drawAt(dst *image.RGBA, yOffset, canvasWidth int) {
	o := b.opts
	o.Width = canvasWidth
	r, err := New(func(t *Options) { *t = o })
	if err != nil {
		return
	}
	defer r.Close()
	r.renderOnto(dst, yOffset, b.text)
}

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

	op := 1.0 // default: fully opaque
	if b.opts.opacitySet {
		op = b.opts.opacity
	}
	if op <= 0 {
		return // fully transparent — skip
	}
	if op >= 1.0 {
		stdraw.Draw(dst, destRect, b.img, srcB.Min, stdraw.Over)
	} else {
		alpha := uint8(op*255 + 0.5)
		stdraw.DrawMask(dst, destRect, b.img, srcB.Min,
			image.NewUniform(color.Alpha{A: alpha}), image.Point{}, stdraw.Over)
	}
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
// Pixels outside the circle are transparent (alpha = 0).
//
// Fast path: when src is *image.RGBA (always the case after processImage scaling),
// pixels are copied directly via Pix slices, avoiding per-pixel interface allocations.
func circularClip(src image.Image) image.Image {
	b := src.Bounds()
	size := min(b.Dy(), b.Dx())
	r := size / 2
	r2 := r * r
	out := image.NewRGBA(image.Rect(0, 0, size, size))

	if rgba, ok := src.(*image.RGBA); ok {
		// Fast path — zero interface allocations.
		for y := range size {
			srcRow := (b.Min.Y+y)*rgba.Stride + b.Min.X*4
			dstRow := y * out.Stride
			for x := range size {
				dx, dy := x-r, y-r
				if dx*dx+dy*dy <= r2 {
					s := srcRow + x*4
					d := dstRow + x*4
					out.Pix[d] = rgba.Pix[s]
					out.Pix[d+1] = rgba.Pix[s+1]
					out.Pix[d+2] = rgba.Pix[s+2]
					out.Pix[d+3] = rgba.Pix[s+3]
				}
			}
		}
		return out
	}

	// Slow path for non-RGBA source images.
	for y := range size {
		for x := range size {
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
//
// Same fast/slow path strategy as [circularClip].
func roundedClip(src image.Image, radius int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	r2 := radius * radius

	if rgba, ok := src.(*image.RGBA); ok {
		for y := range h {
			srcRow := (b.Min.Y+y)*rgba.Stride + b.Min.X*4
			dstRow := y * out.Stride
			for x := range w {
				if inRoundedRect(x, y, w, h, radius, r2) {
					s := srcRow + x*4
					d := dstRow + x*4
					out.Pix[d] = rgba.Pix[s]
					out.Pix[d+1] = rgba.Pix[s+1]
					out.Pix[d+2] = rgba.Pix[s+2]
					out.Pix[d+3] = rgba.Pix[s+3]
				}
			}
		}
		return out
	}

	for y := range h {
		for x := range w {
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

// ─── Divider ──────────────────────────────────────────────────────────────────

// DividerOption configures a horizontal divider added by [Canvas.AddDivider].
type DividerOption func(*dividerOpts)

type dividerOpts struct {
	lineColor color.Color
	thickness int // line pixel height
	insetX    int // horizontal inset from canvas edges on each side
	paddingY  int // vertical space above and below the line
}

// WithDividerColor sets the divider line colour.
func WithDividerColor(c color.Color) DividerOption {
	return func(o *dividerOpts) { o.lineColor = c }
}

// WithDividerThickness sets the line height in pixels (default 1).
func WithDividerThickness(px int) DividerOption {
	return func(o *dividerOpts) { o.thickness = px }
}

// WithDividerInset sets how many pixels the line is inset from each canvas edge.
func WithDividerInset(px int) DividerOption {
	return func(o *dividerOpts) { o.insetX = px }
}

// WithDividerPadding sets the vertical space (pixels) above and below the line.
func WithDividerPadding(py int) DividerOption {
	return func(o *dividerOpts) { o.paddingY = py }
}

type dividerBlock struct {
	canvasWidth int
	opts        dividerOpts
}

func (b *dividerBlock) blockHeight() int { return b.opts.thickness + 2*b.opts.paddingY }
func (b *dividerBlock) drawAt(dst *image.RGBA, yOffset, canvasWidth int) {
	lineY := yOffset + b.opts.paddingY
	x0 := b.opts.insetX
	x1 := canvasWidth - b.opts.insetX
	if x1 <= x0 {
		return
	}
	dstB := dst.Bounds()
	for y := lineY; y < lineY+b.opts.thickness; y++ {
		if y < dstB.Min.Y || y >= dstB.Max.Y {
			continue
		}
		for x := x0; x < x1; x++ {
			if x >= dstB.Min.X && x < dstB.Max.X {
				dst.Set(x, y, b.opts.lineColor)
			}
		}
	}
}

// AddDivider appends a horizontal divider line to the canvas.
//
// Example:
//
//	c.AddDivider(
//	    textimage.WithDividerColor(color.RGBA{R:80, G:80, B:80, A:180}),
//	    textimage.WithDividerThickness(1),
//	    textimage.WithDividerInset(24),
//	    textimage.WithDividerPadding(8),
//	)
func (c *Canvas) AddDivider(opts ...DividerOption) {
	do := dividerOpts{
		lineColor: color.RGBA{R: 180, G: 180, B: 180, A: 200},
		thickness: 1,
		paddingY:  8,
	}
	for _, fn := range opts {
		fn(&do)
	}
	c.blocks = append(c.blocks, &dividerBlock{canvasWidth: c.width, opts: do})
}

// ─── ProgressBar ──────────────────────────────────────────────────────────────

// ProgressBarOption configures a progress bar added by [Canvas.AddProgressBar].
type ProgressBarOption func(*progressBarOpts)

type progressBarOpts struct {
	fillColor  color.Color
	trackColor color.Color
	height     int
	radius     int // rounded corner radius
	paddingX   int // horizontal inset from canvas edges
	paddingY   int // vertical space above and below the bar
}

// WithProgressFillColor sets the colour of the filled (progress) portion.
func WithProgressFillColor(c color.Color) ProgressBarOption {
	return func(o *progressBarOpts) { o.fillColor = c }
}

// WithProgressTrackColor sets the colour of the unfilled track background.
func WithProgressTrackColor(c color.Color) ProgressBarOption {
	return func(o *progressBarOpts) { o.trackColor = c }
}

// WithProgressHeight sets the bar height in pixels (default 12).
func WithProgressHeight(px int) ProgressBarOption {
	return func(o *progressBarOpts) { o.height = px }
}

// WithProgressRadius sets the rounded-corner radius of the bar (default 6).
func WithProgressRadius(r int) ProgressBarOption {
	return func(o *progressBarOpts) { o.radius = r }
}

// WithProgressPadding sets horizontal inset (x) and vertical spacing (y).
func WithProgressPadding(x, y int) ProgressBarOption {
	return func(o *progressBarOpts) { o.paddingX = x; o.paddingY = y }
}

type progressBlock struct {
	value float64 // clamped to [0, 1]
	opts  progressBarOpts
}

func (b *progressBlock) blockHeight() int { return b.opts.height + 2*b.opts.paddingY }

func (b *progressBlock) drawAt(dst *image.RGBA, yOffset, canvasWidth int) {
	px, py := b.opts.paddingX, b.opts.paddingY
	barW := canvasWidth - 2*px
	barH := b.opts.height
	if barW < 1 || barH < 1 {
		return
	}

	barRect := image.Rect(px, yOffset+py, px+barW, yOffset+py+barH).Intersect(dst.Bounds())
	if barRect.Empty() {
		return
	}

	// Draw track (full bar background).
	drawFilledRoundedRect(dst, barRect, b.opts.radius, b.opts.trackColor)

	// Draw fill — iterate only the fill columns but test against the full bar
	// shape so corners are correctly rounded on both the fill and the track.
	v := b.value
	if v <= 0 {
		return
	}
	if v > 1 {
		v = 1
	}
	fillW := int(float64(barRect.Dx()) * v)
	if fillW <= 0 {
		return
	}

	r, r2 := b.opts.radius, b.opts.radius*b.opts.radius
	fullW, fullH := barRect.Dx(), barRect.Dy()
	for y := range fullH {
		for x := range fillW {
			if r == 0 || inRoundedRect(x, y, fullW, fullH, r, r2) {
				dst.Set(barRect.Min.X+x, barRect.Min.Y+y, b.opts.fillColor)
			}
		}
	}
}

// AddProgressBar appends a horizontal progress bar to the canvas.
// value is the current level; max is the maximum (value/max is clamped to [0,1]).
//
// Example — CPU usage bar:
//
//	c.AddProgressBar(cpuPercent, 100,
//	    textimage.WithProgressFillColor(color.RGBA{R:80, G:200, B:120, A:255}),
//	    textimage.WithProgressTrackColor(color.RGBA{R:40, G:40, B:50, A:255}),
//	    textimage.WithProgressHeight(14),
//	    textimage.WithProgressRadius(7),
//	    textimage.WithProgressPadding(24, 4),
//	)
func (c *Canvas) AddProgressBar(value, max float64, opts ...ProgressBarOption) {
	po := progressBarOpts{
		fillColor:  color.RGBA{R: 80, G: 160, B: 240, A: 255},
		trackColor: color.RGBA{R: 50, G: 50, B: 60, A: 255},
		height:     12,
		radius:     6,
		paddingY:   4,
	}
	for _, fn := range opts {
		fn(&po)
	}
	v := 0.0
	if max > 0 {
		v = value / max
	}
	c.blocks = append(c.blocks, &progressBlock{value: v, opts: po})
}

// ─── Drawing helpers ──────────────────────────────────────────────────────────

// drawFilledRoundedRect paints a filled rounded rectangle into dst.
// All coordinates are in dst's absolute pixel space.
func drawFilledRoundedRect(dst *image.RGBA, rect image.Rectangle, radius int, c color.Color) {
	rect = rect.Intersect(dst.Bounds())
	if rect.Empty() {
		return
	}
	w, h := rect.Dx(), rect.Dy()
	if radius <= 0 {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				dst.Set(x, y, c)
			}
		}
		return
	}
	r2 := radius * radius
	for row := range h {
		for col := range w {
			if inRoundedRect(col, row, w, h, radius, r2) {
				dst.Set(rect.Min.X+col, rect.Min.Y+row, c)
			}
		}
	}
}
