package css

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/satutil"
	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

const cardWidth = 800

func renderCard(lat, lng, alt, speed float64, history []AltPoint, trend Trend, oem *OEMEphemeris) ([]byte, error) {
	bg := textimage.LinearGradient(cardWidth, 1400, 150,
		textimage.Stop(0.0, color.RGBA{R: 180, G: 20, B: 20, A: 255}),
		textimage.Stop(0.3, color.RGBA{R: 200, G: 40, B: 30, A: 255}),
		textimage.Stop(0.6, color.RGBA{R: 180, G: 60, B: 30, A: 255}),
		textimage.Stop(1.0, color.RGBA{R: 150, G: 30, B: 20, A: 255}),
	)

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.RGBA{R: 255, G: 255, B: 255, A: 255}),
		textimage.WithLineHeight(1.6),
		textimage.WithPadding(36, 16),
	)
	if err != nil {
		return nil, err
	}

	titleColor := color.RGBA{R: 255, G: 215, B: 55, A: 255}
	labelColor := color.RGBA{R: 255, G: 200, B: 180, A: 255}
	valueColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	badgeColor := color.RGBA{R: 255, G: 215, B: 55, A: 200}
	badgeBg := color.RGBA{R: 120, G: 10, B: 10, A: 150}

	canvas.AddSpacer(24)

	canvas.AddText("中国空间站 - 天宫 (轨道预报)",
		textimage.WithFontSize(26),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(titleColor),
		textimage.WithTextShadow(color.RGBA{A: 120}, 1, 2, 4),
		textimage.WithPadding(32, 10),
	)

	canvas.AddText(" 天和核心舱 ",
		textimage.WithFontSize(13),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(badgeColor),
		textimage.WithTextBackdrop(badgeBg, 0),
		textimage.WithTextBackdropPadding(14, 4),
		textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 8),
		textimage.WithPadding(28, 4),
	)

	canvas.AddSpacer(20)

	canvas.AddRow(
		textimage.RowItem{
			Text: "纬度",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(14), textimage.WithFontColor(labelColor),
				textimage.WithAlign(textimage.AlignCenter), textimage.WithPadding(36, 4),
			},
		},
		textimage.RowItem{
			Text: "经度",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(14), textimage.WithFontColor(labelColor),
				textimage.WithAlign(textimage.AlignCenter), textimage.WithPadding(36, 4),
			},
		},
	)

	canvas.AddRow(
		textimage.RowItem{
			Text: fmtLat(lat),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(20), textimage.WithFontColor(valueColor),
				textimage.WithAlign(textimage.AlignCenter), textimage.WithPadding(36, 2),
			},
		},
		textimage.RowItem{
			Text: fmtLng(lng),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(20), textimage.WithFontColor(valueColor),
				textimage.WithAlign(textimage.AlignCenter), textimage.WithPadding(36, 2),
			},
		},
	)

	canvas.AddSpacer(20)

	canvas.AddText(fmt.Sprintf("%.1f km", alt),
		textimage.WithFontSize(44),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(titleColor),
		textimage.WithTextShadow(color.RGBA{A: 100}, 1, 3, 6),
		textimage.WithPadding(32, 6),
	)

	canvas.AddText(fmt.Sprintf("速度 %.2f km/s", speed),
		textimage.WithFontSize(16),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(valueColor),
		textimage.WithPadding(28, 4),
	)

	period := satutil.OrbitalPeriod(alt)

	gmst := satutil.GMST(time.Now())
	ex, ey, ez := satutil.GeodeticToECEF(lat, lng, alt)
	ix, iy, iz := satutil.ECEFtoECI(ex, ey, ez, gmst)
	eclipse := satutil.IsInEclipse(ix, iy, iz, gmst)
	eclipseLabel := "[光照]"
	if eclipse {
		eclipseLabel = "[地影]"
	}

	canvas.AddRow(
		textimage.RowItem{
			Text: fmt.Sprintf("轨道周期 %.1f min", period),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(15), textimage.WithFontColor(labelColor),
				textimage.WithAlign(textimage.AlignLeft), textimage.WithPadding(36, 6),
			},
		},
		textimage.RowItem{
			Text: eclipseLabel,
			TextOpts: []textimage.Option{
				textimage.WithFontSize(15), textimage.WithFontColor(labelColor),
				textimage.WithAlign(textimage.AlignRight), textimage.WithPadding(36, 6),
			},
		},
	)

	minLat, maxLat, minLng, maxLng := satutil.VisibleBounds(lat, lng, alt)

	canvas.AddText("可见区域",
		textimage.WithFontSize(15),
		textimage.WithFontColor(labelColor),
		textimage.WithAlign(textimage.AlignLeft),
		textimage.WithPadding(36, 4),
	)

	canvas.AddText(fmt.Sprintf("纬度 %.0f ~ %.0f   经度 %.0f ~ %.0f",
		minLat, maxLat, minLng, maxLng),
		textimage.WithFontSize(14), textimage.WithFontColor(labelColor),
		textimage.WithPadding(36, 2),
	)

	canvas.AddSpacer(16)

	if len(history) >= 2 {
		interpolated := interpolatePoints(history, 60)
		minAlt, maxAlt := minMaxAlt(interpolated)
		chart := renderAltChart(interpolated, minAlt, maxAlt)
		if chart != nil {
			canvas.AddSpacer(12)

			canvas.AddText("近4小时轨道高度 (预报值, km)",
				textimage.WithFontSize(15),
				textimage.WithFontColor(titleColor),
				textimage.WithAlign(textimage.AlignLeft),
				textimage.WithPadding(36, 6),
			)

			if err := canvas.AddImage(chart,
				textimage.WithImgWidth(cardWidth-80),
				textimage.WithImgAlign(textimage.AlignCenter),
				textimage.WithImgPadding(0, 8),
			); err != nil {
				canvas.AddText(fmt.Sprintf("高度区间: %.1f - %.1f km", trend.MinAlt, trend.MaxAlt),
					textimage.WithFontSize(14), textimage.WithFontColor(labelColor),
					textimage.WithPadding(36, 4),
				)
			}
		}
	}

	canvas.AddSpacer(16)

	infoColor := color.RGBA{R: 255, G: 200, B: 180, A: 220}
	canvas.AddText(fmt.Sprintf("近地点: %.1f km    远地点: %.1f km", trend.MinAlt, trend.MaxAlt),
		textimage.WithFontSize(15),
		textimage.WithFontColor(infoColor),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithPadding(28, 4),
	)

	if trend.Slope != 0 {
		dir := "[上升]"
		if trend.Slope < 0 {
			dir = "[下降]"
		}
		abs := trend.Slope
		if abs < 0 {
			abs = -abs
		}
		trendStr := fmt.Sprintf("轨道趋势: %s %.2f km/天", dir, abs)
		if abs < 1 {
			trendStr = fmt.Sprintf("轨道趋势: %s %.0f m/天", dir, abs*1000)
		}
		canvas.AddText(trendStr,
			textimage.WithFontSize(15),
			textimage.WithFontColor(infoColor),
			textimage.WithAlign(textimage.AlignCenter),
			textimage.WithPadding(28, 4),
		)
	}

	canvas.AddSpacer(20)

	canvas.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 200, G: 100, B: 80, A: 140}),
		textimage.WithDividerThickness(1),
		textimage.WithDividerInset(36),
		textimage.WithDividerPadding(4),
	)

	canvas.AddSpacer(8)

	sourceColor := color.RGBA{R: 220, G: 180, B: 160, A: 200}
	canvas.AddRow(
		textimage.RowItem{
			Text: "数据来源: 中国载人航天工程办公室",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(11), textimage.WithFontColor(sourceColor),
				textimage.WithAlign(textimage.AlignLeft), textimage.WithPadding(28, 6),
				textimage.WithLineHeight(1.3),
			},
		},
		textimage.RowItem{
			Text: time.Now().Format("2006-01-02 15:04"),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(11), textimage.WithFontColor(sourceColor),
				textimage.WithAlign(textimage.AlignRight), textimage.WithPadding(28, 6),
				textimage.WithLineHeight(1.3),
			},
		},
	)

	if oem != nil {
		canvas.AddText(fmt.Sprintf("基于 CMSE 7 天轨道预报  |  %s ~ %s",
			oem.StartTime.Format("01-02 15:04"), oem.StopTime.Format("01-02 15:04")),
			textimage.WithFontSize(11),
			textimage.WithFontColor(sourceColor),
			textimage.WithAlign(textimage.AlignCenter),
			textimage.WithPadding(28, 2),
			textimage.WithLineHeight(1.3),
		)
	}

	canvas.AddSpacer(16)

	return canvas.ResultPNG()
}

func renderAltChart(points []AltPoint, minAlt, maxAlt float64) *image.RGBA {
	if len(points) < 2 {
		return nil
	}

	marginL := 55.0
	marginR := 15.0
	marginT := 25.0
	marginB := 35.0
	chartW := 720.0
	chartH := 220.0
	innerW := chartW - marginL - marginR
	innerH := chartH - marginT - marginB

	vc := textimage.NewVectorCanvas(int(chartW), int(chartH))

	if fp := textimage.SystemCJKFontPath(); fp != "" {
		vc.LoadFontFace(fp, 12)
	}

	vc.SetColor(color.RGBA{R: 50, G: 6, B: 4, A: 255})
	vc.DrawRectangle(0, 0, chartW, chartH)
	vc.Fill()

	altRange := maxAlt - minAlt
	if altRange < 0.5 {
		altRange = 0.5
	}
	padAlt := altRange * 0.05
	yMin := minAlt - padAlt
	yMax := maxAlt + padAlt
	yRange := yMax - yMin

	norm := make([]float64, len(points))
	for i, p := range points {
		norm[i] = (p.Altitude - yMin) / yRange
	}

	chartX := marginL
	chartY := marginT

	lineColor := color.RGBA{R: 255, G: 215, B: 55, A: 255}
	fillColor := color.RGBA{R: 200, G: 100, B: 30, A: 100}
	vc.DrawLineChartFilled(chartX, chartY, innerW, innerH, norm, lineColor, fillColor, 2)

	gridColor := color.RGBA{R: 200, G: 120, B: 80, A: 80}
	labelColor := color.RGBA{R: 220, G: 180, B: 160, A: 220}

	// 绘制 5 条水平网格线及 Y 轴刻度值
	const yTicks = 5
	for i := 0; i < yTicks; i++ {
		frac := float64(i) / float64(yTicks-1)
		val := yMin + frac*yRange
		y := chartY + innerH*(1-frac)

		// 网格线
		vc.SetColor(gridColor)
		vc.SetLineWidth(0.5)
		vc.MoveTo(chartX, y)
		vc.LineTo(chartX+innerW, y)
		vc.Stroke()

		// 刻度值
		vc.SetColor(labelColor)
		vc.DrawString(fmt.Sprintf("%.1f", val), 3, y+4)
	}

	// 单位
	vc.SetColor(labelColor)
	vc.DrawString("km", 3, chartH-marginB+14)

	// X 轴时间标签（5 个均匀分布）
	if len(points) >= 2 {
		t0 := points[0].Time
		t1 := points[len(points)-1].Time
		dur := t1.Sub(t0)
		const xTicks = 5
		for i := 0; i < xTicks; i++ {
			t := t0.Add(time.Duration(float64(i) / float64(xTicks-1) * float64(dur)))
			x := chartX + float64(i)*innerW/float64(xTicks-1)
			vc.SetColor(labelColor)
			vc.DrawString(t.Format("15:04"), x-16, chartH-5)
		}
		vc.SetColor(labelColor)
		vc.DrawString("UTC", chartX+innerW-20, marginT-5)
	}

	return vc.Image().(*image.RGBA)
}

func minMaxAlt(points []AltPoint) (float64, float64) {
	if len(points) == 0 {
		return 0, 0
	}
	mn, mx := points[0].Altitude, points[0].Altitude
	for _, p := range points {
		if p.Altitude < mn {
			mn = p.Altitude
		}
		if p.Altitude > mx {
			mx = p.Altitude
		}
	}
	return mn, mx
}

func fmtLat(lat float64) string {
	dir := "N"
	if lat < 0 {
		dir = "S"
		lat = -lat
	}
	return fmt.Sprintf("%.4f %s", lat, dir)
}

func fmtLng(lng float64) string {
	dir := "E"
	if lng < 0 {
		dir = "W"
		lng = -lng
	}
	return fmt.Sprintf("%.4f %s", lng, dir)
}
