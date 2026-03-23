package textimage_test

// Benchmark suite for infra/textimage.
//
// Run all benchmarks:
//
//	go test -count=1 -run "^$" -bench "." -benchmem ./infra/textimage/
//
// Run a specific group (e.g. Canvas):
//
//	go test -count=1 -run "^$" -bench "BenchmarkCanvas" -benchmem ./infra/textimage/
//
// Produce CPU/mem profiles for the health-report scenario:
//
//	go test -count=1 -run "^$" -bench "BenchmarkCanvas_HealthReport$" \
//	    -cpuprofile cpu.prof -memprofile mem.prof ./infra/textimage/
//	go tool pprof cpu.prof

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// ─── Fixture data ─────────────────────────────────────────────────────────────

const (
	shortASCII = "Hello, World!"

	mediumASCII = "The quick brown fox jumps over the lazy dog. " +
		"Pack my box with five dozen liquor jugs."

	longASCII = "Lorem ipsum dolor sit amet, consectetur adipiscing elit. " +
		"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
		"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris " +
		"nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in " +
		"reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla " +
		"pariatur. Excepteur sint occaecat cupidatat non proident, sunt in " +
		"culpa qui officia deserunt mollit anim id est laborum."

	multilineASCII = "Line 1: system boot sequence initiated\n" +
		"Line 2: loading configuration\n" +
		"Line 3: connecting to gateway\n" +
		"Line 4: shard 0/1 ready\n" +
		"Line 5: all systems nominal"

	healthReport = "Bot Health Report  2026-03-23\n" +
		"--------------------------------\n" +
		"Uptime        72 h 14 m\n" +
		"Memory        48 MB / 256 MB\n" +
		"CPU           3.2 %\n" +
		"Messages      1,024,388\n" +
		"Active users  2,731\n" +
		"Latency       12 ms\n" +
		"--------------------------------\n" +
		"Status: OK"
)

// solidImg returns a w x h RGBA image filled with a flat colour.
func solidImg(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r8, g8, b8, a8 := c.RGBA()
	flat := color.RGBA{
		R: uint8(r8 >> 8),
		G: uint8(g8 >> 8),
		B: uint8(b8 >> 8),
		A: uint8(a8 >> 8),
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, flat)
		}
	}
	return img
}

// encodedPNG returns PNG-encoded bytes for a solid-colour image.
func encodedPNG(w, h int, c color.Color) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, solidImg(w, h, c))
	return buf.Bytes()
}

// ─── 1. Renderer construction ─────────────────────────────────────────────────

// BenchmarkNew measures the one-time setup cost per bot session.
func BenchmarkNew_BuiltinFont(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r, err := textimage.New()
		if err != nil {
			b.Fatal(err)
		}
		_ = r.Close()
	}
}

func BenchmarkNew_CJKFont(b *testing.B) {
	path := textimage.SystemCJKFontPath()
	if path == "" {
		b.Skip("no system CJK font found")
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r, err := textimage.New(textimage.WithFontPath(path))
		if err != nil {
			b.Fatal(err)
		}
		_ = r.Close()
	}
}

// ─── 2. Renderer.Render — text layout + rasterisation ────────────────────────

func BenchmarkRender_ShortASCII(b *testing.B) {
	r := textimage.MustNew()
	defer func() { _ = r.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img, err := r.Render(shortASCII)
		if err != nil {
			b.Fatal(err)
		}
		_ = img
	}
}

func BenchmarkRender_MediumASCII(b *testing.B) {
	r := textimage.MustNew(textimage.WithSize(640, 0))
	defer func() { _ = r.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img, _ := r.Render(mediumASCII)
		_ = img
	}
}

func BenchmarkRender_LongASCII_Wrapped(b *testing.B) {
	r := textimage.MustNew(textimage.WithSize(480, 0))
	defer func() { _ = r.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img, _ := r.Render(longASCII)
		_ = img
	}
}

func BenchmarkRender_Multiline5(b *testing.B) {
	r := textimage.MustNew()
	defer func() { _ = r.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img, _ := r.Render(multilineASCII)
		_ = img
	}
}

func BenchmarkRender_Multiline20(b *testing.B) {
	text := strings.Repeat("Log line: bot processed 128 events in 50 ms\n", 20)
	r := textimage.MustNew(textimage.WithSize(640, 0))
	defer func() { _ = r.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img, _ := r.Render(text)
		_ = img
	}
}

// BenchmarkRender_FontSizes shows how rasterisation cost scales with point size.
func BenchmarkRender_FontSizes(b *testing.B) {
	for _, size := range []float64{12, 16, 24, 36, 48} {
		size := size
		b.Run(fmt.Sprintf("pt%.0f", size), func(b *testing.B) {
			r := textimage.MustNew(textimage.WithFontSize(size))
			defer func() { _ = r.Close() }()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				img, _ := r.Render(mediumASCII)
				_ = img
			}
		})
	}
}

// BenchmarkRender_CanvasWidths shows memory allocation growth with canvas width.
func BenchmarkRender_CanvasWidths(b *testing.B) {
	for _, w := range []int{320, 480, 640, 960, 1280} {
		w := w
		b.Run(fmt.Sprintf("w%d", w), func(b *testing.B) {
			r := textimage.MustNew(textimage.WithSize(w, 0))
			defer func() { _ = r.Close() }()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				img, _ := r.Render(longASCII)
				_ = img
			}
		})
	}
}

// BenchmarkRender_CJK measures rendering of Chinese text.
func BenchmarkRender_CJK(b *testing.B) {
	path := textimage.SystemCJKFontPath()
	if path == "" {
		b.Skip("no system CJK font found")
	}
	r := textimage.MustNew(
		textimage.WithFontPath(path),
		textimage.WithFontSize(18),
		textimage.WithSize(520, 0),
	)
	defer func() { _ = r.Close() }()
	cjkText := "系统状态报告\n运行时间：72 小时 14 分\n内存占用：48 MB / 256 MB\nCPU：3.2%\n延迟：12 ms\n状态：一切正常"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img, _ := r.Render(cjkText)
		_ = img
	}
}

// ─── 3. Encoding (PNG / JPEG) ─────────────────────────────────────────────────

func BenchmarkEncode_PNG(b *testing.B) {
	r := textimage.MustNew(textimage.WithSize(640, 0))
	defer func() { _ = r.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := r.RenderToPNG(longASCII)
		b.SetBytes(int64(len(data)))
		_ = data
	}
}

func BenchmarkEncode_JPEG(b *testing.B) {
	r := textimage.MustNew(textimage.WithSize(640, 0))
	defer func() { _ = r.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := r.RenderToJPEG(longASCII, 85)
		b.SetBytes(int64(len(data)))
		_ = data
	}
}

// BenchmarkEncode_PNGvsJPEG compares both encoders at the same canvas size.
func BenchmarkEncode_PNGvsJPEG(b *testing.B) {
	r := textimage.MustNew(textimage.WithSize(640, 0))
	defer func() { _ = r.Close() }()

	img, _ := r.Render(longASCII)
	bounds := img.Bounds()
	b.Logf("canvas size: %dx%d px", bounds.Dx(), bounds.Dy())

	b.Run("PNG", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			data, _ := r.RenderToPNG(longASCII)
			_ = data
		}
	})
	b.Run("JPEG_q85", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			data, _ := r.RenderToJPEG(longASCII, 85)
			_ = data
		}
	})
	b.Run("JPEG_q60", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			data, _ := r.RenderToJPEG(longASCII, 60)
			_ = data
		}
	})
}

// ─── 4. Image processing ──────────────────────────────────────────────────────

func BenchmarkImgProcess_NoOp(b *testing.B) {
	src := solidImg(200, 200, color.RGBA{R: 100, G: 150, B: 200, A: 255})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := textimage.NewCanvas(640)
		// Image fits canvas — no scaling, no clip.
		_ = c.AddImage(src)
	}
}

// BenchmarkImgProcess_Scale measures CatmullRom downscaling at various source sizes.
func BenchmarkImgProcess_Scale(b *testing.B) {
	for _, sz := range []int{128, 256, 512, 1024} {
		sz := sz
		src := solidImg(sz, sz, color.Gray{Y: 128})
		b.Run(fmt.Sprintf("src%d_to64", sz), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, _ := textimage.NewCanvas(640)
				_ = c.AddImage(src, textimage.WithImgWidth(64))
			}
		})
	}
}

// BenchmarkImgProcess_CircleClip measures the optimised (zero-alloc) circle clip.
func BenchmarkImgProcess_CircleClip(b *testing.B) {
	for _, sz := range []int{64, 128, 256} {
		sz := sz
		src := solidImg(sz, sz, color.RGBA{R: 80, G: 120, B: 200, A: 255})
		b.Run(fmt.Sprintf("sz%d", sz), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, _ := textimage.NewCanvas(640)
				_ = c.AddImage(src, textimage.WithImgCircle())
			}
		})
	}
}

// BenchmarkImgProcess_RoundedCorners measures the optimised rounded-corner clip.
func BenchmarkImgProcess_RoundedCorners(b *testing.B) {
	for _, sz := range []int{64, 128, 256} {
		sz := sz
		src := solidImg(sz, sz, color.RGBA{G: 180, A: 255})
		b.Run(fmt.Sprintf("sz%d_r12", sz), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, _ := textimage.NewCanvas(640)
				_ = c.AddImage(src, textimage.WithImgRoundRadius(12))
			}
		})
	}
}

// BenchmarkImgProcess_ScaleAndCircle is the typical avatar pipeline:
// download 256x256 -> scale to 80x80 -> circle clip.
func BenchmarkImgProcess_ScaleAndCircle(b *testing.B) {
	src := solidImg(256, 256, color.RGBA{R: 80, G: 120, B: 200, A: 255})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := textimage.NewCanvas(640)
		_ = c.AddImage(src, textimage.WithImgWidth(80), textimage.WithImgCircle())
	}
}

// ─── 5. Canvas compositing ────────────────────────────────────────────────────

func BenchmarkCanvas_TextOnly(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := textimage.NewCanvas(640)
		_ = c.AddText(healthReport)
		_ = c.Result()
	}
}

func BenchmarkCanvas_TextMultiBlock(b *testing.B) {
	title := "System Report  2026-03-23"
	body1 := "Uptime: 72 h 14 m   Memory: 48/256 MB"
	body2 := "CPU: 3.2%   Latency: 12 ms"
	footer := "Status: all systems nominal"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := textimage.NewCanvas(640)
		_ = c.AddText(title, textimage.WithFontSize(20))
		c.AddSpacer(8)
		_ = c.AddText(body1)
		_ = c.AddText(body2)
		c.AddSpacer(8)
		_ = c.AddText(footer)
		_ = c.Result()
	}
}

func BenchmarkCanvas_ImageOnly(b *testing.B) {
	src := solidImg(600, 400, color.RGBA{R: 60, G: 90, B: 130, A: 255})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := textimage.NewCanvas(640)
		_ = c.AddImage(src)
		_ = c.Result()
	}
}

func BenchmarkCanvas_AvatarRow(b *testing.B) {
	// One 96x96 circular avatar on the left, status text on the right.
	avatar := solidImg(96, 96, color.RGBA{R: 80, G: 120, B: 200, A: 255})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := textimage.NewCanvas(640)
		_ = c.AddRow(
			textimage.RowItem{
				Width:     100,
				Image:     avatar,
				ImageOpts: []textimage.ImageOption{textimage.WithImgCircle()},
			},
			textimage.RowItem{Text: "Remilia Bot\nv2.0.0 Online"},
		)
		_ = c.Result()
	}
}

// BenchmarkCanvas_HealthReport is the primary real-world scenario:
// circular avatar header + multi-line health text -> PNG bytes.
func BenchmarkCanvas_HealthReport(b *testing.B) {
	avatar := solidImg(96, 96, color.RGBA{R: 80, G: 120, B: 200, A: 255})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := textimage.NewCanvas(520)
		c.AddSpacer(12)
		_ = c.AddRow(
			textimage.RowItem{
				Width:     100,
				Image:     avatar,
				ImageOpts: []textimage.ImageOption{textimage.WithImgCircle()},
			},
			textimage.RowItem{
				Text:     "Remilia Bot  v2.0.0\nShard 0/1  Online",
				TextOpts: []textimage.Option{textimage.WithFontSize(17)},
			},
		)
		c.AddSpacer(8)
		_ = c.AddText(healthReport)
		c.AddSpacer(12)
		_, _ = c.ResultPNG()
	}
}

// BenchmarkCanvas_HealthReport_JPEG is the same card but JPEG-encoded.
func BenchmarkCanvas_HealthReport_JPEG(b *testing.B) {
	avatar := solidImg(96, 96, color.RGBA{R: 80, G: 120, B: 200, A: 255})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := textimage.NewCanvas(520)
		c.AddSpacer(12)
		_ = c.AddRow(
			textimage.RowItem{
				Width:     100,
				Image:     avatar,
				ImageOpts: []textimage.ImageOption{textimage.WithImgCircle()},
			},
			textimage.RowItem{Text: "Remilia Bot  v2.0.0\nShard 0/1  Online"},
		)
		c.AddSpacer(8)
		_ = c.AddText(healthReport)
		c.AddSpacer(12)
		_, _ = c.ResultJPEG(85)
	}
}

// BenchmarkCanvas_Widths shows how canvas width affects the full pipeline.
func BenchmarkCanvas_Widths(b *testing.B) {
	avatar := solidImg(80, 80, color.RGBA{R: 80, G: 120, B: 200, A: 255})
	for _, w := range []int{320, 480, 640, 960} {
		w := w
		b.Run(fmt.Sprintf("w%d", w), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, _ := textimage.NewCanvas(w)
				_ = c.AddRow(
					textimage.RowItem{
						Width:     80,
						Image:     avatar,
						ImageOpts: []textimage.ImageOption{textimage.WithImgCircle()},
					},
					textimage.RowItem{Text: "Bot Status\nOnline"},
				)
				c.AddSpacer(6)
				_ = c.AddText(healthReport)
				_, _ = c.ResultPNG()
			}
		})
	}
}

// ─── 6. AddImageBytes decode + process pipeline ───────────────────────────────

func BenchmarkCanvas_AddImageBytes(b *testing.B) {
	pngData := encodedPNG(256, 256, color.RGBA{R: 100, G: 150, B: 200, A: 255})
	b.SetBytes(int64(len(pngData)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := textimage.NewCanvas(640)
		_ = c.AddImageBytes(pngData, textimage.WithImgWidth(120), textimage.WithImgCircle())
		_ = c.Result()
	}
}

// ─── 7. Parallel throughput ───────────────────────────────────────────────────

// BenchmarkRender_Parallel_Shared calls Render() concurrently on a single
// Renderer. The internal sync.Mutex serialises font-face access, so this
// measures lock-contention overhead rather than true parallelism.
func BenchmarkRender_Parallel_Shared(b *testing.B) {
	r := textimage.MustNew(textimage.WithSize(640, 0))
	defer func() { _ = r.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			img, _ := r.Render(healthReport)
			_ = img
		}
	})
}

// BenchmarkRender_Parallel_PerGoroutine gives each goroutine its own Renderer
// so font-face access is never shared — this is the recommended high-throughput pattern.
func BenchmarkRender_Parallel_PerGoroutine(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := textimage.MustNew(textimage.WithSize(640, 0))
		defer func() { _ = r.Close() }()
		for pb.Next() {
			img, _ := r.Render(healthReport)
			_ = img
		}
	})
}

// BenchmarkCanvas_HealthReport_Parallel is naturally parallel because each
// Canvas creates its own Renderer — no sharing occurs.
func BenchmarkCanvas_HealthReport_Parallel(b *testing.B) {
	avatar := solidImg(96, 96, color.RGBA{R: 80, G: 120, B: 200, A: 255})
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c, _ := textimage.NewCanvas(520)
			c.AddSpacer(12)
			_ = c.AddRow(
				textimage.RowItem{
					Width:     100,
					Image:     avatar,
					ImageOpts: []textimage.ImageOption{textimage.WithImgCircle()},
				},
				textimage.RowItem{Text: "Remilia Bot\nOnline"},
			)
			c.AddSpacer(8)
			_ = c.AddText(healthReport)
			_, _ = c.ResultPNG()
		}
	})
}

// ─── 8. TextSize (layout-only, no rasterisation) ──────────────────────────────

func BenchmarkTextSize(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w, h, _ := textimage.TextSize(longASCII, textimage.WithSize(640, 0))
		_, _ = w, h
	}
}

// ─── 9. Convenience wrappers ──────────────────────────────────────────────────

// BenchmarkRenderText creates and destroys a Renderer per call — worst case,
// useful for measuring the cost when callers do not cache the Renderer.
func BenchmarkRenderText(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := textimage.RenderText(shortASCII)
		_ = data
	}
}
