package satutil

import (
	"math"
	"time"
)

// omegaEarthRadPerSec 地球平自转角速度（弧度/秒，WGS84）。
const omegaEarthRadPerSec = 7.2921159e-5

// TrackPoint 卫星星下点轨迹上的一个采样点。
type TrackPoint struct {
	Time time.Time
	Lat  float64 // 纬度（度）
	Lon  float64 // 经度（度，-180~180）
}

// SubSatellitePoint 将 ECI（EME2000）位置向量转换为星下点大地坐标。
func SubSatellitePoint(x, y, z float64, t time.Time) (lat, lon, alt float64) {
	gmst := GMST(t)
	ex, ey, ez := ECItoECEF(x, y, z, gmst)
	return ECEFtoGeodetic(ex, ey, ez)
}

// IsInEclipseAt 判断星下点 (lat, lon, altKm) 在时刻 t 是否位于地球阴影中。
//
// 内部完成 大地坐标 → ECEF → ECI 的换算，供地影判定复用。
func IsInEclipseAt(lat, lon, altKm float64, t time.Time) bool {
	gmst := GMST(t)
	ex, ey, ez := GeodeticToECEF(lat, lon, altKm)
	ix, iy, iz := ECEFtoECI(ex, ey, ez, gmst)
	return IsInEclipse(ix, iy, iz, gmst)
}

// InclinationDeg 由 ECI 状态向量计算轨道倾角（度，0~180）。
//
// 倾角即轨道角动量方向与地球自转轴（Z 轴）的夹角：cos i = h_z / |h|。
func InclinationDeg(x, y, z, vx, vy, vz float64) float64 {
	hx := y*vz - z*vy
	hy := z*vx - x*vz
	hz := x*vy - y*vx
	h := math.Sqrt(hx*hx + hy*hy + hz*hz)
	if h == 0 {
		return 0
	}
	return math.Acos(clampValue(hz/h, -1, 1)) * radToDeg
}

// OrbitGroundTrack 以圆轨道近似外推星下点轨迹。
//
// 给定参考时刻的星下点 (lat0, lon0)、轨道高度与倾角，沿轨道平面按匀角速度
// 推进，并叠加地球自转造成的经度西移，返回 [from, to] 区间内的星下点序列。
// ascending 表示参考时刻卫星是否处于升段（纬度递增）。
//
// 该模型用于绘制示意轨迹；若需精确轨迹（如 CCSDS OEM 预报），
// 应改为用 SubSatellitePoint 逐点换算真实状态矢量。
func OrbitGroundTrack(ref time.Time, lat0, lon0, altKm, inclinationDeg float64, from, to time.Time, step time.Duration, ascending bool) []TrackPoint {
	if step <= 0 || to.Before(from) {
		return nil
	}
	periodSec := OrbitalPeriod(altKm) * 60
	if periodSec <= 0 {
		return nil
	}
	sini := math.Sin(inclinationDeg * degToRad)
	if math.Abs(sini) < 1e-9 {
		return nil
	}
	cosi := math.Cos(inclinationDeg * degToRad)

	// 幅角 u：由 sin(lat) = sin(i)·sin(u) 反解；降段取互补分支。
	u0 := math.Asin(clampValue(math.Sin(lat0*degToRad)/sini, -1, 1))
	if !ascending {
		u0 = math.Pi - u0
	}
	// 参考时刻的交点经度：lon = nodeLon + atan2(cos i·sin u, cos u) − ωE·Δt
	nodeLon0 := lon0*degToRad - math.Atan2(cosi*math.Sin(u0), math.Cos(u0))
	meanMotion := 2 * math.Pi / periodSec

	var out []TrackPoint
	for t := from; !t.After(to); t = t.Add(step) {
		dt := t.Sub(ref).Seconds()
		u := u0 + meanMotion*dt
		lat := math.Asin(clampValue(sini*math.Sin(u), -1, 1)) * radToDeg
		offset := math.Atan2(cosi*math.Sin(u), math.Cos(u))
		lon := (nodeLon0 + offset - omegaEarthRadPerSec*dt) * radToDeg
		out = append(out, TrackPoint{Time: t, Lat: lat, Lon: NormalizeLon(lon)})
	}
	return out
}

// NormalizeLon 将经度归一化到 [-180, 180)。
func NormalizeLon(lon float64) float64 {
	lon = math.Mod(lon+180, 360)
	if lon < 0 {
		lon += 360
	}
	return lon - 180
}

// ScanPhase 以 step 为步长扫描 [from, until]，返回 from 时刻的相位状态
// 以及该状态持续到切换点的时长（未发现切换时为窗口总长，found=false）。
//
// state 为相位判定函数（例如“是否处于地影”）。返回的 remaining 是
// 扫描精度的上界：真实切换时刻位于 (t−step, t] 内。
func ScanPhase(from, until time.Time, step time.Duration, state func(time.Time) bool) (initial bool, remaining time.Duration, found bool) {
	if step <= 0 || !until.After(from) {
		return state(from), 0, false
	}
	current := state(from)
	for t := from.Add(step); !t.After(until); t = t.Add(step) {
		if state(t) != current {
			return current, t.Sub(from), true
		}
	}
	return current, until.Sub(from), false
}

// clampValue 将 v 限制在 [lo, hi] 区间内。
func clampValue(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
