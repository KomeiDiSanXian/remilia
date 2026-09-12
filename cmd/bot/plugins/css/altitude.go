package css

import (
	"math"
	"time"

	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/satutil"
)

// AltPoint 表示一个时间点上的轨道高度。
type AltPoint struct {
	Time     time.Time
	Altitude float64
}

// Trend 轨道高度趋势分析结果。
type Trend struct {
	Slope     float64 // 衰减率 (km/天)，正值表示上升，负值表示下降
	Intercept float64
	MinAlt    float64 // 近地点 (km)
	MaxAlt    float64 // 远地点 (km)
}

// computePosition 根据 OEM 数据计算指定时刻的中国空间站位置。
//
// 流程:
//  1. 在 OEM 向量序列中找到当前时间的前后两个最近数据点
//  2. 用两端位置与速度做三次 Hermite 插值得到当前位置（EME2000 坐标系）
//  3. 通过 GMST 旋转将 EME2000 转换为 ECEF
//  4. 使用 WGS84 椭球模型迭代求解大地坐标(纬度、经度、海拔)
//
// 返回 lat(度), lng(度), alt(km) 和是否成功。
func computePosition(oem *OEMEphemeris, t time.Time) (lat, lng, alt float64, ok bool) {
	v0, v1, ratio, dur := findInterval(oem, t)
	if dur <= 0 {
		return 0, 0, 0, false
	}

	// 相邻状态矢量间隔 4 分钟（约 15.5° 轨道弧），线性插值的弦高误差可达
	// 约 60 km（高度被严重低估）；Hermite 插值误差在百米量级。
	dt := dur.Seconds()
	x, _ := hermite(v0.X, v0.Vx, v1.X, v1.Vx, dt, ratio)
	y, _ := hermite(v0.Y, v0.Vy, v1.Y, v1.Vy, dt, ratio)
	z, _ := hermite(v0.Z, v0.Vz, v1.Z, v1.Vz, dt, ratio)

	// EME2000(ECI) → ECEF → Geodetic
	gmst := satutil.GMST(t)
	ex, ey, ez := satutil.ECItoECEF(x, y, z, gmst)
	lat, lng, alt = satutil.ECEFtoGeodetic(ex, ey, ez)
	return lat, lng, alt, true
}

// hermite 对单个分量做三次 Hermite 插值，同时返回位置与速度。
//
// p0/v0 与 p1/v1 为区间两端的位置(km)与速度(km/s)，dt 为区间时长(秒)，
// s ∈ [0,1] 为区间内的归一化比例。返回 pos 单位为 km、vel 单位为 km/s。
func hermite(p0, v0, p1, v1, dt, s float64) (pos, vel float64) {
	s2 := s * s
	s3 := s2 * s
	pos = (2*s3-3*s2+1)*p0 +
		(s3-2*s2+s)*dt*v0 +
		(-2*s3+3*s2)*p1 +
		(s3-s2)*dt*v1
	vel = ((6*s2-6*s)*p0 +
		(3*s2-4*s+1)*dt*v0 +
		(-6*s2+6*s)*p1 +
		(3*s2-2*s)*dt*v1) / dt
	return pos, vel
}

// computeAltWindow 从 OEM 数据中提取 [from, to] 区间内所有状态向量的轨道高度。
func computeAltWindow(oem *OEMEphemeris, from, to time.Time) []AltPoint {
	if len(oem.Vectors) < 2 {
		return nil
	}

	points := make([]AltPoint, 0, len(oem.Vectors))
	for _, v := range oem.Vectors {
		if v.Time.Before(from) || v.Time.After(to) {
			continue
		}
		_, _, alt := satutil.SubSatellitePoint(v.X, v.Y, v.Z, v.Time)
		points = append(points, AltPoint{Time: v.Time, Altitude: alt})
	}
	return points
}

// computeGroundTrack 从 OEM 状态矢量提取 [from, to] 区间内的星下点轨迹。
//
// 与 [computeAltWindow] 的区别在于保留经纬度，用于绘制地面轨迹图。
func computeGroundTrack(oem *OEMEphemeris, from, to time.Time) []satutil.TrackPoint {
	if len(oem.Vectors) < 2 {
		return nil
	}
	points := make([]satutil.TrackPoint, 0, len(oem.Vectors))
	for _, v := range oem.Vectors {
		if v.Time.Before(from) || v.Time.After(to) {
			continue
		}
		lat, lon, _ := satutil.SubSatellitePoint(v.X, v.Y, v.Z, v.Time)
		points = append(points, satutil.TrackPoint{Time: v.Time, Lat: lat, Lon: lon})
	}
	return points
}

// cardTrend 汇总卡片展示所需的趋势信息：
// 近/远地点取自当前时刻前后各约一个轨道周期的径向高度极值，
// 变化率取自相位对齐的长期拟合（参见 [computeDecayTrend]）。
func cardTrend(oem *OEMEphemeris, now time.Time) Trend {
	trend := computeTrend(computeRadialWindow(oem, now.Add(-50*time.Minute), now.Add(95*time.Minute)))
	if decay, ok := computeDecayTrend(oem, now, 3*24*time.Hour); ok {
		trend.Slope = decay.Slope
		trend.Intercept = decay.Intercept
	}
	return trend
}

// computeRadialWindow 提取 [from, to] 区间状态矢量的径向高度序列
// （|r| − 地球平均半径）。
//
// 与 [computeAltWindow] 返回的大地高度不同，径向高度不含大地纬度引起的
// 椭球起伏（41.5° 倾角上约 ±8 km），因此更适合用于近/远地点判定与
// 长期趋势拟合。
func computeRadialWindow(oem *OEMEphemeris, from, to time.Time) []AltPoint {
	if len(oem.Vectors) < 2 {
		return nil
	}
	points := make([]AltPoint, 0, len(oem.Vectors))
	for _, v := range oem.Vectors {
		if v.Time.Before(from) || v.Time.After(to) {
			continue
		}
		points = append(points, AltPoint{Time: v.Time, Altitude: radialAltitude(v)})
	}
	return points
}

// radialAltitude 返回状态矢量对应的径向高度（km，相对地球平均半径）。
func radialAltitude(v StateVector) float64 {
	return math.Sqrt(v.X*v.X+v.Y*v.Y+v.Z*v.Z) - satutil.EarthRadiusKm
}

// computeDecayTrend 估计轨道高度的长期变化率（km/天）。
//
// 采用“相位对齐”采样：每隔一个轨道周期在同一轨道相位上取一个样点，
// 使近圆轨道高度中随轨道周期起伏的分量（椭圆度、地球椭球）在拟合中
// 相互抵消，只保留长期抬升/衰减趋势。采样窗口超出数据范围时自动收窄。
//
// 返回的 MinAlt/MaxAlt 为采样序列的极值；有效样本不足 3 个时 ok 为 false。
func computeDecayTrend(oem *OEMEphemeris, now time.Time, span time.Duration) (Trend, bool) {
	idx := nearestVector(oem, now)
	if idx < 0 || span <= 0 {
		return Trend{}, false
	}
	period := satutil.OrbitalPeriod(radialAltitude(oem.Vectors[idx]))
	if period <= 0 {
		return Trend{}, false
	}
	step := time.Duration(period * float64(time.Minute))

	var samples []AltPoint
	lastIdx := -1
	for k := -int(span / step); k <= int(span/step); k++ {
		target := now.Add(time.Duration(k) * step)
		if target.Before(oem.StartTime) || target.After(oem.StopTime) {
			continue
		}
		i := nearestVector(oem, target)
		if i < 0 || i == lastIdx {
			continue
		}
		lastIdx = i
		samples = append(samples, AltPoint{Time: oem.Vectors[i].Time, Altitude: radialAltitude(oem.Vectors[i])})
	}
	if len(samples) < 3 {
		return Trend{}, false
	}
	return computeTrend(samples), true
}

// computeInclination 返回 t 时刻最接近的状态向量所对应的轨道倾角（度）。
func computeInclination(oem *OEMEphemeris, t time.Time) float64 {
	idx := nearestVector(oem, t)
	if idx < 0 {
		return 0
	}
	v := oem.Vectors[idx]
	return satutil.InclinationDeg(v.X, v.Y, v.Z, v.Vx, v.Vy, v.Vz)
}

// computeEclipsePhase 判断当前是否处于地影，并估算当前相位剩余时长。
//
// lookahead 为向前扫描的最大时长（通常取一个轨道周期）。known 为 false
// 表示扫描窗口内没有出现相位切换，此时 remaining 即为窗口长度。
func computeEclipsePhase(oem *OEMEphemeris, now time.Time, lookahead time.Duration) (eclipse bool, remaining time.Duration, known bool) {
	state := func(t time.Time) bool {
		lat, lon, alt, ok := computePosition(oem, t)
		if !ok {
			return false
		}
		return satutil.IsInEclipseAt(lat, lon, alt, t)
	}
	return satutil.ScanPhase(now, now.Add(lookahead), 30*time.Second, state)
}

// nearestVector 返回 OEM 数据中时间上最接近 t 的状态向量下标；无数据时返回 -1。
func nearestVector(oem *OEMEphemeris, t time.Time) int {
	if len(oem.Vectors) == 0 {
		return -1
	}
	idx := 0
	best := t.Sub(oem.Vectors[0].Time)
	if best < 0 {
		best = -best
	}
	for i, v := range oem.Vectors {
		d := t.Sub(v.Time)
		if d < 0 {
			d = -d
		}
		if d < best {
			best = d
			idx = i
		}
	}
	return idx
}

// computeTrend 分析高度时间序列，计算近地点、远地点和线性衰减率。
func computeTrend(points []AltPoint) Trend {
	if len(points) == 0 {
		return Trend{}
	}

	minAlt := points[0].Altitude
	maxAlt := points[0].Altitude
	for _, p := range points {
		if p.Altitude < minAlt {
			minAlt = p.Altitude
		}
		if p.Altitude > maxAlt {
			maxAlt = p.Altitude
		}
	}

	// 至少 3 个点才做线性拟合
	if len(points) < 3 {
		return Trend{MinAlt: minAlt, MaxAlt: maxAlt}
	}

	t0 := points[0].Time
	var sumX, sumY, sumXY, sumX2 float64
	n := float64(len(points))
	for _, p := range points {
		x := float64(p.Time.Sub(t0)) / float64(time.Hour)
		y := p.Altitude
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	slope := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
	intercept := (sumY - slope*sumX) / n
	slopePerDay := slope * 24.0

	return Trend{
		Slope:     slopePerDay,
		Intercept: intercept,
		MinAlt:    minAlt,
		MaxAlt:    maxAlt,
	}
}

// findInterval 在 OEM 向量序列中找到包含时间 t 的前后两个状态向量及插值比例。
func findInterval(oem *OEMEphemeris, t time.Time) (v0, v1 StateVector, ratio float64, dur time.Duration) {
	vectors := oem.Vectors
	if len(vectors) < 2 {
		return
	}
	if t.Before(vectors[0].Time) || t.After(vectors[len(vectors)-1].Time) {
		return
	}

	idx := -1
	for i := 0; i < len(vectors)-1; i++ {
		if (t.Equal(vectors[i].Time) || t.After(vectors[i].Time)) &&
			(t.Before(vectors[i+1].Time) || t.Equal(vectors[i+1].Time)) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}

	v0 = vectors[idx]
	v1 = vectors[idx+1]
	dur = v1.Time.Sub(v0.Time)
	if dur <= 0 {
		return
	}
	ratio = float64(t.Sub(v0.Time)) / float64(dur)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return
}

// computeSpeed 计算指定时刻的空间站速度（km/s）。
//
// 使用与 [computePosition] 一致的三次 Hermite 插值取导数，避免线性插值
// 速度矢量造成的幅值低估（约 1%）。
func computeSpeed(oem *OEMEphemeris, t time.Time) float64 {
	v0, v1, ratio, dur := findInterval(oem, t)
	if dur <= 0 {
		return 0
	}

	dt := dur.Seconds()
	_, vx := hermite(v0.X, v0.Vx, v1.X, v1.Vx, dt, ratio)
	_, vy := hermite(v0.Y, v0.Vy, v1.Y, v1.Vy, dt, ratio)
	_, vz := hermite(v0.Z, v0.Vz, v1.Z, v1.Vz, dt, ratio)

	return math.Sqrt(vx*vx + vy*vy + vz*vz)
}
