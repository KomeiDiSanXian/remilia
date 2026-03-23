// Package textimage converts text strings — and mixed text+image layouts — to
// raster images suitable for sending through chat-bot platforms.
//
// # Core renderer (text only)
//
// [Renderer] converts a plain string to an [image.Image].
//
//   - Custom or built-in fonts (Go Regular is used by default)
//   - Configurable font size, DPI, text color, background color
//   - Automatic word-wrap and multi-line rendering ("\n" honoured)
//   - Left / center / right horizontal alignment
//   - PNG and JPEG output (bytes, file, or [io.Writer])
//
// Basic usage:
//
//	r, _ := textimage.New(textimage.WithFontSize(24), textimage.WithCJKFont())
//	defer r.Close()
//	png, _ := r.RenderToPNG("Hello, 世界！")
//
// # Canvas (mixed text + image compositor)
//
// [Canvas] stacks text blocks, images, spacers, and side-by-side rows
// vertically into a single composite image.  It is ideal for rich bot
// responses such as health reports, leaderboards, or status cards that
// combine an avatar with text.
//
//   - [Canvas.AddText]       — word-wrapped text block (full canvas width)
//   - [Canvas.AddImage]      — pre-decoded image with scaling, alignment, and
//     circular / rounded-corner clipping
//   - [Canvas.AddImageBytes] — decode-and-add in one step (PNG / JPEG / GIF …)
//   - [Canvas.AddSpacer]     — vertical whitespace
//   - [Canvas.AddRow]        — multiple cells laid out side-by-side (e.g. avatar + text)
//
// Example — bot health card with a circular avatar:
//
//	c, _ := textimage.NewCanvas(640,
//	    textimage.WithCJKFont(),
//	    textimage.WithFontSize(16),
//	    textimage.WithBgColor(color.RGBA{R: 30, G: 30, B: 40, A: 255}),
//	    textimage.WithFontColor(color.White),
//	)
//	_ = c.AddRow(
//	    textimage.RowItem{
//	        Width: 72, Image: avatarImg,
//	        ImageOpts: []textimage.ImageOption{textimage.WithImgCircle()},
//	    },
//	    textimage.RowItem{Text: "MyBot  v1.2.3\n🟢 Online"},
//	)
//	_ = c.AddSpacer(8)
//	_ = c.AddText(systemReport)
//	pngBytes, _ := c.ResultPNG()
//
// # System font helpers
//
// [SystemCJKFontPath] returns the path to the best CJK font found on the
// current OS.  Platform-specific search logic lives in the sibling
// sysfont_windows.go / sysfont_darwin.go / sysfont_unix.go files, selected
// at compile time via Go build tags.
//
// [WithCJKFont] is a convenience [Option] that calls [SystemCJKFontPath]
// automatically.
package textimage
