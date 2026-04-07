// Package gif provides a frame-by-frame GIF animation encoder.
//
// It accepts any [image.Image] type and handles color-quantisation internally,
// converting each frame to the 256-colour indexed format required by the GIF
// specification.  Floyd-Steinberg dithering is applied by default to reduce
// colour banding on gradients; it can be disabled with [WithDithering].
//
// # Quick start
//
//	enc := gif.New(gif.WithLoopCount(0)) // loop forever
//
//	for _, img := range frames {
//	    if err := enc.AddFrame(img, 50); err != nil { // 50 ms per frame
//	        return err
//	    }
//	}
//
//	data, err := enc.Bytes()
//
// # Palette
//
// By default a 216-color web-safe palette is used (6×6×6 RGB cube + 40 grey
// shades, 256 entries total).  Supply a custom palette with [WithPalette]:
//
//	import "github.com/golang/freetype/raster" // just for illustration
//	enc := gif.New(gif.WithPalette(myPalette))
//
// # Disposal method
//
// Each frame uses [image/gif.DisposalBackground] (clear to background between
// frames).  Override on a per-frame basis by calling [Encoder.AddPaletted]
// directly.
package gif
