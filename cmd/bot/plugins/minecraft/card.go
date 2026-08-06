package minecraft

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const cardWidthMC = 600

var (
	mcTextPrimary = color.RGBA{R: 235, G: 235, B: 240, A: 255}
	mcTextDim     = color.RGBA{R: 160, G: 162, B: 170, A: 255}
	mcTextFaint   = color.RGBA{R: 115, G: 118, B: 128, A: 255}
	mcAccentGreen = color.RGBA{R: 94, G: 220, B: 110, A: 255}
	mcAccentRed   = color.RGBA{R: 235, G: 92, B: 80, A: 255}
	mcAccentBlue  = color.RGBA{R: 90, G: 170, B: 235, A: 255}
	mcAccentGold  = color.RGBA{R: 240, G: 200, B: 80, A: 255}
	mcBgDark      = color.RGBA{R: 24, G: 26, B: 30, A: 255}
	mcBgCard      = color.RGBA{R: 45, G: 48, B: 55, A: 255}
	mcBgCardHover = color.RGBA{R: 56, G: 60, B: 68, A: 255}
	mcDivider     = color.RGBA{R: 58, G: 62, B: 70, A: 255}
	mcTrackColor  = color.RGBA{R: 38, G: 40, B: 46, A: 255}
)

// renderMCCard 渲染 Minecraft 服务器状态卡片。
func renderMCCard(status *MCServerStatus) ([]byte, error) {
	if !status.Online {
		return renderMCOfflineCard(status)
	}

	bg := textimage.LinearGradient(cardWidthMC, 640, 160,
		textimage.Stop(0.0, color.RGBA{R: 26, G: 34, B: 30, A: 255}),
		textimage.Stop(0.55, mcBgDark),
		textimage.Stop(1.0, color.RGBA{R: 22, G: 24, B: 30, A: 255}),
	)

	canvas, err := textimage.NewCanvas(cardWidthMC,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(mcTextPrimary),
		textimage.WithLineHeight(1.55),
		textimage.WithPadding(24, 18),
	)
	if err != nil {
		return nil, err
	}

	// ── 头部：favicon + 地址/版本 ──────────────────────────────
	var favicon image.Image
	if len(status.Favicon) > 0 {
		if img, _, err := image.Decode(bytes.NewReader(status.Favicon)); err == nil && img.Bounds().Dx() > 0 {
			favicon = img
		}
	}

	editionLabel := "Java"
	if status.Edition == "bedrock" {
		editionLabel = "Bedrock"
	}
	addr := fmt.Sprintf("%s:%d", status.Host, status.Port)

	headerItems := []textimage.RowItem{}
	if favicon != nil {
		headerItems = append(headerItems, textimage.RowItem{
			Width: 64,
			Image: favicon,
			ImageOpts: []textimage.ImageOption{
				textimage.WithImgWidth(56),
				textimage.WithImgHeight(56),
			},
		})
	}
	headerItems = append(headerItems,
		textimage.RowItem{
			Text: fmt.Sprintf("%s  ·  %s", addr, editionLabel),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(19),
				textimage.WithFontColor(mcTextPrimary),
			},
		},
	)
	canvas.AddRow(headerItems...)
	canvas.AddSpacer(2)

	// ── 状态徽章行 ─────────────────────────────────────────────
	latencyStr := fmt.Sprintf("%dms", status.Latency.Milliseconds())
	if status.Latency < 0 {
		latencyStr = "N/A"
	}
	canvas.AddBadgeRow(
		[]textimage.BadgeItem{
			{Text: "● 在线", BgColor: color.RGBA{R: 30, G: 70, B: 38, A: 255}, TextColor: mcAccentGreen},
			{Text: editionLabel, BgColor: color.RGBA{R: 30, G: 55, B: 78, A: 255}, TextColor: mcAccentBlue},
			{Text: "延迟 " + latencyStr, BgColor: color.RGBA{R: 66, G: 54, B: 26, A: 255}, TextColor: mcAccentGold},
		},
		textimage.WithBadgeFontSize(12),
		textimage.WithBadgePadding(10, 4),
		textimage.WithBadgeGap(6),
	)
	canvas.AddSpacer(10)

	// ── MOTD 彩色横幅 ──────────────────────────────────────────
	if len(status.MOTD) > 0 {
		motdImg, err := renderMotdImage(status.MOTD, cardWidthMC-48, 17)
		if err == nil {
			canvas.AddImage(motdImg, textimage.WithImgAlign(textimage.AlignCenter))
			canvas.AddSpacer(10)
		}
	}

	canvas.AddDivider(textimage.WithDividerColor(mcDivider))
	canvas.AddSpacer(12)

	// ── 玩家进度 ───────────────────────────────────────────────
	online, max := status.Players.Online, status.Players.Max
	if max < 1 {
		max = 1
	}
	canvas.AddRow(
		textimage.RowItem{
			Text: "玩家",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(14),
				textimage.WithFontColor(mcTextDim),
			},
		},
		textimage.RowItem{
			Text: fmt.Sprintf("%d / %d", online, max),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(14),
				textimage.WithFontColor(mcAccentGreen),
				textimage.WithAlign(textimage.AlignRight),
			},
		},
	)
	canvas.AddSpacer(6)

	fillColor := mcAccentGreen
	ratio := float64(online) / float64(max)
	if ratio >= 0.85 {
		fillColor = mcAccentRed
	} else if ratio >= 0.55 {
		fillColor = mcAccentGold
	}
	canvas.AddProgressBar(float64(online), float64(max),
		textimage.WithProgressHeight(8),
		textimage.WithProgressFillColor(fillColor),
		textimage.WithProgressTrackColor(mcTrackColor),
		textimage.WithProgressRadius(4),
		textimage.WithProgressPadding(72, 6),
	)
	canvas.AddSpacer(10)

	// ── 版本信息行 ─────────────────────────────────────────────
	canvas.AddRow(
		textimage.RowItem{
			Text: "版本",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(13),
				textimage.WithFontColor(mcTextFaint),
			},
		},
		textimage.RowItem{
			Text: status.Version,
			TextOpts: []textimage.Option{
				textimage.WithFontSize(13),
				textimage.WithFontColor(mcTextPrimary),
				textimage.WithAlign(textimage.AlignRight),
			},
		},
	)
	if status.Protocol > 0 {
		canvas.AddSpacer(2)
		canvas.AddRow(
			textimage.RowItem{
				Text: "协议",
				TextOpts: []textimage.Option{
					textimage.WithFontSize(12),
					textimage.WithFontColor(mcTextFaint),
				},
			},
			textimage.RowItem{
				Text: fmt.Sprintf("%d", status.Protocol),
				TextOpts: []textimage.Option{
					textimage.WithFontSize(12),
					textimage.WithFontColor(mcTextDim),
					textimage.WithAlign(textimage.AlignRight),
				},
			},
		)
	}
	canvas.AddSpacer(12)

	// ── 玩家列表 ───────────────────────────────────────────────
	if len(status.Players.List) > 0 {
		canvas.AddDivider(textimage.WithDividerColor(mcDivider))
		canvas.AddSpacer(10)

		canvas.AddRow(
			textimage.RowItem{
				Text: "在线玩家",
				TextOpts: []textimage.Option{
					textimage.WithFontSize(14),
					textimage.WithFontColor(mcTextDim),
				},
			},
			textimage.RowItem{
				Text: fmt.Sprintf("%d", len(status.Players.List)),
				TextOpts: []textimage.Option{
					textimage.WithFontSize(14),
					textimage.WithFontColor(mcTextFaint),
					textimage.WithAlign(textimage.AlignRight),
				},
			},
		)
		canvas.AddSpacer(8)

		var badges []textimage.BadgeItem
		maxPlayers := min(len(status.Players.List), 10)
		for _, player := range status.Players.List[:maxPlayers] {
			badges = append(badges, textimage.BadgeItem{
				// MC 玩家名上限 16 字符；截断异常数据防止 badge 过宽
				Text:      truncateRunes(player.Name, 16),
				BgColor:   mcBgCard,
				TextColor: mcTextPrimary,
			})
		}
		if len(status.Players.List) > maxPlayers {
			badges = append(badges, textimage.BadgeItem{
				Text:      fmt.Sprintf("+%d", len(status.Players.List)-maxPlayers),
				BgColor:   mcBgCardHover,
				TextColor: mcTextDim,
			})
		}
		canvas.AddBadgeRow(badges,
			textimage.WithBadgeFontSize(12),
			textimage.WithBadgePadding(8, 4),
			textimage.WithBadgeGap(4),
		)
	}

	// ── 底部时间戳 ─────────────────────────────────────────────
	canvas.AddSpacer(14)
	canvas.AddText("查询于 "+time.Now().Format("2006-01-02 15:04:05"),
		textimage.WithFontSize(11),
		textimage.WithFontColor(mcTextFaint),
		textimage.WithAlign(textimage.AlignCenter),
	)

	return canvas.ResultPNG()
}

func renderMCOfflineCard(status *MCServerStatus) ([]byte, error) {
	bg := textimage.LinearGradient(600, 300, 160,
		textimage.Stop(0.0, color.RGBA{R: 34, G: 27, B: 28, A: 255}),
		textimage.Stop(0.55, mcBgDark),
		textimage.Stop(1.0, color.RGBA{R: 30, G: 24, B: 26, A: 255}),
	)

	canvas, err := textimage.NewCanvas(cardWidthMC,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(mcTextPrimary),
		textimage.WithLineHeight(1.5),
		textimage.WithPadding(32, 26),
	)
	if err != nil {
		return nil, err
	}

	// 头部：favicon（若有）+ 地址
	var favicon image.Image
	if len(status.Favicon) > 0 {
		if img, _, err := image.Decode(bytes.NewReader(status.Favicon)); err == nil && img.Bounds().Dx() > 0 {
			favicon = img
		}
	}
	headerItems := []textimage.RowItem{}
	if favicon != nil {
		headerItems = append(headerItems, textimage.RowItem{
			Width: 64,
			Image: favicon,
			ImageOpts: []textimage.ImageOption{
				textimage.WithImgWidth(52),
				textimage.WithImgHeight(52),
			},
		})
	}
	headerItems = append(headerItems, textimage.RowItem{
		Text: fmt.Sprintf("%s:%d", status.Host, status.Port),
		TextOpts: []textimage.Option{
			textimage.WithFontSize(19),
			textimage.WithFontColor(mcTextPrimary),
		},
	})
	canvas.AddRow(headerItems...)
	canvas.AddSpacer(4)

	canvas.AddBadgeRow(
		[]textimage.BadgeItem{
			{Text: "● 离线", BgColor: color.RGBA{R: 70, G: 32, B: 30, A: 255}, TextColor: mcAccentRed},
		},
		textimage.WithBadgeFontSize(12),
		textimage.WithBadgePadding(10, 4),
		textimage.WithBadgeGap(6),
	)
	canvas.AddSpacer(14)

	canvas.AddText("服务器无法连接或已关闭",
		textimage.WithFontSize(14),
		textimage.WithFontColor(mcTextDim),
		textimage.WithAlign(textimage.AlignCenter),
	)
	canvas.AddSpacer(6)
	canvas.AddText("请检查主机名、端口与服务器运行状态",
		textimage.WithFontSize(12),
		textimage.WithFontColor(mcTextFaint),
		textimage.WithAlign(textimage.AlignCenter),
	)

	return canvas.ResultPNG()
}

func renderMotdImage(segments []MotdSegment, maxWidth int, fontSize float64) (image.Image, error) {
	fontPath := textimage.SystemCJKFontPath()
	if fontPath == "" {
		return nil, fmt.Errorf("no CJK font available")
	}

	raw, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, err
	}

	var parsed *opentype.Font
	if isTTCBytes(raw) {
		col, err := opentype.ParseCollection(raw)
		if err != nil {
			return nil, err
		}
		parsed, err = col.Font(0)
		if err != nil {
			return nil, err
		}
	} else {
		parsed, err = opentype.Parse(raw)
		if err != nil {
			return nil, err
		}
	}

	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: fontSize, DPI: 72})
	if err != nil {
		return nil, err
	}
	defer face.Close()

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()

	totalW := 0
	for _, seg := range segments {
		w := font.MeasureString(face, seg.Text).Ceil()
		totalW += w
	}
	if totalW < 1 {
		totalW = 1
	}
	// 图片宽度不得超过 maxWidth：超长 MOTD 截断到可用宽度，
	// 避免单个超长段把图片撑到画布之外。
	if totalW > maxWidth {
		totalW = maxWidth
	}

	lineH := max((metrics.Ascent + metrics.Descent).Ceil(), 1)

	// 双层高度：上方留出阴影偏移空间（MOTD 常为亮色，深色阴影保证在浅色上可读）
	padTop := 2
	img := image.NewRGBA(image.Rect(0, 0, totalW, lineH+padTop))

	x := fixed.Int26_6(0)
	remaining := fixed.Int26_6(maxWidth * 64)
	for _, seg := range segments {
		if x >= remaining {
			break
		}
		text := seg.Text
		w := font.MeasureString(face, text)
		// 单个段超出剩余空间时按字符逐步截断（避免首段超长导致整行空白）
		if x+w > remaining {
			text = truncateSegToWidth(face, seg.Text, remaining-x)
			w = font.MeasureString(face, text)
		}
		// 深色阴影（MC 服务器默认加阴影，提升可读性）
		sh := &font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(color.RGBA{R: 20, G: 20, B: 24, A: 200}),
			Face: face,
			Dot:  fixed.P(x.Round()+1, ascent+1+padTop),
		}
		sh.DrawString(text)
		// 主体
		d := &font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(seg.Color),
			Face: face,
			Dot:  fixed.P(x.Round(), ascent+padTop),
		}
		d.DrawString(text)
		x += w
	}

	return img, nil
}

// truncateSegToWidth 按可用像素宽度截断单段文本（字符级二分，兼容 CJK）。
func truncateSegToWidth(face font.Face, s string, avail fixed.Int26_6) string {
	runes := []rune(s)
	if len(runes) <= 1 {
		return s
	}
	lo, hi := 1, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if font.MeasureString(face, string(runes[:mid])) <= avail {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(runes[:lo])
}

// truncateRunes 按 rune 数截断字符串，超长加省略号。
func truncateRunes(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "..."
}

func isTTCBytes(data []byte) bool {
	return len(data) >= 4 && data[0] == 0x74 && data[1] == 0x74 &&
		data[2] == 0x63 && data[3] == 0x66
}

// RenderCardForTest 仅用于测试：渲染卡片并返回 PNG 字节。
func RenderCardForTest(status *MCServerStatus) ([]byte, error) {
	return renderMCCard(status)
}

// formatMCText 将服务器状态格式化为纯文本（图片渲染失败时的备用方案）。
func formatMCText(status *MCServerStatus) string {
	editionLabel := "Java 版"
	if status.Edition == "bedrock" {
		editionLabel = "Bedrock 版"
	}
	statusLabel := "在线"
	if !status.Online {
		statusLabel = "离线"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("⛏ %s:%d (%s)\n", status.Host, status.Port, editionLabel))
	b.WriteString(fmt.Sprintf("状态: %s", statusLabel))
	if status.Online {
		b.WriteString(fmt.Sprintf(" | 延迟: %dms", status.Latency.Milliseconds()))
	}
	b.WriteString("\n")

	if !status.Online {
		return b.String()
	}

	if status.MOTDPlain != "" {
		b.WriteString(fmt.Sprintf("MOTD: %s\n", status.MOTDPlain))
	}
	b.WriteString(fmt.Sprintf("版本: %s\n", status.Version))
	b.WriteString(fmt.Sprintf("玩家: %d / %d\n", status.Players.Online, status.Players.Max))
	if len(status.Players.List) > 0 {
		var names []string
		max := min(len(status.Players.List), 10)
		for _, player := range status.Players.List[:max] {
			names = append(names, player.Name)
		}
		b.WriteString("玩家列表: " + strings.Join(names, ", "))
		if len(status.Players.List) > max {
			b.WriteString(fmt.Sprintf(" 等 %d 人", len(status.Players.List)))
		}
	}
	return b.String()
}
