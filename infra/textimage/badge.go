package textimage

import (
	"fmt"
	"image"
	"image/color"
	stdraw "image/draw"
)

// ─── Badge / Tag ──────────────────────────────────────────────────────────────

// BadgeItem 表示一个徽章（彩色圆角标签）。
type BadgeItem struct {
	Text      string      // 标签文字
	BgColor   color.Color // 背景颜色（nil 则使用默认蓝色）
	TextColor color.Color // 文字颜色（nil 则使用白色）
}

// BadgeRowOption 配置徽章行的渲染选项。
type BadgeRowOption func(*badgeRowOpts)

type badgeRowOpts struct {
	fontSize    float64
	fontPath    string
	fontData    []byte
	roundR      int // 圆角半径（像素）
	paddingX    int // 徽章内部水平内边距
	paddingY    int // 徽章内部垂直内边距
	gapX        int // 相邻徽章之间的水平间距
	rowPaddingX int // 整行相对于画布左右的外边距
	rowPaddingY int // 整行上下的外边距
}

func defaultBadgeRowOpts() badgeRowOpts {
	return badgeRowOpts{
		fontSize:    13,
		roundR:      10,
		paddingX:    10,
		paddingY:    5,
		gapX:        6,
		rowPaddingX: 10,
		rowPaddingY: 6,
	}
}

// WithBadgeFontSize 设置徽章文字大小（默认 13）。
func WithBadgeFontSize(size float64) BadgeRowOption {
	return func(o *badgeRowOpts) { o.fontSize = size }
}

// WithBadgeFontPath 设置徽章字体路径（空字符串表示使用内置字体）。
func WithBadgeFontPath(path string) BadgeRowOption {
	return func(o *badgeRowOpts) { o.fontPath = path }
}

// WithBadgeFontData 设置徽章字体原始字节数据。
func WithBadgeFontData(data []byte) BadgeRowOption {
	return func(o *badgeRowOpts) { o.fontData = data }
}

// WithBadgeRadius 设置徽章圆角半径（默认 10）。
func WithBadgeRadius(r int) BadgeRowOption {
	return func(o *badgeRowOpts) { o.roundR = r }
}

// WithBadgePadding 设置徽章内部水平（x）和垂直（y）内边距（默认 10, 5）。
func WithBadgePadding(x, y int) BadgeRowOption {
	return func(o *badgeRowOpts) { o.paddingX = x; o.paddingY = y }
}

// WithBadgeGap 设置相邻徽章之间的水平间距（默认 6）。
func WithBadgeGap(gap int) BadgeRowOption {
	return func(o *badgeRowOpts) { o.gapX = gap }
}

// WithBadgeRowPadding 设置整行相对于画布的外边距（默认 10, 6）。
func WithBadgeRowPadding(x, y int) BadgeRowOption {
	return func(o *badgeRowOpts) { o.rowPaddingX = x; o.rowPaddingY = y }
}

// renderBadge 将单个 [BadgeItem] 渲染为带圆角的图片。
// 使用 [roundedClip] 对渲染结果做圆角裁剪（与 compose.go 共用辅助函数）。
func renderBadge(item BadgeItem, o badgeRowOpts) (image.Image, error) {
	textColor := item.TextColor
	if textColor == nil {
		textColor = color.White
	}
	bgColor := item.BgColor
	if bgColor == nil {
		bgColor = color.RGBA{R: 80, G: 120, B: 200, A: 255}
	}

	textOpts := []Option{
		WithFontSize(o.fontSize),
		WithFontColor(textColor),
		WithBgColor(bgColor),
		WithPadding(o.paddingX, o.paddingY),
	}
	if o.fontPath != "" {
		textOpts = append(textOpts, WithFontPath(o.fontPath))
	} else if len(o.fontData) > 0 {
		textOpts = append(textOpts, WithFontData(o.fontData))
	}

	r, err := New(textOpts...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	img, err := r.Render(item.Text)
	if err != nil {
		return nil, err
	}
	if o.roundR > 0 {
		return roundedClip(img, o.roundR), nil
	}
	return img, nil
}

// ─── badgeRowBlock ────────────────────────────────────────────────────────────

// badgeRowBlock 存储预渲染的徽章行图片，是 [canvasBlock] 的一种实现。
type badgeRowBlock struct {
	img      image.Image
	paddingY int
}

func (b *badgeRowBlock) blockHeight() int { return b.img.Bounds().Dy() + 2*b.paddingY }

func (b *badgeRowBlock) drawAt(dst *image.RGBA, yOffset, _ int) {
	srcB := b.img.Bounds()
	destRect := srcB.Add(image.Pt(0, yOffset+b.paddingY))
	stdraw.Draw(dst, destRect, b.img, srcB.Min, stdraw.Over)
}

// ─── Canvas methods ───────────────────────────────────────────────────────────

// AddBadgeRow 向画布追加一行水平排列的徽章/标签。
// 徽章超出画布宽度时会自动在可用宽度内尽量排布（不换行）。
//
// 示例 — 状态面板上的徽章行：
//
//	c.AddBadgeRow(
//	    []textimage.BadgeItem{
//	        {Text: "✅ Online",  BgColor: color.RGBA{R: 50, G: 180, B: 90, A: 255}},
//	        {Text: "⚡ v1.2.0", BgColor: color.RGBA{R: 90, G: 90, B: 200, A: 255}},
//	        {Text: "🔧 Beta",   BgColor: color.RGBA{R: 200, G: 120, B: 40, A: 255}},
//	    },
//	    textimage.WithBadgeFontSize(12),
//	    textimage.WithBadgeRadius(8),
//	)
func (c *Canvas) AddBadgeRow(items []BadgeItem, opts ...BadgeRowOption) error {
	if len(items) == 0 {
		return nil
	}
	o := defaultBadgeRowOpts()
	for _, fn := range opts {
		fn(&o)
	}
	// 字体继承：若调用方未显式指定字体，则沿用画布级字体设置（含 CJK 字体路径）。
	// 与 AddText / AddRow 保持一致的"画布默认，可覆盖"语义。
	if o.fontPath == "" && len(o.fontData) == 0 {
		o.fontPath = c.opts.FontPath
		o.fontData = c.opts.FontData
	}

	// 渲染每个徽章为独立图片
	badges := make([]image.Image, 0, len(items))
	maxH := 0
	for _, item := range items {
		img, err := renderBadge(item, o)
		if err != nil {
			return fmt.Errorf("textimage canvas AddBadgeRow: %w", err)
		}
		badges = append(badges, img)
		if h := img.Bounds().Dy(); h > maxH {
			maxH = h
		}
	}
	if maxH < 1 {
		maxH = 1
	}

	// 将徽章水平排布到与画布等宽的行图片上。
	// 行容器使用透明背景：Canvas.Result() 已提前绘制 BgColor + BgImage，
	// 不再填充纯色，避免遮盖渐变/图片背景。徽章本身携带各自的 BgColor。
	rowW := c.width
	rowImg := image.NewRGBA(image.Rect(0, 0, rowW, maxH))

	x := o.rowPaddingX
	for _, badge := range badges {
		b := badge.Bounds()
		// 若超出画布宽度则停止（不换行）
		if x+b.Dx() > rowW-o.rowPaddingX {
			break
		}
		yOff := (maxH - b.Dy()) / 2
		dest := b.Add(image.Pt(x, yOff))
		stdraw.Draw(rowImg, dest, badge, b.Min, stdraw.Over)
		x += b.Dx() + o.gapX
	}

	c.blocks = append(c.blocks, &badgeRowBlock{img: rowImg, paddingY: o.rowPaddingY})
	return nil
}

// AddBadge 向画布追加单个徽章（是 [Canvas.AddBadgeRow] 的单项便捷包装）。
func (c *Canvas) AddBadge(item BadgeItem, opts ...BadgeRowOption) error {
	return c.AddBadgeRow([]BadgeItem{item}, opts...)
}
