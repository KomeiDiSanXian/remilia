package fortune

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// cardJPEGQuality 卡片图的 JPEG 质量。
//
// 卡片由牌面插图/签纸扫描加上文字叠加而成，属于照片类内容，PNG 并不划算：
// 同为 q90 时 JPEG 体积约为 PNG 的 1/4（三张牌阵 1.2 MB → 274 KB），
// 而文字边缘与插图细节肉眼无差别。
const cardJPEGQuality = 90

// tarotCardWidth 塔罗牌阵中每一列的宽度（牌面按 320 宽居中）。
const tarotCardWidth = 400

// tarotSpreadGap 牌阵中各列之间的间隔。
const tarotSpreadGap = 12

// omikujiPageGap 御神签两页扫描之间的间隔。
const omikujiPageGap = 12

var (
	paperBg     = color.RGBA{R: 252, G: 250, B: 245, A: 255} // 签纸底色，用于补齐两页高度差
	tarotCardBg = color.RGBA{R: 50, G: 40, B: 60, A: 255}    // 塔罗卡片底色
	white       = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

// joinHorizontal 把若干张图片横向拼接为一张图，各列顶部对齐，间隔处填底色。
//
// 传入的 nil 图片会被跳过；全部为空时返回错误，由调用方决定如何降级。
func joinHorizontal(bg color.RGBA, gap int, imgs ...image.Image) (image.Image, error) {
	valid := make([]image.Image, 0, len(imgs))
	for _, img := range imgs {
		if img != nil {
			valid = append(valid, img)
		}
	}
	if len(valid) == 0 {
		return nil, errors.New("fortune: 没有可用的图片")
	}

	width, height := 0, 0
	for i, img := range valid {
		bounds := img.Bounds()
		width += bounds.Dx()
		if i > 0 {
			width += gap
		}
		height = max(height, bounds.Dy())
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	x := 0
	for _, img := range valid {
		bounds := img.Bounds()
		draw.Draw(canvas, image.Rect(x, 0, x+bounds.Dx(), bounds.Dy()), img, bounds.Min, draw.Src)
		x += bounds.Dx() + gap
	}
	return canvas, nil
}

// encodeCard 把卡片图编码为 JPEG。
func encodeCard(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: cardJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderOmikujiCard 把御神签的两页扫描（签文页与解签页）并排合成为一张图片。
//
// 番号、吉凶、漢詩与解签都由签纸本身承载，因此不再叠加任何生成文本。
// nil 页面会被跳过；两页都缺失时返回错误，由调用方降级处理。
func renderOmikujiCard(pages ...image.Image) ([]byte, error) {
	canvas, err := joinHorizontal(paperBg, omikujiPageGap, pages...)
	if err != nil {
		return nil, errors.New("fortune: 御神签扫描不可用")
	}
	return encodeCard(canvas)
}

// tarotColumn 描述牌阵中的一列，即一张牌。
type tarotColumn struct {
	Reading  TarotReading // 牌与正逆位
	Face     image.Image  // 牌面图，nil 时该列只渲染文字
	Position string       // 位置名，如「过去」；单张牌阵留空
}

// renderTarotColumn 渲染牌阵中的一列。位置名非空时显示在牌面上方。
func renderTarotColumn(col tarotColumn) (image.Image, error) {
	canvas, err := textimage.NewCanvas(tarotCardWidth,
		textimage.WithCJKFont(),
		textimage.WithFontColor(white),
		textimage.WithLineHeight(1.6),
		textimage.WithPadding(16, 12),
		textimage.WithBgColor(tarotCardBg),
	)
	if err != nil {
		return nil, err
	}

	if col.Position != "" {
		canvas.AddText(col.Position,
			textimage.WithFontSize(15),
			textimage.WithAlign(textimage.AlignCenter),
			textimage.WithFontColor(color.RGBA{R: 235, G: 205, B: 140, A: 255}),
		)
		canvas.AddSpacer(6)
	}

	if col.Face != nil {
		face := col.Face
		if col.Reading.IsReverse {
			face = rotate180(face)
		}
		canvas.AddImage(face,
			textimage.WithImgMaxWidth(320),
			textimage.WithImgAlign(textimage.AlignCenter),
			textimage.WithImgPadding(0, 8),
		)
	}

	canvas.AddSpacer(8)

	orientationColor := color.RGBA{R: 200, G: 200, B: 80, A: 255}
	if col.Reading.IsReverse {
		orientationColor = color.RGBA{R: 180, G: 120, B: 120, A: 255}
	}
	canvas.AddText(fmt.Sprintf("%s  (%s)", col.Reading.Card.NameCN, col.Reading.Orientation()),
		textimage.WithFontSize(16),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(orientationColor),
		textimage.WithTextShadow(color.RGBA{R: 0, G: 0, B: 0, A: 100}, 1, 1, 3),
	)

	canvas.AddSpacer(6)

	canvas.AddText(col.Reading.Card.NameEN,
		textimage.WithFontSize(12),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(color.RGBA{R: 200, G: 180, B: 220, A: 200}),
	)

	canvas.AddSpacer(8)

	canvas.AddText(col.Reading.Meaning(),
		textimage.WithFontSize(14),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(white),
		textimage.WithPadding(16, 4),
	)

	canvas.AddSpacer(4)

	canvas.AddText(col.Reading.Card.Suit.String(),
		textimage.WithFontSize(11),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(color.RGBA{R: 180, G: 160, B: 200, A: 180}),
	)

	return canvas.Result(), nil
}

// renderTarotSpread 把牌阵的每一列并排合成为一张图片，整副牌阵只发一条消息。
// 任一列渲染失败即返回错误，由调用方降级为纯文字。
func renderTarotSpread(cols []tarotColumn) ([]byte, error) {
	imgs := make([]image.Image, 0, len(cols))
	for _, col := range cols {
		img, err := renderTarotColumn(col)
		if err != nil {
			return nil, err
		}
		imgs = append(imgs, img)
	}

	canvas, err := joinHorizontal(tarotCardBg, tarotSpreadGap, imgs...)
	if err != nil {
		return nil, errors.New("fortune: 塔罗牌阵不可用")
	}
	return encodeCard(canvas)
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
