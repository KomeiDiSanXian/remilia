// Package satutil 提供卫星轨道计算的通用工具，以及空间站卡片（CSS / ISS）
// 共用的深色玻璃拟态视觉组件。
//
// 注意：本文件中的颜色一律使用 color.NRGBA（非预乘）。image/color 的
// color.RGBA 是 alpha 预乘色彩空间，要求各通道值 ≤ alpha；若用
// color.RGBA{R:255, G:255, B:255, A:20} 这类"高通道 + 低 alpha"的字面量，
// 渲染器会按预乘值处理（RGB > A 属越界），最终 PNG 反预乘时通道被放大、
// 混色错乱，表现为莫名的彩色条纹或过度发白的面板。
package satutil

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// ─── 主题 ─────────────────────────────────────────────────────────────────────

// CardTheme 描述空间站卡片的配色方案（深色玻璃拟态风格）。
//
// 所有颜色均为 color.NRGBA，透明通道可自由使用。
type CardTheme struct {
	BGTop       color.NRGBA // 背景渐变起点
	BGMid       color.NRGBA // 背景渐变中部
	BGBottom    color.NRGBA // 背景渐变终点
	Glow        color.NRGBA // 顶部柔光颜色（A 为峰值透明度）
	Title       color.NRGBA // 标题与强调文字
	Label       color.NRGBA // 次级说明文字
	Value       color.NRGBA // 主要数值
	Muted       color.NRGBA // 弱化文字（脚注、坐标标签）
	Accent      color.NRGBA // 主强调色（曲线、标记、仪表）
	PanelFill   color.NRGBA // 面板填充（含 alpha）
	PanelStroke color.NRGBA // 面板描边（含 alpha）
	Track       color.NRGBA // 仪表轨道（含 alpha）
	Grid        color.NRGBA // 网格线（含 alpha）
	GridStrong  color.NRGBA // 强调网格线（赤道 / 本初子午线）
	Coast       color.NRGBA // 海岸线轮廓（含 alpha）
	Sea         color.NRGBA // 地图底色（含 alpha）
}

// ISSTheme 返回国际空间站卡片主题（深空靛蓝 + 青色强调）。
func ISSTheme() CardTheme {
	return CardTheme{
		BGTop:       color.NRGBA{R: 9, G: 14, B: 40, A: 255},
		BGMid:       color.NRGBA{R: 14, G: 22, B: 58, A: 255},
		BGBottom:    color.NRGBA{R: 9, G: 14, B: 38, A: 255},
		Glow:        color.NRGBA{R: 70, G: 130, B: 255, A: 78},
		Title:       color.NRGBA{R: 132, G: 204, B: 255, A: 255},
		Label:       color.NRGBA{R: 172, G: 192, B: 224, A: 255},
		Value:       color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Muted:       color.NRGBA{R: 126, G: 146, B: 180, A: 255},
		Accent:      color.NRGBA{R: 96, G: 214, B: 255, A: 255},
		PanelFill:   color.NRGBA{R: 160, G: 200, B: 255, A: 26},
		PanelStroke: color.NRGBA{R: 150, G: 180, B: 230, A: 74},
		Track:       color.NRGBA{R: 110, G: 140, B: 190, A: 108},
		Grid:        color.NRGBA{R: 104, G: 140, B: 200, A: 96},
		GridStrong:  color.NRGBA{R: 130, G: 170, B: 230, A: 150},
		Coast:       color.NRGBA{R: 132, G: 202, B: 190, A: 160},
		Sea:         color.NRGBA{R: 12, G: 20, B: 52, A: 158},
	}
}

// CSSTheme 返回中国空间站卡片主题（赤金暗红）。
func CSSTheme() CardTheme {
	return CardTheme{
		BGTop:       color.NRGBA{R: 52, G: 12, B: 16, A: 255},
		BGMid:       color.NRGBA{R: 78, G: 20, B: 22, A: 255},
		BGBottom:    color.NRGBA{R: 44, G: 12, B: 16, A: 255},
		Glow:        color.NRGBA{R: 255, G: 138, B: 72, A: 74},
		Title:       color.NRGBA{R: 255, G: 206, B: 120, A: 255},
		Label:       color.NRGBA{R: 244, G: 210, B: 192, A: 255},
		Value:       color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Muted:       color.NRGBA{R: 206, G: 158, B: 144, A: 255},
		Accent:      color.NRGBA{R: 255, G: 180, B: 84, A: 255},
		PanelFill:   color.NRGBA{R: 255, G: 210, B: 190, A: 26},
		PanelStroke: color.NRGBA{R: 232, G: 158, B: 126, A: 76},
		Track:       color.NRGBA{R: 176, G: 116, B: 96, A: 108},
		Grid:        color.NRGBA{R: 190, G: 122, B: 100, A: 96},
		GridStrong:  color.NRGBA{R: 226, G: 154, B: 112, A: 150},
		Coast:       color.NRGBA{R: 244, G: 222, B: 184, A: 168},
		Sea:         color.NRGBA{R: 44, G: 12, B: 16, A: 158},
	}
}

// ─── 字体 ─────────────────────────────────────────────────────────────────────

// panelScale 为超采样倍率：组件按逻辑尺寸 × panelScale 绘制，再由画布缩放，
// 使圆角、弧线与文字边缘保持锐利。
const panelScale = 2

var (
	fontMu    sync.Mutex
	faceCache = map[string]font.Face{}
)

// cjkFontPath 返回可用的 CJK 字体文件路径，找不到时返回空字符串。
func cjkFontPath() string {
	if p := textimage.SystemCJKFontPath(); p != "" {
		return p
	}
	for _, name := range []string{
		"simhei.ttf", "simkai.ttf", "msyh.ttc", "simsun.ttc",
		"NotoSansCJK-Regular.ttc", "wqy-microhei.ttc",
		"PingFang.ttc", "STHeiti Light.ttc",
	} {
		if p := textimage.SystemFontPath(name); p != "" {
			return p
		}
	}
	return ""
}

// boldFontPath 返回可用的粗体 CJK 字体路径（用于标题与主数值），找不到时返回空字符串。
func boldFontPath() string {
	for _, name := range []string{
		"msyhbd.ttc", "msyhbd.ttf", "simhei.ttf", "NotoSansCJK-Bold.ttc",
		"PingFang.ttc", "STHeiti Medium.ttc",
	} {
		if p := textimage.SystemFontPath(name); p != "" {
			return p
		}
	}
	return ""
}

// BoldFontOption 返回画布级粗体字体选项，用于标题与主数值的视觉层级；
// 未找到粗体字体时返回空操作选项（回退到常规字重）。
func BoldFontOption() textimage.Option {
	if p := boldFontPath(); p != "" {
		return textimage.WithFontPath(p)
	}
	return func(*textimage.Options) {}
}

// parseFontFile 解析 ttf/otf/ttc 字体文件（ttc 取第一个字体）。
func parseFontFile(path string, size float64) (font.Face, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(path)
	opts := &opentype.FaceOptions{Size: size, DPI: 72}
	if strings.HasSuffix(lower, ".ttc") || strings.HasSuffix(lower, ".otc") {
		col, err := opentype.ParseCollection(raw)
		if err != nil {
			return nil, err
		}
		f, err := col.Font(0)
		if err != nil {
			return nil, err
		}
		return opentype.NewFace(f, opts)
	}
	f, err := opentype.Parse(raw)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, opts)
}

// vectorFont 返回指定像素字号的 CJK 字体（字体面带缓存）。
func vectorFont(size float64, bold bool) font.Face {
	path := cjkFontPath()
	if bold {
		if p := boldFontPath(); p != "" {
			path = p
		}
	}
	if path == "" {
		return nil
	}
	key := fmt.Sprintf("%s|%.2f", path, size)

	fontMu.Lock()
	defer fontMu.Unlock()
	if face, ok := faceCache[key]; ok {
		return face // 可能是 nil：该字体加载失败，无需重复尝试
	}
	face, err := parseFontFile(path, size)
	if err != nil {
		face = nil
	}
	faceCache[key] = face
	return face
}

// useFont 将矢量画布的当前字体切换为 size 像素的 CJK 字体。
func useFont(vc *textimage.VectorCanvas, size float64) {
	if face := vectorFont(size, false); face != nil {
		vc.SetFontFace(face)
	}
}

// useBoldFont 将矢量画布的当前字体切换为粗体。
func useBoldFont(vc *textimage.VectorCanvas, size float64) {
	if face := vectorFont(size, true); face != nil {
		vc.SetFontFace(face)
	}
}

// ─── 背景 ─────────────────────────────────────────────────────────────────────

// CardBackground 生成卡片背景：斜向渐变 + 顶部柔光。
//
// h 只需给出一个"不小于绘制所需顶部区域"的估算值：作为背景图传入画布后
// 会按 cover 语义等比铺满（放大而非裁剪），因此建议略小于卡片最终高度，
// 以免顶部柔光被居中裁剪掉。
func CardBackground(t CardTheme, w, h int) image.Image {
	base := textimage.LinearGradient(w, h, 165,
		textimage.Stop(0.0, t.BGTop),
		textimage.Stop(0.55, t.BGMid),
		textimage.Stop(1.0, t.BGBottom),
	)

	vc := textimage.NewVectorCanvas(w, h)
	vc.DrawImageAnchored(base, 0, 0, 0, 0)

	// 顶部柔光：以径向渐变绘制（逐像素平滑，无环带），圆心对齐卡片上边缘。
	glow := textimage.RadialGradient(w, h,
		textimage.Stop(0.0, t.Glow),
		textimage.Stop(0.45, color.NRGBA{R: t.Glow.R, G: t.Glow.G, B: t.Glow.B, A: t.Glow.A * 2 / 5}),
		textimage.Stop(1.0, color.NRGBA{R: t.Glow.R, G: t.Glow.G, B: t.Glow.B, A: 0}),
	)
	vc.DrawImageAnchored(glow, w/2, 0, 0.5, 0.5)
	return vc.Image().(*image.RGBA)
}

// ─── 基础绘制辅助 ─────────────────────────────────────────────────────────────

// drawPanelBox 绘制玻璃面板底（圆角填充 + 描边）。
func drawPanelBox(vc *textimage.VectorCanvas, x, y, w, h, r float64, t CardTheme) {
	vc.SetColor(t.PanelFill)
	vc.DrawRoundedRectangle(x, y, w, h, r)
	vc.Fill()

	vc.SetColor(t.PanelStroke)
	vc.SetLineWidth(1)
	vc.DrawRoundedRectangle(x, y, w, h, r)
	vc.Stroke()
}

// smoothSamples 以 Catmull-Rom 样条对折线加密采样，得到平滑曲线坐标。
func smoothSamples(xs, ys []float64, perSegment int) ([]float64, []float64) {
	n := len(xs)
	if n < 3 || perSegment < 2 {
		return xs, ys
	}
	sx := make([]float64, 0, (n-1)*perSegment+1)
	sy := make([]float64, 0, (n-1)*perSegment+1)
	for i := 0; i < n-1; i++ {
		x0, y0 := xs[max(i-1, 0)], ys[max(i-1, 0)]
		x1, y1 := xs[i], ys[i]
		x2, y2 := xs[i+1], ys[i+1]
		x3, y3 := xs[min(i+2, n-1)], ys[min(i+2, n-1)]
		for s := range perSegment {
			u := float64(s) / float64(perSegment)
			sx = append(sx, catmullRom(x0, x1, x2, x3, u))
			sy = append(sy, catmullRom(y0, y1, y2, y3, u))
		}
	}
	sx = append(sx, xs[n-1])
	sy = append(sy, ys[n-1])
	return sx, sy
}

// catmullRom 计算均匀 Catmull-Rom 样条在参数 u∈[0,1] 上的插值。
func catmullRom(p0, p1, p2, p3, u float64) float64 {
	return 0.5 * ((2 * p1) +
		(-p0+p2)*u +
		(2*p0-5*p1+4*p2-p3)*u*u +
		(-p0+3*p1-3*p2+p3)*u*u*u)
}

// strokePolyline 沿给定坐标绘制折线（用于轨迹与曲线）。
func strokePolyline(vc *textimage.VectorCanvas, xs, ys []float64) {
	if len(xs) < 2 {
		return
	}
	vc.MoveTo(xs[0], ys[0])
	for i := 1; i < len(xs); i++ {
		vc.LineTo(xs[i], ys[i])
	}
	vc.Stroke()
}

// dashedLine 绘制一条虚线。
func dashedLine(vc *textimage.VectorCanvas, x0, y0, x1, y1, width, gap float64, c color.Color) {
	vc.SetColor(c)
	vc.SetLineWidth(width)
	vc.SetDash(gap, gap)
	vc.MoveTo(x0, y0)
	vc.LineTo(x1, y1)
	vc.Stroke()
	vc.SetDash()
}

// niceTicks 生成落在"整齐"数值上的刻度序列，并返回对应的标签格式。
// 例如区间 383~392 会得到 384/386/388/390/392（步长 2），而不是 383.2/385.4…。
func niceTicks(minV, maxV float64, target int) ([]float64, string) {
	span := maxV - minV
	if span <= 0 {
		span = 1
	}
	if target < 2 {
		target = 2
	}
	raw := span / float64(target-1)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step := 10 * mag
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if s := m * mag; s >= raw {
			step = s
			break
		}
	}
	var ticks []float64
	for v := math.Ceil(minV/step) * step; v <= maxV+1e-9; v += step {
		ticks = append(ticks, v)
	}
	format := "%.1f"
	if step >= 1 {
		format = "%.0f"
	}
	return ticks, format
}

// lonLabel 将经度格式化为紧凑标签（如 120°E / 0° / 180°）。
func lonLabel(deg float64) string {
	switch {
	case deg == 0:
		return "0°"
	case math.Abs(deg) == 180:
		return "180°"
	case deg > 0:
		return fmt.Sprintf("%.0f°E", deg)
	default:
		return fmt.Sprintf("%.0f°W", -deg)
	}
}

// latLabel 将纬度格式化为紧凑标签（如 60°N / 30°S / 0°）。
func latLabel(deg float64) string {
	switch {
	case deg == 0:
		return "0°"
	case deg > 0:
		return fmt.Sprintf("%.0f°N", deg)
	default:
		return fmt.Sprintf("%.0f°S", -deg)
	}
}

// splitTrack 按经度 ±180° 把轨迹拆成多段，并在两侧补上插值出的边界交点，
// 使星下点轨迹延伸到地图左右边缘，而不是在边界内侧突然截断。
func splitTrack(track []TrackPoint) [][]TrackPoint {
	if len(track) == 0 {
		return nil
	}
	segs := make([][]TrackPoint, 0, 2)
	cur := []TrackPoint{track[0]}
	for _, p := range track[1:] {
		prev := cur[len(cur)-1]
		if math.Abs(p.Lon-prev.Lon) > 180 {
			crossLat, crossTime := antimeridianCross(prev, p)
			edge := math.Copysign(180, prev.Lon)
			cur = append(cur, TrackPoint{Time: crossTime, Lat: crossLat, Lon: edge})
			segs = append(segs, cur)
			cur = []TrackPoint{{Time: crossTime, Lat: crossLat, Lon: -edge}}
		}
		cur = append(cur, p)
	}
	segs = append(segs, cur)
	return segs
}

// antimeridianCross 返回从 prev 到 next 跨越 ±180° 经线时的交点纬度与时刻。
func antimeridianCross(prev, next TrackPoint) (lat float64, at time.Time) {
	span := next.Lon - prev.Lon
	target := -180.0
	if span < -180 { // 向东跨越 +180°
		span += 360
		target = 180
	} else { // 向西跨越 −180°
		span -= 360
	}
	if span == 0 {
		return prev.Lat, prev.Time
	}
	ratio := (target - prev.Lon) / span
	return prev.Lat + (next.Lat-prev.Lat)*ratio,
		prev.Time.Add(time.Duration(float64(next.Time.Sub(prev.Time)) * ratio))
}

// ─── 数据面板（弧形仪表） ──────────────────────────────────────────────────────

// StatPanelSpec 描述一个数据面板。
type StatPanelSpec struct {
	Width  int    // 逻辑宽度（像素）
	Height int    // 逻辑高度（像素）
	Title  string // 顶部标题
	Value  string // 主数值
	Sub    string // 数值下方说明（可选）
	Arc    float64
	// Arc < 0 时不绘制仪表，仅显示数值；Arc ∈ [0,1] 时绘制 270° 弧形仪表。
	ArcColor color.Color // 仪表填充色（Arc >= 0 时必需）
}

// StatPanel 渲染一个玻璃拟态数据面板：标题 + 弧形仪表 + 数值 + 说明。
//
// 返回图片为逻辑尺寸 × [panelScale]，调用方应使用
// [textimage.WithImgWidth]（传逻辑宽度）缩放。
func StatPanel(t CardTheme, spec StatPanelSpec) *image.RGBA {
	w := max(spec.Width, 1) * panelScale
	h := max(spec.Height, 1) * panelScale
	s := float64(panelScale)
	vc := textimage.NewVectorCanvas(w, h)
	drawPanelBox(vc, 0.5, 0.5, float64(w)-1, float64(h)-1, 12*s, t)

	pad := 14 * s
	useFont(vc, 13*s)
	vc.SetColor(t.Label)
	vc.DrawStringAnchored(spec.Title, pad, 21*s, 0, 0.5)

	cx := float64(w) / 2

	// 说明文字的垂直位置固定预留，确保不会被面板底边裁切。
	subH := 28 * s
	subCenterY := float64(h) - 6*s - subH/2

	if spec.Arc < 0 {
		useBoldFont(vc, 27*s)
		vc.SetColor(t.Value)
		vc.DrawStringAnchored(spec.Value, cx, (float64(40*s)+subCenterY-subH/2)/2, 0.5, 0.5)
		if spec.Sub != "" {
			useFont(vc, 12*s)
			vc.SetColor(t.Muted)
			vc.DrawStringAnchored(spec.Sub, cx, subCenterY, 0.5, 0.5)
		}
		return vc.Image().(*image.RGBA)
	}

	// 仪表圆环区：位于标题与说明文字之间，两个方向均需完整容纳。
	gaugeTop := 42 * s
	gaugeBottom := subCenterY - subH/2 - 6*s
	cy := (gaugeTop + gaugeBottom) / 2
	radius := math.Min(float64(w)*0.23, (gaugeBottom-gaugeTop)/2)
	if radius < 10*s {
		radius = 10 * s
	}
	const start = 0.75 * math.Pi
	const sweep = 1.5 * math.Pi

	vc.SetLineCapRound()
	vc.SetColor(t.Track)
	vc.SetLineWidth(6 * s)
	vc.DrawArc(cx, cy, radius, start, start+sweep)
	vc.Stroke()

	// 非零值至少给出一小段可见弧度，避免极小的数值看起来像渲染残点。
	frac := clamp01(spec.Arc)
	if frac > 0 && frac < 0.05 {
		frac = 0.05
	}
	if frac > 0 {
		vc.SetColor(spec.ArcColor)
		vc.SetLineWidth(6 * s)
		vc.DrawArc(cx, cy, radius, start, start+sweep*frac)
		vc.Stroke()

		end := start + sweep*frac
		vc.SetColor(spec.ArcColor)
		vc.DrawCircle(cx+radius*math.Cos(end), cy+radius*math.Sin(end), 3.4*s)
		vc.Fill()
	}

	useBoldFont(vc, 22*s)
	vc.SetColor(t.Value)
	vc.DrawStringAnchored(spec.Value, cx, cy, 0.5, 0.5)

	if spec.Sub != "" {
		useFont(vc, 12*s)
		vc.SetColor(t.Muted)
		vc.DrawStringAnchored(spec.Sub, cx, subCenterY, 0.5, 0.5)
	}
	return vc.Image().(*image.RGBA)
}

// ─── 标签墙（自动换行） ────────────────────────────────────────────────────────

// ChipSpec 描述一组自动换行的标签。
type ChipSpec struct {
	Width int      // 逻辑宽度（像素）
	Title string   // 面板标题（可选）
	Chips []string // 标签文字
}

// ChipWall 将标签按可用宽度自动换行排布，返回一个高度自适应的面板。
func ChipWall(t CardTheme, spec ChipSpec) *image.RGBA {
	width := max(spec.Width, 40)
	w := width * panelScale
	s := float64(panelScale)

	measure := textimage.NewVectorCanvas(1, 1)
	useFont(measure, 13*s)
	titleH := 0.0
	if spec.Title != "" {
		titleH = 30 * s
	}
	padX, padY := 14*s, 12*s
	chipPadX, chipPadY := 12*s, 6*s
	gapX, gapY := 8*s, 9*s

	avail := float64(w) - 2*padX
	type chipBox struct {
		text string
		w    float64
		h    float64
	}
	boxes := make([]chipBox, 0, len(spec.Chips))
	for _, c := range spec.Chips {
		text := strings.TrimSpace(c)
		if text == "" {
			continue
		}
		tw, th := measure.MeasureString(text)
		boxes = append(boxes, chipBox{text: text, w: tw + 2*chipPadX, h: th + 2*chipPadY})
	}

	// 排布：按行累积宽度，超出可用宽度则换行。
	lines := make([][]int, 0, 2)
	var line []int
	lineW := 0.0
	for i, b := range boxes {
		if len(line) > 0 && lineW+gapX+b.w > avail {
			lines = append(lines, line)
			line = nil
			lineW = 0
		}
		line = append(line, i)
		lineW += b.w + gapX
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}

	contentH := 0.0
	for _, ln := range lines {
		rowH := 0.0
		for _, idx := range ln {
			rowH = math.Max(rowH, boxes[idx].h)
		}
		contentH += rowH + gapY
	}
	height := titleH + contentH + 2*padY - gapY
	if len(lines) == 0 {
		height = titleH + 2*padY
	}

	h := int(math.Ceil(height))
	vc := textimage.NewVectorCanvas(w, h)
	drawPanelBox(vc, 0.5, 0.5, float64(w)-1, float64(h)-1, 12*s, t)

	y := padY
	if spec.Title != "" {
		useFont(vc, 13*s)
		vc.SetColor(t.Title)
		vc.DrawStringAnchored(spec.Title, padX, y+11*s, 0, 0.5)
		y += titleH
	}

	useFont(vc, 13*s)
	for _, ln := range lines {
		rowH := 0.0
		for _, idx := range ln {
			rowH = math.Max(rowH, boxes[idx].h)
		}
		x := padX
		for _, idx := range ln {
			b := boxes[idx]
			by := y + (rowH-b.h)/2
			vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 46})
			vc.DrawRoundedRectangle(x, by, b.w, b.h, b.h/2)
			vc.Fill()
			vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 120})
			vc.SetLineWidth(1)
			vc.DrawRoundedRectangle(x, by, b.w, b.h, b.h/2)
			vc.Stroke()

			vc.SetColor(t.Value)
			vc.DrawStringAnchored(b.text, x+b.w/2, by+b.h/2, 0.5, 0.5)
			x += b.w + gapX
		}
		y += rowH + gapY
	}
	return vc.Image().(*image.RGBA)
}

// ─── 地面轨迹图 ───────────────────────────────────────────────────────────────

// 地图面板的边距（逻辑像素）：左侧留给纬度刻度，顶部留给标题，
// 右侧留白，底部留给经度刻度与说明文字。
const (
	mapPadLeft   = 46
	mapPadTop    = 34
	mapPadRight  = 16
	mapPadBottom = 44
)

// WorldMapPanelHeight 返回给定宽度下绘制等距圆柱世界地图所需的面板高度（逻辑像素）。
//
// 等经纬投影下 360° 经度与 180° 纬度须占用相同比例（绘图区严格 2:1），否则大陆
// 轮廓会被拉伸变形；按该高度构造 MapSpec 可让绘图区正好填满面板。
func WorldMapPanelHeight(width int) int {
	return (width-mapPadLeft-mapPadRight)/2 + mapPadTop + mapPadBottom
}

// mapPlotRect 计算绘图区在面板内的位置与尺寸（设备像素）。
//
// 绘图区须保持 2:1 才与等经纬投影相符；面板比例不是 2:1 时按 2:1 收紧并居中，
// 宁可留白也不让大陆轮廓失真。
func mapPlotRect(panelW, panelH, s float64) (x, y, w, h float64) {
	x = mapPadLeft * s
	y = mapPadTop * s
	w = panelW - x - mapPadRight*s
	h = panelH - y - mapPadBottom*s
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0
	}
	switch {
	case w > 2*h:
		shrink := w - 2*h
		x += shrink / 2
		w = 2 * h
	case h > w/2:
		shrink := h - w/2
		y += shrink / 2
		h = w / 2
	}
	return x, y, w, h
}

// MapSpec 描述一张等距圆柱（等经纬）投影的地面轨迹图。
type MapSpec struct {
	Width        int          // 逻辑宽度
	Height       int          // 逻辑高度
	Track        []TrackPoint // 地面轨迹点
	Now          time.Time    // 区分历史（虚线）与未来（实线）；为零值时全部按未来绘制
	Lat, Lon     float64      // 当前星下点
	FootprintDeg float64      // 可见范围地心角半径（度）；<= 0 不绘制
	Title        string       // 左上角标题
	Note         string       // 右上角说明
	Caption      string       // 底部说明
}

// GroundTrackPanel 绘制地面轨迹图：经纬网 + 轨迹 + 可见覆盖圈 + 当前星下点。
func GroundTrackPanel(t CardTheme, spec MapSpec) *image.RGBA {
	w := max(spec.Width, 80) * panelScale
	h := max(spec.Height, 80) * panelScale
	s := float64(panelScale)
	vc := textimage.NewVectorCanvas(w, h)
	drawPanelBox(vc, 0.5, 0.5, float64(w)-1, float64(h)-1, 12*s, t)

	useFont(vc, 13*s)
	vc.SetColor(t.Title)
	vc.DrawStringAnchored(spec.Title, 16*s, 21*s, 0, 0.5)
	if spec.Note != "" {
		useFont(vc, 11*s)
		vc.SetColor(t.Muted)
		vc.DrawStringAnchored(spec.Note, float64(w)-16*s, 21*s, 1, 0.5)
	}

	// 绘图区：等经纬投影下须保持 2:1，否则大陆轮廓会被拉伸。
	plotX, plotY, plotW, plotH := mapPlotRect(float64(w), float64(h), s)
	if plotW < 10 || plotH < 10 {
		return vc.Image().(*image.RGBA)
	}
	projX := func(lon float64) float64 { return plotX + (lon+180)/360*plotW }
	projY := func(lat float64) float64 { return plotY + (90-lat)/180*plotH }
	panelPath := func() {
		vc.DrawRoundedRectangle(plotX, plotY, plotW, plotH, 6*s)
	}

	// 海洋底色
	vc.SetColor(t.Sea)
	panelPath()
	vc.Fill()

	// 经纬网
	useFont(vc, 10*s)
	for lat := -60.0; lat <= 60; lat += 30 {
		y := projY(lat)
		if lat == 0 {
			vc.SetColor(t.GridStrong)
			vc.SetLineWidth(1.1 * s)
		} else {
			vc.SetColor(t.Grid)
			vc.SetLineWidth(0.9 * s)
		}
		vc.MoveTo(plotX, y)
		vc.LineTo(plotX+plotW, y)
		vc.Stroke()

		vc.SetColor(t.Muted)
		vc.DrawStringAnchored(latLabel(lat), plotX-8*s, y, 1, 0.5)
	}
	for lon := -180.0; lon <= 180; lon += 60 {
		x := projX(lon)
		if lon == 0 || math.Abs(lon) == 180 {
			vc.SetColor(t.GridStrong)
			vc.SetLineWidth(1.1 * s)
		} else {
			vc.SetColor(t.Grid)
			vc.SetLineWidth(0.9 * s)
		}
		vc.MoveTo(x, plotY)
		vc.LineTo(x, plotY+plotH)
		vc.Stroke()

		vc.SetColor(t.Muted)
		label := lonLabel(lon)
		switch {
		case lon <= -180:
			vc.DrawStringAnchored(label, x+3*s, plotY+plotH+12*s, 0, 0.5)
		case lon >= 180:
			vc.DrawStringAnchored(label, x-3*s, plotY+plotH+12*s, 1, 0.5)
		default:
			vc.DrawStringAnchored(label, x, plotY+plotH+12*s, 0.5, 0.5)
		}
	}

	// 海岸线轮廓：数据与地图同为等经纬坐标，因此与经纬网、星下点轨迹严格对齐。
	vc.Push()
	panelPath()
	vc.Clip()
	drawCoastlines(vc, t, projX, projY, 1.3*s)
	vc.Pop()

	// 可见覆盖范围（近似椭圆：纬度按地心角，经度按纬度拉伸）
	if spec.FootprintDeg > 0 {
		cosLat := math.Cos(spec.Lat * degToRad)
		if cosLat < 0.15 {
			cosLat = 0.15
		}
		vc.Push()
		panelPath()
		vc.Clip()
		cx, cy := projX(spec.Lon), projY(spec.Lat)
		rx := spec.FootprintDeg / cosLat * (plotW / 360)
		ry := spec.FootprintDeg * (plotH / 180)
		vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 34})
		vc.DrawEllipse(cx, cy, rx, ry)
		vc.Fill()
		vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 150})
		vc.SetLineWidth(1 * s)
		vc.SetDash(5*s, 5*s)
		vc.DrawEllipse(cx, cy, rx, ry)
		vc.Stroke()
		vc.SetDash()
		vc.Pop()
	}

	// 轨迹：以当前时刻为界分成已飞过（虚线）与待飞行（实线）两段，
	// 各自再按 ±180° 经线断开，并在两侧补上边界交点，避免出现
	// 横贯全图的假线段或悬空的孤立线段。
	projXs := func(seg []TrackPoint) ([]float64, []float64) {
		xs := make([]float64, len(seg))
		ys := make([]float64, len(seg))
		for i, p := range seg {
			xs[i] = projX(p.Lon)
			ys[i] = projY(p.Lat)
		}
		return xs, ys
	}
	drawSeg := func(seg []TrackPoint, past bool) {
		if len(seg) < 2 {
			return
		}
		xs, ys := projXs(seg)
		vc.SetLineCapRound()
		if past {
			vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 135})
			vc.SetLineWidth(1.9 * s)
			vc.SetDash(9*s, 3.5*s)
			strokePolyline(vc, xs, ys)
			vc.SetDash()
			return
		}
		vc.SetLineJoinRound()
		vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 60})
		vc.SetLineWidth(6 * s)
		strokePolyline(vc, xs, ys)
		vc.SetColor(t.Accent)
		vc.SetLineWidth(2.2 * s)
		strokePolyline(vc, xs, ys)
	}

	drawTrack := func(points []TrackPoint, past bool) {
		for _, seg := range splitTrack(points) {
			drawSeg(seg, past)
		}
	}
	if spec.Now.IsZero() {
		drawTrack(spec.Track, false)
	} else {
		var pastPts, futurePts []TrackPoint
		for _, p := range spec.Track {
			if p.Time.Before(spec.Now) {
				pastPts = append(pastPts, p)
			} else {
				futurePts = append(futurePts, p)
			}
		}
		drawTrack(pastPts, true)
		drawTrack(futurePts, false)
	}

	// 当前星下点：十字准线 + 双层圆点
	mx, my := projX(spec.Lon), projY(spec.Lat)
	vc.Push()
	panelPath()
	vc.Clip()
	dashedLine(vc, plotX, my, plotX+plotW, my, 0.9*s, 6*s,
		color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 95})
	dashedLine(vc, mx, plotY, mx, plotY+plotH, 0.9*s, 6*s,
		color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 95})
	vc.Pop()

	vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 90})
	vc.DrawCircle(mx, my, 9*s)
	vc.Fill()
	vc.SetColor(t.Value)
	vc.SetLineWidth(1.8 * s)
	vc.DrawCircle(mx, my, 4.5*s)
	vc.Stroke()
	vc.SetColor(t.Accent)
	vc.DrawCircle(mx, my, 2.6*s)
	vc.Fill()

	if spec.Caption != "" {
		useFont(vc, 11*s)
		vc.SetColor(t.Muted)
		vc.DrawStringAnchored(spec.Caption, float64(w)/2, float64(h)-14*s, 0.5, 0.5)
	}
	return vc.Image().(*image.RGBA)
}

// drawCoastlines 在绘图区内描绘世界海岸线轮廓（Natural Earth 110m）。
//
// 折线经纬度与 projX / projY 使用同一套等经纬投影，因此描出的轮廓与经纬网、
// 星下点轨迹严格对齐，不需任何近似拟合。折线数据已在 ±180° 处断开，逐段描绘
// 不会产生横贯全图的假线段；越界部分由调用方裁剪。
func drawCoastlines(vc *textimage.VectorCanvas, t CardTheme, projX, projY func(float64) float64, lineWidth float64) {
	lines := Coastlines()
	if len(lines) == 0 {
		return
	}
	vc.SetColor(t.Coast)
	vc.SetLineWidth(lineWidth)
	vc.SetLineCapRound()
	vc.SetLineJoinRound()
	for _, line := range lines {
		if len(line) < 2 {
			continue
		}
		vc.MoveTo(projX(line[0].Lon), projY(line[0].Lat))
		for _, p := range line[1:] {
			vc.LineTo(projX(p.Lon), projY(p.Lat))
		}
	}
	vc.Stroke()
}

// ─── 高度曲线图 ───────────────────────────────────────────────────────────────

// ChartPoint 曲线图上的一个数据点。
type ChartPoint struct {
	Time  time.Time
	Value float64
}

// ChartSpec 描述一张高度曲线图。
type ChartSpec struct {
	Width    int          // 逻辑宽度
	Height   int          // 逻辑高度
	Points   []ChartPoint // 数据点（按时间升序）
	Title    string       // 左上角标题
	Unit     string       // 右上角单位说明
	Now      time.Time    // 当前时刻（用于定位“现在”标记）；为零值时不标记
	NowLabel string       // “现在”标记文字
	Caption  string       // 底部说明
}

// AltitudeChart 绘制高度曲线图：网格 + 平滑曲线 + 渐变填充 + 极值/当前点标记。
func AltitudeChart(t CardTheme, spec ChartSpec) *image.RGBA {
	if len(spec.Points) < 2 {
		return nil
	}
	w := max(spec.Width, 80) * panelScale
	h := max(spec.Height, 80) * panelScale
	s := float64(panelScale)
	vc := textimage.NewVectorCanvas(w, h)
	drawPanelBox(vc, 0.5, 0.5, float64(w)-1, float64(h)-1, 12*s, t)

	useFont(vc, 13*s)
	vc.SetColor(t.Title)
	vc.DrawStringAnchored(spec.Title, 16*s, 21*s, 0, 0.5)
	if spec.Unit != "" {
		useFont(vc, 11*s)
		vc.SetColor(t.Muted)
		vc.DrawStringAnchored(spec.Unit, float64(w)-16*s, 21*s, 1, 0.5)
	}

	plotX := 54 * s
	plotY := 34 * s
	plotW := float64(w) - plotX - 30*s
	plotH := float64(h) - plotY - 40*s
	if plotW < 10 || plotH < 10 {
		return vc.Image().(*image.RGBA)
	}

	minV, maxV := spec.Points[0].Value, spec.Points[0].Value
	for _, p := range spec.Points {
		minV = math.Min(minV, p.Value)
		maxV = math.Max(maxV, p.Value)
	}
	if maxV-minV < 0.2 {
		mid := (maxV + minV) / 2
		minV, maxV = mid-0.1, mid+0.1
	}
	padV := (maxV - minV) * 0.18
	yMin, yMax := minV-padV, maxV+padV
	yRange := yMax - yMin

	t0 := spec.Points[0].Time
	t1 := spec.Points[len(spec.Points)-1].Time
	spanNs := float64(t1.Sub(t0))
	if spanNs <= 0 {
		spanNs = 1
	}
	projX := func(tm time.Time) float64 { return plotX + plotW*float64(tm.Sub(t0))/spanNs }
	projY := func(v float64) float64 { return plotY + plotH*(1-(v-yMin)/yRange) }

	// Y 轴网格与刻度（取整齐数值）
	ticks, tickFormat := niceTicks(yMin, yMax, 5)
	useFont(vc, 10.5*s)
	for _, val := range ticks {
		y := projY(val)
		vc.SetColor(t.Grid)
		vc.SetLineWidth(0.9 * s)
		vc.SetDash(4*s, 4*s)
		vc.MoveTo(plotX, y)
		vc.LineTo(plotX+plotW, y)
		vc.Stroke()
		vc.SetDash()
		vc.SetColor(t.Muted)
		vc.DrawStringAnchored(fmt.Sprintf(tickFormat, val), plotX-9*s, y, 1, 0.5)
	}

	// X 轴时间刻度
	const xTicks = 5
	for i := range xTicks {
		frac := float64(i) / float64(xTicks-1)
		x := plotX + plotW*frac
		tm := t0.Add(time.Duration(frac * spanNs))
		vc.SetColor(t.Grid)
		vc.SetLineWidth(0.9 * s)
		vc.MoveTo(x, plotY)
		vc.LineTo(x, plotY+plotH)
		vc.Stroke()

		vc.SetColor(t.Muted)
		var ax float64
		switch i {
		case 0:
			ax = 0
		case xTicks - 1:
			ax = 1
		default:
			ax = 0.5
		}
		vc.DrawStringAnchored(tm.UTC().Format("15:04"), x, plotY+plotH+12*s, ax, 0.5)
	}

	xs := make([]float64, len(spec.Points))
	ys := make([]float64, len(spec.Points))
	for i, p := range spec.Points {
		xs[i] = projX(p.Time)
		ys[i] = projY(p.Value)
	}
	sx, sy := smoothSamples(xs, ys, 12)

	// 渐变填充（裁剪路径后用渐变图片着色）
	gradImg := textimage.LinearGradient(int(plotW)+1, int(plotH)+1, 90,
		textimage.Stop(0, color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 132}),
		textimage.Stop(1, color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 6}),
	)
	vc.Push()
	vc.MoveTo(sx[0], plotY+plotH)
	for i := range sx {
		vc.LineTo(sx[i], sy[i])
	}
	vc.LineTo(sx[len(sx)-1], plotY+plotH)
	vc.ClosePath()
	vc.Clip()
	vc.DrawImageAnchored(gradImg, int(plotX), int(plotY), 0, 0)
	vc.Pop()

	// 曲线：外发光 + 主线
	vc.SetLineCapRound()
	vc.SetLineJoinRound()
	vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 46})
	vc.SetLineWidth(7 * s)
	strokePolyline(vc, sx, sy)
	vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 110})
	vc.SetLineWidth(3.4 * s)
	strokePolyline(vc, sx, sy)
	vc.SetColor(t.Accent)
	vc.SetLineWidth(1.8 * s)
	strokePolyline(vc, sx, sy)

	// 极值点
	minIdx, maxIdx := 0, 0
	for i, p := range spec.Points {
		if p.Value < spec.Points[minIdx].Value {
			minIdx = i
		}
		if p.Value > spec.Points[maxIdx].Value {
			maxIdx = i
		}
	}
	for _, idx := range []int{minIdx, maxIdx} {
		vc.SetColor(t.Label)
		vc.DrawCircle(xs[idx], ys[idx], 2.8*s)
		vc.Fill()
	}

	// “现在”标记
	if !spec.Now.IsZero() {
		nx := projX(spec.Now)
		if nx >= plotX && nx <= plotX+plotW {
			dashedLine(vc, nx, plotY, nx, plotY+plotH, 0.9*s, 6*s,
				color.NRGBA{R: t.Value.R, G: t.Value.G, B: t.Value.B, A: 120})
			best := 0
			bestDiff := math.MaxFloat64
			for i, p := range spec.Points {
				d := math.Abs(float64(p.Time.Sub(spec.Now)))
				if d < bestDiff {
					bestDiff = d
					best = i
				}
			}
			vc.SetColor(color.NRGBA{R: t.Accent.R, G: t.Accent.G, B: t.Accent.B, A: 80})
			vc.DrawCircle(xs[best], ys[best], 8*s)
			vc.Fill()
			vc.SetColor(t.Value)
			vc.DrawCircle(xs[best], ys[best], 3.6*s)
			vc.Fill()

			if spec.NowLabel != "" {
				useFont(vc, 11*s)
				vc.SetColor(t.Value)
				// 靠右时改为右对齐，避免标签超出面板右边缘。
				lx, ax := xs[best]+10*s, 0.0
				if xs[best] > plotX+plotW*0.7 {
					lx, ax = xs[best]-10*s, 1.0
				}
				vc.DrawStringAnchored(spec.NowLabel, lx, math.Max(ys[best]-12*s, plotY+10*s), ax, 0.5)
			}
		}
	}

	if spec.Caption != "" {
		useFont(vc, 11*s)
		vc.SetColor(t.Muted)
		vc.DrawStringAnchored(spec.Caption, float64(w)/2, float64(h)-13*s, 0.5, 0.5)
	}
	return vc.Image().(*image.RGBA)
}

// ─── 图标 ─────────────────────────────────────────────────────────────────────

// iconCanvas 创建一个边长为 size × [panelScale] 的方形画布。
func iconCanvas(size int) (*textimage.VectorCanvas, float64) {
	dim := max(size, 8) * panelScale
	return textimage.NewVectorCanvas(dim, dim), float64(dim)
}

// IconSpaceStation 绘制空间站图标（桁架 + 舱段 + 太阳能帆板）。
//
// 返回图片为 size × [panelScale]，调用方用 [textimage.WithImgWidth] 传 size 缩放。
func IconSpaceStation(size int, c color.Color) *image.RGBA {
	vc, dim := iconCanvas(size)
	cx, cy := dim/2, dim/2
	cr, cg, cb, _ := c.RGBA()
	base := func(a uint8) color.NRGBA {
		return color.NRGBA{R: uint8(cr >> 8), G: uint8(cg >> 8), B: uint8(cb >> 8), A: a}
	}

	// 主桁架
	vc.SetColor(base(200))
	vc.DrawRoundedRectangle(dim*0.10, cy-dim*0.028, dim*0.80, dim*0.056, dim*0.028)
	vc.Fill()

	// 四片太阳能帆板（外侧略窄、内侧略宽）
	type panel struct{ x0, x1, y0, y1 float64 }
	panels := []panel{
		{0.02, 0.15, 0.28, 0.72},
		{0.17, 0.30, 0.24, 0.76},
		{0.70, 0.83, 0.24, 0.76},
		{0.85, 0.98, 0.28, 0.72},
	}
	for _, p := range panels {
		x, y := dim*p.x0, dim*p.y0
		pw, ph := dim*(p.x1-p.x0), dim*(p.y1-p.y0)
		vc.SetColor(base(70))
		vc.DrawRoundedRectangle(x, y, pw, ph, dim*0.02)
		vc.Fill()
		vc.SetColor(base(170))
		vc.SetLineWidth(1)
		vc.DrawRoundedRectangle(x, y, pw, ph, dim*0.02)
		vc.Stroke()

		// 电池片分隔线
		vc.SetColor(base(110))
		vc.SetLineWidth(0.8)
		for i := 1; i <= 3; i++ {
			gx := x + pw*float64(i)/4
			vc.MoveTo(gx, y+ph*0.08)
			vc.LineTo(gx, y+ph*0.92)
			vc.Stroke()
		}
		for i := 1; i <= 2; i++ {
			gy := y + ph*float64(i)/3
			vc.MoveTo(x+pw*0.08, gy)
			vc.LineTo(x+pw*0.92, gy)
			vc.Stroke()
		}
	}

	// 核心舱段
	vc.SetColor(base(245))
	vc.DrawRoundedRectangle(dim*0.38, cy-dim*0.115, dim*0.24, dim*0.23, dim*0.05)
	vc.Fill()
	vc.SetColor(color.NRGBA{R: 255, G: 255, B: 255, A: 210})
	vc.SetLineWidth(1.2)
	vc.DrawRoundedRectangle(dim*0.42, cy-dim*0.075, dim*0.16, dim*0.15, dim*0.03)
	vc.Stroke()

	// 对接口 / 天线
	vc.SetColor(base(200))
	vc.SetLineWidth(dim * 0.018)
	vc.MoveTo(cx, cy+dim*0.115)
	vc.LineTo(cx, cy+dim*0.22)
	vc.Stroke()
	vc.DrawCircle(cx, cy+dim*0.245, dim*0.028)
	vc.Fill()

	return vc.Image().(*image.RGBA)
}

// IconSun 绘制太阳图标（圆盘 + 八道光芒）。
func IconSun(size int, c color.Color) *image.RGBA {
	vc, dim := iconCanvas(size)
	cx, cy := dim/2, dim/2
	vc.SetColor(c)
	vc.DrawCircle(cx, cy, dim*0.24)
	vc.Fill()
	vc.SetLineWidth(dim * 0.055)
	vc.SetLineCapRound()
	for i := range 8 {
		a := float64(i) * math.Pi / 4
		vc.MoveTo(cx+dim*0.33*math.Cos(a), cy+dim*0.33*math.Sin(a))
		vc.LineTo(cx+dim*0.46*math.Cos(a), cy+dim*0.46*math.Sin(a))
		vc.Stroke()
	}
	return vc.Image().(*image.RGBA)
}

// IconMoon 绘制月牙图标（偶奇填充实现）。
func IconMoon(size int, c color.Color) *image.RGBA {
	vc, dim := iconCanvas(size)
	cx, cy := dim/2, dim/2
	r := dim * 0.34
	vc.SetFillRuleEvenOdd()
	vc.SetColor(c)
	vc.DrawCircle(cx, cy, r)
	vc.DrawCircle(cx+r*0.42, cy-r*0.28, r*0.92)
	vc.Fill()
	vc.SetFillRuleWinding()
	return vc.Image().(*image.RGBA)
}

// clamp01 将 v 截断到 [0, 1]。
func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
