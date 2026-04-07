package textimage

import (
	"image/color"
	"testing"
)

// ─── Badge tests ──────────────────────────────────────────────────────────────

func TestAddBadgeRow_Basic(t *testing.T) {
	c, err := NewCanvas(400)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	items := []BadgeItem{
		{Text: "Online", BgColor: color.RGBA{R: 50, G: 180, B: 90, A: 255}},
		{Text: "v1.0.0", BgColor: color.RGBA{R: 90, G: 90, B: 200, A: 255}},
	}
	if err := c.AddBadgeRow(items); err != nil {
		t.Fatalf("AddBadgeRow: %v", err)
	}
	img := c.Result()
	if img == nil || img.Bounds().Empty() {
		t.Fatal("expected non-empty result image")
	}
}

func TestAddBadgeRow_Empty(t *testing.T) {
	c, _ := NewCanvas(400)
	// Empty items should be a no-op
	if err := c.AddBadgeRow(nil); err != nil {
		t.Fatalf("AddBadgeRow(nil) should not error: %v", err)
	}
}

func TestAddBadge_Single(t *testing.T) {
	c, _ := NewCanvas(300)
	item := BadgeItem{Text: "Beta", BgColor: color.RGBA{R: 200, G: 80, B: 40, A: 255}}
	if err := c.AddBadge(item); err != nil {
		t.Fatalf("AddBadge: %v", err)
	}
}

func TestAddBadgeRow_NilColors(t *testing.T) {
	c, _ := NewCanvas(400)
	// nil BgColor/TextColor should use defaults
	items := []BadgeItem{
		{Text: "Default"},
	}
	if err := c.AddBadgeRow(items); err != nil {
		t.Fatalf("AddBadgeRow with nil colors: %v", err)
	}
}

func TestAddBadgeRow_Options(t *testing.T) {
	c, _ := NewCanvas(500)
	items := []BadgeItem{
		{Text: "A", BgColor: color.RGBA{R: 100, G: 100, B: 200, A: 255}},
		{Text: "B", BgColor: color.RGBA{R: 200, G: 100, B: 100, A: 255}},
	}
	if err := c.AddBadgeRow(items,
		WithBadgeFontSize(16),
		WithBadgeRadius(6),
		WithBadgePadding(12, 6),
		WithBadgeGap(10),
		WithBadgeRowPadding(8, 4),
	); err != nil {
		t.Fatalf("AddBadgeRow with options: %v", err)
	}
	img := c.Result()
	if img.Bounds().Dy() <= 0 {
		t.Fatal("expected positive height after AddBadgeRow")
	}
}

// ─── BarChart tests ───────────────────────────────────────────────────────────

func TestAddBarChart_Basic(t *testing.T) {
	c, err := NewCanvas(500)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	items := []BarItem{
		{Label: "CPU", Value: 72, Color: color.RGBA{R: 80, G: 200, B: 120, A: 255}},
		{Label: "Memory", Value: 58},
		{Label: "Disk", Value: 90, Color: color.RGBA{R: 220, G: 80, B: 80, A: 255}},
	}
	if err := c.AddBarChart(items, 100); err != nil {
		t.Fatalf("AddBarChart: %v", err)
	}
	img := c.Result()
	if img == nil || img.Bounds().Empty() {
		t.Fatal("expected non-empty result image")
	}
}

func TestAddBarChart_Empty(t *testing.T) {
	c, _ := NewCanvas(400)
	if err := c.AddBarChart(nil, 100); err != nil {
		t.Fatalf("AddBarChart(nil) should not error: %v", err)
	}
}

func TestAddBarChart_AutoMaxValue(t *testing.T) {
	c, _ := NewCanvas(400)
	items := []BarItem{
		{Label: "A", Value: 30},
		{Label: "B", Value: 70},
	}
	// maxValue=0 should auto-detect 70 as max
	if err := c.AddBarChart(items, 0); err != nil {
		t.Fatalf("AddBarChart with auto maxValue: %v", err)
	}
	img := c.Result()
	if img.Bounds().Empty() {
		t.Fatal("expected non-empty image")
	}
}

func TestAddBarChart_ZeroValues(t *testing.T) {
	c, _ := NewCanvas(400)
	items := []BarItem{
		{Label: "Zero", Value: 0},
		{Label: "Full", Value: 100},
	}
	if err := c.AddBarChart(items, 100); err != nil {
		t.Fatalf("AddBarChart with zero value item: %v", err)
	}
}

func TestAddBarChart_Options(t *testing.T) {
	c, _ := NewCanvas(500)
	items := []BarItem{
		{Label: "X", Value: 50},
	}
	if err := c.AddBarChart(items, 100,
		WithBarHeight(24),
		WithBarLabelWidth(80),
		WithBarValueWidth(60),
		WithBarSpacing(8),
		WithBarFontSize(14),
		WithBarPaddingX(20),
		WithBarShowValue(true),
		WithBarTrackColor(color.RGBA{R: 30, G: 30, B: 40, A: 200}),
		WithBarDefaultColor(color.RGBA{R: 100, G: 180, B: 255, A: 255}),
	); err != nil {
		t.Fatalf("AddBarChart with options: %v", err)
	}
}

func TestAddBarChart_NoValue(t *testing.T) {
	c, _ := NewCanvas(400)
	items := []BarItem{{Label: "X", Value: 50}}
	if err := c.AddBarChart(items, 100, WithBarShowValue(false)); err != nil {
		t.Fatalf("AddBarChart(showValue=false): %v", err)
	}
}

// ─── 综合 canvas 测试 ─────────────────────────────────────────────────────────

func TestCanvas_BadgeAndChart_Combined(t *testing.T) {
	c, err := NewCanvas(600, WithBgColor(color.RGBA{R: 30, G: 30, B: 40, A: 255}))
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}

	_ = c.AddText("System Status", WithFontColor(color.White))
	c.AddDivider()
	_ = c.AddBadgeRow([]BadgeItem{
		{Text: "Online", BgColor: color.RGBA{R: 50, G: 180, B: 90, A: 255}},
		{Text: "v2.0", BgColor: color.RGBA{R: 90, G: 90, B: 200, A: 255}},
	})
	_ = c.AddBarChart([]BarItem{
		{Label: "CPU", Value: 65},
		{Label: "RAM", Value: 42},
	}, 100, WithBarFontColor(color.White))

	img := c.Result()
	if img.Bounds().Empty() {
		t.Fatal("expected non-empty combined image")
	}
}
