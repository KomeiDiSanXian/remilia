package bilibili

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

const cardWidth = 600

var (
	accentColor   = color.RGBA{R: 0, G: 161, B: 214, A: 255}
	textPrimary   = color.RGBA{R: 30, G: 30, B: 30, A: 255}
	textSecondary = color.RGBA{R: 120, G: 120, B: 120, A: 255}
	dividerColor  = color.RGBA{R: 220, G: 220, B: 220, A: 255}
	bgStart       = color.RGBA{R: 245, G: 245, B: 250, A: 255}
	bgEnd         = color.RGBA{R: 230, G: 240, B: 250, A: 255}
)

// renderUserCard 渲染 UP 主信息卡片为 PNG 图片。
// 包含头像、用户名、等级、关注/粉丝/动态统计、签名。
func renderUserCard(user *UserInfo, rel *RelationStat) ([]byte, error) {
	bg := textimage.LinearGradient(cardWidth, 600, 160,
		textimage.Stop(0.0, bgStart),
		textimage.Stop(0.5, bgEnd),
		textimage.Stop(1.0, bgStart),
	)

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(textPrimary),
		textimage.WithLineHeight(1.5),
		textimage.WithPadding(24, 16),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddText("B站用户信息", textimage.WithFontSize(22), textimage.WithFontColor(accentColor), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(12)

	var avatarImg image.Image
	if user.Avatar != "" {
		avatarImg, _ = fetchImage(user.Avatar)
	}
	if avatarImg != nil {
		canvas.AddImage(avatarImg, textimage.WithImgCircle(), textimage.WithImgWidth(80), textimage.WithImgAlign(textimage.AlignCenter))
	} else {
		canvas.AddText("[头像]", textimage.WithFontSize(40), textimage.WithAlign(textimage.AlignCenter))
	}
	canvas.AddSpacer(8)

	canvas.AddText(user.Name, textimage.WithFontSize(24), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(4)

	levelText := fmt.Sprintf("Lv.%d", user.Level)
	canvas.AddText(levelText, textimage.WithFontSize(14), textimage.WithFontColor(color.RGBA{R: 0, G: 180, B: 80, A: 255}), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(12)

	if rel != nil {
		statsText := fmt.Sprintf("关注 %d    粉丝 %d", rel.Following, rel.Follower)
		canvas.AddText(statsText, textimage.WithFontSize(16), textimage.WithFontColor(textSecondary), textimage.WithAlign(textimage.AlignCenter))
		canvas.AddSpacer(10)
	}

	canvas.AddDivider(textimage.WithDividerColor(dividerColor), textimage.WithDividerInset(20))

	if user.Sign != "" {
		canvas.AddSpacer(6)
		signText := user.Sign
		if len([]rune(signText)) > 60 {
			signText = string([]rune(signText)[:60]) + "..."
		}
		canvas.AddText("签名："+signText, textimage.WithFontSize(14), textimage.WithFontColor(textSecondary))
	}

	return canvas.ResultPNG()
}

// renderLiveCard 渲染直播状态卡片为 PNG 图片。
// 包含直播状态、封面图、标题、观看人数、直播间链接。
func renderLiveCard(live *LiveInfo) ([]byte, error) {
	bg := textimage.LinearGradient(cardWidth, 500, 160,
		textimage.Stop(0.0, bgStart),
		textimage.Stop(0.5, bgEnd),
		textimage.Stop(1.0, bgStart),
	)

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(textPrimary),
		textimage.WithLineHeight(1.5),
		textimage.WithPadding(24, 16),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddText("B站直播间", textimage.WithFontSize(22), textimage.WithFontColor(accentColor), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(12)

	statusColor := color.RGBA{R: 200, G: 50, B: 50, A: 255}
	statusText := "未开播"
	if live.IsLiving {
		statusColor = color.RGBA{R: 50, G: 200, B: 50, A: 255}
		statusText = "直播中"
	}
	canvas.AddText("● "+statusText, textimage.WithFontSize(20), textimage.WithFontColor(statusColor), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(10)

	if live.IsLiving {
		if live.Cover != "" {
			cover, _ := fetchImage(live.Cover)
			if cover != nil {
				canvas.AddImage(cover, textimage.WithImgWidth(400), textimage.WithImgAlign(textimage.AlignCenter))
				canvas.AddSpacer(10)
			}
		}
		canvas.AddText(live.Title, textimage.WithFontSize(18), textimage.WithAlign(textimage.AlignCenter))
		canvas.AddSpacer(6)
		canvas.AddText(fmt.Sprintf("观看人数: %d", live.WatcherNum), textimage.WithFontSize(14), textimage.WithFontColor(textSecondary), textimage.WithAlign(textimage.AlignCenter))
		canvas.AddSpacer(6)
		canvas.AddText(fmt.Sprintf("直播间: https://live.bilibili.com/%d", live.RoomID), textimage.WithFontSize(13), textimage.WithFontColor(accentColor), textimage.WithAlign(textimage.AlignCenter))
	} else {
		canvas.AddText("主播暂时没有开播哦~", textimage.WithFontSize(16), textimage.WithFontColor(textSecondary), textimage.WithAlign(textimage.AlignCenter))
		canvas.AddSpacer(6)
		if live.RoomID > 0 {
			canvas.AddText(fmt.Sprintf("直播间: https://live.bilibili.com/%d", live.RoomID), textimage.WithFontSize(13), textimage.WithFontColor(textSecondary), textimage.WithAlign(textimage.AlignCenter))
		}
	}

	return canvas.ResultPNG()
}

// renderErrorCard 渲染错误提示卡片为 PNG 图片（图片渲染失败时的备用方案）。
func renderErrorCard(message string) ([]byte, error) {
	bg := textimage.LinearGradient(cardWidth, 200, 160,
		textimage.Stop(0.0, color.RGBA{R: 255, G: 240, B: 240, A: 255}),
		textimage.Stop(1.0, color.RGBA{R: 255, G: 250, B: 250, A: 255}),
	)
	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.RGBA{R: 200, G: 50, B: 50, A: 255}),
		textimage.WithPadding(24, 16),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddText("B站查询出错", textimage.WithFontSize(20), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(8)
	canvas.AddText(message, textimage.WithFontSize(14), textimage.WithAlign(textimage.AlignCenter))
	return canvas.ResultPNG()
}

// formatBiliUserText 将 UP 主信息格式化为纯文本（图片渲染失败时的备用方案）。
func formatBiliUserText(user *UserInfo, rel *RelationStat) string {
	followers := "?"
	following := "?"
	if rel != nil {
		followers = fmt.Sprintf("%d", rel.Follower)
		following = fmt.Sprintf("%d", rel.Following)
	}
	return fmt.Sprintf("[B站] %s\nUID: %d\nLv.%d\n粉丝: %s  关注: %s\n签名: %s",
		user.Name, user.Mid, user.Level, followers, following, user.Sign)
}

// formatLiveText 将直播状态格式化为纯文本（图片渲染失败时的备用方案）。
func formatLiveText(live *LiveInfo) string {
	if live.IsLiving {
		return fmt.Sprintf("[B站] %s\n状态: 直播中\n标题: %s\n观看: %d\n直播间: https://live.bilibili.com/%d",
			live.UserName, live.Title, live.WatcherNum, live.RoomID)
	}
	return fmt.Sprintf("[B站] %s\n状态: 未开播", live.UserName)
}

// renderUserSearchResults 渲染用户搜索结果卡片为 PNG 图片。
// 显示最多 8 个匹配用户（含等级、粉丝数、直播状态）。
func renderUserSearchResults(results []SearchUserResult, keyword string) ([]byte, error) {
	bg := textimage.LinearGradient(cardWidth, 200, 160,
		textimage.Stop(0.0, bgStart),
		textimage.Stop(1.0, bgEnd),
	)

	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(textPrimary),
		textimage.WithLineHeight(1.5),
		textimage.WithPadding(20, 12),
	)
	if err != nil {
		return nil, err
	}

	title := fmt.Sprintf("🔍 搜索用户: %s", keyword)
	canvas.AddText(title, textimage.WithFontSize(20), textimage.WithFontColor(accentColor), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(10)

	maxShow := 8
	if len(results) > maxShow {
		results = results[:maxShow]
	}

	for i, r := range results {
		prefix := fmt.Sprintf("%d. ", i+1)
		liveStatus := ""
		if r.IsLive == 1 {
			liveStatus = " 🔴直播中"
		}
		fansStr := formatFans(r.Fans)
		line := fmt.Sprintf("%s%s  Lv.%d  👤%s%s", prefix, r.Name, r.Level, fansStr, liveStatus)
		canvas.AddText(line, textimage.WithFontSize(14))
		canvas.AddSpacer(2)
	}

	if len(results) == 0 {
		canvas.AddText("未找到匹配的用户", textimage.WithFontSize(14), textimage.WithFontColor(textSecondary), textimage.WithAlign(textimage.AlignCenter))
	}

	return canvas.ResultPNG()
}

// formatFans 将粉丝数格式化为中文可读形式（如 "342.1万"）。
func formatFans(n int64) string {
	switch {
	case n >= 10000:
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// formatSearchUserText 将搜索结果格式化为纯文本（图片渲染失败时的备用方案）。
func formatSearchUserText(results []SearchUserResult) string {
	if len(results) == 0 {
		return "未找到匹配的用户"
	}
	var b strings.Builder
	for i, r := range results {
		if i >= 10 {
			b.WriteString(fmt.Sprintf("…等%d个结果", len(results)))
			break
		}
		liveStatus := ""
		if r.IsLive == 1 {
			liveStatus = " [直播中]"
		}
		b.WriteString(fmt.Sprintf("%d. %s (UID: %d) Lv.%d 粉丝%s%s\n", i+1, r.Name, r.Mid, r.Level, formatFans(r.Fans), liveStatus))
	}
	return b.String()
}
