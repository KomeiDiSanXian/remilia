package bilibili

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

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

	hostText := live.UserName
	if hostText == "" {
		hostText = fmt.Sprintf("UID: %d", live.UID)
	}
	canvas.AddText(hostText, textimage.WithFontSize(15), textimage.WithFontColor(textSecondary), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(8)

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
		if live.RoomID > 0 {
			canvas.AddText(fmt.Sprintf("直播间: https://live.bilibili.com/%d", live.RoomID), textimage.WithFontSize(13), textimage.WithFontColor(accentColor), textimage.WithAlign(textimage.AlignCenter))
		}
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
func renderErrorCard(message string) ([]byte, error) { //nolint:unused
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

	title := fmt.Sprintf("[搜索用户] %s", keyword)
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
			liveStatus = " [直播中]"
		}
		fansStr := formatFans(r.Fans)
		line := fmt.Sprintf("%s%s  Lv.%d  粉丝:%s%s", prefix, r.Name, r.Level, fansStr, liveStatus)
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

// renderVideoCard 渲染视频信息卡片为 PNG 图片。
// 包含封面、标题、UP 主、发布时间与统计数据。
func renderVideoCard(info *VideoInfo) ([]byte, error) {
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

	canvas.AddText("B站视频信息", textimage.WithFontSize(22), textimage.WithFontColor(accentColor), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(12)

	if info.Pic != "" {
		if cover, err := fetchImage(info.Pic); err == nil && cover != nil {
			canvas.AddImage(cover, textimage.WithImgWidth(400), textimage.WithImgAlign(textimage.AlignCenter))
			canvas.AddSpacer(10)
		}
	}

	canvas.AddText(truncateText(info.Title, 50), textimage.WithFontSize(18), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(6)

	hostText := fmt.Sprintf("UP: %s (UID: %d)", info.Owner.Name, info.Owner.MID)
	if info.PubDate > 0 {
		hostText += fmt.Sprintf("    发布: %s", time.Unix(info.PubDate, 0).Format("2006-01-02"))
	}
	canvas.AddText(hostText, textimage.WithFontSize(13), textimage.WithFontColor(textSecondary), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(10)

	canvas.AddDivider(textimage.WithDividerColor(dividerColor), textimage.WithDividerInset(20))

	dur := formatDuration(info.Duration)
	statsLine := fmt.Sprintf("播放 %s    弹幕 %s", formatBigNum(info.Stat.View), formatBigNum(info.Stat.Danmaku))
	canvas.AddText(statsLine, textimage.WithFontSize(14), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(4)
	statsLine2 := fmt.Sprintf("点赞 %s    硬币 %s    收藏 %s    分享 %s", formatBigNum(info.Stat.Like), formatBigNum(info.Stat.Coin), formatBigNum(info.Stat.Favorite), formatBigNum(info.Stat.Share))
	canvas.AddText(statsLine2, textimage.WithFontSize(13), textimage.WithFontColor(textSecondary), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(4)
	canvas.AddText(fmt.Sprintf("时长 %s    评论 %s", dur, formatBigNum(info.Stat.Reply)), textimage.WithFontSize(13), textimage.WithFontColor(textSecondary), textimage.WithAlign(textimage.AlignCenter))

	if info.Desc != "" {
		canvas.AddSpacer(8)
		canvas.AddText("简介: "+truncateText(info.Desc, 120), textimage.WithFontSize(13), textimage.WithFontColor(textSecondary))
	}

	return canvas.ResultPNG()
}

// formatVideoText 将视频信息格式化为纯文本（图片渲染失败时的备用方案）。
func formatVideoText(info *VideoInfo) string {
	dur := formatDuration(info.Duration)
	pub := ""
	if info.PubDate > 0 {
		pub = time.Unix(info.PubDate, 0).Format("2006-01-02")
	}
	return fmt.Sprintf("[B站] %s\nUP: %s (UID: %d)\n播放: %d  弹幕: %d  点赞: %d\n硬币: %d  收藏: %d  分享: %d\n时长: %s  发布: %s\nhttps://www.bilibili.com/video/%s",
		info.Title, info.Owner.Name, info.Owner.MID,
		info.Stat.View, info.Stat.Danmaku, info.Stat.Like,
		info.Stat.Coin, info.Stat.Favorite, info.Stat.Share,
		dur, pub, info.BVID)
}

// renderVideoListCard 渲染 UP 主投稿列表卡片为 PNG 图片。
func renderVideoListCard(items []VideoItem, author string) ([]byte, error) {
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

	canvas.AddText(fmt.Sprintf("[最新投稿] %s", author), textimage.WithFontSize(18), textimage.WithFontColor(accentColor), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(10)

	maxShow := 8
	if len(items) > maxShow {
		items = items[:maxShow]
	}
	for i, it := range items {
		pub := ""
		if it.Created > 0 {
			pub = time.Unix(it.Created, 0).Format("01-02")
		}
		line := fmt.Sprintf("%d. %s\n    播放 %s  %s  %s", i+1, truncateText(it.Title, 40), formatBigNum(it.Play), it.Duration, pub)
		canvas.AddText(line, textimage.WithFontSize(13))
		canvas.AddSpacer(4)
	}

	return canvas.ResultPNG()
}

// formatVideoListText 将投稿列表格式化为纯文本（图片渲染失败时的备用方案）。
func formatVideoListText(items []VideoItem, author string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[B站] %s 的最新投稿:\n", author))
	for i, it := range items {
		if i >= 10 {
			b.WriteString("…等更多\n")
			break
		}
		b.WriteString(fmt.Sprintf("%d. %s (播放 %s)\n", i+1, it.Title, formatBigNum(it.Play)))
	}
	return b.String()
}

// renderBangumiResults 渲染番剧搜索结果卡片为 PNG 图片。
func renderBangumiResults(results []BangumiResult, keyword string) ([]byte, error) {
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

	canvas.AddText(fmt.Sprintf("[番剧搜索] %s", keyword), textimage.WithFontSize(18), textimage.WithFontColor(accentColor), textimage.WithAlign(textimage.AlignCenter))
	canvas.AddSpacer(10)

	for i, r := range results {
		scoreStr := ""
		if r.Score > 0 {
			scoreStr = fmt.Sprintf("  评分 %.1f", r.Score)
		}
		areas := ""
		if r.Areas != "" {
			areas = "  [" + r.Areas + "]"
		}
		line := fmt.Sprintf("%d. %s%s%s", i+1, stripHTML(r.Title), areas, scoreStr)
		canvas.AddText(line, textimage.WithFontSize(14))
		canvas.AddSpacer(2)
	}

	return canvas.ResultPNG()
}

// formatBangumiText 将番剧搜索结果格式化为纯文本（图片渲染失败时的备用方案）。
func formatBangumiText(results []BangumiResult) string {
	var b strings.Builder
	for i, r := range results {
		scoreStr := ""
		if r.Score > 0 {
			scoreStr = fmt.Sprintf("  (评分 %.1f)", r.Score)
		}
		b.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, stripHTML(r.Title), scoreStr))
	}
	return b.String()
}

// stripHTML 去除搜索接口返回标题中的 <em> 等 HTML 标签。
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// truncateText 按 rune 数截断文本，超出加省略号。
func truncateText(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "..."
}

// formatBigNum 将大数字格式化为可读形式（如 1.02亿 / 342.1万）。
func formatBigNum(n int64) string {
	switch {
	case n >= 100000000:
		return fmt.Sprintf("%.2f亿", float64(n)/100000000)
	case n >= 10000:
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// formatDuration 将秒数格式化为 mm:ss 或 h:mm:ss。
func formatDuration(sec int64) string {
	if sec <= 0 {
		return "00:00"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
