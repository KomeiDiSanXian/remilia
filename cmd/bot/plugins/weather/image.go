package weather

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

const cardWidth = 800

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func conditionBackground(cond string) image.Image {
	switch {
	case contains(cond, "晴"):
		return textimage.LinearGradient(cardWidth, 1200, 150,
			textimage.Stop(0.0, color.RGBA{R: 255, G: 165, B: 40, A: 255}),
			textimage.Stop(0.5, color.RGBA{R: 255, G: 195, B: 75, A: 255}),
			textimage.Stop(1.0, color.RGBA{R: 240, G: 155, B: 50, A: 255}),
		)
	case contains(cond, "雨"):
		return textimage.LinearGradient(cardWidth, 1200, 150,
			textimage.Stop(0.0, color.RGBA{R: 55, G: 75, B: 105, A: 255}),
			textimage.Stop(0.5, color.RGBA{R: 75, G: 95, B: 125, A: 255}),
			textimage.Stop(1.0, color.RGBA{R: 55, G: 75, B: 105, A: 255}),
		)
	case contains(cond, "雪"):
		return textimage.LinearGradient(cardWidth, 1200, 150,
			textimage.Stop(0.0, color.RGBA{R: 195, G: 205, B: 215, A: 255}),
			textimage.Stop(0.5, color.RGBA{R: 215, G: 222, B: 230, A: 255}),
			textimage.Stop(1.0, color.RGBA{R: 195, G: 205, B: 215, A: 255}),
		)
	case contains(cond, "云"), contains(cond, "阴"):
		return textimage.LinearGradient(cardWidth, 1200, 150,
			textimage.Stop(0.0, color.RGBA{R: 85, G: 105, B: 125, A: 255}),
			textimage.Stop(0.5, color.RGBA{R: 105, G: 125, B: 145, A: 255}),
			textimage.Stop(1.0, color.RGBA{R: 85, G: 105, B: 125, A: 255}),
		)
	case contains(cond, "雾"):
		return textimage.LinearGradient(cardWidth, 1200, 150,
			textimage.Stop(0.0, color.RGBA{R: 135, G: 145, B: 150, A: 255}),
			textimage.Stop(0.5, color.RGBA{R: 155, G: 165, B: 170, A: 255}),
			textimage.Stop(1.0, color.RGBA{R: 135, G: 145, B: 150, A: 255}),
		)
	default:
		return textimage.LinearGradient(cardWidth, 1200, 150,
			textimage.Stop(0.0, color.RGBA{R: 30, G: 100, B: 200, A: 255}),
			textimage.Stop(0.5, color.RGBA{R: 50, G: 120, B: 210, A: 255}),
			textimage.Stop(1.0, color.RGBA{R: 100, G: 160, B: 230, A: 255}),
		)
	}
}

func isLightBackground(cond string) bool {
	return contains(cond, "晴") || contains(cond, "雪") || contains(cond, "雾")
}

func drawWeatherIcon(cond string) *image.RGBA {
	vc := textimage.NewVectorCanvas(72, 72)

	switch {
	case contains(cond, "晴"):
		vc.SetColor(color.RGBA{R: 255, G: 220, B: 50, A: 255})
		vc.DrawCircle(36, 36, 16)
		vc.Fill()
		vc.SetLineWidth(3)
		vc.SetColor(color.RGBA{R: 255, G: 220, B: 50, A: 200})
		for i := range 8 {
			angle := float64(i) * math.Pi / 4
			x1 := 36 + 22*math.Cos(angle)
			y1 := 36 + 22*math.Sin(angle)
			x2 := 36 + 30*math.Cos(angle)
			y2 := 36 + 30*math.Sin(angle)
			vc.MoveTo(x1, y1)
			vc.LineTo(x2, y2)
			vc.Stroke()
		}

	case contains(cond, "雨"):
		cloudColor := color.RGBA{R: 150, G: 160, B: 170, A: 255}
		vc.SetColor(cloudColor)
		vc.DrawCircle(28, 32, 11)
		vc.Fill()
		vc.DrawCircle(40, 28, 14)
		vc.Fill()
		vc.DrawCircle(50, 32, 10)
		vc.Fill()
		vc.DrawRectangle(24, 28, 28, 14)
		vc.Fill()
		vc.SetLineWidth(2.5)
		vc.SetColor(color.RGBA{R: 100, G: 160, B: 240, A: 230})
		for _, x := range []float64{30, 38, 46} {
			vc.MoveTo(x, 48)
			vc.LineTo(x-2, 62)
			vc.Stroke()
		}

	case contains(cond, "雪"):
		cloudColor := color.RGBA{R: 200, G: 210, B: 220, A: 255}
		vc.SetColor(cloudColor)
		vc.DrawCircle(28, 32, 11)
		vc.Fill()
		vc.DrawCircle(40, 28, 14)
		vc.Fill()
		vc.DrawCircle(50, 32, 10)
		vc.Fill()
		vc.DrawRectangle(24, 28, 28, 14)
		vc.Fill()
		vc.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 240})
		for _, p := range [][2]float64{{28, 52}, {36, 58}, {44, 50}, {50, 56}} {
			vc.DrawCircle(p[0], p[1], 3)
			vc.Fill()
		}

	case contains(cond, "云"), contains(cond, "阴"):
		vc.SetColor(color.RGBA{R: 210, G: 215, B: 220, A: 255})
		vc.DrawCircle(28, 34, 12)
		vc.Fill()
		vc.DrawCircle(40, 30, 15)
		vc.Fill()
		vc.DrawCircle(50, 34, 11)
		vc.Fill()
		vc.DrawRectangle(24, 30, 28, 15)
		vc.Fill()

	default:
		vc.SetColor(color.RGBA{R: 255, G: 200, B: 60, A: 255})
		vc.DrawCircle(36, 36, 16)
		vc.Fill()
		vc.SetLineWidth(3)
		vc.SetColor(color.RGBA{R: 255, G: 200, B: 60, A: 180})
		for i := range 8 {
			angle := float64(i) * math.Pi / 4
			x1 := 36 + 22*math.Cos(angle)
			y1 := 36 + 22*math.Sin(angle)
			x2 := 36 + 30*math.Cos(angle)
			y2 := 36 + 30*math.Sin(angle)
			vc.MoveTo(x1, y1)
			vc.LineTo(x2, y2)
			vc.Stroke()
		}
	}

	return vc.Image().(*image.RGBA)
}

type arcData struct {
	percent float64 // 0.0–1.0
	label   string  // 显示文字如 "57%"
}

type glassModule struct {
	w, h             int
	lines            []string // 文字行（居中）
	subtitle         string   // 副标题（显示在 arc 数值下方，仅 arc 模块）
	arc              *arcData // 环形进度（nil = 纯文本）
	fgGauge, bgGauge color.Color
	padL, padR       int // 透明边距（Row 居中）
}

type colorStop struct {
	pos float64
	c   color.Color
}

var (
	gaugeBlue   = color.RGBA{R: 60, G: 140, B: 255, A: 230}
	gaugeCyan   = color.RGBA{R: 60, G: 210, B: 255, A: 230}
	gaugeGreen  = color.RGBA{R: 80, G: 210, B: 100, A: 230}
	gaugeYellow = color.RGBA{R: 240, G: 210, B: 60, A: 230}
	gaugeOrange = color.RGBA{R: 255, G: 170, B: 50, A: 230}
	gaugeRed    = color.RGBA{R: 255, G: 80, B: 60, A: 230}
)

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func lerpRGBA(a, b color.RGBA, t float64) color.RGBA {
	t = clamp01(t)
	return color.RGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t + 0.5),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t + 0.5),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t + 0.5),
		A: uint8(float64(a.A)*(1-t) + float64(b.A)*t + 0.5),
	}
}

func colorFromStops(stops []colorStop, t float64) color.Color {
	if len(stops) == 0 {
		return color.RGBA{R: 200, G: 200, B: 200, A: 230}
	}
	if math.IsNaN(t) {
		return stops[0].c
	}
	if t <= stops[0].pos {
		return stops[0].c
	}
	if t >= stops[len(stops)-1].pos {
		return stops[len(stops)-1].c
	}
	for i := 0; i < len(stops)-1; i++ {
		if t >= stops[i].pos && t <= stops[i+1].pos {
			span := stops[i+1].pos - stops[i].pos
			if span == 0 {
				return stops[i+1].c
			}
			f := (t - stops[i].pos) / span
			return lerpRGBA(stops[i].c.(color.RGBA), stops[i+1].c.(color.RGBA), f)
		}
	}
	return stops[len(stops)-1].c
}

func tempGaugeColor(temp float64) color.Color {
	return colorFromStops([]colorStop{
		{0.00, gaugeBlue},
		{0.27, gaugeCyan},
		{0.45, gaugeGreen},
		{0.62, gaugeYellow},
		{0.82, gaugeOrange},
		{1.00, gaugeRed},
	}, clamp01((temp+10)/55))
}

func windGaugeColor(speed float64) color.Color {
	return colorFromStops([]colorStop{
		{0.0, gaugeGreen},
		{0.5, gaugeYellow},
		{1.0, gaugeRed},
	}, clamp01(speed/100))
}

func visGaugeColor(vis float64) color.Color {
	return colorFromStops([]colorStop{
		{0.00, gaugeRed},
		{0.25, gaugeYellow},
		{0.75, gaugeGreen},
		{1.00, gaugeGreen},
	}, clamp01(vis/20))
}

func shadowDrawString(vc *textimage.VectorCanvas, s string, x, y float64, textColor color.Color) {
	vc.SetColor(color.RGBA{R: 0, G: 0, B: 0, A: 120})
	vc.DrawString(s, x+1, y+1)
	vc.SetColor(textColor)
	vc.DrawString(s, x, y)
}

func createGlassModule(m glassModule, lightBg bool, textColor color.Color) *image.RGBA {
	scale := 2
	cardW := m.w * scale
	cardH := m.h * scale
	padL := m.padL * scale
	padR := m.padR * scale
	totalW := cardW + padL + padR

	vc := textimage.NewVectorCanvas(totalW, cardH)
	fh := float64(cardH)
	offsetX := float64(padL)

	if fp := textimage.SystemCJKFontPath(); fp != "" {
		vc.LoadFontFace(fp, 14*float64(scale))
	}

	s := float64(scale)
	cr := 10 * s

	if lightBg {
		vc.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 110})
	} else {
		vc.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 70})
	}
	vc.DrawRoundedRectangle(offsetX, 0, float64(cardW), fh, cr)
	vc.Fill()

	if lightBg {
		vc.SetColor(color.RGBA{R: 200, G: 200, B: 210, A: 80})
	} else {
		vc.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 55})
	}
	vc.SetLineWidth(1 * s)
	vc.DrawRoundedRectangle(offsetX, 0, float64(cardW), fh, cr)
	vc.Stroke()

	cx := offsetX + float64(cardW)/2

	if m.arc != nil {
		yLabel := 22 * s
		lw, _ := vc.MeasureString(m.lines[0])
		shadowDrawString(vc, m.lines[0], cx-lw/2, yLabel, textColor)

		arcCentery := fh * 0.52
		radius := float64(cardW) * 0.10

		vc.SetColor(m.bgGauge)
		vc.SetLineWidth(5 * s)
		vc.DrawArc(cx, arcCentery, radius, 0, 2*math.Pi)
		vc.Stroke()

		if m.arc.percent > 0 {
			start := -math.Pi / 2
			sweep := 2 * math.Pi * m.arc.percent
			vc.SetColor(m.fgGauge)
			vc.SetLineWidth(5 * s)
			vc.DrawArc(cx, arcCentery, radius, start, start+sweep)
			vc.Stroke()

			endAngle := start + sweep
			vc.DrawCircle(cx+radius*math.Cos(endAngle), arcCentery+radius*math.Sin(endAngle), 2.5*s)
			vc.Fill()
		}

		vw, _ := vc.MeasureString(m.arc.label)
		shadowDrawString(vc, m.arc.label, cx-vw/2, arcCentery+radius+18*s, textColor)

		if m.subtitle != "" {
			sw, _ := vc.MeasureString(m.subtitle)
			shadowDrawString(vc, m.subtitle, cx-sw/2, arcCentery+radius+18*s+19*s, textColor)
		}
	} else {
		totalH := len(m.lines) * (22 * scale)
		startY := (fh - float64(totalH)) / 2
		for i, line := range m.lines {
			lw, _ := vc.MeasureString(line)
			shadowDrawString(vc, line, cx-lw/2, startY+float64(i)*(22*s), textColor)
		}
	}

	return vc.Image().(*image.RGBA)
}

func renderCard(r *Result) ([]byte, error) {
	bg := conditionBackground(r.Condition)
	lightBg := isLightBackground(r.Condition)

	var baseColor, titleColor, labelColor, accentColor color.Color
	if lightBg {
		baseColor = color.RGBA{R: 60, G: 45, B: 35, A: 255}
		titleColor = color.RGBA{R: 50, G: 38, B: 28, A: 255}
		labelColor = color.RGBA{R: 85, G: 68, B: 52, A: 255}
		accentColor = color.RGBA{R: 190, G: 100, B: 25, A: 255}
	} else {
		baseColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		titleColor = color.RGBA{R: 255, G: 215, B: 55, A: 255}
		labelColor = color.RGBA{R: 200, G: 220, B: 245, A: 255}
		accentColor = color.RGBA{R: 255, G: 215, B: 55, A: 255}
	}

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(baseColor),
		textimage.WithLineHeight(1.6),
		textimage.WithPadding(36, 16),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddSpacer(16)

	icon := drawWeatherIcon(r.Condition)
	canvas.AddImage(icon,
		textimage.WithImgWidth(72),
		textimage.WithImgAlign(textimage.AlignCenter),
		textimage.WithImgPadding(0, 4),
	)

	canvas.AddSpacer(6)

	canvas.AddText(r.City,
		textimage.WithFontSize(28),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(titleColor),
		textimage.WithTextShadow(color.RGBA{A: 100}, 1, 2, 4),
		textimage.WithPadding(32, 6),
	)

	canvas.AddSpacer(4)

	canvas.AddText(fmt.Sprintf("%.0f °C", r.TempC),
		textimage.WithFontSize(52),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(accentColor),
		textimage.WithTextShadow(color.RGBA{A: 100}, 1, 3, 6),
		textimage.WithPadding(28, 8),
	)

	canvas.AddText(r.Condition,
		textimage.WithFontSize(16),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithPadding(28, 4),
	)

	canvas.AddSpacer(16)

	modW := 320
	cellW := 400
	padSide := (cellW - modW) / 2
	gaugeTrack := color.RGBA{R: 120, G: 160, B: 210, A: 100}

	modH := 165
	modHshort := 150

	imgOpt := func(w int) []textimage.ImageOption {
		return []textimage.ImageOption{textimage.WithImgWidth(w)}
	}

	feelsPct := clamp01((r.FeelsLikeC + 10) / 55)
	humPct := clamp01(float64(r.Humidity) / 100)

	feelsLike := createGlassModule(glassModule{
		w: modW, h: modHshort, padL: padSide, padR: padSide,
		lines:   []string{"体感"},
		arc:     &arcData{percent: feelsPct, label: fmt.Sprintf("%.0f °C", r.FeelsLikeC)},
		fgGauge: tempGaugeColor(r.FeelsLikeC), bgGauge: gaugeTrack,
	}, lightBg, labelColor)

	humidity := createGlassModule(glassModule{
		w: modW, h: modHshort, padL: padSide, padR: padSide,
		lines:   []string{"湿度"},
		arc:     &arcData{percent: humPct, label: fmt.Sprintf("%d %%", r.Humidity)},
		fgGauge: color.RGBA{R: 80, G: 200, B: 255, A: 230}, bgGauge: gaugeTrack,
	}, lightBg, labelColor)

	canvas.AddRow(
		textimage.RowItem{Image: feelsLike, ImageOpts: imgOpt(cellW)},
		textimage.RowItem{Image: humidity, ImageOpts: imgOpt(cellW)},
	)

	canvas.AddSpacer(12)

	windDir := r.WindDir
	if windDir == "" {
		windDir = "-"
	}
	windPct := clamp01(r.WindSpeedKmph / 100)
	visPct := clamp01(r.VisibilityKM / 20)

	pressureLabel := fmt.Sprintf("气压 %.0f hPa", r.PressureMB)
	if r.PressureMB < 1 {
		pressureLabel = "气压 -- hPa"
	}

	windCard := createGlassModule(glassModule{
		w: modW, h: modH, padL: padSide, padR: padSide,
		lines:    []string{"风速"},
		subtitle: fmt.Sprintf("风向 %s", windDir),
		arc:      &arcData{percent: windPct, label: fmt.Sprintf("%.1f km/h", r.WindSpeedKmph)},
		fgGauge:  windGaugeColor(r.WindSpeedKmph), bgGauge: gaugeTrack,
	}, lightBg, labelColor)

	visCard := createGlassModule(glassModule{
		w: modW, h: modH, padL: padSide, padR: padSide,
		lines:    []string{"能见度"},
		subtitle: pressureLabel,
		arc:      &arcData{percent: visPct, label: fmt.Sprintf("%.1f km", r.VisibilityKM)},
		fgGauge:  visGaugeColor(r.VisibilityKM), bgGauge: gaugeTrack,
	}, lightBg, labelColor)

	canvas.AddRow(
		textimage.RowItem{Image: windCard, ImageOpts: imgOpt(cellW)},
		textimage.RowItem{Image: visCard, ImageOpts: imgOpt(cellW)},
	)

	canvas.AddSpacer(12)

	uvPct := clamp01(float64(r.UV) / 11)
	cloudPct := clamp01(float64(r.Cloud) / 100)

	uvCard := createGlassModule(glassModule{
		w: modW, h: modHshort, padL: padSide, padR: padSide,
		lines:   []string{"紫外线"},
		arc:     &arcData{percent: uvPct, label: fmt.Sprintf("%d", r.UV)},
		fgGauge: color.RGBA{R: 255, G: 200, B: 80, A: 230}, bgGauge: gaugeTrack,
	}, lightBg, labelColor)

	cloudCard := createGlassModule(glassModule{
		w: modW, h: modHshort, padL: padSide, padR: padSide,
		lines:   []string{"云量"},
		arc:     &arcData{percent: cloudPct, label: fmt.Sprintf("%d %%", r.Cloud)},
		fgGauge: color.RGBA{R: 200, G: 210, B: 220, A: 200}, bgGauge: gaugeTrack,
	}, lightBg, labelColor)

	canvas.AddRow(
		textimage.RowItem{Image: uvCard, ImageOpts: imgOpt(cellW)},
		textimage.RowItem{Image: cloudCard, ImageOpts: imgOpt(cellW)},
	)

	canvas.AddSpacer(16)

	canvas.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 150, G: 200, B: 250, A: 100}),
		textimage.WithDividerThickness(1),
		textimage.WithDividerInset(36),
		textimage.WithDividerPadding(4),
	)

	canvas.AddSpacer(8)

	sourceColor := color.RGBA{R: 180, G: 200, B: 230, A: 200}
	if lightBg {
		sourceColor = color.RGBA{R: 120, G: 100, B: 80, A: 220}
	}
	canvas.AddRow(
		textimage.RowItem{
			Text: fmt.Sprintf("数据来源: %s", r.Source),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(12), textimage.WithFontColor(sourceColor),
				textimage.WithAlign(textimage.AlignLeft), textimage.WithPadding(28, 6),
				textimage.WithLineHeight(1.3),
			},
		},
		textimage.RowItem{
			Text: time.Now().Format("2006-01-02 15:04"),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(12), textimage.WithFontColor(sourceColor),
				textimage.WithAlign(textimage.AlignRight), textimage.WithPadding(28, 6),
				textimage.WithLineHeight(1.3),
			},
		},
	)

	canvas.AddSpacer(16)

	return canvas.ResultPNG()
}
