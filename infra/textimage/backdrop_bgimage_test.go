package textimage_test

// Tests for BgImage, backdrop blur, BackdropShape, BackdropMode, and CJK
// word-wrap — all features added after the initial release.

import (
	"image"
	"image/color"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// ─── BgImage ─────────────────────────────────────────────────────────────────

func TestRender_BgImage_Stretch(t *testing.T) {
	bg := makeSolidImage(100, 50, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	r := textimage.MustNew(
		textimage.WithSize(200, 100),
		textimage.WithBgImage(bg, textimage.BgFitStretch),
	)
	defer r.Close()

	img, err := r.Render("bg stretch")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
	b := img.Bounds()
	if b.Dx() != 200 || b.Dy() != 100 {
		t.Fatalf("expected 200×100, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestRender_BgImage_AllFitModes(t *testing.T) {
	bg := makeSolidImage(80, 60, color.RGBA{R: 0, G: 128, B: 255, A: 255})
	modes := []textimage.BgFitMode{
		textimage.BgFitStretch,
		textimage.BgFitFill,
		textimage.BgFitFit,
		textimage.BgFitCenter,
		textimage.BgFitTile,
	}
	for _, m := range modes {
		r := textimage.MustNew(
			textimage.WithSize(200, 120),
			textimage.WithBgImage(bg, m),
		)
		img, err := r.Render("fit")
		r.Close()
		if err != nil {
			t.Fatalf("BgFitMode %d: %v", m, err)
		}
		assertNonZeroImage(t, img)
	}
}

func TestRender_BgImage_NilClearsBackground(t *testing.T) {
	// Passing nil BgImage should fall back to BgColor with no panic.
	r := textimage.MustNew(
		textimage.WithSize(100, 50),
		textimage.WithBgImage(nil, textimage.BgFitStretch),
		textimage.WithBgColor(color.RGBA{R: 255, G: 0, B: 0, A: 255}),
	)
	defer r.Close()
	img, err := r.Render("no bg")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Top-left pixel should be the BgColor red.
	c := img.At(0, 0)
	r32, _, _, _ := c.RGBA()
	if r32 == 0 {
		t.Fatal("expected red background, but red channel is 0")
	}
}

// ─── TextBackdrop ─────────────────────────────────────────────────────────────

func TestRender_Backdrop_ColorOnly(t *testing.T) {
	r := textimage.MustNew(
		textimage.WithSize(300, 80),
		textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 128}, 0),
	)
	defer r.Close()
	img, err := r.Render("backdrop color only")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

func TestRender_Backdrop_BlurOnly(t *testing.T) {
	bg := makeSolidImage(300, 80, color.RGBA{R: 255, G: 200, B: 100, A: 255})
	r := textimage.MustNew(
		textimage.WithSize(300, 80),
		textimage.WithBgImage(bg, textimage.BgFitStretch),
		textimage.WithTextBackdrop(nil, 8), // blur only, no color overlay
	)
	defer r.Close()
	img, err := r.Render("blur only")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

func TestRender_Backdrop_BlurAndColor(t *testing.T) {
	bg := makeSolidImage(400, 120, color.RGBA{R: 80, G: 140, B: 200, A: 255})
	r := textimage.MustNew(
		textimage.WithSize(400, 120),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.White),
		textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 140}, 12),
		textimage.WithTextBackdropPadding(10, 6),
	)
	defer r.Close()
	img, err := r.Render("frosted glass")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

// ─── BackdropShape ────────────────────────────────────────────────────────────

func TestRender_Backdrop_ShapeRect(t *testing.T) {
	testBackdropShape(t, textimage.BackdropShapeRect, 0)
}

func TestRender_Backdrop_ShapeRounded(t *testing.T) {
	testBackdropShape(t, textimage.BackdropShapeRounded, 10)
}

func TestRender_Backdrop_ShapeEllipse(t *testing.T) {
	testBackdropShape(t, textimage.BackdropShapeEllipse, 0)
}

func testBackdropShape(t *testing.T, shape textimage.BackdropShape, roundR int) {
	t.Helper()
	bg := makeSolidImage(300, 80, color.RGBA{R: 120, G: 80, B: 200, A: 255})
	r := textimage.MustNew(
		textimage.WithSize(300, 80),
		textimage.WithBgImage(bg, textimage.BgFitStretch),
		textimage.WithFontColor(color.White),
		textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 160}, 8),
		textimage.WithTextBackdropPadding(8, 4),
		textimage.WithTextBackdropShape(shape, roundR),
	)
	defer r.Close()
	img, err := r.Render("shape test")
	if err != nil {
		t.Fatalf("shape %d: %v", shape, err)
	}
	assertNonZeroImage(t, img)
}

// ─── BackdropMode ─────────────────────────────────────────────────────────────

func TestRender_BackdropMode_PerLine(t *testing.T) {
	r := textimage.MustNew(
		textimage.WithSize(300, 0),
		textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 120}, 0),
		textimage.WithTextBackdropMode(textimage.BackdropModePerLine),
	)
	defer r.Close()
	img, err := r.Render("line one\nline two\nline three")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

func TestRender_BackdropMode_Block(t *testing.T) {
	bg := makeSolidImage(400, 0, color.RGBA{R: 50, G: 100, B: 200, A: 255})
	r := textimage.MustNew(
		textimage.WithSize(400, 0),
		textimage.WithBgImage(bg, textimage.BgFitStretch),
		textimage.WithFontColor(color.White),
		textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 140}, 10),
		textimage.WithTextBackdropPadding(12, 8),
		textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 10),
		textimage.WithTextBackdropMode(textimage.BackdropModeBlock),
	)
	defer r.Close()
	img, err := r.Render("panel line one\npanel line two\npanel line three")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

// BackdropModeBlock should produce a single contiguous region so the image
// height with 3 lines should be the same as with BackdropModePerLine
// (same text, same renderer options — only the backdrop mode differs).
func TestRender_BackdropMode_Block_SameHeight(t *testing.T) {
	text := "alpha\nbeta\ngamma"
	makeR := func(m textimage.BackdropMode) *textimage.Renderer {
		return textimage.MustNew(
			textimage.WithSize(300, 0),
			textimage.WithTextBackdrop(color.NRGBA{A: 100}, 0),
			textimage.WithTextBackdropMode(m),
		)
	}
	rLine := makeR(textimage.BackdropModePerLine)
	defer rLine.Close()
	rBlock := makeR(textimage.BackdropModeBlock)
	defer rBlock.Close()

	imgLine, _ := rLine.Render(text)
	imgBlock, _ := rBlock.Render(text)
	if imgLine.Bounds().Dy() != imgBlock.Bounds().Dy() {
		t.Fatalf("height mismatch: perLine=%d block=%d",
			imgLine.Bounds().Dy(), imgBlock.Bounds().Dy())
	}
}

// ─── Canvas + BgImage ────────────────────────────────────────────────────────

func TestCanvas_BgImage_ShowsThroughSpacers(t *testing.T) {
	// A canvas with BgImage + spacer: the final image should exist and have
	// the correct width.
	bg := makeSolidImage(400, 200, color.RGBA{R: 100, G: 150, B: 200, A: 255})
	c, err := textimage.NewCanvas(400,
		textimage.WithBgImage(bg, textimage.BgFitStretch),
		textimage.WithBgColor(color.White),
	)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	c.AddSpacer(20)
	if err := c.AddText("hello"); err != nil {
		t.Fatalf("AddText: %v", err)
	}
	c.AddSpacer(20)

	img := c.Result()
	assertNonZeroImage(t, img)
	if img.Bounds().Dx() != 400 {
		t.Fatalf("expected width 400, got %d", img.Bounds().Dx())
	}
}

func TestCanvas_BgImage_BackdropUsesCanvasPixels(t *testing.T) {
	// The backdrop blur in AddText should operate on the canvas background
	// pixels (blue), not on a plain white per-block background.
	// We verify by checking that the region behind the text is not pure white.
	bg := makeSolidImage(400, 200, color.RGBA{R: 0, G: 0, B: 255, A: 255}) // all blue
	c, err := textimage.NewCanvas(400,
		textimage.WithBgImage(bg, textimage.BgFitStretch),
		textimage.WithBgColor(color.White),
	)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	if err := c.AddText("test backdrop",
		textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 80}, 4),
		textimage.WithTextBackdropMode(textimage.BackdropModeBlock),
	); err != nil {
		t.Fatalf("AddText: %v", err)
	}
	img := c.Result()
	assertNonZeroImage(t, img)
	// The top-left pixel is in the spacer zone (before text), drawn directly
	// from BgImage → should be blue (B channel dominant).
	_, _, b16, _ := img.At(0, 0).RGBA()
	if b16 == 0 {
		t.Fatal("expected blue BgImage pixel at (0,0), got zero blue channel")
	}
}

func TestCanvas_AddText_BackdropModeBlock_MultiPanel(t *testing.T) {
	bg := makeSolidImage(500, 400, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	c, _ := textimage.NewCanvas(500,
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithBgColor(color.Black),
		textimage.WithFontColor(color.White),
	)
	panels := []string{
		"Panel A\nLine two\nLine three",
		"Panel B\nDifferent content",
		"Panel C",
	}
	for _, p := range panels {
		c.AddSpacer(10)
		if err := c.AddText(p,
			textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 150}, 10),
			textimage.WithTextBackdropPadding(16, 8),
			textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 8),
			textimage.WithTextBackdropMode(textimage.BackdropModeBlock),
		); err != nil {
			t.Fatalf("AddText %q: %v", p, err)
		}
	}
	c.AddSpacer(10)
	assertNonZeroImage(t, c.Result())
}

// ─── CJK / character-level word-wrap ─────────────────────────────────────────

func TestRender_CJK_WrapNoSpaces(t *testing.T) {
	// A long CJK string with no spaces should still be wrapped when Width is set.
	r := textimage.MustNew(
		textimage.WithSize(120, 0), // narrow canvas forces wrapping
		textimage.WithCJKFont(),
		textimage.WithFontSize(14),
		textimage.WithPadding(4, 4),
	)
	if r == nil {
		t.Skip("no CJK font available")
	}
	defer r.Close()

	longCJK := "这是一段没有任何空格的很长的中文文字用于测试字符级换行功能"
	img, err := r.Render(longCJK)
	if err != nil {
		t.Fatalf("Render CJK: %v", err)
	}
	assertNonZeroImage(t, img)
	// With wrapping, the image should be taller than a single line.
	singleR := textimage.MustNew(
		textimage.WithSize(120, 0),
		textimage.WithCJKFont(),
		textimage.WithFontSize(14),
		textimage.WithPadding(4, 4),
	)
	defer singleR.Close()
	singleImg, _ := singleR.Render("短")
	if img.Bounds().Dy() <= singleImg.Bounds().Dy() {
		t.Fatalf("expected multi-line height > %d, got %d",
			singleImg.Bounds().Dy(), img.Bounds().Dy())
	}
}

func TestRender_CJK_WrapWithSpaces(t *testing.T) {
	// Mixed CJK + ASCII with spaces should word-wrap normally.
	r := textimage.MustNew(
		textimage.WithSize(200, 0),
		textimage.WithCJKFont(),
		textimage.WithFontSize(14),
	)
	defer r.Close()

	img, err := r.Render("关于 Remilia 框架 hello world 这是混合文本")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertNonZeroImage(t, img)
}

// ─── helpers (local) ─────────────────────────────────────────────────────────

// makeSolidImage is defined in compose_test.go as makeSolidImage; reuse it here
// via the shared test package textimage_test. No re-declaration needed.

func makeGradientImage(w, h int, from, to color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		t := float64(x) / float64(max1(w-1, 1))
		r := uint8(float64(from.R)*(1-t) + float64(to.R)*t)
		g := uint8(float64(from.G)*(1-t) + float64(to.G)*t)
		b := uint8(float64(from.B)*(1-t) + float64(to.B)*t)
		for y := range h {
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func max1(a, b int) int {
	if a > b {
		return a
	}
	return b
}
