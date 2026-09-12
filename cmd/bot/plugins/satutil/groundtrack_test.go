package satutil

import (
	"math"
	"testing"
	"time"
)

// circularStateVectors 生成一条圆轨道（倾角 inc、高度 h）在 [start, end] 内的
// 状态向量，用于构造可验证的测试数据。
//
// 轨道面由升交点方向 X 轴与倾角确定：r = R(cosθ, sinθ·cos i, sinθ·sin i)。
func circularStateVectors(start time.Time, count int, step time.Duration, h, incDeg float64) (ts []time.Time, xs, ys, zs, vxs, vys, vzs []float64) {
	r := EarthRadiusKm + h
	v := VelocityKmS(h)
	inc := incDeg * degToRad
	period := OrbitalPeriod(h) * 60
	n := 2 * math.Pi / period
	for i := range count {
		tm := start.Add(time.Duration(i) * step)
		theta := n * float64(i) * step.Seconds()
		sinT, cosT := math.Sin(theta), math.Cos(theta)
		ts = append(ts, tm)
		xs = append(xs, r*cosT)
		ys = append(ys, r*sinT*math.Cos(inc))
		zs = append(zs, r*sinT*math.Sin(inc))
		vxs = append(vxs, -v*sinT)
		vys = append(vys, v*cosT*math.Cos(inc))
		vzs = append(vzs, v*cosT*math.Sin(inc))
	}
	return
}

func TestOrbitGroundTrackPassesReferencePoint(t *testing.T) {
	ref := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	const lat0, lon0, alt, inc = 12.5, -73.2, 420.0, 51.64

	track := OrbitGroundTrack(ref, lat0, lon0, alt, inc,
		ref.Add(-45*time.Minute), ref.Add(45*time.Minute), time.Minute, true)
	if len(track) != 91 {
		t.Fatalf("轨迹点数 = %d, want 91", len(track))
	}

	got := track[45] // from = ref-45min，步长 1min，故第 45 个点即参考时刻
	if !got.Time.Equal(ref) {
		t.Fatalf("参考点时间 = %s, want %s", got.Time, ref)
	}
	if math.Abs(got.Lat-lat0) > 0.01 {
		t.Errorf("参考点纬度 = %.4f, want %.4f", got.Lat, lat0)
	}
	if math.Abs(got.Lon-lon0) > 0.01 {
		t.Errorf("参考点经度 = %.4f, want %.4f", got.Lon, lon0)
	}
}

func TestOrbitGroundTrackLatitudeWithinInclination(t *testing.T) {
	ref := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	const inc = 41.5
	track := OrbitGroundTrack(ref, 0, 0, 390, inc,
		ref.Add(-90*time.Minute), ref.Add(90*time.Minute), time.Minute, true)

	maxAbs := 0.0
	for _, p := range track {
		if abs := math.Abs(p.Lat); abs > maxAbs {
			maxAbs = abs
		}
		if math.Abs(p.Lat) > inc+0.01 {
			t.Fatalf("纬度 %.3f 超出倾角 %.2f", p.Lat, inc)
		}
		if p.Lon < -180 || p.Lon >= 180 {
			t.Fatalf("经度 %.3f 未归一化", p.Lon)
		}
	}
	if maxAbs < inc-1 {
		t.Errorf("最大纬度 %.2f 明显小于倾角 %.2f，轨迹可能未覆盖转向点", maxAbs, inc)
	}
}

func TestOrbitGroundTrackDescendingBranch(t *testing.T) {
	ref := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	asc := OrbitGroundTrack(ref, 20, 30, 420, 51.64, ref, ref.Add(5*time.Minute), time.Minute, true)
	desc := OrbitGroundTrack(ref, 20, 30, 420, 51.64, ref, ref.Add(5*time.Minute), time.Minute, false)

	if len(asc) != 6 || len(desc) != 6 {
		t.Fatalf("点数 asc=%d desc=%d, want 6/6", len(asc), len(desc))
	}
	if asc[1].Lat <= asc[0].Lat {
		t.Errorf("升段纬度应递增: %.3f → %.3f", asc[0].Lat, asc[1].Lat)
	}
	if desc[1].Lat >= desc[0].Lat {
		t.Errorf("降段纬度应递减: %.3f → %.3f", desc[0].Lat, desc[1].Lat)
	}
	if math.Abs(asc[0].Lat-desc[0].Lat) > 0.01 || math.Abs(asc[0].Lon-desc[0].Lon) > 0.01 {
		t.Errorf("两个分支应共享参考点: asc=(%.3f,%.3f) desc=(%.3f,%.3f)",
			asc[0].Lat, asc[0].Lon, desc[0].Lat, desc[0].Lon)
	}
}

func TestOrbitGroundTrackInvalidInput(t *testing.T) {
	ref := time.Now()
	if got := OrbitGroundTrack(ref, 0, 0, 420, 51.6, ref, ref.Add(time.Hour), 0, true); got != nil {
		t.Errorf("step=0 应返回 nil, got %d 点", len(got))
	}
	if got := OrbitGroundTrack(ref, 0, 0, 420, 51.6, ref.Add(time.Hour), ref, time.Minute, true); got != nil {
		t.Errorf("to < from 应返回 nil, got %d 点", len(got))
	}
	if got := OrbitGroundTrack(ref, 0, 0, 420, 0, ref, ref.Add(time.Hour), time.Minute, true); got != nil {
		t.Errorf("倾角为 0 应返回 nil, got %d 点", len(got))
	}
}

func TestNormalizeLon(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 0}, {180, -180}, {-180, -180}, {190, -170}, {-190, 170},
		{360, 0}, {-360, 0}, {450, 90},
	}
	for _, c := range cases {
		if got := NormalizeLon(c.in); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("NormalizeLon(%.1f) = %.3f, want %.3f", c.in, got, c.want)
		}
	}
}

func TestInclinationDeg(t *testing.T) {
	for _, inc := range []float64{0, 28.5, 41.5, 51.64, 98.2} {
		_, xs, ys, zs, vxs, vys, vzs := circularStateVectors(time.Now(), 1, time.Minute, 420, inc)
		got := InclinationDeg(xs[0], ys[0], zs[0], vxs[0], vys[0], vzs[0])
		if math.Abs(got-inc) > 1e-6 {
			t.Errorf("InclinationDeg = %.6f, want %.6f", got, inc)
		}
	}
	if got := InclinationDeg(0, 0, 0, 0, 0, 0); got != 0 {
		t.Errorf("零向量倾角 = %.3f, want 0", got)
	}
}

func TestSubSatellitePointRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	gmst := GMST(now)
	for _, c := range []struct{ lat, lon, alt float64 }{
		{0, 0, 420}, {51.6, -120.4, 400}, {-33.3, 151.2, 390}, {78.9, 179.5, 420},
	} {
		ex, ey, ez := GeodeticToECEF(c.lat, c.lon, c.alt)
		ix, iy, iz := ECEFtoECI(ex, ey, ez, gmst)
		lat, lon, alt := SubSatellitePoint(ix, iy, iz, now)
		if math.Abs(lat-c.lat) > 1e-6 || math.Abs(lon-c.lon) > 1e-6 || math.Abs(alt-c.alt) > 1e-6 {
			t.Errorf("往返转换 (%.2f,%.2f,%.1f) → (%.6f,%.6f,%.6f)",
				c.lat, c.lon, c.alt, lat, lon, alt)
		}
	}
}

func TestIsInEclipseAt(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	// 简化太阳模型下，本初子午线赤道点位于背阳面，对跖点位于向阳面。
	if !IsInEclipseAt(0, 0, 420, now) {
		t.Error("星下点 (0,0) 应判定为地影")
	}
	if IsInEclipseAt(0, 180, 420, now) {
		t.Error("星下点 (0,180) 应判定为光照")
	}
}

func TestScanPhase(t *testing.T) {
	from := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	state := func(t time.Time) bool { return t.Sub(from) < 10*time.Minute }

	initial, remaining, found := ScanPhase(from, from.Add(time.Hour), time.Minute, state)
	if !initial || !found {
		t.Fatalf("initial=%v found=%v, want true/true", initial, found)
	}
	if remaining != 10*time.Minute {
		t.Errorf("剩余时长 = %s, want 10m", remaining)
	}

	// 窗口内不切换
	initial, remaining, found = ScanPhase(from, from.Add(5*time.Minute), time.Minute, state)
	if !initial || found {
		t.Errorf("initial=%v found=%v, want true/false", initial, found)
	}
	if remaining != 5*time.Minute {
		t.Errorf("未切换时剩余时长 = %s, want 5m", remaining)
	}

	// 退化窗口
	if _, _, found := ScanPhase(from, from, time.Minute, state); found {
		t.Error("空窗口不应报告切换")
	}
}

func TestVisibleHelpers(t *testing.T) {
	// 高度越高的可见范围越大
	if VisibleAngularRadius(420) <= VisibleAngularRadius(300) {
		t.Error("可见范围地心角应随高度增加")
	}
	// 圆形轨道周期随高度增加
	if OrbitalPeriod(420) <= OrbitalPeriod(300) {
		t.Error("轨道周期应随高度增加")
	}
}
