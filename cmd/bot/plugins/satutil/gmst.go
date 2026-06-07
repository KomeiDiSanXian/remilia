package satutil

import (
	"math"
	"time"
)

// J2000 相关常数：J2000.0 历元（2000-01-01T12:00:00 UTC）的 Unix 秒数和儒略日基准。
const (
	j2000Epoch    = 946728000
	julianDayBase = 2451545.0
)

// GMST 计算给定 UTC 时间的格林威治平恒星时（弧度）。
//
// 算法来源：Astronomical Almanac，基于 J2000.0 起算的儒略日数。
// 适用于卫星轨道从惯性系(ECI)到地固系(ECEF)的坐标旋转。
func GMST(t time.Time) float64 {
	ut := float64(t.Unix() - j2000Epoch)
	jd := julianDayBase + ut/86400.0
	d := jd - julianDayBase
	gmstSec := 280.46061837 + 360.98564736629*d + 0.000387933*d*d + d*d*d/38710000.0
	gmstSec = math.Mod(gmstSec, 360.0)
	if gmstSec < 0 {
		gmstSec += 360.0
	}
	return gmstSec * math.Pi / 180.0
}

// ECItoECEF 将 ECI（地心惯性系，EME2000）坐标绕 Z 轴旋转 GMST 角度到 ECEF（地心地固系）。
//
// 旋转矩阵 Rz(-θ)：
//
//	[cosθ  sinθ  0] [X]
//	[-sinθ cosθ  0] [Y]
//	[0     0     1] [Z]
func ECItoECEF(x, y, z, gmst float64) (float64, float64, float64) {
	cosG := math.Cos(gmst)
	sinG := math.Sin(gmst)
	return cosG*x + sinG*y, -sinG*x + cosG*y, z
}
