package textimage

import (
	"fmt"
	"image"
	"image/color"
	stdraw "image/draw"
)

// ─── Horizontal Bar Chart ─────────────────────────────────────────────────────

// BarItem 横向条形图中的一项数据。
type BarItem struct {
	Label string      // 左侧标签文字
	Value float64     // 当前数值（用于计算条形宽度）
	Color color.Color // 条形填充颜色（nil 则使用 [WithBarDefaultColor] 的设置）
}

// BarChartOption 配置条形图的渲染选项。
type BarChartOption func(*barChartOpts)

type barChartOpts struct {
	barHeight    int
	labelWidth   int         // 标签区域像素宽
	valueWidth   int         // 数值文字区域像素宽（0 表示不显示数值）
	barSpacingY  int         // 每个条形上下的间距
	paddingX     int         // 整体左右外边距
	trackColor   color.Color // 未填充轨道颜色
	defaultColor color.Color // 未指定颜色时使用的默认条形颜色
	fontSize     float64
	fontPath     string
	fontData     []byte
	fontColor    color.Color // 标签和数值的字体颜色
	showValue    bool        // 是否显示右侧数值文字
}

func defaultBarChartOpts() barChartOpts {
	return barChartOpts{
		barHeight:    18,
		labelWidth:   120,
		valueWidth:   55,
		barSpacingY:  5,
		paddingX:     16,
		trackColor:   color.RGBA{R: 50, G: 55, B: 65, A: 220},
		defaultColor: color.RGBA{R: 80, G: 160, B: 240, A: 255},
		fontSize:     13,
		fontColor:    color.RGBA{R: 220, G: 220, B: 230, A: 255},
		showValue:    true,
	}
}

// WithBarHeight 设置每个条形的像素高度（默认 18）。
func WithBarHeight(h int) BarChartOption { return func(o *barChartOpts) { o.barHeight = h } }

// WithBarLabelWidth 设置左侧标签区域的宽度（默认 120）。
func WithBarLabelWidth(w int) BarChartOption { return func(o *barChartOpts) { o.labelWidth = w } }

// WithBarValueWidth 设置右侧数值文字区域的宽度（默认 55；0 = 不显示数值）。
func WithBarValueWidth(w int) BarChartOption { return func(o *barChartOpts) { o.valueWidth = w } }

// WithBarSpacing 设置条形之间的垂直间距（默认 5）。
func WithBarSpacing(py int) BarChartOption { return func(o *barChartOpts) { o.barSpacingY = py } }

// WithBarTrackColor 设置未填充轨道的颜色。
func WithBarTrackColor(c color.Color) BarChartOption {
	return func(o *barChartOpts) { o.trackColor = c }
}

// WithBarDefaultColor 设置未指定颜色的条形的默认填充颜色。
func WithBarDefaultColor(c color.Color) BarChartOption {
	return func(o *barChartOpts) { o.defaultColor = c }
}

// WithBarFontSize 设置标签和数值的字体大小（默认 13）。
func WithBarFontSize(size float64) BarChartOption {
	return func(o *barChartOpts) { o.fontSize = size }
}

// WithBarFontPath 设置标签字体路径（空字符串表示使用内置字体）。
func WithBarFontPath(path string) BarChartOption {
	return func(o *barChartOpts) { o.fontPath = path }
}

// WithBarFontData 设置标签字体原始字节数据。
func WithBarFontData(data []byte) BarChartOption {
	return func(o *barChartOpts) { o.fontData = data }
}

// WithBarFontColor 设置标签和数值的字体颜色（默认浅灰白色）。
func WithBarFontColor(c color.Color) BarChartOption {
	return func(o *barChartOpts) { o.fontColor = c }
}

// WithBarPaddingX 设置图表整体的左右外边距（默认 16）。
func WithBarPaddingX(px int) BarChartOption { return func(o *barChartOpts) { o.paddingX = px } }

// WithBarShowValue 设置是否在条形右侧显示数值文字（默认显示）。
func WithBarShowValue(show bool) BarChartOption {
	return func(o *barChartOpts) { o.showValue = show }
}

// ─── barChartBlock ────────────────────────────────────────────────────────────

// barChartBlock 存储预渲染的条形图图片，是 [canvasBlock] 的一种实现。
type barChartBlock struct {
	img image.Image
}

func (b *barChartBlock) blockHeight() int { return b.img.Bounds().Dy() }

func (b *barChartBlock) drawAt(dst *image.RGBA, yOffset, _ int) {
	srcB := b.img.Bounds()
	destRect := srcB.Add(image.Pt(0, yOffset))
	stdraw.Draw(dst, destRect, b.img, srcB.Min, stdraw.Over)
}

// ─── Canvas method ────────────────────────────────────────────────────────────

// AddBarChart 向画布追加一个横向条形图。
//
// maxValue 是所有条形的参考最大值（条形长度按 value/maxValue 的比例计算）。
// 若 maxValue ≤ 0，自动取 items 中 Value 字段的最大值。
//
// 示例 — 系统资源监控卡片：
//
//	c.AddBarChart(
//	    []textimage.BarItem{
//	        {Label: "CPU",    Value: 72, Color: color.RGBA{R: 80, G: 200, B: 120, A: 255}},
//	        {Label: "Memory", Value: 58},
//	        {Label: "Disk",   Value: 90, Color: color.RGBA{R: 220, G: 80, B: 80, A: 255}},
//	    },
//	    100,
//	    textimage.WithBarFontSize(13),
//	    textimage.WithBarLabelWidth(80),
//	    textimage.WithBarShowValue(true),
//	)
func (c *Canvas) AddBarChart(items []BarItem, maxValue float64, opts ...BarChartOption) error {
	if len(items) == 0 {
		return nil
	}
	o := defaultBarChartOpts()
	for _, fn := range opts {
		fn(&o)
	}
	// 字体继承：若调用方未显式指定字体，则沿用画布级字体设置（含 CJK 字体路径）。
	// 与 AddText / AddRow 保持一致的"画布默认，可覆盖"语义。
	if o.fontPath == "" && len(o.fontData) == 0 {
		o.fontPath = c.opts.FontPath
		o.fontData = c.opts.FontData
	}

	// 若未指定 maxValue，取 items 中最大值
	if maxValue <= 0 {
		for _, item := range items {
			if item.Value > maxValue {
				maxValue = item.Value
			}
		}
	}
	if maxValue <= 0 {
		maxValue = 1
	}

	// 每行高度 = barHeight + 2×barSpacingY
	rowH := o.barHeight + 2*o.barSpacingY
	totalH := rowH * len(items)
	if totalH < 1 {
		totalH = 1
	}

	canvasW := c.width
	// 图表缓冲区使用透明背景：Canvas.Result() 已提前绘制 BgColor + BgImage，
	// 不再填充纯色，避免遮盖渐变/图片背景。轨道和填充条由后续代码显式上色。
	img := image.NewRGBA(image.Rect(0, 0, canvasW, totalH))

	// 构建标签/数值文字渲染器（透明背景，方便叠加到图表上）
	transparent := color.RGBA{}
	textOpts := []Option{
		WithFontSize(o.fontSize),
		WithFontColor(o.fontColor),
		WithBgColor(transparent),
		WithPadding(0, 0),
	}
	if o.fontPath != "" {
		textOpts = append(textOpts, WithFontPath(o.fontPath))
	} else if len(o.fontData) > 0 {
		textOpts = append(textOpts, WithFontData(o.fontData))
	}
	renderer, err := New(textOpts...)
	if err != nil {
		return fmt.Errorf("textimage canvas AddBarChart: build renderer: %w", err)
	}
	defer renderer.Close()

	// 计算各区域起始 x 坐标
	labelStartX := o.paddingX                   // 标签起始 x
	barStartX := labelStartX + o.labelWidth + 8 // 条形起始 x（8px label→bar 间距）
	valW := 0
	if o.showValue && o.valueWidth > 0 {
		valW = o.valueWidth + 4 // 4px bar→value 间距
	}
	barW := canvasW - barStartX - valW - o.paddingX
	if barW < 4 {
		barW = 4
	}
	barRadius := o.barHeight / 2

	for i, item := range items {
		yBase := i * rowH
		barY := yBase + o.barSpacingY // 条形顶部 y

		// ── 1. 绘制标签文字 ──────────────────────────────────────────────────
		if item.Label != "" {
			labelImg, lerr := renderer.Render(item.Label)
			if lerr != nil {
				return fmt.Errorf("textimage canvas AddBarChart: render label %q: %w", item.Label, lerr)
			}
			lb := labelImg.Bounds()
			// 垂直居中于本行
			labelY := barY + (o.barHeight-lb.Dy())/2
			if labelY < 0 {
				labelY = 0
			}
			labelDest := lb.Add(image.Pt(labelStartX, labelY))
			stdraw.Draw(img, labelDest, labelImg, lb.Min, stdraw.Over)
		}

		// ── 2. 绘制轨道（空白背景） ───────────────────────────────────────────
		trackRect := image.Rect(barStartX, barY, barStartX+barW, barY+o.barHeight).Intersect(img.Bounds())
		drawFilledRoundedRect(img, trackRect, barRadius, o.trackColor)

		// ── 3. 绘制填充条 ────────────────────────────────────────────────────
		fillRatio := item.Value / maxValue
		if fillRatio > 1 {
			fillRatio = 1
		}
		if fillRatio > 0 {
			fillW := int(float64(barW) * fillRatio)
			if fillW < 1 {
				fillW = 1
			}
			fillColor := item.Color
			if fillColor == nil {
				fillColor = o.defaultColor
			}
			r2 := barRadius * barRadius
			fullH := o.barHeight
			for row := range fullH {
				for col := range fillW {
					if barRadius == 0 || inRoundedRect(col, row, barW, fullH, barRadius, r2) {
						img.Set(barStartX+col, barY+row, fillColor)
					}
				}
			}
		}

		// ── 4. 绘制数值文字 ──────────────────────────────────────────────────
		if o.showValue && o.valueWidth > 0 {
			valueStr := fmt.Sprintf("%.0f", item.Value)
			valImg, verr := renderer.Render(valueStr)
			if verr == nil {
				vb := valImg.Bounds()
				valX := barStartX + barW + 4
				valY := barY + (o.barHeight-vb.Dy())/2
				if valY < 0 {
					valY = 0
				}
				valDest := vb.Add(image.Pt(valX, valY))
				stdraw.Draw(img, valDest, valImg, vb.Min, stdraw.Over)
			}
		}
	}

	c.blocks = append(c.blocks, &barChartBlock{img: img})
	return nil
}
