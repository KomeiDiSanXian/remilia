package dealornodeal

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// 图片只绘制 ASCII（数字、货币符号与拉丁标签），所以不依赖系统 CJK 字体，
// 跨平台一致；中文说明随消息文本发送。

var (
	colBg        = color.RGBA{R: 18, G: 18, B: 19, A: 255}
	colPanel     = color.RGBA{R: 28, G: 28, B: 31, A: 255}
	colTile      = color.RGBA{R: 40, G: 40, B: 45, A: 255}
	colTileBorde = color.RGBA{R: 86, G: 87, B: 88, A: 255}
	colOpened    = color.RGBA{R: 52, G: 32, B: 38, A: 255}
	colOpenedVal = color.RGBA{R: 176, G: 106, B: 114, A: 255}
	colOwn       = color.RGBA{R: 201, G: 180, B: 88, A: 255}
	colOwnText   = color.RGBA{R: 26, G: 22, B: 6, A: 255}
	colText      = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	colMuted     = color.RGBA{R: 150, G: 152, B: 158, A: 255}
	colStruck    = color.RGBA{R: 104, G: 106, B: 112, A: 255}
	colBanner    = color.RGBA{R: 31, G: 58, B: 95, A: 255}
	colBannerDl  = color.RGBA{R: 46, G: 92, B: 62, A: 255}
	colBannerWin = color.RGBA{R: 92, G: 72, B: 20, A: 255}
	colBannerBrd = color.RGBA{R: 201, G: 180, B: 88, A: 255}
	colBorder    = color.RGBA{R: 58, G: 58, B: 62, A: 255}
)

var (
	fontOnce sync.Once
	fontBase *opentype.Font
	fontErr  error
)

func baseFont() (*opentype.Font, error) {
	fontOnce.Do(func() {
		fontBase, fontErr = opentype.Parse(textimage.DefaultFontTTF())
	})
	return fontBase, fontErr
}

func newFace(size float64) (font.Face, error) {
	f, err := baseFont()
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
}

// stageView 是渲染一张阶段图片所需的全部数据（纯 ASCII 标签）。
type stageView struct {
	Phase Phase
	// Round / Rounds 是 1 起的当前轮次与总轮次。
	Round  int
	Rounds int

	Values []int64
	Opened []bool
	// OwnCase 是 0 起的自己箱子下标；-1 表示未选。
	OwnCase int

	Offer      int64
	OfferReady bool

	Remaining       int
	ToOpenThisRound int

	Won     int64
	Dealt   bool
	Swapped bool
	// Currency 是金额前缀，默认 "$"。
	Currency string
}

// renderStage 把对局渲染为 PNG 图片：顶部阶段横幅，左侧箱子网格，右侧奖金面板。
func renderStage(v stageView) ([]byte, error) {
	cases := len(v.Values)
	if cases == 0 || len(v.Opened) != cases {
		return nil, errors.New("dealornodeal: 无效对局数据")
	}
	cur := v.Currency
	if cur == "" {
		cur = "$"
	}

	const (
		pad       = 24.0
		colGap    = 24.0
		tileW     = 64.0
		tileH     = 52.0
		tileGap   = 8.0
		boardRowH = 20.0
		boardColW = 152.0
		boardGap  = 12.0
		headerH   = 76.0
		bannerH   = 62.0
		footerH   = 30.0
	)

	// 箱子网格列数按箱数自适应（4-8 列），保证网格接近方形且行数不过多。
	cols := min(max(int(math.Round(math.Sqrt(float64(cases)*2))), 4), 8)
	rows := (cases + cols - 1) / cols
	gridW := float64(cols)*tileW + float64(cols-1)*tileGap
	gridH := float64(rows)*tileH + float64(rows-1)*tileGap

	boardCols := 2
	boardRows := (cases + boardCols - 1) / boardCols
	boardW := float64(boardCols)*boardColW + float64(boardCols-1)*boardGap
	boardH := float64(boardRows) * boardRowH

	bodyH := math.Max(gridH, boardH)
	width := pad*2 + gridW + colGap + boardW
	height := pad*2 + headerH + bannerH + 16 + bodyH + footerH

	dc := textimage.NewVectorCanvas(int(width), int(height))
	dc.SetColor(colBg)
	dc.Clear()

	faces := map[float64]font.Face{}
	faceFor := func(size float64) font.Face {
		if f, ok := faces[size]; ok {
			return f
		}
		f, err := newFace(size)
		if err != nil {
			return nil
		}
		faces[size] = f
		return f
	}
	draw := func(text string, size, x, y, ax, ay float64, col color.Color) {
		f := faceFor(size)
		if f == nil {
			return
		}
		dc.SetFontFace(f)
		dc.SetColor(col)
		dc.DrawStringAnchored(text, x, y, ax, ay)
	}

	// 标题与副标题。
	draw("DEAL OR NO DEAL", 26, width/2, pad+20, 0.5, 0.5, colText)
	sub := fmt.Sprintf("%d CASES  -  %s", cases, phaseLabel(v))
	if v.Rounds > 0 && (v.Phase == PhaseOpening || v.Phase == PhaseOffer) {
		sub = fmt.Sprintf("ROUND %d/%d  -  %s", v.Round, v.Rounds, phaseLabel(v))
	}
	draw(sub, 14, width/2, pad+52, 0.5, 0.5, colMuted)

	// 阶段横幅。
	bannerTop := pad + headerH
	bannerCol := colBanner
	if v.Phase == PhaseFinished {
		if v.Dealt {
			bannerCol = colBannerDl
		} else {
			bannerCol = colBannerWin
		}
	}
	dc.SetColor(bannerCol)
	dc.DrawRoundedRectangle(pad, bannerTop, width-pad*2, bannerH, 12)
	dc.Fill()
	dc.SetLineWidth(2)
	dc.SetColor(colBannerBrd)
	dc.DrawRoundedRectangle(pad, bannerTop, width-pad*2, bannerH, 12)
	dc.Stroke()
	draw(bannerText(v, cur), 20, width/2, bannerTop+bannerH/2, 0.5, 0.5, colText)

	// 左侧箱子网格。
	bodyTop := bannerTop + bannerH + 16
	elim := map[int64]int{}
	for i, opened := range v.Opened {
		if opened {
			elim[v.Values[i]]++
		}
	}
	for i := range cases {
		r, c := i/cols, i%cols
		x := pad + float64(c)*(tileW+tileGap)
		y := bodyTop + float64(r)*(tileH+tileGap)
		opened := v.Opened[i]
		isOwn := i == v.OwnCase

		fill := colTile
		border := colTileBorde
		switch {
		case isOwn:
			fill, border = colOwn, colOwn
		case opened:
			fill, border = colOpened, colBorder
		}
		dc.SetColor(fill)
		dc.DrawRoundedRectangle(x, y, tileW, tileH, 8)
		dc.Fill()
		dc.SetLineWidth(2)
		dc.SetColor(border)
		dc.DrawRoundedRectangle(x, y, tileW, tileH, 8)
		dc.Stroke()

		if opened && !isOwn {
			draw(shortMoney(v.Values[i], cur), 12, x+tileW/2, y+tileH/2, 0.5, 0.5, colOpenedVal)
			continue
		}
		numCol := colText
		if isOwn {
			numCol = colOwnText
		}
		draw(strconv.Itoa(i+1), 22, x+tileW/2, y+tileH/2-6, 0.5, 0.5, numCol)
		if isOwn {
			draw("YOU", 10, x+tileW/2, y+tileH-12, 0.5, 0.5, colOwnText)
		}
	}

	// 右侧奖金面板：已排除金额置灰并加删除线。
	boardX := pad + gridW + colGap
	dc.SetColor(colPanel)
	dc.DrawRoundedRectangle(boardX-10, bodyTop-10, boardW+20, bodyH+20, 10)
	dc.Fill()
	sorted := make([]int64, cases)
	copy(sorted, v.Values)
	slices.Sort(sorted)
	left := map[int64]int{}
	maps.Copy(left, elim)
	for i, val := range sorted {
		r, c := i%boardRows, i/boardRows
		if c >= boardCols {
			break
		}
		x := boardX + float64(c)*(boardColW+boardGap)
		y := bodyTop + float64(r)*boardRowH
		col := colText
		struck := false
		if left[val] > 0 {
			left[val]--
			col = colStruck
			struck = true
		}
		text := money(val, cur)
		draw(text, 14, x, y+boardRowH/2, 0, 0.5, col)
		if struck {
			txtW := float64(len(text)) * 7.2
			dc.SetLineWidth(1.6)
			dc.SetColor(colStruck)
			dc.DrawLine(x, y+boardRowH/2, x+txtW, y+boardRowH/2)
			dc.Stroke()
		}
	}

	// 底部状态行。
	footer := fmt.Sprintf("REMAINING %d", v.Remaining)
	if v.Phase == PhaseFinished {
		footer = fmt.Sprintf("YOU WON %s", money(v.Won, cur))
	} else if v.OfferReady {
		footer = fmt.Sprintf("OFFER %s  -  %d CASES LEFT", money(v.Offer, cur), v.Remaining)
	}
	draw(footer, 13, width/2, height-pad+2, 0.5, 0.5, colMuted)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderStageText 是图片渲染失败时的纯文本降级。
func renderStageText(v stageView) string {
	cur := v.Currency
	if cur == "" {
		cur = "$"
	}
	var b strings.Builder
	b.WriteString(bannerText(v, cur))
	b.WriteString("\n")
	for i, val := range v.Values {
		if i > 0 {
			b.WriteString("  ")
		}
		switch {
		case i == v.OwnCase:
			fmt.Fprintf(&b, "[%d:YOU]", i+1)
		case v.Opened[i]:
			fmt.Fprintf(&b, "[%d:%s]", i+1, shortMoney(val, cur))
		default:
			fmt.Fprintf(&b, "[%d]", i+1)
		}
		if (i+1)%7 == 0 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// phaseLabel 返回阶段（ASCII）标签。
func phaseLabel(v stageView) string {
	switch v.Phase {
	case PhasePick:
		return "PICK YOUR CASE"
	case PhaseOpening:
		return fmt.Sprintf("OPEN %d MORE", v.ToOpenThisRound)
	case PhaseOffer:
		if v.OfferReady {
			return "BANKER OFFER"
		}
		return "OPENING"
	case PhaseFinal:
		return "SWAP OR KEEP"
	default:
		return "GAME OVER"
	}
}

// bannerText 返回横幅文案（ASCII）。
func bannerText(v stageView, cur string) string {
	switch v.Phase {
	case PhasePick:
		return fmt.Sprintf("PICK YOUR CASE  (1-%d)", len(v.Values))
	case PhaseOpening:
		return fmt.Sprintf("OPEN %d CASE(S) THIS ROUND", v.ToOpenThisRound)
	case PhaseOffer:
		if v.OfferReady {
			return fmt.Sprintf("BANKER OFFER  %s      DEAL  /  NO DEAL", money(v.Offer, cur))
		}
		return "OPENING"
	case PhaseFinal:
		return "TWO CASES LEFT      SWAP  /  KEEP"
	default:
		if v.Dealt {
			return fmt.Sprintf("DEAL!  YOU TAKE %s", money(v.Won, cur))
		}
		return fmt.Sprintf("YOUR CASE HELD %s", money(v.Won, cur))
	}
}

// money 格式化金额，带千位分隔符。
func money(v int64, cur string) string {
	return cur + groupDigits(v)
}

// shortMoney 是箱子内金额的紧凑写法（≥百万用 M，≥千用 K）。
func shortMoney(v int64, cur string) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("%s%gM", cur, float64(v)/1_000_000)
	case v >= 10_000:
		return fmt.Sprintf("%s%gK", cur, float64(v)/1_000)
	default:
		return cur + groupDigits(v)
	}
}

// groupDigits 在整数中插入千位分隔符。
func groupDigits(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, ch := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, ch)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
