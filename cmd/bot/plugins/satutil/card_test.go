package satutil

import (
	"image/color"
	"math"
	"testing"
	"time"
)

func TestStatPanelDimensions(t *testing.T) {
	for _, theme := range []CardTheme{ISSTheme(), CSSTheme()} {
		img := StatPanel(theme, StatPanelSpec{
			Width: 232, Height: 150, Title: "轨道周期",
			Value: "92.6", Sub: "分钟 / 圈", Arc: 0.5, ArcColor: theme.Accent,
		})
		if got, want := img.Bounds().Dx(), 232*panelScale; got != want {
			t.Errorf("面板宽度 = %d, want %d", got, want)
		}
		if got, want := img.Bounds().Dy(), 150*panelScale; got != want {
			t.Errorf("面板高度 = %d, want %d", got, want)
		}
	}
}

func TestStatPanelWithoutArc(t *testing.T) {
	theme := ISSTheme()
	img := StatPanel(theme, StatPanelSpec{Width: 120, Height: 80, Value: "7.66", Arc: -1})
	if img.Bounds().Dx() != 240 || img.Bounds().Dy() != 160 {
		t.Errorf("无仪表面板尺寸 = %v", img.Bounds())
	}
}

func TestGroundTrackPanelDimensions(t *testing.T) {
	theme := ISSTheme()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	track := OrbitGroundTrack(now, 20, -30, 420, 51.64,
		now.Add(-45*time.Minute), now.Add(45*time.Minute), time.Minute, true)

	img := GroundTrackPanel(theme, MapSpec{
		Width: 500, Height: 250, Track: track, Now: now,
		Lat: 20, Lon: -30, FootprintDeg: 20,
		Title: "地面轨迹", Note: "单圈示意", Caption: "圆轨道近似",
	})
	if img.Bounds().Dx() != 1000 || img.Bounds().Dy() != 500 {
		t.Fatalf("轨迹图尺寸 = %v, want 1000x500", img.Bounds())
	}
}

func TestGroundTrackPanelWrapsAntimeridian(t *testing.T) {
	theme := ISSTheme()
	now := time.Now()
	// 经度跨越 ±180°：不应panic，且需正常输出
	track := []TrackPoint{
		{Time: now, Lat: 10, Lon: 179},
		{Time: now.Add(time.Minute), Lat: 12, Lon: -179},
		{Time: now.Add(2 * time.Minute), Lat: 14, Lon: -176},
	}
	img := GroundTrackPanel(theme, MapSpec{Width: 300, Height: 160, Track: track, Lat: 14, Lon: -176})
	if img == nil || img.Bounds().Dx() != 600 {
		t.Fatalf("跨经度 180° 的轨迹图异常: %v", img.Bounds())
	}
}

func TestGroundTrackPanelNoTrack(t *testing.T) {
	img := GroundTrackPanel(CSSTheme(), MapSpec{Width: 300, Height: 160, Lat: 0, Lon: 0})
	if img == nil {
		t.Fatal("空轨迹也应返回面板底图")
	}
}

func TestAltitudeChart(t *testing.T) {
	theme := CSSTheme()
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	points := make([]ChartPoint, 0, 24)
	for i := range 24 {
		points = append(points, ChartPoint{
			Time:  base.Add(time.Duration(i) * 4 * time.Minute),
			Value: 390 + 4*float64(i%7) - 0.05*float64(i),
		})
	}
	img := AltitudeChart(theme, ChartSpec{
		Width: 500, Height: 220, Points: points,
		Title: "轨道高度预报", Unit: "km · UTC", Now: base.Add(50 * time.Minute),
		NowLabel: "现在", Caption: "基于 CMSE 7 天轨道预报",
	})
	if img == nil {
		t.Fatal("曲线图不应为 nil")
	}
	if img.Bounds().Dx() != 1000 || img.Bounds().Dy() != 440 {
		t.Fatalf("曲线图尺寸 = %v, want 1000x440", img.Bounds())
	}
}

func TestAltitudeChartRequiresTwoPoints(t *testing.T) {
	if img := AltitudeChart(ISSTheme(), ChartSpec{Width: 300, Height: 200, Points: nil}); img != nil {
		t.Error("少于两个数据点应返回 nil")
	}
	one := []ChartPoint{{Time: time.Now(), Value: 400}}
	if img := AltitudeChart(ISSTheme(), ChartSpec{Width: 300, Height: 200, Points: one}); img != nil {
		t.Error("单个数据点应返回 nil")
	}
}

func TestAltitudeChartFlatSeries(t *testing.T) {
	base := time.Now()
	points := []ChartPoint{{Time: base, Value: 420}, {Time: base.Add(time.Minute), Value: 420}}
	img := AltitudeChart(ISSTheme(), ChartSpec{Width: 300, Height: 200, Points: points, Now: base})
	if img == nil {
		t.Fatal("恒定高度序列不应返回 nil")
	}
}

func TestChipWallWrapsAndFits(t *testing.T) {
	theme := ISSTheme()
	chips := []string{"Oleg Kononenko", "Nikolai Chub", "Tracy Dyson",
		"Matthew Dominick", "Michael Barratt", "Jeanette Epps", "Alexander Grebenkin"}

	img := ChipWall(theme, ChipSpec{Width: 400, Title: "在轨航天员 · 7 人", Chips: chips})
	if img.Bounds().Dx() != 800 {
		t.Fatalf("标签墙宽度 = %d, want 800", img.Bounds().Dx())
	}
	singleRow := ChipWall(theme, ChipSpec{Width: 400, Chips: []string{"A"}})
	if img.Bounds().Dy() <= singleRow.Bounds().Dy() {
		t.Errorf("多标签换行后应更高: %d <= %d", img.Bounds().Dy(), singleRow.Bounds().Dy())
	}
}

func TestChipWallSkipsBlankChips(t *testing.T) {
	img := ChipWall(ISSTheme(), ChipSpec{Width: 300, Chips: []string{"", "   "}})
	if img == nil || img.Bounds().Dx() != 600 {
		t.Fatal("空白标签应被忽略但仍返回面板")
	}
}

func TestSplitTrackAddsBoundaryPoints(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	track := []TrackPoint{
		{Time: now, Lat: 10, Lon: 178},
		{Time: now.Add(time.Minute), Lat: 12, Lon: -179},
		{Time: now.Add(2 * time.Minute), Lat: 14, Lon: -176},
	}
	segs := splitTrack(track)
	if len(segs) != 2 {
		t.Fatalf("分段数 = %d, want 2", len(segs))
	}

	last := segs[0][len(segs[0])-1]
	if last.Lon != 180 {
		t.Errorf("前一段应终止于经度 180°, got %.2f", last.Lon)
	}
	first := segs[1][0]
	if first.Lon != -180 {
		t.Errorf("后一段应从经度 −180° 开始, got %.2f", first.Lon)
	}
	if math.Abs(last.Lat-first.Lat) > 1e-9 {
		t.Errorf("两侧交点纬度应一致: %.4f vs %.4f", last.Lat, first.Lat)
	}
	// 178 → −179 跨越 +180°：插值纬度应在 10~12 之间
	if last.Lat < 10 || last.Lat > 12 {
		t.Errorf("交点纬度 %.3f 不在 [10, 12] 区间", last.Lat)
	}
	if !last.Time.Equal(first.Time) {
		t.Errorf("两侧交点时刻应一致: %s vs %s", last.Time, first.Time)
	}
}

func TestSplitTrackWithoutCrossing(t *testing.T) {
	now := time.Now()
	track := []TrackPoint{
		{Time: now, Lat: 1, Lon: -10},
		{Time: now.Add(time.Minute), Lat: 2, Lon: 0},
		{Time: now.Add(2 * time.Minute), Lat: 3, Lon: 10},
	}
	segs := splitTrack(track)
	if len(segs) != 1 || len(segs[0]) != 3 {
		t.Fatalf("无跨经度时不应分段: %d 段", len(segs))
	}
	if got := splitTrack(nil); got != nil {
		t.Errorf("空轨迹应返回 nil, got %v", got)
	}
}

func TestAntimeridianCrossBothDirections(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// 向东：178 → −178（跨 +180°）
	lat, at := antimeridianCross(TrackPoint{Time: now, Lat: 0, Lon: 178}, TrackPoint{Time: now.Add(time.Minute), Lat: 2, Lon: -178})
	if math.Abs(lat-1) > 1e-6 {
		t.Errorf("向东跨经度交点纬度 = %.4f, want 1", lat)
	}
	if !at.Equal(now.Add(30 * time.Second)) {
		t.Errorf("向东跨经度交点时刻 = %s", at)
	}

	// 向西：−178 → 178（跨 −180°）
	lat, at = antimeridianCross(TrackPoint{Time: now, Lat: 0, Lon: -178}, TrackPoint{Time: now.Add(time.Minute), Lat: 2, Lon: 178})
	if math.Abs(lat-1) > 1e-6 {
		t.Errorf("向西跨经度交点纬度 = %.4f, want 1", lat)
	}
	if !at.Equal(now.Add(30 * time.Second)) {
		t.Errorf("向西跨经度交点时刻 = %s", at)
	}
}

func TestIconsDimensions(t *testing.T) {
	if got := IconSpaceStation(64, color.White).Bounds().Dx(); got != 128 {
		t.Errorf("空间站图标宽度 = %d, want 128", got)
	}
	if got := IconSun(48, color.White).Bounds().Dy(); got != 96 {
		t.Errorf("太阳图标高度 = %d, want 96", got)
	}
	if got := IconMoon(32, color.White).Bounds().Dx(); got != 64 {
		t.Errorf("月亮图标宽度 = %d, want 64", got)
	}
}

func TestCardBackgroundGradient(t *testing.T) {
	for _, theme := range []CardTheme{ISSTheme(), CSSTheme()} {
		img := CardBackground(theme, 120, 240)
		if img.Bounds().Dx() != 120 || img.Bounds().Dy() != 240 {
			t.Fatalf("背景尺寸 = %v", img.Bounds())
		}
		top := color.RGBAModel.Convert(img.At(60, 5)).(color.RGBA)
		bottom := color.RGBAModel.Convert(img.At(60, 235)).(color.RGBA)
		if top == bottom {
			t.Errorf("背景上下应存在渐变差异: top=%v bottom=%v", top, bottom)
		}
	}
}

func TestSmoothSamples(t *testing.T) {
	xs := []float64{0, 10, 20, 30}
	ys := []float64{0, 5, -5, 0}
	sx, sy := smoothSamples(xs, ys, 8)
	if len(sx) != len(sy) {
		t.Fatalf("采样长度不一致: %d vs %d", len(sx), len(sy))
	}
	if len(sx) != (len(xs)-1)*8+1 {
		t.Errorf("采样点数 = %d, want %d", len(sx), (len(xs)-1)*8+1)
	}
	if sx[0] != xs[0] || sy[0] != ys[0] {
		t.Error("样条应经过首点")
	}
	if sx[len(sx)-1] != xs[len(xs)-1] || sy[len(sy)-1] != ys[len(ys)-1] {
		t.Error("样条应经过末点")
	}
	// 少于 3 个点时原样返回
	if bx, by := smoothSamples([]float64{1, 2}, []float64{3, 4}, 8); len(bx) != 2 || len(by) != 2 {
		t.Error("少于 3 点应原样返回")
	}
}

func TestCatmullRomEndpoints(t *testing.T) {
	// u=0 取 p1，u=1 取 p2
	if got := catmullRom(1, 5, 9, 13, 0); got != 5 {
		t.Errorf("u=0 应为 p1=5, got %.3f", got)
	}
	if got := catmullRom(1, 5, 9, 13, 1); got != 9 {
		t.Errorf("u=1 应为 p2=9, got %.3f", got)
	}
}

func TestCoordinateLabels(t *testing.T) {
	lonCases := map[float64]string{0: "0°", 120: "120°E", -120: "120°W", 180: "180°", -180: "180°"}
	for in, want := range lonCases {
		if got := lonLabel(in); got != want {
			t.Errorf("lonLabel(%.0f) = %q, want %q", in, got, want)
		}
	}
	latCases := map[float64]string{0: "0°", 60: "60°N", -30: "30°S"}
	for in, want := range latCases {
		if got := latLabel(in); got != want {
			t.Errorf("latLabel(%.0f) = %q, want %q", in, got, want)
		}
	}
}
