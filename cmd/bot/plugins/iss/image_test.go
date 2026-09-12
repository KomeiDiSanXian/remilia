package iss

import (
	"bytes"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/satutil"
)

// dumpPreview 在设置 SATCARD_PREVIEW_DIR 时把渲染结果落盘，便于人工查看排版。
func dumpPreview(t *testing.T, name string, data []byte) {
	t.Helper()
	dir := os.Getenv("SATCARD_PREVIEW_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建预览目录失败: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入预览失败: %v", err)
	}
	t.Logf("预览已写入: %s", path)
}

// syntheticHistory 构造一段带缓慢衰减的高度历史（每 5 分钟一条）。
func syntheticHistory(now time.Time, count int) []AltRecord {
	out := make([]AltRecord, 0, count)
	for i := range count {
		// 24 小时累积衰减约 0.1 km，叠加一个小的周期波动
		decay := -0.1 * float64(count-1-i) / float64(max(count-1, 1))
		wave := 0.25 * math.Sin(float64(i)/7)
		out = append(out, AltRecord{
			Time:     now.Add(-time.Duration(count-1-i) * 5 * time.Minute),
			Altitude: 419.8 + decay + wave,
		})
	}
	return out
}

func TestIssGroundTrackSpansOneOrbit(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	pos := &IssPosition{Latitude: 24.7, Longitude: -78.3, Altitude: 419.5, Velocity: 27580}

	track := issGroundTrack(pos, now)
	if len(track) < 80 {
		t.Fatalf("单圈轨迹点数 = %d, want >= 80", len(track))
	}

	// 轨迹应跨越约一个轨道周期，且当前时刻位于中点附近
	period := satutil.OrbitalPeriod(pos.Altitude)
	span := track[len(track)-1].Time.Sub(track[0].Time).Minutes()
	if math.Abs(span-period) > 3 {
		t.Errorf("轨迹时间跨度 = %.1f min, want %.1f±3", span, period)
	}
	if track[0].Time.After(now) || track[len(track)-1].Time.Before(now) {
		t.Errorf("轨迹未覆盖当前时刻: %s ~ %s", track[0].Time, track[len(track)-1].Time)
	}
	mid := track[len(track)/2].Time
	if math.Abs(mid.Sub(now).Minutes()) > 2 {
		t.Errorf("轨迹中点 %s 偏离当前时刻 %s 过多", mid, now)
	}

	maxAbs := 0.0
	for _, p := range track {
		maxAbs = math.Max(maxAbs, math.Abs(p.Lat))
		if math.Abs(p.Lat) > issInclinationDeg+0.01 {
			t.Fatalf("纬度 %.3f 超出倾角 %.2f", p.Lat, issInclinationDeg)
		}
	}
	if maxAbs < issInclinationDeg-1 {
		t.Errorf("最大纬度 %.2f，轨迹未覆盖转向点", maxAbs)
	}
}

func TestIssGroundTrackDegenerateAltitude(t *testing.T) {
	if got := issGroundTrack(&IssPosition{Altitude: -satutil.EarthRadiusKm}, time.Now()); got != nil {
		t.Errorf("非法高度应返回 nil, got %d 点", len(got))
	}
}

func TestRenderCardProducesValidPNG(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	history := syntheticHistory(now, 288) // 24 小时
	pos := &IssPosition{
		Latitude: 24.7, Longitude: -78.3, Altitude: 419.5,
		Velocity: 27580, Visibility: "daylight", Timestamp: now,
	}
	astros := []string{"Oleg Kononenko", "Nikolai Chub", "Tracy Dyson", "Matthew Dominick"}

	data := cardData{
		Now:         now,
		Pos:         pos,
		Astros:      astros,
		AstroCount:  7,
		Series:      history,
		Trend:       computeTrend(history),
		Inclination: issInclinationDeg,
		PeriodMin:   satutil.OrbitalPeriod(pos.Altitude),
		FootprintKm: satutil.FootprintDiameter(pos.Altitude),
		Track:       issGroundTrack(pos, now),
	}

	pngBytes, err := renderCard(data)
	if err != nil {
		t.Fatalf("renderCard: %v", err)
	}
	if len(pngBytes) == 0 {
		t.Fatal("渲染结果为空")
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("解码 PNG: %v", err)
	}
	if got := img.Bounds().Dx(); got != cardWidth {
		t.Errorf("卡片宽度 = %d, want %d", got, cardWidth)
	}
	if got := img.Bounds().Dy(); got < 900 {
		t.Errorf("卡片高度 = %d，明显偏小，可能缺少内容块", got)
	}
	t.Logf("ISS 卡片: %dx%d, %d bytes", img.Bounds().Dx(), img.Bounds().Dy(), len(pngBytes))
	dumpPreview(t, "iss_card.png", pngBytes)
}

func TestRenderCardEclipsedWithoutHistory(t *testing.T) {
	// 地影 + 无历史 + 无航天员列表：应降级渲染而非报错
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	pos := &IssPosition{Latitude: -33.3, Longitude: 151.2, Altitude: 415, Velocity: 27600, Visibility: "eclipsed", Timestamp: now}
	data := cardData{
		Now: now, Pos: pos, Inclination: issInclinationDeg,
		PeriodMin:   satutil.OrbitalPeriod(pos.Altitude),
		FootprintKm: satutil.FootprintDiameter(pos.Altitude),
		Track:       issGroundTrack(pos, now),
	}
	pngBytes, err := renderCard(data)
	if err != nil {
		t.Fatalf("降级渲染失败: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(pngBytes)); err != nil {
		t.Fatalf("解码 PNG: %v", err)
	}
}

func TestHistoryCaption(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	series := syntheticHistory(now, 288)
	caption := historyCaption(series, Trend{MinAlt: 418.5, MaxAlt: 421.2})
	if !bytes.Contains([]byte(caption), []byte("近 23.9 小时")) {
		t.Errorf("说明文字 = %q", caption)
	}
	if !bytes.Contains([]byte(caption), []byte("418–421 km")) {
		t.Errorf("说明文字缺少高度区间: %q", caption)
	}
	if got := historyCaption(nil, Trend{}); got != "" {
		t.Errorf("空序列说明 = %q, want \"\"", got)
	}
	short := syntheticHistory(now, 6) // 25 分钟
	if got := historyCaption(short, Trend{MinAlt: 419, MaxAlt: 420.5}); !bytes.Contains([]byte(got), []byte("分钟")) {
		t.Errorf("短序列说明 = %q", got)
	}
}

func TestToChartPoints(t *testing.T) {
	now := time.Now()
	series := []AltRecord{{Time: now, Altitude: 419.5}, {Time: now.Add(time.Minute), Altitude: 419.7}}
	points := toChartPoints(series)
	if len(points) != 2 {
		t.Fatalf("点数 = %d, want 2", len(points))
	}
	if points[1].Value != 419.7 || !points[1].Time.Equal(now.Add(time.Minute)) {
		t.Errorf("转换结果错误: %+v", points[1])
	}
}

func TestFormatISSTextContainsKeyFields(t *testing.T) {
	pos := &IssPosition{Latitude: 24.7, Longitude: -78.3, Altitude: 419.5, Velocity: 27580, Visibility: "eclipsed"}
	text := formatISSText(pos, []string{"Oleg Kononenko"}, 7, Trend{Slope: -0.05, MinAlt: 418, MaxAlt: 421})
	for _, want := range []string{"[ISS] 国际空间站", "高度: 419.5 km", "光照: [地影]", "Oleg Kononenko"} {
		if !bytes.Contains([]byte(text), []byte(want)) {
			t.Errorf("纯文本输出缺少 %q:\n%s", want, text)
		}
	}
}
