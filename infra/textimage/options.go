package textimage

import "image/color"

// Alignment specifies the horizontal text alignment within the image.
type Alignment int

const (
	AlignLeft   Alignment = iota // default
	AlignCenter                  // centered horizontally
	AlignRight                   // flush right
)

// Options holds all configuration for a [Renderer].
// Use the With* functional-option helpers when calling [New].
type Options struct {
	// Width of the output image in pixels.
	// 0 (default) means auto-fit to content width.
	Width int

	// Height of the output image in pixels.
	// 0 (default) means auto-fit to content height.
	Height int

	// FontSize is the font size in points. Default: 16.
	FontSize float64

	// DPI for font rendering. Default: 72.
	DPI float64

	// FontPath is a filesystem path to a TrueType/OpenType font file (.ttf / .otf).
	// Takes precedence over FontData.
	// If both FontPath and FontData are empty, the built-in Go Regular font is used.
	FontPath string

	// FontData contains the raw bytes of a TrueType/OpenType font.
	// Used when FontPath is empty.
	FontData []byte

	// FontColor is the color of rendered text. Default: black.
	FontColor color.Color

	// BgColor is the background fill color of the image. Default: white.
	BgColor color.Color

	// PaddingX is the left/right padding in pixels. Default: 10.
	PaddingX int

	// PaddingY is the top/bottom padding in pixels. Default: 10.
	PaddingY int

	// LineHeight is a multiplier applied to the natural line height. Default: 1.4.
	LineHeight float64

	// MaxWidth is the maximum line width in pixels used for word wrapping.
	// 0 (default) means Width − 2×PaddingX when Width > 0, otherwise no wrapping.
	MaxWidth int

	// Align controls horizontal text alignment. Default: AlignLeft.
	Align Alignment

	// TTCIndex is the zero-based index of the font to use inside a TrueType
	// Collection (.ttc / .otc) file. Default: 0 (Regular weight).
	TTCIndex int
}

// Option is a functional option for configuring a [Renderer].
type Option func(*Options)

// defaultOptions returns sensible defaults.
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

// WithSize sets the output image dimensions.
// Set either value to 0 to let the renderer auto-fit that dimension.
func WithSize(width, height int) Option {
	return func(o *Options) {
		o.Width = width
		o.Height = height
	}
}

// WithFontSize sets the font size in points.
func WithFontSize(size float64) Option {
	return func(o *Options) { o.FontSize = size }
}

// WithDPI sets the DPI used when rasterising the font.
func WithDPI(dpi float64) Option {
	return func(o *Options) { o.DPI = dpi }
}

// WithFontPath sets the filesystem path to a TTF/OTF font file.
func WithFontPath(path string) Option {
	return func(o *Options) { o.FontPath = path }
}

// WithFontData sets the raw bytes of a TTF/OTF font.
func WithFontData(data []byte) Option {
	return func(o *Options) { o.FontData = data }
}

// WithFontColor sets the text color.
func WithFontColor(c color.Color) Option {
	return func(o *Options) { o.FontColor = c }
}

// WithBgColor sets the background color.
func WithBgColor(c color.Color) Option {
	return func(o *Options) { o.BgColor = c }
}

// WithPadding sets the horizontal (x) and vertical (y) padding in pixels.
func WithPadding(x, y int) Option {
	return func(o *Options) {
		o.PaddingX = x
		o.PaddingY = y
	}
}

// WithLineHeight sets the line-height multiplier (e.g. 1.5 for 150% spacing).
func WithLineHeight(h float64) Option {
	return func(o *Options) { o.LineHeight = h }
}

// WithMaxWidth sets the maximum line width in pixels for word wrapping.
func WithMaxWidth(w int) Option {
	return func(o *Options) { o.MaxWidth = w }
}

// WithAlign sets the horizontal alignment.
func WithAlign(a Alignment) Option {
	return func(o *Options) { o.Align = a }
}

// WithTTCIndex selects which font to use inside a TrueType Collection file.
// The default index 0 is the Regular weight in most CJK bundles.
func WithTTCIndex(i int) Option {
	return func(o *Options) { o.TTCIndex = i }
}

// WithCJKFont is a convenience option that calls [SystemCJKFontPath] and sets
// [WithFontPath] automatically.  It returns the no-op option if no suitable
// system CJK font can be found, and the caller should set a font manually.
func WithCJKFont() Option {
	path := SystemCJKFontPath()
	if path == "" {
		return func(*Options) {} // no-op
	}
	return WithFontPath(path)
}
