package satutil

import "math"

const degToRad = math.Pi / 180.0
const radToDeg = 180.0 / math.Pi

// 地球常数
const (
	EarthRadiusKm = 6371.0
	EarthGM       = 398600.4418 // km^3/s^2
)

// OrbitalPeriod 计算圆形轨道的轨道周期（分钟）。
// h: 轨道高度 (km)
func OrbitalPeriod(h float64) float64 {
	a := EarthRadiusKm + h
	return 2 * math.Pi * math.Sqrt(a*a*a/EarthGM) / 60.0
}

// FootprintDiameter 计算卫星在当前高度能看到的圆形覆盖区域直径（km）。
func FootprintDiameter(h float64) float64 {
	ratio := EarthRadiusKm / (EarthRadiusKm + h)
	if ratio > 1 {
		ratio = 1
	}
	return 2 * EarthRadiusKm * math.Acos(ratio)
}

// VelocityKmS 计算圆形轨道的速度（km/s），用于交叉验证测量值。
func VelocityKmS(h float64) float64 {
	a := EarthRadiusKm + h
	return math.Sqrt(EarthGM / a)
}

// VisibleAngularRadius 计算卫星可见区域的地心角（度）。
// 即从星下点向四周延伸到地平线的角度距离。
func VisibleAngularRadius(h float64) float64 {
	ratio := EarthRadiusKm / (EarthRadiusKm + h)
	if ratio > 1 {
		ratio = 1
	}
	return math.Acos(ratio) * radToDeg
}

// VisibleBounds 计算卫星可见区域的经纬度边界。
// 返回 (minLat, maxLat, minLng, maxLng)。
// lat, lng 为星下点经纬度（度），h 为高度（km）。
func VisibleBounds(lat, lng, h float64) (float64, float64, float64, float64) {
	angRad := VisibleAngularRadius(h)
	minLat := lat - angRad
	maxLat := lat + angRad

	// 经度跨度需要根据纬度修正
	cosLat := math.Cos(lat * degToRad)
	if cosLat < 0.01 {
		cosLat = 0.01
	}
	lonSpan := angRad / cosLat
	minLng := lng - lonSpan
	maxLng := lng + lonSpan
	return minLat, maxLat, minLng, maxLng
}

// IsInEclipse 判断卫星是否处于地球阴影中。
//
// 输入:
//   - x, y, z: 卫星在 ECI 坐标系下的位置 (km)
//   - t: 当前时间（用于计算太阳位置）
//
// 方法:
//  1. 计算卫星到地球中心的距离 r
//  2. 计算太阳在 ECI 下的单位方向向量
//  3. 如果卫星与太阳的夹角使卫星位于地球背后，则判定为阴影
//
// 使用 umbra 近似: 卫星位于地球背向太阳一侧，且距地影轴足够近。
func IsInEclipse(x, y, z float64, gmstRad float64) bool {
	// 计算太阳在 ECI 中的粗略方向（简化：假设太阳在黄道面上）
	// 实际计算应考虑黄赤交角，此处近似已够用
	sunLon := gmstRad + math.Pi // 太阳大致在相反方向
	sunX := math.Cos(sunLon)
	sunY := math.Sin(sunLon)

	// 卫星位置单位向量（仅用 XY 平面近似，足够判定光照）
	r := math.Sqrt(x*x + y*y + z*z)
	if r == 0 {
		return false
	}

	// 卫星-太阳夹角：dot product
	dot := (x/r)*sunX + (y/r)*sunY

	// 如果 dot > 0，卫星与太阳在地球同一侧 → 阳光照射
	if dot > 0 {
		return false
	}

	// dot <= 0: 卫星在背向太阳一侧
	// 进一步检查是否在地影锥内
	// 地影角: sin(theta) = R_earth / r
	sinTheta := EarthRadiusKm / r
	if sinTheta > 1 {
		sinTheta = 1
	}
	theta := math.Asin(sinTheta)

	// 卫星-地心-太阳夹角
	// 当此角小于 theta + 太阳视差时，卫星进入地影
	satAngle := math.Acos(-dot)

	return satAngle < theta
}
