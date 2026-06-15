package fortune

import (
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

const cardWidth = 800

var (
	red    = color.RGBA{R: 200, G: 40, B: 40, A: 255}
	gold   = color.RGBA{R: 210, G: 170, B: 50, A: 255}
	white  = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black  = color.RGBA{R: 40, G: 30, B: 20, A: 255}
	darkBg = color.RGBA{R: 140, G: 20, B: 30, A: 255}
	greyBg = color.RGBA{R: 80, G: 80, B: 85, A: 255}
)

// levelColor 返回运势等级对应的文字颜色。
func levelColor(level FortuneLevel) color.Color {
	switch level {
	case Daikichi:
		return gold
	case Kichi:
		return color.RGBA{R: 220, G: 60, B: 50, A: 255}
	case Chukichi:
		return color.RGBA{R: 240, G: 140, B: 40, A: 255}
	case Shokichi:
		return color.RGBA{R: 60, G: 180, B: 80, A: 255}
	case Matsukichi:
		return color.RGBA{R: 60, G: 120, B: 200, A: 255}
	case Kyo:
		return color.RGBA{R: 130, G: 130, B: 140, A: 255}
	case Daikyo:
		return black
	default:
		return white
	}
}

// levelBgColor 返回运势等级对应的卡片背景色。
func levelBgColor(level FortuneLevel) color.Color {
	switch level {
	case Daikichi:
		return color.RGBA{R: 180, G: 30, B: 40, A: 255}
	case Kyo, Daikyo:
		return color.RGBA{R: 60, G: 55, B: 55, A: 255}
	default:
		return color.RGBA{R: 160, G: 40, B: 50, A: 255}
	}
}

// renderOmikujiCard 渲染御神签图片卡片。
// bgImg 为浅草寺签文背景图，为 nil 时只使用纯色背景。
func renderOmikujiCard(slip *OmikujiSlip, bgImg image.Image) ([]byte, error) {
	canvas, err := textimage.NewCanvas(cardWidth,
		textimage.WithCJKFont(),
		textimage.WithFontColor(white),
		textimage.WithLineHeight(1.7),
		textimage.WithPadding(32, 16),
		textimage.WithBgColor(levelBgColor(slip.Level)),
	)
	if err != nil {
		return nil, err
	}

	if bgImg != nil {
		canvas.AddImage(bgImg,
			textimage.WithImgWidth(cardWidth),
			textimage.WithImgAlign(textimage.AlignCenter),
		)
		canvas.AddSpacer(8)
	}

	lvl := slip.Level.String()
	lc := levelColor(slip.Level)

	canvas.AddText("✦ 御神签 ✦",
		textimage.WithFontSize(22),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(gold),
		textimage.WithTextShadow(color.RGBA{R: 0, G: 0, B: 0, A: 120}, 1, 2, 4),
		textimage.WithPadding(20, 4),
	)

	canvas.AddText(fmt.Sprintf("第 %d 番", slip.Number),
		textimage.WithFontSize(14),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(color.RGBA{R: 255, G: 220, B: 180, A: 220}),
		textimage.WithPadding(8, 2),
	)

	canvas.AddSpacer(12)

	canvas.AddText(lvl,
		textimage.WithFontSize(48),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(lc),
		textimage.WithTextShadow(color.RGBA{R: 0, G: 0, B: 0, A: 100}, 2, 3, 8),
		textimage.WithPadding(32, 8),
		textimage.WithTextBackdrop(color.RGBA{R: 0, G: 0, B: 0, A: 80}, 4),
		textimage.WithTextBackdropPadding(24, 12),
	)

	canvas.AddSpacer(16)

	canvas.AddText(slip.Translation,
		textimage.WithFontSize(16),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(white),
		textimage.WithTextShadow(color.RGBA{R: 0, G: 0, B: 0, A: 100}, 1, 1, 3),
		textimage.WithPadding(28, 4),
	)

	canvas.AddSpacer(20)

	canvas.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 255, G: 200, B: 150, A: 100}),
	)

	canvas.AddSpacer(8)

	attrs := slip.LuckyAttrs()
	info := fmt.Sprintf("幸运方向: %s    幸运色: %s    幸运数字: %d",
		attrs.Direction, attrs.Color, attrs.Number)
	canvas.AddText(info,
		textimage.WithFontSize(14),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(color.RGBA{R: 255, G: 220, B: 180, A: 220}),
		textimage.WithPadding(20, 4),
	)

	canvas.AddSpacer(4)

	badgeText := fmt.Sprintf("愿望: %s  |  待人: %s  |  失物: %s  |  旅: %s",
		slip.Wish, slip.Waiting, slip.LostItem, slip.Travel)
	canvas.AddText(badgeText,
		textimage.WithFontSize(12),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(color.RGBA{R: 255, G: 200, B: 160, A: 180}),
		textimage.WithPadding(12, 2),
	)

	return canvas.ResultPNG()
}

// renderTarotCard 渲染单张塔罗牌图片卡片。
// cardImg 为从 sacred-texts 缓存的牌面图片，为 nil 时只显示文字。
func renderTarotCard(reading *TarotReading, cardImg image.Image) ([]byte, error) {
	canvas, err := textimage.NewCanvas(400,
		textimage.WithCJKFont(),
		textimage.WithFontColor(white),
		textimage.WithLineHeight(1.6),
		textimage.WithPadding(16, 12),
		textimage.WithBgColor(color.RGBA{R: 50, G: 40, B: 60, A: 255}),
	)
	if err != nil {
		return nil, err
	}

	if cardImg != nil {
		canvas.AddImage(cardImg,
			textimage.WithImgMaxWidth(320),
			textimage.WithImgAlign(textimage.AlignCenter),
			textimage.WithImgPadding(0, 8),
		)
	}

	canvas.AddSpacer(8)

	title := reading.Card.NameCN
	orientation := reading.Orientation()
	orientationColor := color.RGBA{R: 200, G: 200, B: 80, A: 255}
	if reading.IsReverse {
		orientationColor = color.RGBA{R: 180, G: 120, B: 120, A: 255}
	}
	canvas.AddText(fmt.Sprintf("%s  (%s)", title, orientation),
		textimage.WithFontSize(16),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(orientationColor),
		textimage.WithTextShadow(color.RGBA{R: 0, G: 0, B: 0, A: 100}, 1, 1, 3),
	)

	canvas.AddSpacer(6)

	canvas.AddText(reading.Card.NameEN,
		textimage.WithFontSize(12),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(color.RGBA{R: 200, G: 180, B: 220, A: 200}),
	)

	canvas.AddSpacer(8)

	canvas.AddText(reading.Meaning(),
		textimage.WithFontSize(14),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(white),
		textimage.WithPadding(16, 4),
	)

	canvas.AddSpacer(4)

	canvas.AddText(reading.Card.Suit.String(),
		textimage.WithFontSize(11),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(color.RGBA{R: 180, G: 160, B: 200, A: 180}),
	)

	return canvas.ResultPNG()
}

// renderErrorCard 渲染错误提示卡片（图片渲染失败时的备用方案）。
func renderErrorCard(message string) ([]byte, error) {
	canvas, err := textimage.NewCanvas(400,
		textimage.WithCJKFont(),
		textimage.WithFontColor(white),
		textimage.WithBgColor(color.RGBA{R: 60, G: 50, B: 50, A: 255}),
		textimage.WithPadding(32, 16),
	)
	if err != nil {
		return nil, err
	}
	canvas.AddText("⚠️ 占卜失败",
		textimage.WithFontSize(20),
		textimage.WithAlign(textimage.AlignCenter),
	)
	canvas.AddSpacer(12)
	canvas.AddText(message,
		textimage.WithFontSize(14),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(color.RGBA{R: 200, G: 180, B: 180, A: 255}),
	)
	return canvas.ResultPNG()
}

// formatOmikujiText 将御神签格式化为纯文本（图片渲染失败的备用方案）。
func formatOmikujiText(slip *OmikujiSlip) string {
	attrs := slip.LuckyAttrs()
	return fmt.Sprintf("第%d番 %s\n%s\n愿望: %s | 待人: %s | 失物: %s | 旅: %s\n幸运方向: %s 幸运色: %s 幸运数字: %d",
		slip.Number, slip.Level.String(),
		slip.Translation,
		slip.Wish, slip.Waiting, slip.LostItem, slip.Travel,
		attrs.Direction, attrs.Color, attrs.Number,
	)
}

// formatTarotText 将塔罗占卜结果格式化为纯文本（图片渲染失败的备用方案）。
func formatTarotText(readings []TarotReading) string {
	var buf strings.Builder
	for i, r := range readings {
		if len(readings) > 1 {
			pos := ""
			switch i {
			case 0:
				pos = "【过去】"
			case 1:
				pos = "【现在】"
			case 2:
				pos = "【未来】"
			}
			buf.WriteString(pos + " ")
		}
		buf.WriteString(fmt.Sprintf("%s (%s)\n%s\n%s\n",
			r.Card.NameCN, r.Orientation(),
			r.Card.NameEN, r.Meaning()))
		if i < len(readings)-1 {
			buf.WriteString("\n")
		}
	}
	return buf.String()
}

// pickOmikujiVariant 随机选择浅草寺签图的变体（0 或 1）。
func pickOmikujiVariant() int {
	return rand.Intn(2)
}

// sensojiImageURL 返回指定番号和变体的浅草寺签图原始 URL。
func sensojiImageURL(number, variant int) string {
	return fmt.Sprintf("https://raw.githubusercontent.com/fumiama/senso-ji-omikuji/main/%d_%d.jpg", number, variant)
}

// sensojiCacheKey 返回浅草寺签图的缓存键名。
func sensojiCacheKey(number, variant int) string {
	return fmt.Sprintf("sensoji_%d_%d.jpg", number, variant)
}
