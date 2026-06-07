package iss

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/satutil"
	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

const cardWidth = 800

func renderCard(pos *IssPosition, astros []string, astroCount int, history []AltRecord, trend Trend) ([]byte, error) {
	bg := textimage.LinearGradient(cardWidth, 1400, 150,
		textimage.Stop(0.0, color.RGBA{R: 8, G: 4, B: 30, A: 255}),
		textimage.Stop(0.5, color.RGBA{R: 16, G: 10, B: 50, A: 255}),
		textimage.Stop(1.0, color.RGBA{R: 25, G: 15, B: 60, A: 255}),
	)

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.RGBA{R: 220, G: 225, B: 240, A: 255}),
		textimage.WithLineHeight(1.6),
		textimage.WithPadding(36, 16),
	)
	if err != nil {
		return nil, err
	}

	titleColor := color.RGBA{R: 100, G: 200, B: 255, A: 255}
	labelColor := color.RGBA{R: 180, G: 200, B: 230, A: 255}
	valueColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	canvas.AddSpacer(24)

	canvas.AddText("国际空间站 (ISS)",
		textimage.WithFontSize(26),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(titleColor),
		textimage.WithTextShadow(color.RGBA{A: 120}, 1, 2, 4),
		textimage.WithPadding(32, 10),
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
			Text: fmtLat(pos.Latitude),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(20), textimage.WithFontColor(valueColor),
				textimage.WithAlign(textimage.AlignCenter), textimage.WithPadding(36, 2),
			},
		},
		textimage.RowItem{
			Text: fmtLng(pos.Longitude),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(20), textimage.WithFontColor(valueColor),
				textimage.WithAlign(textimage.AlignCenter), textimage.WithPadding(36, 2),
			},
		},
	)

	canvas.AddSpacer(16)

	canvas.AddRow(
		textimage.RowItem{
			Text: fmt.Sprintf("高度 %.1f km", pos.Altitude),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(18), textimage.WithFontColor(valueColor),
				textimage.WithAlign(textimage.AlignLeft), textimage.WithPadding(36, 8),
			},
		},
		textimage.RowItem{
			Text: fmt.Sprintf("速度 %.2f km/s", pos.Velocity/3600),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(18), textimage.WithFontColor(valueColor),
				textimage.WithAlign(textimage.AlignRight), textimage.WithPadding(36, 8),
			},
		},
	)

	period := satutil.OrbitalPeriod(pos.Altitude)

	visLabel := "[光照]"
	if pos.Visibility == "eclipsed" {
		visLabel = "[地影]"
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
			Text: visLabel,
			TextOpts: []textimage.Option{
				textimage.WithFontSize(15), textimage.WithFontColor(labelColor),
				textimage.WithAlign(textimage.AlignRight), textimage.WithPadding(36, 6),
			},
		},
	)

	minLat, maxLat, minLng, maxLng := satutil.VisibleBounds(pos.Latitude, pos.Longitude, pos.Altitude)

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

	canvas.AddSpacer(12)

	if len(history) >= 2 {
		minAlt, maxAlt := minMaxHistory(history)
		chartImg := renderAltChart(history, minAlt, maxAlt)
		if chartImg != nil {
			canvas.AddSpacer(16)
			canvas.AddText("过去24小时轨道高度 (km)",
				textimage.WithFontSize(15),
				textimage.WithFontColor(titleColor),
				textimage.WithAlign(textimage.AlignLeft),
				textimage.WithPadding(36, 6),
			)

			if err := canvas.AddImage(chartImg,
				textimage.WithImgWidth(cardWidth-80),
				textimage.WithImgAlign(textimage.AlignCenter),
				textimage.WithImgPadding(0, 8),
			); err != nil {
				canvas.AddText(fmt.Sprintf("高度区间: %.1f - %.1f km", minAlt, maxAlt),
					textimage.WithFontSize(14),
					textimage.WithFontColor(labelColor),
					textimage.WithPadding(36, 4),
				)
			}
		}
	}

	canvas.AddSpacer(12)

	canvas.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 80, G: 100, B: 150, A: 140}),
		textimage.WithDividerThickness(1),
		textimage.WithDividerInset(36),
		textimage.WithDividerPadding(4),
	)

	canvas.AddSpacer(8)

	infoColor := color.RGBA{R: 180, G: 200, B: 230, A: 220}
	if trend.Slope != 0 {
		dir := "[上升]"
		if trend.Slope < 0 {
			dir = "[下降]"
		}
		abs := trend.Slope
		if abs < 0 {
			abs = -abs
		}
		trendStr := fmt.Sprintf("高度趋势: %s %.2f km/天", dir, abs)
		if abs < 1 {
			trendStr = fmt.Sprintf("高度趋势: %s %.0f m/天", dir, abs*1000)
		}
		canvas.AddText(trendStr,
			textimage.WithFontSize(14),
			textimage.WithFontColor(infoColor),
			textimage.WithAlign(textimage.AlignCenter),
			textimage.WithPadding(28, 4),
		)
		canvas.AddSpacer(4)
	}

	canvas.AddText(fmt.Sprintf("在轨: %d人", astroCount),
		textimage.WithFontSize(16),
		textimage.WithFontColor(titleColor),
		textimage.WithPadding(36, 6),
	)

	for _, name := range astros {
		canvas.AddText(fmt.Sprintf("  . %s", name),
			textimage.WithFontSize(13),
			textimage.WithFontColor(labelColor),
			textimage.WithPadding(36, 2),
		)
	}

	canvas.AddSpacer(16)

	sourceColor := color.RGBA{R: 120, G: 140, B: 180, A: 200}
	canvas.AddRow(
		textimage.RowItem{
			Text: "数据来源: wheretheiss.at / open-notify.org",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(11), textimage.WithFontColor(sourceColor),
				textimage.WithAlign(textimage.AlignLeft), textimage.WithPadding(28, 6),
				textimage.WithLineHeight(1.3),
			},
		},
		textimage.RowItem{
			Text: pos.Timestamp.Format("2006-01-02 15:04"),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(11), textimage.WithFontColor(sourceColor),
				textimage.WithAlign(textimage.AlignRight), textimage.WithPadding(28, 6),
				textimage.WithLineHeight(1.3),
			},
		},
	)

	canvas.AddSpacer(16)

	return canvas.ResultPNG()
}

func renderAltChart(history []AltRecord, minAlt, maxAlt float64) *image.RGBA {
	if len(history) < 2 {
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

	vc.SetColor(color.RGBA{R: 10, G: 5, B: 30, A: 255})
	vc.DrawRectangle(0, 0, chartW, chartH)
	vc.Fill()

	altRange := maxAlt - minAlt
	if altRange < 1 {
		altRange = 1
	}
	padAlt := altRange * 0.05
	yMin := minAlt - padAlt
	yMax := maxAlt + padAlt
	yRange := yMax - yMin

	norm := make([]float64, len(history))
	for i, r := range history {
		norm[i] = (r.Altitude - yMin) / yRange
	}

	chartX := marginL
	chartY := marginT

	lineColor := color.RGBA{R: 80, G: 200, B: 255, A: 255}
	fillColor := color.RGBA{R: 40, G: 100, B: 180, A: 100}
	vc.DrawLineChartFilled(chartX, chartY, innerW, innerH, norm, lineColor, fillColor, 2)

	gridColor := color.RGBA{R: 60, G: 80, B: 130, A: 80}
	labelColor := color.RGBA{R: 150, G: 170, B: 200, A: 220}

	const yTicks = 5
	for i := 0; i < yTicks; i++ {
		frac := float64(i) / float64(yTicks-1)
		val := yMin + frac*yRange
		y := chartY + innerH*(1-frac)

		vc.SetColor(gridColor)
		vc.SetLineWidth(0.5)
		vc.MoveTo(chartX, y)
		vc.LineTo(chartX+innerW, y)
		vc.Stroke()

		vc.SetColor(labelColor)
		vc.DrawString(fmt.Sprintf("%.1f", val), 3, y+4)
	}

	vc.SetColor(labelColor)
	vc.DrawString("km", 3, chartH-marginB+14)

	if len(history) >= 2 {
		t0 := history[0].Time
		t1 := history[len(history)-1].Time
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

func minMaxHistory(history []AltRecord) (float64, float64) {
	if len(history) == 0 {
		return 0, 0
	}
	mn, mx := history[0].Altitude, history[0].Altitude
	for _, h := range history {
		if h.Altitude < mn {
			mn = h.Altitude
		}
		if h.Altitude > mx {
			mx = h.Altitude
		}
	}
	return mn, mx
}
