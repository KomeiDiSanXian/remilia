package textimage

// vector.go — 基于 [gg.Context] 的矢量绘图画布（VectorCanvas）。
//
// 本文件为 textimage 包提供矢量绘图能力，弥补纯光栅 [Canvas] 无法完成的场景：
// 折线图、雷达图、任意路径、矩阵变换、自定义裁剪等。

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"

	"github.com/fogleman/gg"
	xdraw "golang.org/x/image/draw"
)

// VectorCanvas 是对 [gg.Context] 的 Bot 友好封装，提供矢量绘图能力。
//
// 所有 [gg.Context] 原生方法均通过嵌入直接可用
// （路径操作、变换矩阵、文字渲染、自定义裁剪等）；
// 本类型在此基础上增加以下 Bot 专属高层方法：
//
//   - [VectorCanvas.DrawAvatar]          — 圆形裁剪头像（等效于 CSS background-size:cover）
//   - [VectorCanvas.DrawProgressBar]     — 圆角进度条（直接坐标绘制）
//   - [VectorCanvas.DrawLineChart]       — 折线图
//   - [VectorCanvas.DrawLineChartFilled] — 填充面积图
//   - [VectorCanvas.DrawRadarChart]      — 雷达图（蛛网图）
//   - [VectorCanvas.DrawRadarGrid]       — 雷达图背景网格
//
// # 与 Canvas 的区别
//
// [Canvas] 是"块堆叠式"布局器：调用 AddText / AddImage / AddRow
// 将内容块由上到下自动拼合为合成图片，无需手动计算坐标。
//
// VectorCanvas 是"直接坐标绘图"：每个图形元素的位置、尺寸均由调用方指定，
// 适合需要精确控制排版、实现折线图 / 雷达图 / 自定义卡片边框等场景。
//
// 两者可以结合使用：先用 VectorCanvas 绘制矢量背景，再通过
// Canvas.AddImageBytes 将其作为背景图传入块布局。
//
// 创建：[NewVectorCanvas] 或 [NewVectorCard]。
// 输出：[VectorCanvas.ToPNG]、[VectorCanvas.ToJPEG]、
// [VectorCanvas.SavePNG]、[VectorCanvas.SaveJPEG]。
type VectorCanvas struct {
	*gg.Context
}

// NewVectorCanvas 创建指定像素尺寸的空白矢量画布。
func NewVectorCanvas(width, height int) *VectorCanvas {
	return &VectorCanvas{Context: gg.NewContext(width, height)}
}

// NewVectorCard 以黄金比例（宽÷φ ≈ 1.618）创建通知卡片尺寸的矢量画布。
// 常用宽度：600、800、1080。
func NewVectorCard(width int) *VectorCanvas {
	return NewVectorCanvas(width, int(float64(width)/1.618))
}

// ─── Bot 专属绘图方法 ──────────────────────────────────────────────────────────

// DrawAvatar 在 (cx, cy) 处绘制半径为 radius 的圆形头像。
// 图片先等比缩放至直径大小（cover 语义），再以圆形路径裁剪。
// 裁剪状态通过 Push/Pop 隔离，不影响后续绘制。
func (c *VectorCanvas) DrawAvatar(img image.Image, cx, cy, radius float64) {
	diam := int(math.Round(2 * radius))
	if diam < 1 {
		diam = 1
	}
	// 将源图缩放至圆形包围盒大小
	scaled := image.NewRGBA(image.Rect(0, 0, diam, diam))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), img, img.Bounds(), xdraw.Over, nil)

	c.Push()
	c.DrawCircle(cx, cy, radius)
	c.Clip()
	// DrawImageAnchored(img, x, y, ax=0.5, ay=0.5) 以 (x,y) 为图片中心点绘制
	c.Context.DrawImageAnchored(scaled, int(math.Round(cx)), int(math.Round(cy)), 0.5, 0.5)
	c.Pop()
}

// DrawProgressBar 在 (x, y) 处绘制宽×高为 w×h 的圆角水平进度条。
//
//   - percent：填充比例 [0.0, 1.0]，超出范围自动截断
//   - fg：填充部分颜色
//   - bg：轨道（未填充）颜色
func (c *VectorCanvas) DrawProgressBar(x, y, w, h, percent float64, fg, bg color.Color) {
	r := h / 2

	// 绘制完整轨道
	c.SetColor(bg)
	c.DrawRoundedRectangle(x, y, w, h, r)
	c.Fill()

	// 绘制填充部分
	pct := vcClamp01(percent)
	if pct <= 0 {
		return
	}
	fillW := w * pct
	if fillW < h { // 防止填充宽度小于高度导致圆角半径超限
		fillW = h
	}
	c.SetColor(fg)
	c.DrawRoundedRectangle(x, y, fillW, h, r)
	c.Fill()
}

// DrawLineChart 在包围盒 [x, x+w] × [y, y+h] 内绘制折线图。
//
//   - points：归一化 y 值切片 [0.0, 1.0]（底=0，顶=1），至少需要 2 个点
//   - lineColor：折线颜色
//   - lineWidth：折线像素宽度
func (c *VectorCanvas) DrawLineChart(x, y, w, h float64, points []float64, lineColor color.Color, lineWidth float64) {
	n := len(points)
	if n < 2 {
		return
	}
	step := w / float64(n-1)

	c.Push()
	c.SetColor(lineColor)
	c.SetLineWidth(lineWidth)
	c.MoveTo(x, y+h*(1-vcClamp01(points[0])))
	for i := 1; i < n; i++ {
		c.LineTo(x+float64(i)*step, y+h*(1-vcClamp01(points[i])))
	}
	c.Stroke()
	c.Pop()
}

// DrawLineChartFilled 绘制带填充色的面积图（折线 + 底部填充区域）。
// fillColor 支持半透明（alpha < 255），可实现叠加效果。
func (c *VectorCanvas) DrawLineChartFilled(x, y, w, h float64, points []float64, lineColor, fillColor color.Color, lineWidth float64) {
	n := len(points)
	if n < 2 {
		return
	}
	step := w / float64(n-1)
	bottom := y + h

	c.Push()

	// 构建填充区域路径（从左下角出发，沿折线到右侧，再闭合至右下角）
	c.MoveTo(x, bottom)
	for i, v := range points {
		c.LineTo(x+float64(i)*step, y+h*(1-vcClamp01(v)))
	}
	c.LineTo(x+w, bottom)
	c.ClosePath()
	c.SetColor(fillColor)
	c.Fill()

	// 在填充区域上方绘制折线
	c.SetColor(lineColor)
	c.SetLineWidth(lineWidth)
	c.MoveTo(x, y+h*(1-vcClamp01(points[0])))
	for i := 1; i < n; i++ {
		c.LineTo(x+float64(i)*step, y+h*(1-vcClamp01(points[i])))
	}
	c.Stroke()

	c.Pop()
}

// DrawRadarChart 以 (cx, cy) 为圆心、radius 为半径绘制雷达图（蛛网图）。
//
//   - values：各坐标轴的归一化值 [0.0, 1.0]，至少需要 3 个轴
//   - fillColor：多边形内部填充色（支持半透明）
//   - strokeColor：多边形边框颜色
//
// 第一个轴指向正上方（角度 = -π/2）。
// 建议先调用 [VectorCanvas.DrawRadarGrid] 绘制背景网格，再调用本方法。
func (c *VectorCanvas) DrawRadarChart(cx, cy, radius float64, values []float64, fillColor, strokeColor color.Color) {
	n := len(values)
	if n < 3 {
		return
	}
	const startAngle = -math.Pi / 2
	pts := make([][2]float64, n)
	for i, v := range values {
		angle := startAngle + float64(i)*2*math.Pi/float64(n)
		r := radius * vcClamp01(v)
		pts[i] = [2]float64{cx + r*math.Cos(angle), cy + r*math.Sin(angle)}
	}

	c.Push()
	c.MoveTo(pts[0][0], pts[0][1])
	for i := 1; i < n; i++ {
		c.LineTo(pts[i][0], pts[i][1])
	}
	c.ClosePath()
	c.SetColor(fillColor)
	c.FillPreserve()
	c.SetColor(strokeColor)
	c.Stroke()
	c.Pop()
}

// DrawRadarGrid 绘制雷达图的背景网格（坐标轴 + 同心环）。
// 应在 [VectorCanvas.DrawRadarChart] 之前调用，确保数据多边形显示在网格上方。
//
//   - n：坐标轴数量（应与数据切片长度一致）
//   - rings：同心参考环数量（如 4 表示 25%/50%/75%/100%）
//   - gridColor：网格颜色（通常为半透明灰色）
func (c *VectorCanvas) DrawRadarGrid(cx, cy, radius float64, n, rings int, gridColor color.Color) {
	if n < 3 || rings < 1 {
		return
	}
	const startAngle = -math.Pi / 2

	c.Push()
	c.SetColor(gridColor)
	c.SetLineWidth(1)

	// 绘制同心环
	for ring := 1; ring <= rings; ring++ {
		r := radius * float64(ring) / float64(rings)
		c.DrawCircle(cx, cy, r)
		c.Stroke()
	}

	// 绘制从圆心到各顶点的坐标轴
	for i := range n {
		angle := startAngle + float64(i)*2*math.Pi/float64(n)
		c.MoveTo(cx, cy)
		c.LineTo(cx+radius*math.Cos(angle), cy+radius*math.Sin(angle))
		c.Stroke()
	}

	c.Pop()
}

// ─── 输出方法 ──────────────────────────────────────────────────────────────────

// ToPNG 将当前画布状态编码为 PNG 字节并返回。
func (c *VectorCanvas) ToPNG() ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, c.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ToJPEG 将当前画布状态编码为 JPEG 字节并返回。
// quality 取值范围 [1, 100]，超出范围时自动取 85。
func (c *VectorCanvas) ToJPEG(quality int) ([]byte, error) {
	if quality < 1 || quality > 100 {
		quality = 85
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, c.Image(), &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SavePNG 将画布编码为 PNG 并写入指定文件路径。
// 内部委托给 [gg.Context.SavePNG]。
func (c *VectorCanvas) SavePNG(path string) error {
	return c.Context.SavePNG(path)
}

// SaveJPEG 将画布编码为 JPEG 并写入指定文件路径。
// quality 取值范围 [1, 100]，超出范围时自动取 85。
func (c *VectorCanvas) SaveJPEG(path string, quality int) error {
	data, err := c.ToJPEG(quality)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ─── 内部辅助 ──────────────────────────────────────────────────────────────────

// vcClamp01 将 v 截断至 [0.0, 1.0] 区间。
// 前缀 "vc" 用于避免与包内其他文件可能存在的同名函数冲突。
func vcClamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
