package textimage

import (
	"image"
	"image/color"
)

// Alignment 指定图片内文本的水平对齐方式。
type Alignment int

const (
	AlignLeft   Alignment = iota // 默认：左对齐
	AlignCenter                  // 水平居中
	AlignRight                   // 右对齐
)

// BgFitMode 控制背景图片的缩放/平铺策略。
type BgFitMode int

const (
	// BgFitStretch 将背景图片拉伸至画布大小（不保持比例）。
	BgFitStretch BgFitMode = iota
	// BgFitFill 等比缩放至能覆盖整个画布（超出部分居中裁剪）。
	BgFitFill
	// BgFitFit 等比缩放至完整显示于画布内（不足处以 BgColor 填充）。
	BgFitFit
	// BgFitCenter 不缩放，将原始图居中放置（超出部分裁剪，不足处留 BgColor）。
	BgFitCenter
	// BgFitTile 以原始尺寸平铺图片。
	BgFitTile
)

// BackdropShape 控制文字背后遮罩（毛玻璃底色）的形状。
type BackdropShape int

const (
	// BackdropShapeRect 矩形（默认）。
	BackdropShapeRect BackdropShape = iota
	// BackdropShapeRounded 圆角矩形，圆角半径由 TextBackdropRoundRadius 控制。
	BackdropShapeRounded
	// BackdropShapeEllipse 以矩形区域内切椭圆裁剪（宽高相等时即为圆形）。
	BackdropShapeEllipse
)

// BackdropMode 控制遮罩的覆盖范围：逐行还是整块。
type BackdropMode int

const (
	// BackdropModePerLine 每行文字各自绘制一块遮罩（默认）。
	BackdropModePerLine BackdropMode = iota
	// BackdropModeBlock 将全部文字行的包围盒合并为一块遮罩，整体绘制。
	// 适合"文字面板"场景：背景图上放一块毛玻璃卡片，卡片内再填充文字。
	BackdropModeBlock
)

// Options 保存 [Renderer] 的全部配置。
// 调用 [New] 时请使用 With* 函数式选项进行设置。
type Options struct {
	// Width 是输出图片的像素宽度。
	// 0（默认）表示自动适应内容宽度。
	Width int

	// Height 是输出图片的像素高度。
	// 0（默认）表示自动适应内容高度。
	Height int

	// FontSize 是字体大小，单位为磅（pt）。默认：16。
	FontSize float64

	// DPI 是字体光栅化时使用的 DPI。默认：72。
	DPI float64

	// FontPath 是 TrueType/OpenType 字体文件（.ttf / .otf）的文件系统路径。
	// 优先级高于 FontData。
	// 若 FontPath 和 FontData 均为空，则使用内置的 Go Regular 字体。
	FontPath string

	// FontData 包含 TrueType/OpenType 字体的原始字节。
	// 当 FontPath 为空时使用。
	FontData []byte

	// FontColor 是渲染文本的颜色。默认：黑色。
	FontColor color.Color

	// BgColor 是图片的背景填充颜色。默认：白色。
	BgColor color.Color

	// PaddingX 是左右内边距，单位像素。默认：10。
	PaddingX int

	// PaddingY 是上下内边距，单位像素。默认：10。
	PaddingY int

	// LineHeight 是应用于自然行高的倍数（例如 1.5 表示 150% 行距）。默认：1.4。
	LineHeight float64

	// MaxWidth 是自动换行时的最大行宽，单位像素。
	// 0（默认）表示当 Width > 0 时取 Width − 2×PaddingX，否则不换行。
	MaxWidth int

	// Align 控制水平文本对齐方式。默认：AlignLeft。
	Align Alignment

	// TTCIndex 是 TrueType Collection（.ttc / .otc）文件中要使用的字体的
	// 零起始索引。默认：0（大多数 CJK 字体包中的 Regular 字重）。
	TTCIndex int

	// BgImage 是作为画布背景使用的图片。
	// 非 nil 时，BgColor 仅用于填充 BgImage 未覆盖的区域（取决于 BgFit）。
	BgImage image.Image

	// BgFit 控制 BgImage 的缩放/对齐策略。默认：BgFitStretch。
	BgFit BgFitMode

	// TextBackdropColor 是在每行文字正后方绘制的半透明遮罩颜色。
	// nil 或完全透明时不绘制遮罩。
	// 典型用法：color.NRGBA{R:0, G:0, B:0, A:128} 表示半透明黑色。
	TextBackdropColor color.Color

	// TextBackdropBlur 是对文字遮罩区域下方背景像素应用的模糊半径（像素）。
	// > 0 时先模糊背景、再叠加 TextBackdropColor 遮罩，形成毛玻璃效果。
	// 0（默认）表示不模糊。
	TextBackdropBlur int

	// TextBackdropPadX / TextBackdropPadY 是遮罩在每行文字周围额外扩展的像素数。
	TextBackdropPadX int
	TextBackdropPadY int

	// TextBackdropShape 控制遮罩的裁剪形状。默认：BackdropShapeRect（矩形）。
	TextBackdropShape BackdropShape

	// TextBackdropRoundRadius 是圆角矩形遮罩的圆角半径（像素）。
	// 仅当 TextBackdropShape == BackdropShapeRounded 时有效。
	TextBackdropRoundRadius int

	// TextBackdropMode 控制遮罩覆盖范围。默认：BackdropModePerLine（逐行）。
	// 设为 BackdropModeBlock 时，所有文字行共用一块整体遮罩（毛玻璃卡片效果）。
	TextBackdropMode BackdropMode

	// TextShadowColor 是文字阴影的颜色。nil 表示不绘制阴影。
	TextShadowColor color.Color

	// TextShadowOffsetX / TextShadowOffsetY 是阴影相对于文字的像素偏移。
	// 正值分别对应向右 / 向下偏移。
	TextShadowOffsetX int
	TextShadowOffsetY int

	// TextShadowBlur 是阴影的模糊半径（像素）。0 = 硬边阴影，>0 = 软化阴影。
	TextShadowBlur int
}

// Option 是用于配置 [Renderer] 的函数式选项。
type Option func(*Options)

// defaultOptions 返回合理的默认值。
func defaultOptions() Options {
	return Options{
		FontSize:   16,
		DPI:        72,
		FontColor:  color.Black,
		BgColor:    color.White,
		PaddingX:   10,
		PaddingY:   10,
		LineHeight: 1.4,
		Align:      AlignLeft,
	}
}

// WithSize 设置输出图片的尺寸。
// 将任一值设为 0 可让渲染器自动适应该维度。
func WithSize(width, height int) Option {
	return func(o *Options) {
		o.Width = width
		o.Height = height
	}
}

// WithFontSize 设置字体大小，单位为磅。
func WithFontSize(size float64) Option {
	return func(o *Options) { o.FontSize = size }
}

// WithDPI 设置字体光栅化时使用的 DPI。
func WithDPI(dpi float64) Option {
	return func(o *Options) { o.DPI = dpi }
}

// WithFontPath 设置 TTF/OTF 字体文件的文件系统路径。
func WithFontPath(path string) Option {
	return func(o *Options) { o.FontPath = path }
}

// WithFontData 设置 TTF/OTF 字体的原始字节数据。
func WithFontData(data []byte) Option {
	return func(o *Options) { o.FontData = data }
}

// WithFontColor 设置文本颜色。
func WithFontColor(c color.Color) Option {
	return func(o *Options) { o.FontColor = c }
}

// WithBgColor 设置背景颜色。
func WithBgColor(c color.Color) Option {
	return func(o *Options) { o.BgColor = c }
}

// WithPadding 设置水平（x）和垂直（y）内边距，单位像素。
func WithPadding(x, y int) Option {
	return func(o *Options) {
		o.PaddingX = x
		o.PaddingY = y
	}
}

// WithLineHeight 设置行高倍数（例如 1.5 表示 150% 行距）。
func WithLineHeight(h float64) Option {
	return func(o *Options) { o.LineHeight = h }
}

// WithMaxWidth 设置自动换行的最大行宽，单位像素。
func WithMaxWidth(w int) Option {
	return func(o *Options) { o.MaxWidth = w }
}

// WithAlign 设置水平对齐方式。
func WithAlign(a Alignment) Option {
	return func(o *Options) { o.Align = a }
}

// WithTTCIndex 选择 TrueType Collection 文件中要使用的字体。
// 默认索引 0 在大多数 CJK 字体包中对应 Regular 字重。
func WithTTCIndex(i int) Option {
	return func(o *Options) { o.TTCIndex = i }
}

// WithCJKFont 是一个便捷选项，会自动调用 [SystemCJKFontPath] 并设置 [WithFontPath]。
// 若未找到合适的系统 CJK 字体，则返回无操作选项，调用方应手动指定字体。
func WithCJKFont() Option {
	path := SystemCJKFontPath()
	if path == "" {
		return func(*Options) {} // 无操作
	}
	return WithFontPath(path)
}

// WithBgImage 将 img 设为画布背景，并以 fit 策略缩放/平铺。
// 传入 nil 可清除背景图片（退回到纯色背景）。
func WithBgImage(img image.Image, fit BgFitMode) Option {
	return func(o *Options) {
		o.BgImage = img
		o.BgFit = fit
	}
}

// WithTextBackdrop 在每行文字背后绘制半透明遮罩，颜色为 c。
// 典型值：color.NRGBA{A: 160} 表示半透明黑色。
// blurRadius > 0 时额外对遮罩下方的背景进行模糊（毛玻璃效果）。
func WithTextBackdrop(c color.Color, blurRadius int) Option {
	return func(o *Options) {
		o.TextBackdropColor = c
		o.TextBackdropBlur = blurRadius
	}
}

// WithTextBackdropPadding 设置文字遮罩在每行文字四周额外扩展的像素数（x 水平，y 垂直）。
func WithTextBackdropPadding(x, y int) Option {
	return func(o *Options) {
		o.TextBackdropPadX = x
		o.TextBackdropPadY = y
	}
}

// WithTextBackdropShape 设置遮罩的裁剪形状。
// 对于 BackdropShapeRounded，roundRadius 指定圆角半径（像素）；其他形状忽略此参数。
func WithTextBackdropShape(s BackdropShape, roundRadius int) Option {
	return func(o *Options) {
		o.TextBackdropShape = s
		o.TextBackdropRoundRadius = roundRadius
	}
}

// WithTextBackdropMode 设置遮罩覆盖范围。
//   - BackdropModePerLine（默认）：每行各自绘制一块遮罩。
//   - BackdropModeBlock：将所有文字行的包围盒合并为一块整体遮罩，
//     形成"毛玻璃卡片"效果，适合背景图上多个独立文字面板场景。
func WithTextBackdropMode(m BackdropMode) Option {
	return func(o *Options) { o.TextBackdropMode = m }
}

// WithTextShadow 为文字添加投影效果。
//   - c          — 阴影颜色（典型值：color.RGBA{A:180} 半透明黑）
//   - offsetX    — 水平偏移像素（正值向右）
//   - offsetY    — 垂直偏移像素（正值向下）
//   - blurRadius — 模糊半径（0 = 硬边阴影；建议值 2–6 获得柔和效果）
//
// 阴影始终绘制在文字正下方，不受遮罩（Backdrop）影响。
func WithTextShadow(c color.Color, offsetX, offsetY, blurRadius int) Option {
	return func(o *Options) {
		o.TextShadowColor = c
		o.TextShadowOffsetX = offsetX
		o.TextShadowOffsetY = offsetY
		o.TextShadowBlur = blurRadius
	}
}
