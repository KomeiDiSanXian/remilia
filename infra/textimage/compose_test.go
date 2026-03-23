package textimage_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// ─── NewCanvas ────────────────────────────────────────────────────────────────

func TestNewCanvas_Defaults(t *testing.T) {
	c, err := textimage.NewCanvas(640)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	img := c.Result()
	if img.Bounds().Dx() != 640 {
		t.Fatalf("expected width 640, got %d", img.Bounds().Dx())
	}
}

func TestNewCanvas_InvalidWidth(t *testing.T) {
	_, err := textimage.NewCanvas(0)
	if err == nil {
		t.Fatal("expected error for width=0")
	}
}

// ─── AddText ─────────────────────────────────────────────────────────────────

func TestCanvas_AddText(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	if err := c.AddText("Hello, Canvas!"); err != nil {
		t.Fatalf("AddText: %v", err)
	}
	img := c.Result()
	assertNonZeroImage(t, img)
}

func TestCanvas_AddText_MultiLine(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	_ = c.AddText("Line A\nLine B\nLine C")
	img := c.Result()
	if img.Bounds().Dy() == 0 {
		t.Fatal("expected non-zero height")
	}
}

func TestCanvas_AddText_PerBlockOptions(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	err := c.AddText("bold-ish",
		textimage.WithFontSize(28),
		textimage.WithFontColor(color.RGBA{R: 255, G: 0, B: 0, A: 255}),
	)
	if err != nil {
		t.Fatalf("AddText with options: %v", err)
	}
}

// ─── AddImage ─────────────────────────────────────────────────────────────────

func TestCanvas_AddImage_Basic(t *testing.T) {
	src := makeSolidImage(100, 80, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	c, _ := textimage.NewCanvas(400)
	if err := c.AddImage(src); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	assertNonZeroImage(t, c.Result())
}

func TestCanvas_AddImage_ScaleToWidth(t *testing.T) {
	src := makeSolidImage(200, 200, color.Black)
	c, _ := textimage.NewCanvas(400)
	_ = c.AddImage(src, textimage.WithImgWidth(50))
	img := c.Result()
	// The image block should be 50 px tall (1:1 aspect → 50×50).
	if img.Bounds().Dy() != 50 {
		t.Fatalf("expected height 50, got %d", img.Bounds().Dy())
	}
}

func TestCanvas_AddImage_CircleClip(t *testing.T) {
	src := makeSolidImage(100, 100, color.RGBA{R: 0, G: 128, B: 255, A: 255})
	c, _ := textimage.NewCanvas(400)
	if err := c.AddImage(src, textimage.WithImgCircle(), textimage.WithImgAlign(textimage.AlignCenter)); err != nil {
		t.Fatalf("AddImage circle: %v", err)
	}
	assertNonZeroImage(t, c.Result())
}

func TestCanvas_AddImage_RoundedCorners(t *testing.T) {
	src := makeSolidImage(120, 80, color.RGBA{G: 200, A: 255})
	c, _ := textimage.NewCanvas(400)
	if err := c.AddImage(src, textimage.WithImgRoundRadius(12)); err != nil {
		t.Fatalf("AddImage rounded: %v", err)
	}
	assertNonZeroImage(t, c.Result())
}

func TestCanvas_AddImage_Nil(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	if err := c.AddImage(nil); err == nil {
		t.Fatal("expected error for nil image")
	}
}

// ─── AddImageBytes ────────────────────────────────────────────────────────────

func TestCanvas_AddImageBytes(t *testing.T) {
	src := makeSolidImage(60, 60, color.White)
	var buf bytes.Buffer
	_ = png.Encode(&buf, src)

	c, _ := textimage.NewCanvas(400)
	if err := c.AddImageBytes(buf.Bytes()); err != nil {
		t.Fatalf("AddImageBytes: %v", err)
	}
	assertNonZeroImage(t, c.Result())
}

func TestCanvas_AddImageBytes_BadData(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	if err := c.AddImageBytes([]byte("not an image")); err == nil {
		t.Fatal("expected error for invalid image bytes")
	}
}

// ─── AddSpacer ────────────────────────────────────────────────────────────────

func TestCanvas_AddSpacer(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	_ = c.AddText("Top")
	c.AddSpacer(20)
	_ = c.AddText("Bottom")
	img := c.Result()

	// Without the spacer the height would be smaller.
	cNoSpacer, _ := textimage.NewCanvas(400)
	_ = cNoSpacer.AddText("Top")
	_ = cNoSpacer.AddText("Bottom")

	if img.Bounds().Dy() <= cNoSpacer.Result().Bounds().Dy() {
		t.Fatal("spacer should increase total height")
	}
}

// ─── AddRow ───────────────────────────────────────────────────────────────────

func TestCanvas_AddRow_TextAndImage(t *testing.T) {
	avatar := makeSolidImage(64, 64, color.RGBA{R: 0, G: 128, B: 255, A: 255})
	c, _ := textimage.NewCanvas(640)
	err := c.AddRow(
		textimage.RowItem{
			Width:     80,
			Image:     avatar,
			ImageOpts: []textimage.ImageOption{textimage.WithImgCircle()},
		},
		textimage.RowItem{
			Text:     "🟢 Online\nPing: 3 ms",
			TextOpts: []textimage.Option{textimage.WithFontSize(16)},
		},
	)
	if err != nil {
		t.Fatalf("AddRow: %v", err)
	}
	assertNonZeroImage(t, c.Result())
}

func TestCanvas_AddRow_Empty(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	if err := c.AddRow(); err != nil {
		t.Fatalf("AddRow() with no items should not error: %v", err)
	}
}

func TestCanvas_AddRow_ExplicitWidths(t *testing.T) {
	c, _ := textimage.NewCanvas(600)
	err := c.AddRow(
		textimage.RowItem{Width: 200, Text: "Left"},
		textimage.RowItem{Width: 200, Text: "Middle"},
		textimage.RowItem{Width: 200, Text: "Right"},
	)
	if err != nil {
		t.Fatalf("AddRow explicit widths: %v", err)
	}
	img := c.Result()
	if img.Bounds().Dx() != 600 {
		t.Fatalf("expected width 600, got %d", img.Bounds().Dx())
	}
}

// ─── Result / ResultPNG / ResultJPEG ─────────────────────────────────────────

func TestCanvas_ResultPNG(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	_ = c.AddText("PNG output")
	data, err := c.ResultPNG()
	if err != nil {
		t.Fatalf("ResultPNG: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ResultPNG returned empty bytes")
	}
	// Verify it is valid PNG.
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("output is not valid PNG: %v", err)
	}
}

func TestCanvas_ResultJPEG(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	_ = c.AddText("JPEG output")
	data, err := c.ResultJPEG(80)
	if err != nil {
		t.Fatalf("ResultJPEG: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ResultJPEG returned empty bytes")
	}
}

func TestCanvas_ResultMultipleCalls(t *testing.T) {
	c, _ := textimage.NewCanvas(400)
	_ = c.AddText("block one")
	img1 := c.Result()
	_ = c.AddText("block two")
	img2 := c.Result()
	if img2.Bounds().Dy() <= img1.Bounds().Dy() {
		t.Fatal("second Result() call should be taller after adding another block")
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func makeSolidImage(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}
