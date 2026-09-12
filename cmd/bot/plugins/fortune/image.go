package fortune

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// tarotCardWidth 塔罗卡片宽度（牌面按 320 宽居中）。
const tarotCardWidth = 400

// omikujiPageGap 御神签两页扫描之间的间隔。
const omikujiPageGap = 12

var (
	paperBg = color.RGBA{R: 252, G: 250, B: 245, A: 255} // 签纸底色，用于补齐两页高度差
	white   = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

// renderOmikujiCard 把御神签的两页扫描（签文页与解签页）并排合成为一张图片。
//
// 番号、吉凶、漢詩与解签都由签纸本身承载，因此不再叠加任何生成文本。
// nil 页面会被跳过；两页都缺失时返回错误，由调用方降级处理。
func renderOmikujiCard(pages ...image.Image) ([]byte, error) {
	valid := make([]image.Image, 0, len(pages))
	for _, page := range pages {
		if page != nil {
			valid = append(valid, page)
		}
	}
	if len(valid) == 0 {
		return nil, errors.New("fortune: 御神签扫描不可用")
	}

	width, height := 0, 0
	for i, page := range valid {
		bounds := page.Bounds()
		width += bounds.Dx()
		if i > 0 {
			width += omikujiPageGap
		}
		height = max(height, bounds.Dy())
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: paperBg}, image.Point{}, draw.Src)

	x := 0
	for _, page := range valid {
		bounds := page.Bounds()
		draw.Draw(canvas, image.Rect(x, 0, x+bounds.Dx(), bounds.Dy()), page, bounds.Min, draw.Src)
		x += bounds.Dx() + omikujiPageGap
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderTarotCard 渲染单张塔罗牌图片卡片。
// cardImg 为从 sacred-texts 缓存的牌面图片，为 nil 时只显示文字。
func renderTarotCard(reading *TarotReading, cardImg image.Image) ([]byte, error) {
	canvas, err := textimage.NewCanvas(tarotCardWidth,
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
		if reading.IsReverse {
			cardImg = rotate180(cardImg)
		}
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
func renderErrorCard(message string) ([]byte, error) { //nolint:unused
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
		fmt.Fprintf(&buf, "%s (%s)\n%s\n%s\n",
			r.Card.NameCN, r.Orientation(),
			r.Card.NameEN, r.Meaning())
		if i < len(readings)-1 {
			buf.WriteString("\n")
		}
	}
	return buf.String()
}

// rotate180 返回旋转 180 度后的图像，用于渲染塔罗逆位牌面。
func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dst.Set(b.Dx()-1-x, b.Dy()-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
