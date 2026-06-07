package css

import (
	"math"
	"sort"
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
//  2. 线性插值得到当前位置向量（EME2000 坐标系）
//  3. 通过 GMST 旋转将 EME2000 转换为 ECEF
//  4. 使用 WGS84 椭球模型迭代求解大地坐标(纬度、经度、海拔)
//
// 返回 lat(度), lng(度), alt(km) 和是否成功。
func computePosition(oem *OEMEphemeris, t time.Time) (lat, lng, alt float64, ok bool) {
	v0, v1, ratio, dur := findInterval(oem, t)
	if dur <= 0 {
		return 0, 0, 0, false
	}

	x := v0.X + (v1.X-v0.X)*ratio
	y := v0.Y + (v1.Y-v0.Y)*ratio
	z := v0.Z + (v1.Z-v0.Z)*ratio

	// EME2000(ECI) → ECEF → Geodetic
	gmst := satutil.GMST(t)
	ex, ey, ez := satutil.ECItoECEF(x, y, z, gmst)
	lat, lng, alt = satutil.ECEFtoGeodetic(ex, ey, ez)
	return lat, lng, alt, true
}

// computeAltHistory 从 OEM 数据中计算指定时间窗口内所有状态向量对应的轨道高度。
// 返回高度时间序列。
func computeAltHistory(oem *OEMEphemeris, window time.Duration) []AltPoint {
	if len(oem.Vectors) < 2 {
		return nil
	}

	now := time.Now()
	cutoff := now.Add(-window)

	points := make([]AltPoint, 0, len(oem.Vectors))
	for _, v := range oem.Vectors {
		if v.Time.Before(cutoff) || v.Time.After(now) {
			continue
		}
		gmst := satutil.GMST(v.Time)
		ex, ey, ez := satutil.ECItoECEF(v.X, v.Y, v.Z, gmst)
		_, _, alt := satutil.ECEFtoGeodetic(ex, ey, ez)
		points = append(points, AltPoint{Time: v.Time, Altitude: alt})
	}

	return points
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

// computeSpeed 计算指定时刻的空间站速度（通过插值 OEM 速度向量）。
func computeSpeed(oem *OEMEphemeris, t time.Time) float64 {
	v0, v1, ratio, dur := findInterval(oem, t)
	if dur <= 0 {
		return 0
	}

	dx := v0.Vx + (v1.Vx-v0.Vx)*ratio
	dy := v0.Vy + (v1.Vy-v0.Vy)*ratio
	dz := v0.Vz + (v1.Vz-v0.Vz)*ratio

	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// interpolatePoints 将高度点序列等距插值为指定数量的点，供图表平滑渲染。
func interpolatePoints(points []AltPoint, count int) []AltPoint {
	if len(points) < 2 || count < 2 {
		return points
	}

	result := make([]AltPoint, count)
	t0 := points[0].Time
	t1 := points[len(points)-1].Time
	dur := float64(t1.Sub(t0))

	for i := range count {
		ratio := float64(i) / float64(count-1)
		t := t0.Add(time.Duration(ratio * dur))
		result[i] = AltPoint{Time: t, Altitude: interpolateValue(points, t)}
	}
	return result
}

// interpolateValue 在高度点序列中对指定时间进行线性插值。
func interpolateValue(points []AltPoint, t time.Time) float64 {
	n := len(points)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return points[0].Altitude
	}
	if !t.After(points[0].Time) {
		return points[0].Altitude
	}
	if !t.Before(points[n-1].Time) {
		return points[n-1].Altitude
	}

	i := sort.Search(n, func(i int) bool {
		return !t.After(points[i].Time)
	})
	if i == 0 {
		return points[0].Altitude
	}
	if i >= n {
		return points[n-1].Altitude
	}

	dur := points[i].Time.Sub(points[i-1].Time)
	if dur <= 0 {
		return points[i-1].Altitude
	}
	ratio := float64(t.Sub(points[i-1].Time)) / float64(dur)
	return points[i-1].Altitude + (points[i].Altitude-points[i-1].Altitude)*ratio
}
