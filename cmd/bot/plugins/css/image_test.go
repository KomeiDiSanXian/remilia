package css

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

// syntheticOEM 构造一条圆轨道（高度 h、倾角 incDeg、可选线性衰减）的 OEM
// 状态矢量序列，用于在不访问网络的前提下验证位置解算与卡片渲染。
//
// decayKmPerDay 为轨道半径的线性变化率（负值表示衰减）。
func syntheticOEM(start time.Time, count int, step time.Duration, h, incDeg, decayKmPerDay float64) *OEMEphemeris {
	oem := &OEMEphemeris{
		CreationDate: start,
		StartTime:    start,
		StopTime:     start.Add(time.Duration(count-1) * step),
	}
	inc := incDeg * math.Pi / 180

	for i := range count {
		elapsed := time.Duration(i) * step
		radius := satutil.EarthRadiusKm + h + decayKmPerDay*elapsed.Hours()/24
		speed := math.Sqrt(satutil.EarthGM / radius)
		meanMotion := 2 * math.Pi / (2 * math.Pi * math.Sqrt(radius*radius*radius/satutil.EarthGM))
		theta := meanMotion * elapsed.Seconds()
		sinT, cosT := math.Sin(theta), math.Cos(theta)

		oem.Vectors = append(oem.Vectors, StateVector{
			Time: start.Add(elapsed),
			X:    radius * cosT,
			Y:    radius * sinT * math.Cos(inc),
			Z:    radius * sinT * math.Sin(inc),
			Vx:   -speed * sinT,
			Vy:   speed * cosT * math.Cos(inc),
			Vz:   speed * cosT * math.Sin(inc),
		})
	}
	return oem
}

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

func TestComputePositionOnSyntheticOrbit(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	const h, inc = 390.0, 41.5
	oem := syntheticOEM(start, 300, 4*time.Minute, h, inc, 0)

	// 特意选取落在两个状态矢量之间的时刻（间隔 4 分钟）：
	// 线性插值在此处的弦高误差最大（可达约 60 km），Hermite 插值应保持在几公里内。
	for _, offset := range []time.Duration{30 * time.Minute, 2 * time.Hour, 5*time.Hour + 2*time.Minute, 18 * time.Hour} {
		lat, lng, alt, ok := computePosition(oem, start.Add(offset))
		if !ok {
			t.Fatalf("t+%s 位置解算失败", offset)
		}
		// 圆轨道在 WGS84 椭球上的大地高度随纬度有 ±8 km 起伏，
		// 因此容差取 10 km；线性插值的 60 km 误差会显著超出该范围。
		if math.Abs(alt-h) > 10 {
			t.Errorf("t+%s 高度 = %.2f km, want %.0f±10（插值精度不足？）", offset, alt, h)
		}
		if math.Abs(lat) > inc+1 {
			t.Errorf("t+%s 纬度 %.2f 超出倾角 %.1f", offset, lat, inc)
		}
		if lng < -180 || lng > 180 {
			t.Errorf("t+%s 经度 %.2f 越界", offset, lng)
		}
	}

	wantSpeed := math.Sqrt(satutil.EarthGM / (satutil.EarthRadiusKm + h))
	if got := computeSpeed(oem, start.Add(30*time.Minute)); math.Abs(got-wantSpeed) > 0.01 {
		t.Errorf("速度 = %.4f km/s, want %.4f", got, wantSpeed)
	}
	if got := computeInclination(oem, start.Add(5*time.Hour)); math.Abs(got-inc) > 1e-6 {
		t.Errorf("倾角 = %.6f, want %.6f", got, inc)
	}
}

func TestHermiteInterpolationAccuracy(t *testing.T) {
	// 直接对比 Hermite 与真实圆轨道在同一时刻的位置半径
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	const h = 390.0
	oem := syntheticOEM(start, 4, 4*time.Minute, h, 41.5, 0)

	mid := start.Add(2 * time.Minute) // 恰好落在两个状态矢量正中间
	v0, v1, ratio, dur := findInterval(oem, mid)
	if dur <= 0 || ratio < 0.4 || ratio > 0.6 {
		t.Fatalf("区间定位异常: ratio=%.3f dur=%s", ratio, dur)
	}

	// 圆轨道半径不变，因此插值半径应≈真实半径
	dt := dur.Seconds()
	x, _ := hermite(v0.X, v0.Vx, v1.X, v1.Vx, dt, ratio)
	y, _ := hermite(v0.Y, v0.Vy, v1.Y, v1.Vy, dt, ratio)
	z, _ := hermite(v0.Z, v0.Vz, v1.Z, v1.Vz, dt, ratio)
	gotR := math.Sqrt(x*x + y*y + z*z)
	wantR := float64(satutil.EarthRadiusKm) + h
	if math.Abs(gotR-wantR) > 0.5 {
		t.Errorf("Hermite 插值半径 = %.3f km, want %.3f±0.5", gotR, wantR)
	}

	// 同一位置若用线性插值，半径误差应有数十公里——验证测试确实覆盖了该缺陷
	lx := v0.X + (v1.X-v0.X)*ratio
	ly := v0.Y + (v1.Y-v0.Y)*ratio
	lz := v0.Z + (v1.Z-v0.Z)*ratio
	linR := math.Sqrt(lx*lx + ly*ly + lz*lz)
	if wantR-linR < 20 {
		t.Errorf("线性插值半径误差 = %.2f km，测试样本未覆盖弦高缺陷", wantR-linR)
	}
}

func TestComputePositionOutsideCoverage(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	oem := syntheticOEM(start, 60, 4*time.Minute, 390, 41.5, 0)
	if _, _, _, ok := computePosition(oem, start.Add(-10*time.Hour)); ok {
		t.Error("超出数据范围时不应解算成功")
	}
}

func TestComputeDecayTrendTracksSyntheticDecay(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	const h, decay = 390.0, -0.5                                    // km/天
	oem := syntheticOEM(start, 1500, 4*time.Minute, h, 41.5, decay) // 约 4.2 天
	now := start.Add(48 * time.Hour)

	trend, ok := computeDecayTrend(oem, now, 24*time.Hour)
	if !ok {
		t.Fatal("长期趋势拟合失败")
	}
	if math.Abs(trend.Slope-decay) > 0.05 {
		t.Errorf("拟合衰减率 = %.4f km/天, want %.2f±0.05", trend.Slope, decay)
	}
}

func TestComputeDecayTrendFallsBackWithoutData(t *testing.T) {
	start := time.Now()
	oem := syntheticOEM(start, 2, 4*time.Minute, 390, 41.5, 0)
	if _, ok := computeDecayTrend(oem, start, 24*time.Hour); ok {
		t.Error("有效样本不足时不应报告趋势")
	}
	empty := &OEMEphemeris{}
	if _, ok := computeDecayTrend(empty, start, 24*time.Hour); ok {
		t.Error("空数据不应报告趋势")
	}
}

func TestRadialWindowAvoidsEllipsoidRipple(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	oem := syntheticOEM(start, 400, time.Minute, 390, 51.64, 0)
	now := start.Add(3 * time.Hour)
	from, to := now.Add(-50*time.Minute), now.Add(95*time.Minute)

	geodetic := computeAltWindow(oem, from, to)
	radial := computeRadialWindow(oem, from, to)
	if len(geodetic) < 20 || len(radial) < 20 {
		t.Fatalf("序列点数不足: geodetic=%d radial=%d", len(geodetic), len(radial))
	}

	geoSpan := spread(geodetic)
	radSpan := spread(radial)
	if geoSpan <= radSpan {
		t.Errorf("大地高度跨度(%.2f km)应大于径向高度跨度(%.2f km)", geoSpan, radSpan)
	}
	if radSpan > 1 {
		t.Errorf("圆轨道径向高度跨度 = %.3f km, 应接近 0", radSpan)
	}
}

// spread 返回序列的极差。
func spread(points []AltPoint) float64 {
	if len(points) == 0 {
		return 0
	}
	mn, mx := points[0].Altitude, points[0].Altitude
	for _, p := range points {
		mn = math.Min(mn, p.Altitude)
		mx = math.Max(mx, p.Altitude)
	}
	return mx - mn
}

func TestCardTrendUsesRadialExtremesAndLongTermSlope(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	const h, decay = 388.0, -0.4
	oem := syntheticOEM(start, 1500, 4*time.Minute, h, 41.5, decay)
	now := start.Add(48 * time.Hour)

	trend := cardTrend(oem, now)
	if trend.MaxAlt-trend.MinAlt > 5 {
		t.Errorf("近/远地点区间 = %.2f ~ %.2f, 圆轨道应接近 %.0f", trend.MinAlt, trend.MaxAlt, h)
	}
	if math.Abs(trend.MinAlt-h) > 5 || math.Abs(trend.MaxAlt-h) > 5 {
		t.Errorf("近/远地点 %.2f ~ %.2f 偏离 %.0f 过多", trend.MinAlt, trend.MaxAlt, h)
	}
	if math.Abs(trend.Slope-decay) > 0.05 {
		t.Errorf("变化率 = %.4f km/天, want %.2f", trend.Slope, decay)
	}
}

func TestComputeGroundTrackNormalized(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	oem := syntheticOEM(start, 400, time.Minute, 420, 51.64, 0)
	now := start.Add(3 * time.Hour)
	track := computeGroundTrack(oem, now.Add(-50*time.Minute), now.Add(95*time.Minute))
	if len(track) < 20 {
		t.Fatalf("轨迹点数 = %d, want >= 20", len(track))
	}
	for _, p := range track {
		if p.Lon < -180 || p.Lon >= 180 {
			t.Fatalf("经度未归一化: %.3f", p.Lon)
		}
		if math.Abs(p.Lat) > 52 {
			t.Fatalf("纬度越界: %.3f", p.Lat)
		}
	}
	for i := 1; i < len(track); i++ {
		if !track[i].Time.After(track[i-1].Time) {
			t.Fatalf("轨迹时间未递增: 第 %d 点", i)
		}
	}
}

func TestComputeEclipsePhaseWithinOneOrbit(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	oem := syntheticOEM(start, 400, time.Minute, 420, 51.64, 0)
	now := start.Add(3 * time.Hour)
	period := satutil.OrbitalPeriod(420)

	inEclipse, remaining, known := computeEclipsePhase(oem, now, time.Duration(period)*time.Minute)
	if !known {
		t.Fatalf("一个轨道周期内应出现地影/光照切换 (地影=%v 剩余=%s)", inEclipse, remaining)
	}
	if remaining <= 0 || remaining > time.Duration(period*2)*time.Minute {
		t.Errorf("相位剩余时长异常: %s", remaining)
	}
}

func TestRenderCardProducesValidPNG(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	const h, inc, decay = 388.5, 41.47, -0.3
	oem := syntheticOEM(start, 1500, 4*time.Minute, h, inc, decay)
	now := start.Add(48 * time.Hour)

	lat, lng, alt, ok := computePosition(oem, now)
	if !ok {
		t.Fatal("位置解算失败")
	}
	windowStart, windowEnd := now.Add(-50*time.Minute), now.Add(95*time.Minute)
	series := computeAltWindow(oem, windowStart, windowEnd)
	inEclipse, remaining, known := computeEclipsePhase(oem, now, time.Hour)

	data := cardData{
		Now:              now,
		Lat:              lat,
		Lon:              lng,
		Alt:              alt,
		Speed:            computeSpeed(oem, now),
		Inclination:      computeInclination(oem, now),
		Eclipse:          inEclipse,
		EclipseRemaining: remaining,
		EclipseKnown:     known,
		PeriodMin:        satutil.OrbitalPeriod(alt),
		FootprintKm:      satutil.FootprintDiameter(alt),
		Series:           series,
		Trend:            cardTrend(oem, now),
		Track:            computeGroundTrack(oem, windowStart, windowEnd),
		OEM:              oem,
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
	t.Logf("CSS 卡片: %dx%d, %d bytes", img.Bounds().Dx(), img.Bounds().Dy(), len(pngBytes))
	dumpPreview(t, "css_card.png", pngBytes)
}

func TestRenderCardWithCachedOEM(t *testing.T) {
	// 使用本仓 data/css 下的真实 OEM 缓存做一次端到端渲染；缓存不存在或已过期时跳过。
	cacheDir := filepath.Join("..", "..", "..", "..", "data", "css")
	oem := LoadCache(cacheDir)
	if oem == nil {
		t.Skipf("无本地 OEM 缓存: %s", cacheDir)
	}
	now := time.Now()
	if !oem.Covers(now) {
		t.Skipf("缓存已过期: %s ~ %s", oem.StartTime, oem.StopTime)
	}

	lat, lng, alt, ok := computePosition(oem, now)
	if !ok {
		t.Fatal("位置解算失败")
	}
	windowStart, windowEnd := now.Add(-50*time.Minute), now.Add(95*time.Minute)
	series := computeAltWindow(oem, windowStart, windowEnd)
	if len(series) < 20 {
		t.Fatalf("真实数据高度序列点数 = %d, want >= 20", len(series))
	}
	for _, p := range series {
		if p.Altitude < 300 || p.Altitude > 500 {
			t.Errorf("真实数据高度异常: %.1f km", p.Altitude)
			break
		}
	}

	trend := cardTrend(oem, now)
	data := cardData{
		Now:         now,
		Lat:         lat,
		Lon:         lng,
		Alt:         alt,
		Speed:       computeSpeed(oem, now),
		Inclination: computeInclination(oem, now),
		PeriodMin:   satutil.OrbitalPeriod(alt),
		FootprintKm: satutil.FootprintDiameter(alt),
		Series:      series,
		Trend:       trend,
		Track:       computeGroundTrack(oem, windowStart, windowEnd),
		OEM:         oem,
	}
	data.Eclipse, data.EclipseRemaining, data.EclipseKnown =
		computeEclipsePhase(oem, now, time.Duration(data.PeriodMin)*time.Minute)

	pngBytes, err := renderCard(data)
	if err != nil {
		t.Fatalf("真实数据渲染失败: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("解码 PNG: %v", err)
	}
	t.Logf("真实数据卡片: 高度 %.1f km 倾角 %.3f° 周期 %.1f min 趋势 %.3f km/天, %dx%d",
		alt, data.Inclination, data.PeriodMin, trend.Slope, img.Bounds().Dx(), img.Bounds().Dy())
	dumpPreview(t, "css_card_real.png", pngBytes)
}

func TestRenderCardWithoutOptionalData(t *testing.T) {
	// 缺少 OEM、序列与轨迹时也应能渲染（降级为只有基础信息）
	data := cardData{
		Now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		Lat: 12.3, Lon: -45.6, Alt: 390, Speed: 7.68,
		Inclination: 41.47, PeriodMin: 92.4, FootprintKm: 4480,
	}
	pngBytes, err := renderCard(data)
	if err != nil {
		t.Fatalf("renderCard 降级渲染失败: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(pngBytes)); err != nil {
		t.Fatalf("解码 PNG: %v", err)
	}
}

func TestEclipseBadgeText(t *testing.T) {
	lit := cardData{EclipseRemaining: 41 * time.Minute, EclipseKnown: true}
	if got := eclipseBadgeText(lit); got != "光照中 · 剩余约 41 分钟" {
		t.Errorf("光照徽章 = %q", got)
	}
	shadow := cardData{Eclipse: true, EclipseRemaining: 32 * time.Minute, EclipseKnown: true}
	if got := eclipseBadgeText(shadow); got != "地影中 · 剩余约 32 分钟" {
		t.Errorf("地影徽章 = %q", got)
	}
	unknown := cardData{Eclipse: true, EclipseRemaining: 92 * time.Minute}
	if got := eclipseBadgeText(unknown); got != "地影中 · 剩余 > 1 小时 32 分" {
		t.Errorf("未找到切换点的徽章 = %q", got)
	}
	if got := eclipseBadgeText(cardData{}); got != "光照中" {
		t.Errorf("无剩余时长信息时徽章 = %q", got)
	}
}

func TestFmtDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{8 * time.Minute, "8 分钟"},
		{90 * time.Second, "2 分钟"},
		{time.Hour, "1 小时"},
		{92*time.Minute + 30*time.Second, "1 小时 33 分"},
		{3 * time.Hour, "3 小时"},
	}
	for _, c := range cases {
		if got := fmtDuration(c.in); got != c.want {
			t.Errorf("fmtDuration(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPerigeeApogeeText(t *testing.T) {
	// 近圆轨道：不展示“388 / 388”这类无信息量的对比
	if got := perigeeApogeeText(Trend{MinAlt: 387.8, MaxAlt: 388.2}); got != "近圆轨道 ≈ 388 km" {
		t.Errorf("近圆轨道徽章 = %q", got)
	}
	if got := perigeeApogeeText(Trend{MinAlt: 385.4, MaxAlt: 392.1}); got != "近/远地点 385 / 392 km" {
		t.Errorf("椭圆轨道徽章 = %q", got)
	}
}

func TestForecastHelpers(t *testing.T) {
	oem := &OEMEphemeris{
		StartTime: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		StopTime:  time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
	}
	if got := forecastBadgeText(oem); got != "官方 7 天预报" {
		t.Errorf("预报徽章 = %q", got)
	}
	if got := forecastBadgeText(nil); got != "轨道预报" {
		t.Errorf("无数据徽章 = %q", got)
	}
	caption := forecastCaption(oem)
	for _, want := range []string{"03-01 00:00", "03-08 00:00", "UTC"} {
		if !bytes.Contains([]byte(caption), []byte(want)) {
			t.Errorf("预报说明缺少 %q: %q", want, caption)
		}
	}
	if got := forecastCaption(nil); got != "含一个完整轨道周期" {
		t.Errorf("无数据说明 = %q", got)
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := fmtLat(-12.3456); got != "12.3456°S" {
		t.Errorf("fmtLat = %q", got)
	}
	if got := fmtLng(120.5); got != "120.5000°E" {
		t.Errorf("fmtLng = %q", got)
	}
	if got := trendValueText(-0.42); got != "−0.42" {
		t.Errorf("trendValueText = %q", got)
	}
	if got := trendSubText(-0.42); got != "km/天 · 长期衰减" {
		t.Errorf("trendSubText = %q", got)
	}
}

func TestFormatCSSTextContainsKeyFields(t *testing.T) {
	oem := syntheticOEM(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 300, 4*time.Minute, 390, 41.5, 0)
	text := formatCSSText(12.3, -45.6, 390.2, 7.68, Trend{Slope: -0.42, MinAlt: 386, MaxAlt: 392}, oem)
	for _, want := range []string{"中国空间站", "高度: 390.2 km", "轨道趋势", "cmse.gov.cn"} {
		if !bytes.Contains([]byte(text), []byte(want)) {
			t.Errorf("纯文本输出缺少 %q:\n%s", want, text)
		}
	}
}
