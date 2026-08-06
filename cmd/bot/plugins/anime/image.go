package anime

import (
	"fmt"
	"image/color"
	"net/http"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

const cardWidth = 640

// coverTransport 复用在 Setup 阶段构造的传输层，使封面下载与 API 请求共享代理配置。
var coverTransport *http.Transport

var (
	accentColorA    = color.RGBA{R: 80, G: 160, B: 240, A: 255}
	primaryColorA   = color.RGBA{R: 30, G: 30, B: 30, A: 255}
	secondaryColorA = color.RGBA{R: 130, G: 130, B: 130, A: 255}
	bgStartA        = color.RGBA{R: 245, G: 245, B: 250, A: 255}
	bgEndA          = color.RGBA{R: 235, G: 240, B: 250, A: 255}
	dividerColorA   = color.RGBA{R: 220, G: 220, B: 225, A: 255}
)

var weekdayNames = []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// renderCalendar 渲染当季番剧时间表为 PNG 图片。
// 按星期分组展示番剧名称、评分和放送日期。
func renderCalendar(entries []WeekdayEntry) ([]byte, error) {
	bg := textimage.LinearGradient(cardWidth, 200, 160,
		textimage.Stop(0.0, bgStartA),
		textimage.Stop(1.0, bgEndA),
	)

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(primaryColorA),
		textimage.WithLineHeight(1.5),
		textimage.WithPadding(20, 12),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddText("📺 当季番剧时间表", textimage.WithFontSize(22), textimage.WithFontColor(accentColorA), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(12)

	for i, e := range entries {
		weekdayName := weekdayNames[e.Weekday.ID%7]

		header := fmt.Sprintf("── %s ── %d部", weekdayName, len(e.Subjects))
		canvas.AddText(header, textimage.WithFontSize(16), textimage.WithFontColor(accentColorA))
		canvas.AddSpacer(4)

		for _, sub := range e.Subjects {
			displayName := sub.NameCN
			if displayName == "" {
				displayName = sub.Name
			}

			ratingStr := ""
			if sub.Rating.Score > 0 {
				ratingStr = fmt.Sprintf(" ⭐ %.1f", sub.Rating.Score)
			}

			line := fmt.Sprintf("  %s%s", displayName, ratingStr)

			if sub.AirDate != "" && len(sub.AirDate) >= 7 {
				line += fmt.Sprintf("  %s", sub.AirDate[:7])
			}

			canvas.AddText(line, textimage.WithFontSize(14))
			canvas.AddSpacer(2)
		}

		if i < len(entries)-1 {
			canvas.AddSpacer(6)
		}
	}

	return canvas.ResultPNG()
}

// renderAnimeCard 渲染番剧详情卡片为 PNG 图片。
// 包含封面、名称、评分、排名、集数、放送日期、简介。
func renderAnimeCard(sub *AnimeSubject) ([]byte, error) {
	bg := textimage.LinearGradient(cardWidth, 300, 160,
		textimage.Stop(0.0, bgStartA),
		textimage.Stop(1.0, bgEndA),
	)

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(primaryColorA),
		textimage.WithLineHeight(1.5),
		textimage.WithPadding(20, 12),
	)
	if err != nil {
		return nil, err
	}

	displayName := sub.NameCN
	if displayName == "" {
		displayName = sub.Name
	}
	canvas.AddText(displayName, textimage.WithFontSize(22), textimage.WithFontColor(accentColorA), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(8)

	if sub.ImageURL() != "" {
		cover, _ := fetchImageA(coverTransport, sub.ImageURL())
		if cover != nil {
			canvas.AddImage(cover, textimage.WithImgWidth(200), textimage.WithImgAlign(textimage.AlignCenter))
			canvas.AddSpacer(8)
		}
	}

	canvas.AddDivider(textimage.WithDividerColor(dividerColorA), textimage.WithDividerInset(16))

	var infoLines []string
	if sub.Rating.Score > 0 {
		infoLines = append(infoLines, fmt.Sprintf("评分: ⭐ %.1f  (评分人数: %d)", sub.Rating.Score, sub.Rating.Total))
	}
	if sub.Rank > 0 {
		infoLines = append(infoLines, fmt.Sprintf("排名: #%d", sub.Rank))
	}
	if sub.Eps > 0 {
		infoLines = append(infoLines, fmt.Sprintf("集数: %d 话", sub.Eps))
	}
	if sub.AirDate != "" {
		if len(sub.AirDate) >= 7 {
			infoLines = append(infoLines, fmt.Sprintf("放送日期: %s", sub.AirDate[:7]))
		} else {
			infoLines = append(infoLines, fmt.Sprintf("放送日期: %s", sub.AirDate))
		}
	}

	for _, line := range infoLines {
		canvas.AddText(line, textimage.WithFontSize(14), textimage.WithFontColor(secondaryColorA))
	}
	canvas.AddSpacer(6)

	if sub.Summary != "" {
		summary := sub.Summary
		runes := []rune(summary)
		if len(runes) > 200 {
			summary = string(runes[:200]) + "..."
		}
		canvas.AddText("简介:", textimage.WithFontSize(14))
		canvas.AddText(summary, textimage.WithFontSize(13), textimage.WithFontColor(secondaryColorA))
	}

	return canvas.ResultPNG()
}

// renderSearchResults 渲染番剧搜索结果卡片为 PNG 图片。
// 每个结果显示名称、评分和简介摘要。
func renderSearchResults(results []AnimeSubject, keyword string) ([]byte, error) {
	bg := textimage.LinearGradient(cardWidth, 200, 160,
		textimage.Stop(0.0, bgStartA),
		textimage.Stop(1.0, bgEndA),
	)

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(primaryColorA),
		textimage.WithLineHeight(1.5),
		textimage.WithPadding(20, 12),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddText(fmt.Sprintf("🔍 搜索结果: %s", keyword), textimage.WithFontSize(20), textimage.WithFontColor(accentColorA), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(8)

	for i, sub := range results {
		displayName := sub.NameCN
		if displayName == "" {
			displayName = sub.Name
		}

		ratingStr := ""
		if sub.Rating.Score > 0 {
			ratingStr = fmt.Sprintf(" ⭐%.1f", sub.Rating.Score)
		}

		canvas.AddText(fmt.Sprintf("%d. %s  (ID: %d)%s", i+1, displayName, sub.ID, ratingStr), textimage.WithFontSize(14))

		if sub.Summary != "" {
			summary := sub.Summary
			runes := []rune(summary)
			if len(runes) > 60 {
				summary = string(runes[:60]) + "..."
			}
			canvas.AddText("   "+summary, textimage.WithFontSize(12), textimage.WithFontColor(secondaryColorA))
		}
		canvas.AddSpacer(4)
	}

	return canvas.ResultPNG()
}

// formatAnimeText 将番剧详情格式化为纯文本（图片渲染失败时的备用方案）。
func formatAnimeText(sub *AnimeSubject) string {
	displayName := sub.NameCN
	if displayName == "" {
		displayName = sub.Name
	}
	ratingStr := "暂无评分"
	if sub.Rating.Score > 0 {
		ratingStr = fmt.Sprintf("%.1f (%d人)", sub.Rating.Score, sub.Rating.Total)
	}
	epsStr := "未知"
	if sub.Eps > 0 {
		epsStr = fmt.Sprintf("%d话", sub.Eps)
	}
	return fmt.Sprintf("[Anime] %s\n评分: %s\n集数: %s\n放送: %s\n简介: %s",
		displayName, ratingStr, epsStr, sub.AirDate, sub.Summary)
}

// formatSearchText 将搜索结果格式化为纯文本（图片渲染失败时的备用方案）。
func formatSearchText(results []AnimeSubject) string {
	var b strings.Builder
	for i, sub := range results {
		displayName := sub.NameCN
		if displayName == "" {
			displayName = sub.Name
		}
		ratingStr := ""
		if sub.Rating.Score > 0 {
			ratingStr = fmt.Sprintf(" ⭐%.1f", sub.Rating.Score)
		}
		b.WriteString(fmt.Sprintf("%d. %s (ID: %d)%s\n", i+1, displayName, sub.ID, ratingStr))
	}
	return b.String()
}
