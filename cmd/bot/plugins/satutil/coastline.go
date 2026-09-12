package satutil

import (
	_ "embed"
	"encoding/binary"
	"sync"
)

// coastlineAssetPath 说明内置海岸线数据的位置与来源，详见 assets/ATTRIBUTION.md。
//
//go:embed assets/world_coastline.bin
var coastlineAsset []byte

// LonLat 表示一个经纬度坐标（单位：度）。
type LonLat struct {
	Lon float64
	Lat float64
}

const (
	// coastlineMagic 为数据文件的魔数，用于快速识别损坏或串档的数据。
	coastlineMagic = "RMC1"
	// coastlineQuant 为坐标量化单位：数据以 1e-2 度存储（约 0.6 km 误差）。
	coastlineQuant = 100
)

var (
	coastlinesOnce sync.Once
	coastlinesData [][]LonLat
)

// Coastlines 返回内置的世界海岸线折线集合（Natural Earth 1:110m，公有领域）。
//
//   - 坐标为等经纬（等距圆柱）投影下的经纬度，与 GroundTrackPanel 使用的
//     投影完全一致，因此可与经纬网、星下点轨迹严格对齐，无需任何近似或拟合；
//   - 折线已在 ±180° 经线处断开，可直接逐段描绘，不会出现横贯全图的假线段；
//   - 坐标以 1e-2 度量化，最大误差约 0.6 km，远小于一张 7xx 像素宽世界地图
//     上单个像素所代表的距离（约 55 km）。
//
// 返回的切片只读，调用方不应修改。数据只解析一次并缓存。
func Coastlines() [][]LonLat {
	coastlinesOnce.Do(func() {
		coastlinesData = decodeCoastlines(coastlineAsset)
	})
	return coastlinesData
}

// decodeCoastlines 解析内置海岸线数据；数据缺失或损坏时返回 nil。
//
// 文件格式（小端）：
//
//	"RMC1"            4 字节魔数
//	uint16            折线条数
//	每条折线：
//	  uint16          点数 n
//	  n × (int16, int16)  经度、纬度 × 100（度）
func decodeCoastlines(data []byte) [][]LonLat {
	head := len(coastlineMagic)
	if len(data) < head+2 || string(data[:head]) != coastlineMagic {
		return nil
	}
	rest := data[head:]
	lineCount := int(binary.LittleEndian.Uint16(rest))
	rest = rest[2:]

	out := make([][]LonLat, 0, lineCount)
	for range lineCount {
		if len(rest) < 2 {
			return nil
		}
		count := int(binary.LittleEndian.Uint16(rest))
		rest = rest[2:]
		if len(rest) < count*4 {
			return nil
		}
		line := make([]LonLat, count)
		for i := range line {
			line[i] = LonLat{
				Lon: float64(int16(binary.LittleEndian.Uint16(rest[4*i:]))) / coastlineQuant,
				Lat: float64(int16(binary.LittleEndian.Uint16(rest[4*i+2:]))) / coastlineQuant,
			}
		}
		rest = rest[count*4:]
		out = append(out, line)
	}
	return out
}
