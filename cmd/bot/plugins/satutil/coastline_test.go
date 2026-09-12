package satutil

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// landmark 为现实中公认的海岸线地标，用于校验内置轮廓与真实地理位置一致。
//
// 期望误差来自 Natural Earth 110m 数据本身的概化程度（顶点间距可达数十公里），
// 而坐标轴序错误（经纬互换）或符号错误会带来数百至数千公里的偏差，
// 因此 120 km 的阈值足以区分“对得上”与“对不上”。
type landmark struct {
	Name     string
	Lon, Lat float64
}

var coastlineLandmarks = []landmark{
	{"好望角", 18.474, -34.357},
	{"合恩角", -67.267, -55.983},
	{"卢因角", 115.136, -34.374},
	{"厄加勒斯角", 20.010, -34.833},
	{"约克角", 142.532, -10.688},
	{"费尔韦尔角", -43.900, 59.780},
	{"巴罗角", -156.479, 71.388},
	{"印度南端", 77.549, 8.078},
	{"下加利福尼亚南端", -109.912, 22.880},
	{"新西兰东角", 178.548, -37.693},
	{"直布罗陀塔里法", -5.606, 36.014},
	{"非洲白角", -17.050, 20.780},
}

// 远离任何大陆的大洋中心点，用于反证轮廓没有“连成一片”或横贯全图的假线段。
var oceanProbes = []landmark{
	{"太平洋中部", -140, 0},
	{"大西洋中部", -30, 10},
	{"印度洋中部", 80, -30},
}

func TestCoastlinesLoaded(t *testing.T) {
	lines := Coastlines()
	if len(lines) < 100 {
		t.Fatalf("海岸线折线数 = %d, want >= 100", len(lines))
	}
	points := 0
	for idx, line := range lines {
		if len(line) < 2 {
			t.Fatalf("第 %d 条折线点数不足: %d", idx, len(line))
		}
		points += len(line)
		for i, p := range line {
			if p.Lon < -180 || p.Lon > 180 {
				t.Fatalf("经度越界: %.4f", p.Lon)
			}
			if p.Lat < -90 || p.Lat > 90 {
				t.Fatalf("纬度越界: %.4f", p.Lat)
			}
			// 折线必须在 ±180° 处断开，否则会在图上画出横贯全图的假线段
			if i > 0 && math.Abs(p.Lon-line[i-1].Lon) > 180 {
				t.Fatalf("第 %d 条折线在 %d 处跨越 ±180° 未断开", idx, i)
			}
		}
	}
	if points < 4000 {
		t.Errorf("海岸线顶点数 = %d, want >= 4000（数据可能被误简化）", points)
	}
}

func TestCoastlinesCoverWholeWorld(t *testing.T) {
	minLon, maxLon := 180.0, -180.0
	minLat, maxLat := 90.0, -90.0
	for _, line := range Coastlines() {
		for _, p := range line {
			minLon = math.Min(minLon, p.Lon)
			maxLon = math.Max(maxLon, p.Lon)
			minLat = math.Min(minLat, p.Lat)
			maxLat = math.Max(maxLat, p.Lat)
		}
	}
	// 南北跨度应覆盖到南极与北极圈附近，经度铺满全球
	if minLat > -80 || maxLat < 80 {
		t.Errorf("纬度覆盖 = %.2f ~ %.2f, 应接近 ±85", minLat, maxLat)
	}
	if minLon > -179 || maxLon < 179 {
		t.Errorf("经度覆盖 = %.2f ~ %.2f, 应铺满 ±180", minLon, maxLon)
	}
}

func TestCoastlinesMatchRealGeography(t *testing.T) {
	lines := Coastlines()
	for _, lm := range coastlineLandmarks {
		got := nearestCoastlineKm(LonLat{Lon: lm.Lon, Lat: lm.Lat}, lines)
		if got > 120 {
			t.Errorf("%s 距最近海岸线 %.1f km，超出 110m 数据的概化误差，轮廓与真实地理不符",
				lm.Name, got)
		}
	}
	for _, probe := range oceanProbes {
		got := nearestCoastlineKm(LonLat{Lon: probe.Lon, Lat: probe.Lat}, lines)
		if got < 500 {
			t.Errorf("%s 距最近海岸线仅 %.1f km，该处本应是大洋中心", probe.Name, got)
		}
	}
}

func TestDecodeCoastlinesRejectsBadData(t *testing.T) {
	good := coastlineAsset
	cases := map[string][]byte{
		"nil":        nil,
		"empty":      {},
		"bad magic":  []byte("XXXX\x00\x00"),
		"truncated":  good[:len(good)-4],
		"len prefix": append([]byte(coastlineMagic), 0xFF, 0x7F),
	}
	for name, data := range cases {
		if got := decodeCoastlines(data); got != nil {
			t.Errorf("%s: 期望 nil, got %d 条折线", name, len(got))
		}
	}
	if got := decodeCoastlines(good); len(got) == 0 {
		t.Error("正常数据不应解析为空")
	}
}

func TestMapPlotRectKeepsEqualAspect(t *testing.T) {
	// 等经纬投影下绘图区必须严格 2:1，否则大陆轮廓会被拉伸
	for _, box := range [][2]float64{{1456, 822}, {1000, 500}, {600, 320}, {2000, 400}, {400, 900}} {
		x, y, w, h := mapPlotRect(box[0], box[1], 1)
		if w <= 0 || h <= 0 {
			t.Fatalf("%v: 绘图区为空 (%gx%g)", box, w, h)
		}
		if math.Abs(w/h-2) > 1e-9 {
			t.Errorf("%v: 绘图区宽高比 = %.4f, want 2", box, w/h)
		}
		if x < 0 || y < 0 || x+w > box[0] || y+h > box[1] {
			t.Errorf("%v: 绘图区越界 (%.1f, %.1f, %.1f, %.1f)", box, x, y, w, h)
		}
	}
}

func TestWorldMapPanelHeightFillsPlotArea(t *testing.T) {
	for _, width := range []int{728, 500, 960} {
		height := WorldMapPanelHeight(width)
		_, _, w, h := mapPlotRect(float64(width*panelScale), float64(height*panelScale), float64(panelScale))
		wantW := float64(width-mapPadLeft-mapPadRight) * panelScale
		if math.Abs(w-wantW) > 2 {
			t.Errorf("宽度 %d: 绘图区宽 = %.1f, want ≈%.1f（面板高度未吃满？）", width, w, wantW)
		}
		if math.Abs(h-wantW/2) > 2 {
			t.Errorf("宽度 %d: 绘图区高 = %.1f, want ≈%.1f", width, h, wantW/2)
		}
	}
}

// probeTheme 只保留海岸线（纯白、不透明），其余元素全部透明，
// 这样渲染结果中的每一个非透明像素都必然来自海岸线轮廓。
func probeTheme() CardTheme {
	return CardTheme{Coast: color.NRGBA{R: 255, G: 255, B: 255, A: 255}}
}

func TestGroundTrackPanelPlacesCoastlinesAtRealCoordinates(t *testing.T) {
	const width = 728
	img := GroundTrackPanel(probeTheme(), MapSpec{
		Width: width, Height: WorldMapPanelHeight(width), Lat: 0, Lon: 0,
	})
	bounds := img.Bounds()

	x, y, pw, ph := mapPlotRect(float64(bounds.Dx()), float64(bounds.Dy()), float64(panelScale))
	projX := func(lon float64) float64 { return x + (lon+180)/360*pw }
	projY := func(lat float64) float64 { return y + (90-lat)/180*ph }

	radius := float64(10 * panelScale) // 设备像素，约合 2.7°（300 km）
	for _, lm := range coastlineLandmarks {
		if !hasCoastPixelNear(img, projX(lm.Lon), projY(lm.Lat), radius) {
			t.Errorf("%s (%.2f, %.2f) 处未见海岸线像素，渲染位置与真实地理位置不符",
				lm.Name, lm.Lon, lm.Lat)
		}
	}
	for _, probe := range oceanProbes {
		if hasCoastPixelNear(img, projX(probe.Lon), projY(probe.Lat), radius) {
			t.Errorf("%s 附近出现海岸线像素，轮廓存在大范围错位或假线段", probe.Name)
		}
	}
}

func TestGroundTrackPanelCoastlineSpansMap(t *testing.T) {
	const width = 728
	img := GroundTrackPanel(probeTheme(), MapSpec{
		Width: width, Height: WorldMapPanelHeight(width), Lat: 0, Lon: 0,
	})
	minX, minY := img.Bounds().Dx(), img.Bounds().Dy()
	maxX, maxY := 0, 0
	for px := range img.Bounds().Dx() {
		for py := range img.Bounds().Dy() {
			if isCoastPixel(img.At(px, py)) {
				minX, maxX = min(minX, px), max(maxX, px)
				minY, maxY = min(minY, py), max(maxY, py)
			}
		}
	}
	x, y, pw, ph := mapPlotRect(float64(img.Bounds().Dx()), float64(img.Bounds().Dy()), float64(panelScale))
	// 海岸线应铺满绘图区：东西向到达两侧边缘（白令海峡 / 南极），
	// 南北向覆盖到阿拉斯加与南极洲附近
	if float64(minX) > x+4 || float64(maxX) < x+pw-4 {
		t.Errorf("海岸线横向范围 = %d~%d, 绘图区 %.0f~%.0f", minX, maxX, x, x+pw)
	}
	if float64(minY) > y+ph*0.15 || float64(maxY) < y+ph*0.9 {
		t.Errorf("海岸线纵向范围 = %d~%d, 绘图区 %.0f~%.0f", minY, maxY, y, y+ph)
	}
}

// hasCoastPixelNear 判断 (cx, cy) 邻域内是否存在海岸线像素。
func hasCoastPixelNear(img *image.RGBA, cx, cy, radius float64) bool {
	for dx := -radius; dx <= radius; dx++ {
		for dy := -radius; dy <= radius; dy++ {
			px, py := int(cx+dx), int(cy+dy)
			if !image.Pt(px, py).In(img.Bounds()) {
				continue
			}
			if isCoastPixel(img.At(px, py)) {
				return true
			}
		}
	}
	return false
}

// isCoastPixel 判断像素是否来自海岸线描边。
//
// probeTheme 下海岸线是唯一的亮色（纯白不透明）元素：星下点十字准线取 Accent 的
// RGB 配固定 alpha，Accent 置零后得到的是“透明黑”，亮度极低而被排除；描边本身的
// 抗锯齿边缘因预乘 alpha 同比例下降，同样不影响判定。
func isCoastPixel(c color.Color) bool {
	r, g, b, a := c.RGBA()
	const bright = 0xC000
	return a > 0x8000 && r > bright && g > bright && b > bright
}

// nearestCoastlineKm 返回点到整条海岸线的最近距离（公里）。
func nearestCoastlineKm(p LonLat, lines [][]LonLat) float64 {
	best := math.Inf(1)
	for _, line := range lines {
		for i := 1; i < len(line); i++ {
			best = math.Min(best, segmentDistanceKm(p, line[i-1], line[i]))
		}
	}
	return best
}

// segmentDistanceKm 近似计算点到线段的最短距离（公里）。
//
// 在线段中点纬度处把经纬度按等距圆柱展开即可满足判别需要：本测试只区分
// “数十公里内的概化误差”与“数百公里以上的位置错误”。
func segmentDistanceKm(p, a, b LonLat) float64 {
	const kmPerDeg = 111.195 // 与地球平均半径 6371 km 对应
	scaleLon := math.Cos((a.Lat + b.Lat) / 2 * degToRad)

	ax, ay := a.Lon*scaleLon, a.Lat
	bx, by := b.Lon*scaleLon, b.Lat
	px, py := p.Lon*scaleLon, p.Lat

	dx, dy := bx-ax, by-ay
	t := 0.0
	if denom := dx*dx + dy*dy; denom > 0 {
		t = clampValue(((px-ax)*dx+(py-ay)*dy)/denom, 0, 1)
	}
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy)) * kmPerDeg
}
