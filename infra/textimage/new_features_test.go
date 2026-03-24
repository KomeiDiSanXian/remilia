package textimage_test

// Tests for the features added in the second feature wave:
//   - LinearGradient / RadialGradient
//   - WithTextShadow (hard and soft)
//   - Canvas.AddDivider
//   - Canvas.AddProgressBar
//   - WithImgOpacity

import (
	"image/color"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// ─── Gradient ─────────────────────────────────────────────────────────────────

func TestLinearGradient_Size(t *testing.T) {
	img := textimage.LinearGradient(300, 200, 0,
		textimage.Stop(0, color.RGBA{R: 255, A: 255}),
		textimage.Stop(1, color.RGBA{B: 255, A: 255}),
	)
	b := img.Bounds()
	if b.Dx() != 300 || b.Dy() != 200 {
		t.Fatalf("expected 300×200, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestLinearGradient_LeftToRight(t *testing.T) {
	// angle=0 → left column = red, right column = blue
	img := textimage.LinearGradient(100, 10, 0,
		textimage.Stop(0, color.RGBA{R: 255, A: 255}),
		textimage.Stop(1, color.RGBA{B: 255, A: 255}),
	)
	leftR, _, _, _ := img.At(0, 5).RGBA()
	rightB := func() uint32 { _, _, b, _ := img.At(99, 5).RGBA(); return b }()
	if leftR == 0 {
		t.Fatal("left pixel should be red-dominant")
	}
	if rightB == 0 {
		t.Fatal("right pixel should be blue-dominant")
	}
}

func TestLinearGradient_TopToBottom(t *testing.T) {
	img := textimage.LinearGradient(10, 100, 90,
		textimage.Stop(0, color.RGBA{R: 255, A: 255}),
		textimage.Stop(1, color.RGBA{B: 255, A: 255}),
	)
	topR, _, _, _ := img.At(5, 0).RGBA()
	botB := func() uint32 { _, _, b, _ := img.At(5, 99).RGBA(); return b }()
	if topR == 0 {
		t.Fatal("top pixel should be red-dominant (angle=90, top→bottom)")
	}
	if botB == 0 {
		t.Fatal("bottom pixel should be blue-dominant")
	}
}

func TestLinearGradient_MultiStop(t *testing.T) {
	img := textimage.LinearGradient(300, 10, 0,
		textimage.Stop(0.0, color.RGBA{R: 255, A: 255}),
		textimage.Stop(0.5, color.RGBA{G: 255, A: 255}),
		textimage.Stop(1.0, color.RGBA{B: 255, A: 255}),
	)
	assertNonZeroImage(t, img)
}

func TestLinearGradient_NoStops(t *testing.T) {
	img := textimage.LinearGradient(50, 50, 0) // no stops → transparent black
	if img == nil {
		t.Fatal("expected non-nil image with no stops")
	}
}

func TestLinearGradient_SmallSize(t *testing.T) {
	img := textimage.LinearGradient(0, 0, 0, textimage.Stop(0, color.White))
	b := img.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		t.Fatalf("degenerate size: %v", b)
	}
}

func TestRadialGradient_Size(t *testing.T) {
	img := textimage.RadialGradient(200, 150,
		textimage.Stop(0, color.White),
		textimage.Stop(1, color.Black),
	)
	b := img.Bounds()
	if b.Dx() != 200 || b.Dy() != 150 {
		t.Fatalf("expected 200×150, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestRadialGradient_CentreIsFirstStop(t *testing.T) {
	img := textimage.RadialGradient(101, 101,
		textimage.Stop(0, color.RGBA{R: 255, A: 255}), // red centre
		textimage.Stop(1, color.RGBA{B: 255, A: 255}), // blue edge
	)
	// Centre pixel should have high red channel.
	cr, _, cb, _ := img.At(50, 50).RGBA()
	if cr < cb {
		t.Fatalf("centre: expected red > blue, got R=%d B=%d", cr>>8, cb>>8)
	}
}

func TestGradient_AsRendererBackground(t *testing.T) {
	bg := textimage.LinearGradient(400, 100, 0,
		textimage.Stop(0, color.RGBA{R: 200, G: 50, B: 20, A: 255}),
		textimage.Stop(1, color.RGBA{R: 20, G: 50, B: 200, A: 255}),
	)
	r := textimage.MustNew(
		textimage.WithSize(400, 100),
		textimage.WithBgImage(bg, textimage.BgFitStretch),
		textimage.WithFontColor(color.White),
	)
	defer r.Close()
	img, err := r.Render("gradient background")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

// ─── Text Shadow ──────────────────────────────────────────────────────────────

func TestTextShadow_HardEdge(t *testing.T) {
	r := textimage.MustNew(
		textimage.WithSize(300, 80),
		textimage.WithTextShadow(color.RGBA{A: 200}, 3, 3, 0),
	)
	defer r.Close()
	img, err := r.Render("hard shadow")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

func TestTextShadow_SoftBlur(t *testing.T) {
	r := textimage.MustNew(
		textimage.WithSize(300, 80),
		textimage.WithTextShadow(color.RGBA{A: 180}, 2, 4, 5),
	)
	defer r.Close()
	img, err := r.Render("soft shadow")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

func TestTextShadow_WithBgImage(t *testing.T) {
	bg := textimage.LinearGradient(400, 120, 90,
		textimage.Stop(0, color.RGBA{R: 255, G: 180, B: 60, A: 255}),
		textimage.Stop(1, color.RGBA{R: 60, G: 120, B: 200, A: 255}),
	)
	r := textimage.MustNew(
		textimage.WithSize(400, 120),
		textimage.WithBgImage(bg, textimage.BgFitStretch),
		textimage.WithFontColor(color.White),
		textimage.WithTextShadow(color.RGBA{A: 200}, 2, 3, 4),
		textimage.WithCJKFont(),
	)
	defer r.Close()
	img, err := r.Render("阴影测试 Shadow Test")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

func TestTextShadow_MultiLine(t *testing.T) {
	r := textimage.MustNew(
		textimage.WithSize(300, 0),
		textimage.WithTextShadow(color.RGBA{A: 160}, 1, 2, 3),
	)
	defer r.Close()
	img, err := r.Render("line one\nline two\nline three")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

// Shadow pixels at the offset position should differ from a render without shadow.
func TestTextShadow_AffectsPixels(t *testing.T) {
	makeR := func(shadow bool) *textimage.Renderer {
		opts := []textimage.Option{
			textimage.WithSize(200, 60),
			textimage.WithBgColor(color.White),
			textimage.WithFontColor(color.Black),
		}
		if shadow {
			opts = append(opts, textimage.WithTextShadow(color.RGBA{A: 255}, 5, 5, 0))
		}
		return textimage.MustNew(opts...)
	}
	rNoShadow := makeR(false)
	defer rNoShadow.Close()
	rShadow := makeR(true)
	defer rShadow.Close()

	imgNo, _ := rNoShadow.Render("X")
	imgYes, _ := rShadow.Render("X")

	// With shadow, the pixel at (text_x+5, text_y+5) should be darker (lower R).
	// Just verify the two images are not identical.
	different := false
	for y := 0; y < 60 && !different; y++ {
		for x := 0; x < 200 && !different; x++ {
			r1, _, _, _ := imgNo.At(x, y).RGBA()
			r2, _, _, _ := imgYes.At(x, y).RGBA()
			if r1 != r2 {
				different = true
			}
		}
	}
	if !different {
		t.Fatal("shadow render should differ from no-shadow render")
	}
}

// ─── Canvas.AddDivider ────────────────────────────────────────────────────────

func TestCanvas_AddDivider_Default(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	_ = c.AddText("above")
	c.AddDivider()
	_ = c.AddText("below")
	img := c.Result()
	assertNonZeroImage(t, img)
}

func TestCanvas_AddDivider_Options(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	_ = c.AddText("section A")
	c.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 100, G: 100, B: 200, A: 255}),
		textimage.WithDividerThickness(2),
		textimage.WithDividerInset(20),
		textimage.WithDividerPadding(12),
	)
	_ = c.AddText("section B")
	img := c.Result()
	assertNonZeroImage(t, img)
}

func TestCanvas_AddDivider_IncreasesHeight(t *testing.T) {
	cWithout, _ := textimage.NewCanvas(400)
	_ = cWithout.AddText("a")
	_ = cWithout.AddText("b")

	cWith, _ := textimage.NewCanvas(400)
	_ = cWith.AddText("a")
	cWith.AddDivider(textimage.WithDividerPadding(10))
	_ = cWith.AddText("b")

	if cWith.Result().Bounds().Dy() <= cWithout.Result().Bounds().Dy() {
		t.Fatal("canvas with divider should be taller")
	}
}

// ─── Canvas.AddProgressBar ────────────────────────────────────────────────────

func TestCanvas_AddProgressBar_Basic(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	c.AddProgressBar(75, 100)
	img := c.Result()
	assertNonZeroImage(t, img)
}

func TestCanvas_AddProgressBar_FullAndEmpty(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	c.AddProgressBar(100, 100) // full
	c.AddSpacer(4)
	c.AddProgressBar(0, 100) // empty
	c.AddSpacer(4)
	c.AddProgressBar(50, 100, // half
		textimage.WithProgressFillColor(color.RGBA{R: 255, G: 100, B: 0, A: 255}),
		textimage.WithProgressTrackColor(color.RGBA{R: 40, G: 40, B: 40, A: 255}),
		textimage.WithProgressHeight(16),
		textimage.WithProgressRadius(8),
		textimage.WithProgressPadding(20, 6),
	)
	assertNonZeroImage(t, c.Result())
}

func TestCanvas_AddProgressBar_OutOfRange(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	c.AddProgressBar(150, 100) // value > max → clamped to 1
	c.AddProgressBar(-10, 100) // negative → clamped to 0
	c.AddProgressBar(50, 0)    // max=0 → value=0
	assertNonZeroImage(t, c.Result())
}

func TestCanvas_AddProgressBar_InHealthReport(t *testing.T) {
	bg := textimage.LinearGradient(500, 0, 90,
		textimage.Stop(0, color.RGBA{R: 20, G: 20, B: 40, A: 255}),
		textimage.Stop(1, color.RGBA{R: 30, G: 10, B: 60, A: 255}),
	)
	c, _ := textimage.NewCanvas(500,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.RGBA{R: 220, G: 220, B: 240, A: 255}),
		textimage.WithPadding(24, 0),
	)
	c.AddSpacer(12)
	_ = c.AddText("CPU  3.2 %")
	c.AddProgressBar(3.2, 100,
		textimage.WithProgressFillColor(color.RGBA{R: 80, G: 200, B: 120, A: 255}),
		textimage.WithProgressTrackColor(color.RGBA{R: 40, G: 40, B: 50, A: 200}),
	)
	_ = c.AddText("内存  48 / 256 MB")
	c.AddProgressBar(48, 256,
		textimage.WithProgressFillColor(color.RGBA{R: 100, G: 160, B: 240, A: 255}),
		textimage.WithProgressTrackColor(color.RGBA{R: 40, G: 40, B: 50, A: 200}),
	)
	c.AddSpacer(12)
	assertNonZeroImage(t, c.Result())
}

// ─── WithImgOpacity ───────────────────────────────────────────────────────────

func TestImgOpacity_FullOpaque(t *testing.T) {
	src := makeSolidImage(100, 100, color.RGBA{R: 255, A: 255})
	c, _ := textimage.NewCanvas(200,
		textimage.WithBgColor(color.White),
	)
	_ = c.AddImage(src, textimage.WithImgOpacity(1.0))
	assertNonZeroImage(t, c.Result())
}

func TestImgOpacity_SemiTransparent(t *testing.T) {
	// A red image at 50% opacity over white background should produce
	// a pixel whose red channel is between white and full red.
	src := makeSolidImage(50, 50, color.RGBA{R: 255, A: 255})
	c, _ := textimage.NewCanvas(50,
		textimage.WithBgColor(color.White),
	)
	_ = c.AddImage(src, textimage.WithImgOpacity(0.5))
	img := c.Result()

	// Any pixel inside the image area should be between white and red.
	r16, g16, _, _ := img.At(25, 25).RGBA()
	r8, g8 := uint8(r16>>8), uint8(g16>>8)
	// Red channel should be close to 255 (white has R=255, red has R=255 → both 255)
	// Green channel should be ~128 (white G=255, red G=0, 50% blend ≈ 128)
	if g8 > 200 {
		t.Fatalf("expected semi-transparent blend (G ≈ 128), got G=%d", g8)
	}
	_ = r8 // always 255 in this test
}

func TestImgOpacity_Zero_Invisible(t *testing.T) {
	// An image with opacity=0 should contribute nothing; background remains.
	bgColor := color.RGBA{R: 0, G: 200, B: 0, A: 255} // green background
	src := makeSolidImage(50, 50, color.RGBA{R: 255, A: 255})
	c, _ := textimage.NewCanvas(50,
		textimage.WithBgColor(bgColor),
	)
	_ = c.AddImage(src, textimage.WithImgOpacity(0.0))
	img := c.Result()
	_, g16, _, _ := img.At(25, 25).RGBA()
	if g16>>8 < 180 {
		t.Fatalf("background green should dominate when opacity=0, got G=%d", g16>>8)
	}
}

func TestImgOpacity_DefaultFullOpaque(t *testing.T) {
	// Default (no WithImgOpacity) should behave as opacity=1.
	src := makeSolidImage(50, 50, color.RGBA{R: 255, A: 255})
	c, _ := textimage.NewCanvas(50, textimage.WithBgColor(color.White))
	_ = c.AddImage(src)
	img := c.Result()
	r16, g16, _, _ := img.At(25, 25).RGBA()
	if r16>>8 < 200 || g16 != 0 {
		t.Fatalf("default opacity should be fully opaque red: R=%d G=%d", r16>>8, g16>>8)
	}
}
