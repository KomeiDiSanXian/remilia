package css

import (
	"fmt"
	"image/color"
	"time"

	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/satutil"
	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

const (
	cardWidth    = 800
	contentWidth = cardWidth - 72 // 与画布左右内边距（36）对齐

	// bgHeight 为背景图高度估算值：作为画布背景按 cover 语义铺满，
	// 取值小于卡片最终高度可确保顶部柔光不被居中裁剪。
	bgHeight = 900
)

// cardData 渲染 /css 卡片所需的全部数据。
type cardData struct {
	Now              time.Time
	Lat, Lon         float64 // 星下点（度）
	Alt, Speed       float64 // 高度 (km)、速度 (km/s)
	Inclination      float64 // 轨道倾角（度）
	Eclipse          bool    // 当前是否处于地影
	EclipseRemaining time.Duration
	EclipseKnown     bool // 是否在预报窗口内找到相位切换
	PeriodMin        float64
	FootprintKm      float64
	Series           []AltPoint
	Trend            Trend
	Track            []satutil.TrackPoint
	OEM              *OEMEphemeris
}

// renderCard 绘制中国空间站信息卡片：
// 头部标识 → 实时高度/速度 → 状态徽章 → 数据面板 → 地面轨迹图
// → 高度预报曲线 → 数据来源。
func renderCard(d cardData) ([]byte, error) {
	theme := satutil.CSSTheme()

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(satutil.CardBackground(theme, cardWidth, bgHeight), textimage.BgFitFill),
		textimage.WithFontColor(theme.Label),
		textimage.WithLineHeight(1.5),
		textimage.WithPadding(36, 14),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddSpacer(8)

	// ── 头部：空间站图标 + 标题 + 副标题 ─────────────────────────────────────
	canvas.AddRow(
		textimage.RowItem{
			Width: 80,
			Image: satutil.IconSpaceStation(64, theme.Title),
			ImageOpts: []textimage.ImageOption{
				textimage.WithImgWidth(64),
				textimage.WithImgAlign(textimage.AlignCenter),
			},
		},
		textimage.RowItem{
			Text: "中国空间站 · 天宫",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(25),
				satutil.BoldFontOption(),
				textimage.WithFontColor(theme.Title),
				textimage.WithAlign(textimage.AlignLeft),
				textimage.WithPadding(0, 2),
			},
		},
	)
	canvas.AddText("CSS · Tiangong  |  CMSE 官方轨道预报",
		textimage.WithFontSize(12),
		textimage.WithFontColor(theme.Muted),
		textimage.WithAlign(textimage.AlignLeft),
		textimage.WithPadding(36, 2),
	)

	canvas.AddSpacer(12)

	// ── 实时高度 ─────────────────────────────────────────────────────────────
	canvas.AddText("轨道高度 (km)",
		textimage.WithFontSize(13),
		textimage.WithFontColor(theme.Label),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithPadding(28, 2),
	)
	canvas.AddText(fmt.Sprintf("%.1f", d.Alt),
		textimage.WithFontSize(54),
		satutil.BoldFontOption(),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(theme.Value),
		textimage.WithTextShadow(color.RGBA{R: 255, G: 150, B: 60, A: 110}, 1, 3, 6),
		textimage.WithPadding(28, 0),
	)
	canvas.AddRow(
		textimage.RowItem{
			Text: fmt.Sprintf("速度 %.2f km/s", d.Speed),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(14), textimage.WithFontColor(theme.Label),
				textimage.WithAlign(textimage.AlignCenter), textimage.WithPadding(28, 4),
			},
		},
		textimage.RowItem{
			Text: fmt.Sprintf("倾角 %.1f°", d.Inclination),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(14), textimage.WithFontColor(theme.Label),
				textimage.WithAlign(textimage.AlignCenter), textimage.WithPadding(28, 4),
			},
		},
	)

	canvas.AddSpacer(10)

	// ── 状态徽章（单行，避免出现参差不齐的换行块）─────────────────────────────
	canvas.AddBadgeRow(
		[]textimage.BadgeItem{
			{Text: eclipseBadgeText(d), BgColor: eclipseBadgeColor(d.Eclipse), TextColor: color.White},
			{
				Text:      perigeeApogeeText(d.Trend),
				BgColor:   color.NRGBA{R: 196, G: 126, B: 46, A: 235},
				TextColor: color.White,
			},
			{
				Text:      forecastBadgeText(d.OEM),
				BgColor:   color.NRGBA{R: 150, G: 92, B: 72, A: 225},
				TextColor: color.White,
			},
		},
		textimage.WithBadgeFontSize(12),
		textimage.WithBadgeRadius(9),
		textimage.WithBadgeRowPadding(30, 4),
	)

	canvas.AddSpacer(14)

	// ── 数据面板 ─────────────────────────────────────────────────────────────
	panelW := 232
	panelH := 162
	periodArc := clamp01((d.PeriodMin - 85) / 15)
	decayArc := clamp01(absf(d.Trend.Slope) / 1.5)
	coverArc := clamp01(d.FootprintKm / 5000)

	canvas.AddRow(
		textimage.RowItem{Image: satutil.StatPanel(theme, satutil.StatPanelSpec{
			Width: panelW, Height: panelH, Title: "轨道周期",
			Value: fmt.Sprintf("%.1f", d.PeriodMin), Sub: "分钟 / 圈", Arc: periodArc, ArcColor: theme.Accent,
		}), ImageOpts: imgWidth(panelW)},
		textimage.RowItem{Image: satutil.StatPanel(theme, satutil.StatPanelSpec{
			Width: panelW, Height: panelH, Title: "高度变化率",
			Value: trendValueText(d.Trend.Slope), Sub: trendSubText(d.Trend.Slope), Arc: decayArc,
			ArcColor: trendColor(d.Trend.Slope),
		}), ImageOpts: imgWidth(panelW)},
		textimage.RowItem{Image: satutil.StatPanel(theme, satutil.StatPanelSpec{
			Width: panelW, Height: panelH, Title: "可见范围",
			Value: fmt.Sprintf("%.0f", d.FootprintKm), Sub: "km / 地面直径", Arc: coverArc, ArcColor: theme.Accent,
		}), ImageOpts: imgWidth(panelW)},
	)

	canvas.AddSpacer(14)

	// ── 地面轨迹 ─────────────────────────────────────────────────────────────
	canvas.AddImage(satutil.GroundTrackPanel(theme, satutil.MapSpec{
		Width: contentWidth, Height: satutil.WorldMapPanelHeight(contentWidth),
		Track: d.Track, Now: d.Now, Lat: d.Lat, Lon: d.Lon,
		FootprintDeg: footprintDeg(d.Alt),
		Title:        "地面轨迹",
		Note:         "过去 1 圈 → 未来 1 圈",
		Caption: fmt.Sprintf("星下点 %.2f°%s %.2f°%s · 虚线为已飞过航迹",
			absf(d.Lat), nsLabel(d.Lat), absf(d.Lon), ewLabel(d.Lon)),
	}), textimage.WithImgWidth(contentWidth), textimage.WithImgAlign(textimage.AlignCenter))

	canvas.AddSpacer(14)

	// ── 高度预报曲线 ─────────────────────────────────────────────────────────
	if chart := satutil.AltitudeChart(theme, satutil.ChartSpec{
		Width: contentWidth, Height: 220,
		Points:   toChartPoints(d.Series),
		Title:    "轨道高度预报",
		Unit:     "高度 km · 时间 UTC",
		Now:      d.Now,
		NowLabel: "现在",
		Caption:  forecastCaption(d.OEM),
	}); chart != nil {
		canvas.AddImage(chart,
			textimage.WithImgWidth(contentWidth),
			textimage.WithImgAlign(textimage.AlignCenter))
	}

	canvas.AddSpacer(12)

	// ── 页脚 ─────────────────────────────────────────────────────────────────
	canvas.AddDivider(
		textimage.WithDividerColor(color.NRGBA{R: 220, G: 140, B: 110, A: 130}),
		textimage.WithDividerThickness(1),
		textimage.WithDividerInset(36),
		textimage.WithDividerPadding(4),
	)
	canvas.AddSpacer(6)

	sourceColor := color.NRGBA{R: 214, G: 172, B: 158, A: 220}
	canvas.AddRow(
		textimage.RowItem{
			Text: "数据来源: 中国载人航天工程办公室 (cmse.gov.cn)",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(11), textimage.WithFontColor(sourceColor),
				textimage.WithAlign(textimage.AlignLeft), textimage.WithPadding(28, 6),
				textimage.WithLineHeight(1.3),
			},
		},
		textimage.RowItem{
			Text: d.Now.Format("2006-01-02 15:04"),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(11), textimage.WithFontColor(sourceColor),
				textimage.WithAlign(textimage.AlignRight), textimage.WithPadding(28, 6),
				textimage.WithLineHeight(1.3),
			},
		},
	)

	canvas.AddSpacer(14)
	return canvas.ResultPNG()
}

// imgWidth 返回设置图片宽度的选项列表。
func imgWidth(w int) []textimage.ImageOption {
	return []textimage.ImageOption{textimage.WithImgWidth(w), textimage.WithImgAlign(textimage.AlignCenter)}
}

// eclipseBadgeText 生成光照/地影徽章文字（含剩余时长）。
func eclipseBadgeText(d cardData) string {
	label := "光照中"
	if d.Eclipse {
		label = "地影中"
	}
	if d.EclipseRemaining <= 0 {
		return label
	}
	prefix := "剩余约 "
	if !d.EclipseKnown {
		prefix = "剩余 > "
	}
	return label + " · " + prefix + fmtDuration(d.EclipseRemaining)
}

// eclipseBadgeColor 返回光照/地影徽章颜色。
func eclipseBadgeColor(eclipse bool) color.Color {
	if eclipse {
		return color.NRGBA{R: 92, G: 106, B: 164, A: 235}
	}
	return color.NRGBA{R: 52, G: 156, B: 104, A: 235}
}

// perigeeApogeeText 生成近/远地点徽章文字；近圆轨道直接给出平均高度。
func perigeeApogeeText(trend Trend) string {
	if trend.MaxAlt-trend.MinAlt < 0.5 {
		return fmt.Sprintf("近圆轨道 ≈ %.0f km", (trend.MinAlt+trend.MaxAlt)/2)
	}
	return fmt.Sprintf("近/远地点 %.0f / %.0f km", trend.MinAlt, trend.MaxAlt)
}

// forecastBadgeText 生成预报跨度徽章文字。
func forecastBadgeText(oem *OEMEphemeris) string {
	if oem == nil {
		return "轨道预报"
	}
	days := oem.StopTime.Sub(oem.StartTime).Hours() / 24
	if days < 1 {
		return "短期预报"
	}
	return fmt.Sprintf("官方 %.0f 天预报", days)
}

// forecastCaption 生成高度预报曲线底部说明（含预报有效期）。
func forecastCaption(oem *OEMEphemeris) string {
	if oem == nil {
		return "含一个完整轨道周期"
	}
	return fmt.Sprintf("预报有效期 %s ~ %s（UTC）· 含一个完整轨道周期",
		oem.StartTime.Format("01-02 15:04"), oem.StopTime.Format("01-02 15:04"))
}

// fmtDuration 将时长格式化为“X 小时 Y 分”/“Y 分钟”，避免 8m0s 这类生硬写法。
func fmtDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Hour {
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) - hours*60
	if minutes == 0 {
		return fmt.Sprintf("%d 小时", hours)
	}
	return fmt.Sprintf("%d 小时 %d 分", hours, minutes)
}

// trendValueText 将高度变化率格式化为面板主数值。
func trendValueText(slope float64) string {
	if slope == 0 {
		return "0"
	}
	sign := "+"
	if slope < 0 {
		sign = "−"
	}
	return fmt.Sprintf("%s%.2f", sign, absf(slope))
}

// trendSubText 返回高度变化率的说明文字。
func trendSubText(slope float64) string {
	if slope > 0.05 {
		return "km/天 · 长期抬升"
	}
	if slope < -0.05 {
		return "km/天 · 长期衰减"
	}
	return "km/天 · 轨道稳定"
}

// trendColor 按高度变化方向返回仪表颜色。
func trendColor(slope float64) color.Color {
	switch {
	case slope < -0.05:
		return color.NRGBA{R: 255, G: 128, B: 92, A: 255}
	case slope > 0.05:
		return color.NRGBA{R: 108, G: 220, B: 148, A: 255}
	default:
		return color.NRGBA{R: 255, G: 196, B: 96, A: 255}
	}
}

// footprintDeg 将轨道高度换算为可见范围的地心角半径（度）。
func footprintDeg(altKm float64) float64 {
	return satutil.VisibleAngularRadius(altKm)
}

// toChartPoints 将高度序列转换为曲线图数据点。
func toChartPoints(series []AltPoint) []satutil.ChartPoint {
	out := make([]satutil.ChartPoint, 0, len(series))
	for _, p := range series {
		out = append(out, satutil.ChartPoint{Time: p.Time, Value: p.Altitude})
	}
	return out
}

// formatCSSText 将 CSS 数据格式化为纯文本（备用方案）。
func formatCSSText(lat, lng, alt, vel float64, trend Trend, oem *OEMEphemeris) string {
	period := satutil.OrbitalPeriod(alt)
	minLat, maxLat, minLng, maxLng := satutil.VisibleBounds(lat, lng, alt)

	eclipseLabel := "[光照]"
	if satutil.IsInEclipseAt(lat, lng, alt, time.Now()) {
		eclipseLabel = "[地影]"
	}

	text := fmt.Sprintf("[CSS] 中国空间站 - 天宫 (轨道预报)\n纬度: %s\n经度: %s\n高度: %.1f km\n速度: %.2f km/s\n轨道周期: %.1f min\n可见区域: 纬度 %.0f~%.0f  经度 %.0f~%.0f\n光照: %s\n近地点: %.1f km\n远地点: %.1f km\n",
		fmtLat(lat), fmtLng(lng), alt, vel, period, minLat, maxLat, minLng, maxLng, eclipseLabel, trend.MinAlt, trend.MaxAlt)
	if trend.Slope != 0 {
		dir := "[上升]"
		if trend.Slope < 0 {
			dir = "[下降]"
		}
		abs := absf(trend.Slope)
		if abs < 1 {
			text += fmt.Sprintf("轨道趋势: %s %.0f m/天\n", dir, abs*1000)
		} else {
			text += fmt.Sprintf("轨道趋势: %s %.2f km/天\n", dir, abs)
		}
	}
	text += "数据来源: 中国载人航天工程办公室 (cmse.gov.cn)\n基于 CMSE 7 天轨道预报"
	return text
}

// fmtLat 将纬度格式化为带方向的字符串。
func fmtLat(lat float64) string {
	return fmt.Sprintf("%.4f°%s", absf(lat), nsLabel(lat))
}

// fmtLng 将经度格式化为带方向的字符串。
func fmtLng(lng float64) string {
	return fmt.Sprintf("%.4f°%s", absf(lng), ewLabel(lng))
}

// nsLabel 返回南北方向后缀。
func nsLabel(lat float64) string {
	if lat < 0 {
		return "S"
	}
	return "N"
}

// ewLabel 返回东西方向后缀。
func ewLabel(lng float64) string {
	if lng < 0 {
		return "W"
	}
	return "E"
}

// absf 返回浮点数绝对值。
func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// clamp01 将 v 截断到 [0, 1]。
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
