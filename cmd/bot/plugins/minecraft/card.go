package minecraft

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const cardWidthMC = 600

// maxPlayerBadges 卡片上最多展示的玩家数量（超出以 "+N" 汇总）。
const maxPlayerBadges = 10

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

	// ── MOTD 彩色横幅（两行：主 MOTD + 次级 MOTD）───────────────
	bannerRendered := false
	if len(status.MOTD) > 0 {
		motdImg, err := renderMotdImage(status.MOTD, cardWidthMC-48, 17)
		if err == nil {
			canvas.AddImage(motdImg, textimage.WithImgAlign(textimage.AlignCenter))
			bannerRendered = true
		}
	}
	if len(status.SubMOTD) > 0 {
		if subImg, err := renderMotdImage(status.SubMOTD, cardWidthMC-48, 15); err == nil {
			if bannerRendered {
				canvas.AddSpacer(4)
			}
			canvas.AddImage(subImg, textimage.WithImgAlign(textimage.AlignCenter))
			bannerRendered = true
		}
	}
	if bannerRendered {
		canvas.AddSpacer(10)
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
	if versionText := displayVersion(status); versionText != "" {
		addKVRow(canvas, "版本", 13, versionText, mcTextPrimary, 13)
	}
	if softwareText := displaySoftware(status); softwareText != "" {
		addKVRow(canvas, "服务端", 12, softwareText, mcTextDim, 12)
	}
	if status.Protocol > 0 && displayVersion(status) == status.Version {
		addKVRow(canvas, "协议", 12, fmt.Sprintf("%d", status.Protocol), mcTextDim, 12)
	}
	if status.GameMode != "" {
		addKVRow(canvas, "模式", 12, truncateRunes(status.GameMode, 24), mcTextDim, 12)
	}
	if status.Map != "" {
		addKVRow(canvas, "地图", 12, truncateRunes(status.Map, 40), mcTextDim, 12)
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

		limit := min(len(status.Players.List), maxPlayerBadges)
		shown := status.Players.List[:limit]

		if hasPlayerHeads(shown) {
			// 头像网格：两列（头像 + 名字）
			for i := 0; i < len(shown); i += 2 {
				items := []textimage.RowItem{headCell(shown[i].Head), nameCell(shown[i].Name)}
				if i+1 < len(shown) {
					items = append(items, headCell(shown[i+1].Head), nameCell(shown[i+1].Name))
				} else {
					items = append(items, textimage.RowItem{}, textimage.RowItem{})
				}
				canvas.AddRow(items...)
				canvas.AddSpacer(4)
			}
		} else {
			var badges []textimage.BadgeItem
			for _, player := range shown {
				badges = append(badges, textimage.BadgeItem{
					// MC 玩家名上限 16 字符；截断异常数据防止 badge 过宽
					Text:      truncateRunes(player.Name, 16),
					BgColor:   mcBgCard,
					TextColor: mcTextPrimary,
				})
			}
			if len(status.Players.List) > limit {
				badges = append(badges, textimage.BadgeItem{
					Text:      fmt.Sprintf("+%d", len(status.Players.List)-limit),
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
	if status.Error != "" {
		canvas.AddSpacer(4)
		canvas.AddText("原因: "+truncateRunes(status.Error, 80),
			textimage.WithFontSize(11),
			textimage.WithFontColor(mcTextFaint),
			textimage.WithAlign(textimage.AlignCenter),
		)
	}
	canvas.AddSpacer(6)
	canvas.AddText("请检查主机名、端口与服务器运行状态",
		textimage.WithFontSize(12),
		textimage.WithFontColor(mcTextFaint),
		textimage.WithAlign(textimage.AlignCenter),
	)

	return canvas.ResultPNG()
}

// motdFont MOTD 渲染字体缓存：字体文件读取与解析只在首次成功时发生，
// 避免每次查询重复读盘 + 解析（CJK 字体可达数十 MB）。
// *opentype.Font 为只读对象，可安全并发复用；font.Face 需每次调用新建。
//
// 这里用互斥锁而非 sync.Once：sync.Once 会把"首次解析失败"也永久固化，
// 之后即使字体文件被补上也会一直走文本回退。
var (
	motdFontMu sync.Mutex
	motdFont   *opentype.Font
)

func motdFontForRender() (*opentype.Font, error) {
	motdFontMu.Lock()
	defer motdFontMu.Unlock()
	if motdFont != nil {
		return motdFont, nil
	}

	fontPath := textimage.SystemCJKFontPath()
	var raw []byte
	if fontPath != "" {
		if data, err := os.ReadFile(fontPath); err == nil {
			raw = data
		}
	}
	if len(raw) == 0 {
		// 无系统 CJK 字体（如精简 CI 环境）：回退内置 Go Regular 字体，
		// 保证 ASCII MOTD（多数服务器名）仍可渲染。
		raw = textimage.DefaultFontTTF()
	}

	var parsed *opentype.Font
	if isTTCBytes(raw) {
		col, err := opentype.ParseCollection(raw)
		if err != nil {
			return nil, fmt.Errorf("解析 MOTD 字体集合失败: %w", err)
		}
		parsed, err = col.Font(0)
		if err != nil {
			return nil, fmt.Errorf("提取 MOTD 字体失败: %w", err)
		}
	} else {
		f, perr := opentype.Parse(raw)
		if perr != nil {
			return nil, fmt.Errorf("解析 MOTD 字体失败: %w", perr)
		}
		parsed = f
	}
	if parsed == nil {
		return nil, errors.New("解析 MOTD 字体失败")
	}
	// 仅在成功时缓存：失败（字体缺失/损坏）留待下次重试
	motdFont = parsed
	return motdFont, nil
}

func renderMotdImage(segments []MotdSegment, maxWidth int, fontSize float64) (image.Image, error) {
	parsed, err := motdFontForRender()
	if err != nil {
		return nil, err
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
		if seg.Bold {
			// 伪加粗：向右偏移 1px 重绘一次
			b := &font.Drawer{
				Dst:  img,
				Src:  image.NewUniform(seg.Color),
				Face: face,
				Dot:  fixed.P(x.Round()+1, ascent+padTop),
			}
			b.DrawString(text)
		}
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

// displayVersion 版本展示文本：Version 优先；缺失时用协议号近似映射兜底。
func displayVersion(status *MCServerStatus) string {
	if status.Version != "" {
		return status.Version
	}
	if status.Protocol > 0 {
		return fmt.Sprintf("≈%s（协议 %d）", protocolVersionName(status.Protocol), status.Protocol)
	}
	return ""
}

// displaySoftware 服务端/插件信息展示文本（GS4 plugins 字段或版本名推断，可为空）。
//
// 早期版本从 version.name 推断出的品牌常常与"版本"行重复（版本行已经是
// "Paper 1.21.1"，服务端行再显示一次 "Paper" 属于噪声），因此当版本名已包含
// 该品牌且没有插件数量可展示时返回空串。
func displaySoftware(status *MCServerStatus) string {
	if status.Software == "" {
		return ""
	}
	text := status.Software
	if status.PluginCount > 0 {
		text += fmt.Sprintf("（%d 个插件）", status.PluginCount)
	}
	if status.PluginCount <= 0 && strings.Contains(status.Version, status.Software) {
		return ""
	}
	return truncateRunes(text, 48)
}

// compareEntry 多服务器对比中的一项（Status 与 Err 二选一）。
type compareEntry struct {
	Label  string
	Status *MCServerStatus
	Err    error
}

// renderMCCompareCard 渲染多服务器对比卡片（单图纵向排列）。
func renderMCCompareCard(entries []compareEntry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, errors.New("没有可对比的服务器")
	}

	height := 200 + 96*len(entries)
	bg := textimage.LinearGradient(cardWidthMC, height, 160,
		textimage.Stop(0.0, color.RGBA{R: 24, G: 32, B: 38, A: 255}),
		textimage.Stop(0.55, mcBgDark),
		textimage.Stop(1.0, color.RGBA{R: 22, G: 24, B: 30, A: 255}),
	)

	canvas, err := textimage.NewCanvas(cardWidthMC,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(mcTextPrimary),
		textimage.WithLineHeight(1.5),
		textimage.WithPadding(24, 18),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddText(fmt.Sprintf("⛏ 服务器对比（%d）", len(entries)),
		textimage.WithFontSize(19),
		textimage.WithFontColor(mcTextPrimary),
	)
	canvas.AddSpacer(10)
	canvas.AddDivider(textimage.WithDividerColor(mcDivider))
	canvas.AddSpacer(12)

	for _, entry := range entries {
		addCompareEntry(canvas, entry)
		canvas.AddSpacer(8)
	}

	canvas.AddDivider(textimage.WithDividerColor(mcDivider))
	canvas.AddSpacer(8)
	canvas.AddText("查询于 "+time.Now().Format("2006-01-02 15:04:05"),
		textimage.WithFontSize(11),
		textimage.WithFontColor(mcTextFaint),
		textimage.WithAlign(textimage.AlignCenter),
	)

	return canvas.ResultPNG()
}

// addCompareEntry 追加一个对比条目：首行为「地址 + 玩家数」，次行为细节。
func addCompareEntry(canvas *textimage.Canvas, entry compareEntry) {
	if entry.Status == nil || !entry.Status.Online {
		reason := "无法连接"
		if entry.Err != nil {
			reason = truncateRunes(entry.Err.Error(), 34)
		}
		addKVRow(canvas, truncateRunes(entry.Label, 34), 14, "● 离线  "+reason, mcAccentRed, 12)
		return
	}
	st := entry.Status
	addKVRow(canvas, truncateRunes(entry.Label, 34), 14,
		fmt.Sprintf("%d / %d 人", st.Players.Online, st.Players.Max), mcAccentGreen, 13)
	if detail := compareDetail(st); detail != "" {
		canvas.AddText("  "+detail,
			textimage.WithFontSize(11),
			textimage.WithFontColor(mcTextFaint),
		)
	}
}

// compareDetail 对比条目的次要信息：版本 · 版本类型 · 延迟 · MOTD 摘要。
func compareDetail(st *MCServerStatus) string {
	parts := make([]string, 0, 4)
	if v := displayVersion(st); v != "" {
		parts = append(parts, v)
	}
	if st.Edition != "" {
		parts = append(parts, st.Edition)
	}
	if st.Latency > 0 {
		parts = append(parts, fmt.Sprintf("%dms", st.Latency.Milliseconds()))
	}
	if motd := truncateRunes(strings.ReplaceAll(st.MOTDPlain, "\n", " / "), 30); motd != "" {
		parts = append(parts, motd)
	}
	return strings.Join(parts, "  ·  ")
}

// formatMCCompareText 多服务器对比的纯文本形式（图片渲染失败时的备用方案）。
func formatMCCompareText(entries []compareEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⛏ 服务器对比（%d）\n", len(entries))
	for _, entry := range entries {
		if entry.Status != nil && entry.Status.Online {
			fmt.Fprintf(&b, "  %s — %d/%d 人  %s\n", entry.Label,
				entry.Status.Players.Online, entry.Status.Players.Max, compareDetail(entry.Status))
			continue
		}
		reason := "无法连接"
		if entry.Err != nil {
			reason = entry.Err.Error()
		}
		fmt.Fprintf(&b, "  %s — 离线（%s）\n", entry.Label, reason)
	}
	return b.String()
}

// addKVRow 追加一行"标签（左，暗色）+ 值（右）"。
func addKVRow(canvas *textimage.Canvas, label string, labelSize float64, value string, valueColor color.Color, valueSize float64) {
	canvas.AddRow(
		textimage.RowItem{
			Text: label,
			TextOpts: []textimage.Option{
				textimage.WithFontSize(labelSize),
				textimage.WithFontColor(mcTextFaint),
			},
		},
		textimage.RowItem{
			Text: value,
			TextOpts: []textimage.Option{
				textimage.WithFontSize(valueSize),
				textimage.WithFontColor(valueColor),
				textimage.WithAlign(textimage.AlignRight),
			},
		},
	)
}

// hasPlayerHeads 判断玩家列表中是否已有头像数据。
func hasPlayerHeads(players []PlayerInfo) bool {
	for _, p := range players {
		if len(p.Head) > 0 {
			return true
		}
	}
	return false
}

// headCell 玩家头像单元格（Head 为空时退化为空白占位）。
func headCell(head []byte) textimage.RowItem {
	if len(head) == 0 {
		return textimage.RowItem{}
	}
	if img, _, err := image.Decode(bytes.NewReader(head)); err == nil {
		return textimage.RowItem{
			Width: 34,
			Image: img,
			ImageOpts: []textimage.ImageOption{
				textimage.WithImgWidth(26),
				textimage.WithImgHeight(26),
				textimage.WithImgRoundRadius(5),
			},
		}
	}
	return textimage.RowItem{}
}

// nameCell 玩家名字单元格。
func nameCell(name string) textimage.RowItem {
	return textimage.RowItem{
		Text: truncateRunes(name, 16),
		TextOpts: []textimage.Option{
			textimage.WithFontSize(13),
			textimage.WithFontColor(mcTextPrimary),
		},
	}
}

// isTTCBytes 判断字体文件是否为 TrueType Collection 格式。
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
	if status.SubMOTDPlain != "" {
		b.WriteString(fmt.Sprintf("次级 MOTD: %s\n", status.SubMOTDPlain))
	}
	if versionText := displayVersion(status); versionText != "" {
		b.WriteString(fmt.Sprintf("版本: %s\n", versionText))
	}
	if softwareText := displaySoftware(status); softwareText != "" {
		b.WriteString(fmt.Sprintf("服务端: %s\n", softwareText))
	}
	b.WriteString(fmt.Sprintf("玩家: %d / %d\n", status.Players.Online, status.Players.Max))
	if status.GameMode != "" {
		b.WriteString(fmt.Sprintf("模式: %s\n", status.GameMode))
	}
	if status.Map != "" {
		b.WriteString(fmt.Sprintf("地图: %s\n", status.Map))
	}
	if len(status.Players.List) > 0 {
		var names []string
		limit := min(len(status.Players.List), maxPlayerBadges)
		for _, player := range status.Players.List[:limit] {
			names = append(names, player.Name)
		}
		b.WriteString("玩家列表: " + strings.Join(names, ", "))
		if len(status.Players.List) > limit {
			b.WriteString(fmt.Sprintf(" 等 %d 人", len(status.Players.List)))
		}
	}
	if status.EnforcesSecureChat {
		b.WriteString("\n安全聊天: 强制签名")
	} else if status.PreviewsChat {
		b.WriteString("\n安全聊天: 开启预览")
	}
	return b.String()
}
