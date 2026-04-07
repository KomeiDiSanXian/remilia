// Package canvas provides a Bot-friendly 2D vector drawing canvas built on top
// of [github.com/fogleman/gg].
//
// It exposes all gg.Context methods via embedding while adding higher-level
// helpers tailored to common Bot notification-card patterns:
//
//   - [Canvas.DrawAvatar]       – circle-masked profile picture
//   - [Canvas.DrawProgressBar]  – rounded progress / level bar
//   - [Canvas.DrawLineChart]    – normalised multi-point line chart
//   - [Canvas.DrawRadarChart]   – radar / spider chart
//
// # Output
//
//   - [Canvas.ToPNG]    – encode to PNG bytes
//   - [Canvas.ToJPEG]   – encode to JPEG bytes
//   - [Canvas.SavePNG]  – write PNG to file (delegates to gg.Context.SavePNG)
//   - [Canvas.SaveJPEG] – write JPEG to file
//
// # Quick start
//
//	c := canvas.New(600, 400)
//
//	// White background
//	c.SetRGB(1, 1, 1)
//	c.Clear()
//
//	// Draw a user avatar
//	c.DrawAvatar(avatarImg, 60, 60, 48)
//
//	// Progress bar (60 % health)
//	blue  := color.RGBA{R: 80, G: 160, B: 240, A: 255}
//	track := color.RGBA{R: 50, G: 50, B: 60, A: 255}
//	c.DrawProgressBar(120, 48, 400, 24, 0.60, blue, track)
//
//	png, err := c.ToPNG()
package canvas
