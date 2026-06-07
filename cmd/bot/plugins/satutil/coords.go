package satutil

import "math"

// GeodeticToECEF 将大地坐标转换为 ECEF 坐标。
func GeodeticToECEF(latDeg, lonDeg, altKm float64) (float64, float64, float64) {
	lat := latDeg * math.Pi / 180.0
	lon := lonDeg * math.Pi / 180.0
	sinLat := math.Sin(lat)
	cosLat := math.Cos(lat)
	n := wgs84A / math.Sqrt(1.0-wgs84E2*sinLat*sinLat)
	h := altKm
	x := (n + h) * cosLat * math.Cos(lon)
	y := (n + h) * cosLat * math.Sin(lon)
	z := ((1.0-wgs84E2)*n + h) * sinLat
	return x, y, z
}

// ECEFtoECI 将 ECEF 坐标旋转回 ECI (EME2000)。
// gmst 为 GMST 弧度，需与 ECItoECEF 使用的值一致。
func ECEFtoECI(x, y, z, gmst float64) (float64, float64, float64) {
	cosG := math.Cos(gmst)
	sinG := math.Sin(gmst)
	return cosG*x - sinG*y, sinG*x + cosG*y, z
}

// WGS84 椭球体参数。
const (
	wgs84A = 6378.137            // 长半轴 (km)
	wgs84F = 1.0 / 298.257223563 // 扁率
)

var (
	wgs84B  = wgs84A * (1.0 - wgs84F)               // 短半轴 (km)
	wgs84E2 = 1.0 - (wgs84B*wgs84B)/(wgs84A*wgs84A) // 第一偏心率平方
)

// ECEFtoGeodetic 将 ECEF（地心地固系）坐标转换为大地坐标。
//
// 参数:
//   - x, y, z: ECEF 坐标 (km)
//
// 返回值:
//   - latDeg: 纬度 (度, -90~90)
//   - lonDeg: 经度 (度, -180~180)
//   - alt:    海拔 (km, WGS84 椭球面以上)
//
// 使用迭代法（Bowring 改进）求解大地纬度，收敛后计算海拔。
func ECEFtoGeodetic(x, y, z float64) (latDeg, lonDeg, alt float64) {
	lon := math.Atan2(y, x)

	p := math.Sqrt(x*x + y*y)
	lat := math.Atan2(z, p*(1.0-wgs84E2))

	for range 10 {
		sinLat := math.Sin(lat)
		n := wgs84A / math.Sqrt(1.0-wgs84E2*sinLat*sinLat)
		h := p/math.Cos(lat) - n
		newLat := math.Atan2(z, p*(1.0-wgs84E2*n/(n+h)))
		if math.Abs(newLat-lat) < 1e-12 {
			lat = newLat
			break
		}
		lat = newLat
	}

	sinLat := math.Sin(lat)
	n := wgs84A / math.Sqrt(1.0-wgs84E2*sinLat*sinLat)
	alt = p/math.Cos(lat) - n

	latDeg = lat * 180.0 / math.Pi
	lonDeg = lon * 180.0 / math.Pi
	return
}
