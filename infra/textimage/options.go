package textimage

import "image/color"

// Alignment 指定图片内文本的水平对齐方式。
type Alignment int

const (
	AlignLeft   Alignment = iota // 默认：左对齐
	AlignCenter                  // 水平居中
	AlignRight                   // 右对齐
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
