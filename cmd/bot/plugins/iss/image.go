package iss

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

// cardData 渲染 /iss 卡片所需的全部数据。
type cardData struct {
	Now         time.Time
	Pos         *IssPosition
	Astros      []string
	AstroCount  int
	Series      []AltRecord
	Trend       Trend
	Inclination float64
	PeriodMin   float64
	FootprintKm float64
	Track       []satutil.TrackPoint
}

// renderCard 绘制国际空间站信息卡片：
// 头部标识 → 实时高度/速度 → 状态徽章 → 数据面板 → 地面轨迹图
// → 高度历史曲线 → 在轨航天员 → 数据来源。
func renderCard(d cardData) ([]byte, error) {
	theme := satutil.ISSTheme()

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
			Text: "国际空间站 · ISS",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(25),
				satutil.BoldFontOption(),
				textimage.WithFontColor(theme.Title),
				textimage.WithAlign(textimage.AlignLeft),
				textimage.WithPadding(0, 2),
			},
		},
	)
	canvas.AddText("International Space Station  |  低地球轨道 · 实时遥测",
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
	canvas.AddText(fmt.Sprintf("%.1f", d.Pos.Altitude),
		textimage.WithFontSize(54),
		satutil.BoldFontOption(),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(theme.Value),
		textimage.WithTextShadow(color.RGBA{R: 96, G: 180, B: 255, A: 110}, 1, 3, 6),
		textimage.WithPadding(28, 0),
	)
	canvas.AddRow(
		textimage.RowItem{
			Text: fmt.Sprintf("速度 %.2f km/s", d.Pos.Velocity/3600),
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

	// ── 状态徽章 ─────────────────────────────────────────────────────────────
	visText := "光照中"
	visColor := color.NRGBA{R: 52, G: 156, B: 104, A: 235}
	if d.Pos.Visibility == "eclipsed" {
		visText = "地影中"
		visColor = color.NRGBA{R: 92, G: 106, B: 164, A: 235}
	}
	crew := len(d.Astros)
	if crew == 0 {
		crew = d.AstroCount
	}
	badges := []textimage.BadgeItem{
		{Text: visText, BgColor: visColor, TextColor: color.White},
		{Text: fmt.Sprintf("在轨 %d 人", crew), BgColor: color.NRGBA{R: 52, G: 108, B: 190, A: 235}, TextColor: color.White},
	}
	if d.PeriodMin > 0 {
		badges = append(badges, textimage.BadgeItem{
			Text:      fmt.Sprintf("周期 %.1f min", d.PeriodMin),
			BgColor:   color.NRGBA{R: 96, G: 70, B: 170, A: 225},
			TextColor: color.White,
		})
	}
	if len(d.Series) >= 3 && absf(d.Trend.Slope) > 0.001 {
		arrow, trendColor := "↑", color.NRGBA{R: 52, G: 156, B: 104, A: 235}
		if d.Trend.Slope < 0 {
			arrow, trendColor = "↓", color.NRGBA{R: 190, G: 112, B: 66, A: 235}
		}
		badges = append(badges, textimage.BadgeItem{
			Text:      fmt.Sprintf("%s %.2f km/天", arrow, absf(d.Trend.Slope)),
			BgColor:   trendColor,
			TextColor: color.White,
		})
	}
	canvas.AddBadgeRow(badges,
		textimage.WithBadgeFontSize(12),
		textimage.WithBadgeRadius(9),
		textimage.WithBadgeRowPadding(30, 4),
	)

	canvas.AddSpacer(14)

	// ── 数据面板 ─────────────────────────────────────────────────────────────
	panelW := 232
	panelH := 162
	periodArc := clamp01((d.PeriodMin - 85) / 15)
	speedArc := clamp01(d.Pos.Velocity / 29000)
	coverArc := clamp01(d.FootprintKm / 5000)

	canvas.AddRow(
		textimage.RowItem{Image: satutil.StatPanel(theme, satutil.StatPanelSpec{
			Width: panelW, Height: panelH, Title: "轨道周期",
			Value: fmt.Sprintf("%.1f", d.PeriodMin), Sub: "分钟 / 圈", Arc: periodArc, ArcColor: theme.Accent,
		}), ImageOpts: imgWidth(panelW)},
		textimage.RowItem{Image: satutil.StatPanel(theme, satutil.StatPanelSpec{
			Width: panelW, Height: panelH, Title: "轨道速度",
			Value: fmt.Sprintf("%.0f", d.Pos.Velocity), Sub: "km/h", Arc: speedArc, ArcColor: theme.Accent,
		}), ImageOpts: imgWidth(panelW)},
		textimage.RowItem{Image: satutil.StatPanel(theme, satutil.StatPanelSpec{
			Width: panelW, Height: panelH, Title: "可见范围",
			Value: fmt.Sprintf("%.0f", d.FootprintKm), Sub: "km / 地面直径", Arc: coverArc, ArcColor: theme.Accent,
		}), ImageOpts: imgWidth(panelW)},
	)

	canvas.AddSpacer(14)

	// ── 地面轨迹 ─────────────────────────────────────────────────────────────
	if len(d.Track) >= 2 {
		// 单圈示意不区分已飞过/待飞行（不传 Now），整条轨迹用实线绘制。
		canvas.AddImage(satutil.GroundTrackPanel(theme, satutil.MapSpec{
			Width: contentWidth, Height: satutil.WorldMapPanelHeight(contentWidth),
			Track: d.Track, Lat: d.Pos.Latitude, Lon: d.Pos.Longitude,
			FootprintDeg: satutil.VisibleAngularRadius(d.Pos.Altitude),
			Title:        "地面轨迹",
			Note:         "单圈示意",
			Caption: fmt.Sprintf("星下点 %.2f°%s %.2f°%s · 圆轨道近似",
				absf(d.Pos.Latitude), nsLabel(d.Pos.Latitude),
				absf(d.Pos.Longitude), ewLabel(d.Pos.Longitude)),
		}), textimage.WithImgWidth(contentWidth), textimage.WithImgAlign(textimage.AlignCenter))

		canvas.AddSpacer(14)
	}

	// ── 高度历史曲线 ─────────────────────────────────────────────────────────
	if chart := satutil.AltitudeChart(theme, satutil.ChartSpec{
		Width: contentWidth, Height: 220,
		Points:   toChartPoints(d.Series),
		Title:    "轨道高度历史",
		Unit:     "高度 km · 时间 UTC",
		Now:      d.Now,
		NowLabel: "现在",
		Caption:  historyCaption(d.Series, d.Trend),
	}); chart != nil {
		canvas.AddImage(chart,
			textimage.WithImgWidth(contentWidth),
			textimage.WithImgAlign(textimage.AlignCenter))
		canvas.AddSpacer(14)
	}

	// ── 在轨航天员 ───────────────────────────────────────────────────────────
	if len(d.Astros) > 0 {
		canvas.AddImage(satutil.ChipWall(theme, satutil.ChipSpec{
			Width: contentWidth,
			Title: fmt.Sprintf("在轨航天员 · %d 人", crew),
			Chips: d.Astros,
		}), textimage.WithImgWidth(contentWidth), textimage.WithImgAlign(textimage.AlignCenter))
		canvas.AddSpacer(12)
	}

	// ── 页脚 ─────────────────────────────────────────────────────────────────
	canvas.AddDivider(
		textimage.WithDividerColor(color.NRGBA{R: 110, G: 150, B: 220, A: 130}),
		textimage.WithDividerThickness(1),
		textimage.WithDividerInset(36),
		textimage.WithDividerPadding(4),
	)
	canvas.AddSpacer(6)

	sourceColor := color.NRGBA{R: 154, G: 178, B: 218, A: 220}
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
			Text: d.Pos.Timestamp.UTC().Format("2006-01-02 15:04"),
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

// toChartPoints 将高度历史转换为曲线图数据点。
func toChartPoints(series []AltRecord) []satutil.ChartPoint {
	out := make([]satutil.ChartPoint, 0, len(series))
	for _, r := range series {
		out = append(out, satutil.ChartPoint{Time: r.Time, Value: r.Altitude})
	}
	return out
}

// historyCaption 生成高度历史曲线的底部说明（时长 + 高度区间）。
func historyCaption(series []AltRecord, trend Trend) string {
	if len(series) < 2 {
		return ""
	}
	span := series[len(series)-1].Time.Sub(series[0].Time)
	rangeText := ""
	if trend.MaxAlt > trend.MinAlt {
		rangeText = fmt.Sprintf(" · 区间 %.0f–%.0f km", trend.MinAlt, trend.MaxAlt)
	}
	if span.Hours() < 1 {
		return fmt.Sprintf("近 %.0f 分钟 · 每 5 分钟采样%s", span.Minutes(), rangeText)
	}
	return fmt.Sprintf("近 %.1f 小时 · 每 5 分钟采样%s", span.Hours(), rangeText)
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
