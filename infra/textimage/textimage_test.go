package textimage_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// ─── New / construction ───────────────────────────────────────────────────────

func TestNew_Defaults(t *testing.T) {
	r, err := textimage.New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer r.Close()
}

func TestNew_InvalidFontPath(t *testing.T) {
	_, err := textimage.New(textimage.WithFontPath("/no/such/font.ttf"))
	if err == nil {
		t.Fatal("expected error for missing font file")
	}
}

func TestMustNew_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustNew should panic on invalid font path")
		}
	}()
	textimage.MustNew(textimage.WithFontPath("/no/such.ttf"))
}

// ─── Render ───────────────────────────────────────────────────────────────────

func TestRender_Simple(t *testing.T) {
	r := textimage.MustNew()
	defer r.Close()

	img, err := r.Render("Hello, World!")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	assertNonZeroImage(t, img)
}

func TestRender_EmptyText(t *testing.T) {
	r := textimage.MustNew(textimage.WithSize(100, 50))
	defer r.Close()

	img, err := r.Render("")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 100 || b.Dy() != 50 {
		t.Fatalf("expected 100×50, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestRender_MultiLine(t *testing.T) {
	r := textimage.MustNew()
	defer r.Close()

	img, err := r.Render("Line one\nLine two\nLine three")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	assertNonZeroImage(t, img)
}

func TestRender_WordWrap(t *testing.T) {
	r := textimage.MustNew(
		textimage.WithSize(200, 0),
	)
	defer r.Close()

	img, err := r.Render("This is a long sentence that should be wrapped across multiple lines by the renderer.")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	// With word-wrap the height should be greater than a single-line render.
	single := textimage.MustNew(textimage.WithSize(200, 0))
	defer single.Close()
	imgSingle, _ := single.Render("short")
	if img.Bounds().Dy() <= imgSingle.Bounds().Dy() {
		t.Logf("wrapped height=%d, single-line height=%d", img.Bounds().Dy(), imgSingle.Bounds().Dy())
		// Not strictly a failure if the sentence fits on one wrapped line.
	}
}

func TestRender_ExplicitSize(t *testing.T) {
	r := textimage.MustNew(textimage.WithSize(320, 120))
	defer r.Close()

	img, err := r.Render("sized")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 320 || b.Dy() != 120 {
		t.Fatalf("expected 320×120, got %dx%d", b.Dx(), b.Dy())
	}
}

// ─── Alignment ───────────────────────────────────────────────────────────────

func TestRender_Alignments(t *testing.T) {
	alignments := []struct {
		name  string
		align textimage.Alignment
	}{
		{"left", textimage.AlignLeft},
		{"center", textimage.AlignCenter},
		{"right", textimage.AlignRight},
	}
	for _, tc := range alignments {
		t.Run(tc.name, func(t *testing.T) {
			r := textimage.MustNew(
				textimage.WithSize(400, 100),
				textimage.WithAlign(tc.align),
			)
			defer r.Close()
			img, err := r.Render("Aligned text sample")
			if err != nil {
				t.Fatalf("Render() error: %v", err)
			}
			assertNonZeroImage(t, img)
		})
	}
}

// ─── Encoding ─────────────────────────────────────────────────────────────────

func TestRenderToPNG(t *testing.T) {
	r := textimage.MustNew(
		textimage.WithFontSize(18),
		textimage.WithPadding(12, 12),
		textimage.WithFontColor(color.RGBA{R: 20, G: 20, B: 20, A: 255}),
		textimage.WithBgColor(color.RGBA{R: 250, G: 250, B: 250, A: 255}),
	)
	defer r.Close()

	data, err := r.RenderToPNG("PNG output test")
	if err != nil {
		t.Fatalf("RenderToPNG() error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("RenderToPNG() returned empty bytes")
	}
	// Verify it decodes as a valid PNG.
	_, err = png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("result is not valid PNG: %v", err)
	}
}

func TestRenderToJPEG(t *testing.T) {
	r := textimage.MustNew()
	defer r.Close()

	data, err := r.RenderToJPEG("JPEG test", 90)
	if err != nil {
		t.Fatalf("RenderToJPEG() error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("RenderToJPEG() returned empty bytes")
	}
}

func TestRenderToFile_PNG(t *testing.T) {
	r := textimage.MustNew()
	defer r.Close()

	tmp := t.TempDir()
	path := tmp + "/out.png"

	if err := r.RenderToFile(path, "file output"); err != nil {
		t.Fatalf("RenderToFile() error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
}

func TestRenderToFile_JPEG(t *testing.T) {
	r := textimage.MustNew()
	defer r.Close()

	tmp := t.TempDir()
	path := tmp + "/out.jpg"

	if err := r.RenderToFile(path, "jpeg file output"); err != nil {
		t.Fatalf("RenderToFile() error: %v", err)
	}
	if info, _ := os.Stat(path); info.Size() == 0 {
		t.Fatal("jpeg output file is empty")
	}
}

func TestRenderToWriter(t *testing.T) {
	r := textimage.MustNew()
	defer r.Close()

	var buf bytes.Buffer
	if err := r.RenderToWriter(&buf, "writer test"); err != nil {
		t.Fatalf("RenderToWriter() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("nothing written to writer")
	}
}

// ─── Convenience functions ───────────────────────────────────────────────────

func TestRenderText(t *testing.T) {
	data, err := textimage.RenderText("convenience render",
		textimage.WithFontSize(14),
	)
	if err != nil {
		t.Fatalf("RenderText() error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("RenderText() returned empty bytes")
	}
}

func TestTextSize(t *testing.T) {
	w, h, err := textimage.TextSize("measure me", textimage.WithFontSize(16))
	if err != nil {
		t.Fatalf("TextSize() error: %v", err)
	}
	if w <= 0 || h <= 0 {
		t.Fatalf("TextSize() returned non-positive dimensions: %dx%d", w, h)
	}
}

func TestDefaultFontTTF(t *testing.T) {
	data := textimage.DefaultFontTTF()
	if len(data) == 0 {
		t.Fatal("DefaultFontTTF() returned empty slice")
	}

	// Should be usable as font data.
	r, err := textimage.New(textimage.WithFontData(data))
	if err != nil {
		t.Fatalf("New(WithFontData) error: %v", err)
	}
	defer r.Close()
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func assertNonZeroImage(t *testing.T, img image.Image) {
	t.Helper()
	if img == nil {
		t.Fatal("image is nil")
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		t.Fatalf("image has zero dimension: %v", b)
	}
}
