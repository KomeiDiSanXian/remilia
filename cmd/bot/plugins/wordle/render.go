package wordle

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// 棋盘与键盘尺寸（像素）。
const (
	tileSize   = 48.0
	tileGap    = 8.0
	padX       = 22.0
	padY       = 18.0
	kbKeyH     = 34.0
	kbKeyGap   = 6.0
	kbRowGap   = 6.0
	titleSize  = 18.0
	badgeSize  = 13.0
	letterSize = 28.0
	keySize    = 15.0
)

// 配色沿用 Wordle 经典色板。
var (
	colBg      = color.RGBA{R: 18, G: 18, B: 19, A: 255}
	colEmpty   = color.RGBA{R: 30, G: 30, B: 32, A: 255}
	colBorder  = color.RGBA{R: 86, G: 87, B: 88, A: 255}
	colCorrect = color.RGBA{R: 106, G: 170, B: 100, A: 255}
	colPresent = color.RGBA{R: 201, G: 180, B: 88, A: 255}
	colAbsent  = color.RGBA{R: 58, G: 58, B: 60, A: 255}
	colNear    = color.RGBA{R: 72, G: 176, B: 178, A: 255}  // 青：邻近命中
	colRepeat  = color.RGBA{R: 150, G: 108, B: 214, A: 255} // 紫：重复字母
	colUnknown = color.RGBA{R: 92, G: 84, B: 74, A: 255}    // 中性：未知格
	colKeyIdle = color.RGBA{R: 129, G: 131, B: 134, A: 255}
	colText    = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	colTitle   = color.RGBA{R: 200, G: 200, B: 205, A: 255}
	colBadge   = color.RGBA{R: 150, G: 152, B: 158, A: 255}
)

// boardView 渲染棋盘所需的全部数据（不含任何需要外部字体的中文，
// 因此渲染不依赖系统 CJK 字体，跨平台一致）。
type boardView struct {
	Title string
	// Badges 是规则/修饰符的 ASCII 徽标，渲染在标题下方一行。
	Badges      []string
	Length      int
	MaxAttempts int
	// RowOffset 是棋盘首行对应的猜测下标；无限机会只画最近窗口时会大于 0。
	RowOffset int
	Guesses   []Guess
	// Boards 是棋盘（谜底）数量，>=1；大于 1 时按网格并排渲染。
	Boards int
	// Solved 标记每个棋盘是否已解出，用于在棋盘标题上标注。
	Solved []bool
	// FogRows > 0 时只显示最近 FogRows 行的判定（迷雾模式）。
	FogRows int
	// Styles 是逐格显示样式（可能被颜色玩法改写）；nil 时回退到真值 Marks。
	Styles [][][]TileStyle
	// KeyStyles 是键盘每字母的显示样式（依据显示值汇总）。
	KeyStyles map[rune]TileStyle
	// HideKey 为真时不给键盘着色（--hidden-key）。
	HideKey bool
	// KeyStates 是逻辑判定汇总，仅作为 Styles/KeyStyles 缺失时的回退。
	KeyStates map[rune]Mark
}

// boardRows 返回棋盘应绘制的行数与首行对应的猜测下标。
//
// 有限机会按"可猜次数"绘制；无限机会只画"已猜 + 1"行的滑动窗口（6-12 行），
// 既避免图片高度随猜测数无限增长，又保证最新几行始终可见。
func boardRows(g *Game) (rows, offset int) {
	if !g.Unlimited {
		return max(g.MaxGuesses(), 1), 0
	}
	rows = min(max(len(g.Guesses)+1, minUnlimitedRows), maxUnlimitedRows)
	offset = max(len(g.Guesses)+1-rows, 0)
	return rows, offset
}

// gameBoardView 从对局构造渲染视图。
func gameBoardView(g *Game) boardView {
	// CHAIN 用带连击数的动态徽标，避免与静态标签重复。
	badges := make([]string, 0, 6)
	for _, label := range g.Rules.Labels() {
		if label != "CHAIN" {
			badges = append(badges, label)
		}
	}
	if g.Rules.Has(RuleChain) {
		badges = append(badges, fmt.Sprintf("CHAIN x%d", g.ChainWins))
	}
	badges = append(badges, g.Modifiers.Labels()...)
	// 棋盘按"可猜次数"绘制：COSTx2 下机会数减半，避免画出永远用不到的空行；
	// 无限机会只画最近的行窗口，标题用 ASCII 的 INF 代替次数。
	rows, offset := boardRows(g)
	title := fmt.Sprintf("WORDLE %dx%d", g.Length, rows)
	if g.Unlimited {
		title = fmt.Sprintf("WORDLE %dxINF", g.Length)
		badges = append(badges, "INF")
	}
	v := boardView{
		Title:       title,
		Badges:      badges,
		Length:      g.Length,
		MaxAttempts: rows,
		RowOffset:   offset,
		Guesses:     g.Guesses,
		Boards:      g.BoardCount(),
		Solved:      g.Solved,
		FogRows:     g.fogVisible(),
		HideKey:     g.Rules.Has(RuleHiddenKey),
	}
	// 迷雾会遮住旧行，键盘状态必须只依据仍然可见的猜测计算，否则等于没雾。
	v.KeyStates = KeyStates(visibleGuesses(v), -1)
	if styles := displayStyles(g); styles != nil {
		v.Styles = styles
		v.KeyStyles = keyStylesVisible(v, styles)
	}
	return v
}

// keyStylesVisible 只依据仍然可见的猜测汇总键盘状态，避免迷雾/衰减泄漏旧信息。
func keyStylesVisible(v boardView, styles [][][]TileStyle) map[rune]TileStyle {
	from := 0
	if v.FogRows > 0 && len(v.Guesses) > v.FogRows {
		from = len(v.Guesses) - v.FogRows
	}
	sub := &Game{Guesses: v.Guesses[from:]}
	return keyStyles(sub, styles[from:])
}

// visibleGuesses 返回迷雾模式下仍然可见的猜测（最近若干行）。
func visibleGuesses(v boardView) []Guess {
	if v.FogRows <= 0 || len(v.Guesses) <= v.FogRows {
		return v.Guesses
	}
	return v.Guesses[len(v.Guesses)-v.FogRows:]
}

var (
	fontOnce sync.Once
	fontBase *opentype.Font
	fontErr  error
)

// baseFont 惰性解析内置 Go Regular 字体。棋盘只绘制 ASCII 字母，
// 因此无需系统字体，保证在无 CJK 字体的容器里也能渲染。
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

// keyboardRows 返回标准 QWERTY 三行键盘。
func keyboardRows() []string {
	return []string{"qwertyuiop", "asdfghjkl", "zxcvbnm"}
}

// renderBoard 把对局渲染为 PNG 图片：上方 N×M 棋盘，下方带状态着色的键盘。
func renderBoard(v boardView) ([]byte, error) {
	if v.Length <= 0 || v.MaxAttempts <= 0 {
		return nil, errors.New("wordle: 无效棋盘尺寸")
	}

	boardW := padX*2 + float64(v.Length)*tileSize + float64(v.Length-1)*tileGap
	rows := keyboardRows()
	maxKeys := 0
	for _, r := range rows {
		if len(r) > maxKeys {
			maxKeys = len(r)
		}
	}
	keyW := (boardW - padX*2 - float64(maxKeys-1)*kbKeyGap) / float64(maxKeys)
	if keyW > 44 {
		keyW = 44
	}
	if keyW < 20 {
		keyW = 20
	}

	rowWidth := func(row string) float64 {
		return float64(len(row))*keyW + float64(len(row)-1)*kbKeyGap
	}
	maxKbW := 0.0
	for _, r := range rows {
		if w := rowWidth(r); w > maxKbW {
			maxKbW = w
		}
	}

	width := boardW
	if w := maxKbW + padX*2; w > width {
		width = w
	}
	badgeText := strings.Join(v.Badges, "  |  ")
	if badgeText != "" {
		// 徽标用拉丁字体渲染，按平均字宽估算所需宽度，避免右侧被裁切。
		if w := float64(len(badgeText))*badgeSize*0.58 + padX*2; w > width {
			width = w
		}
	}
	boardH := float64(v.MaxAttempts)*tileSize + float64(v.MaxAttempts-1)*tileGap
	kbH := float64(len(rows))*kbKeyH + float64(len(rows)-1)*kbRowGap
	headerH := 30.0
	if badgeText != "" {
		headerH = 50.0
	}
	// 多谜底按网格并排：1 块居中，2 块一行，3-4 块 2×2。
	boards := max(v.Boards, 1)
	cols := min(boards, 2)
	gridRows := (boards + cols - 1) / cols
	labelH := 0.0
	if boards > 1 {
		labelH = 18.0
	}
	gridGap := 16.0
	gridW := float64(cols)*boardW + float64(cols-1)*gridGap
	gridH := float64(gridRows)*(labelH+boardH) + float64(gridRows-1)*gridGap

	if w := gridW + padX*2; w > width {
		width = w
	}
	height := padY*2 + headerH + gridH + 18 + kbH

	dc := textimage.NewVectorCanvas(int(width), int(height))
	dc.SetColor(colBg)
	dc.Clear()

	titleFace, err := newFace(titleSize)
	if err != nil {
		return nil, err
	}
	letterFace, err := newFace(letterSize)
	if err != nil {
		return nil, err
	}
	keyFace, err := newFace(keySize)
	if err != nil {
		return nil, err
	}

	dc.SetFontFace(titleFace)
	dc.SetColor(colTitle)
	dc.DrawStringAnchored(v.Title, width/2, padY+12, 0.5, 0.5)
	if badgeText != "" {
		badgeFace, err := newFace(badgeSize)
		if err != nil {
			return nil, err
		}
		dc.SetFontFace(badgeFace)
		dc.SetColor(colBadge)
		dc.DrawStringAnchored(badgeText, width/2, padY+34, 0.5, 0.5)
	}

	gridTop := padY + headerH
	gridLeft := (width - gridW) / 2
	fogFrom := 0
	if v.FogRows > 0 && len(v.Guesses) > v.FogRows {
		fogFrom = len(v.Guesses) - v.FogRows
	}
	labelFace, _ := newFace(badgeSize)
	for bi := range boards {
		gr, gc := bi/cols, bi%cols
		bx := gridLeft + float64(gc)*(boardW+gridGap)
		by := gridTop + float64(gr)*(labelH+boardH+gridGap)
		if labelH > 0 && labelFace != nil {
			label := fmt.Sprintf("WORD %d", bi+1)
			if bi < len(v.Solved) && v.Solved[bi] {
				label += " SOLVED"
			}
			dc.SetFontFace(labelFace)
			dc.SetColor(colTitle)
			dc.DrawStringAnchored(label, bx+boardW/2, by+labelH/2, 0.5, 0.5)
		}
		tileTop := by + labelH
		for r := 0; r < v.MaxAttempts; r++ {
			ri := r + v.RowOffset
			for c := 0; c < v.Length; c++ {
				x := bx + padX + float64(c)*(tileSize+tileGap)
				y := tileTop + float64(r)*(tileSize+tileGap)

				var (
					letter rune
					style  = StyleAbsent
					gg     Guess
					filled bool
				)
				if ri >= fogFrom && ri < len(v.Guesses) {
					gg = v.Guesses[ri]
					if runes := []rune(gg.Word); c < len(runes) {
						letter = runes[c]
						filled = true
					}
					style = v.tileStyleAt(ri, bi, c, gg)
				}

				fill := colEmpty
				hidden := false
				if filled {
					switch style {
					case StyleScore:
						fill = scoreColor(correctCount(gg.MarksFor(bi)), v.Length)
					case StyleUnknown:
						fill = colUnknown
						hidden = true
					default:
						fill = styleColor(style)
					}
				}
				dc.SetColor(fill)
				dc.DrawRoundedRectangle(x, y, tileSize, tileSize, 6)
				dc.Fill()
				if !filled {
					dc.SetLineWidth(2)
					dc.SetColor(colBorder)
					dc.DrawRoundedRectangle(x, y, tileSize, tileSize, 6)
					dc.Stroke()
				}
				glyph := ""
				switch {
				case hidden:
					glyph = "?"
				case letter != 0:
					glyph = strings.ToUpper(string(letter))
				}
				if glyph != "" {
					dc.SetFontFace(letterFace)
					dc.SetColor(colText)
					dc.DrawStringAnchored(glyph,
						x+tileSize/2, y+tileSize/2, 0.5, 0.5)
				}
			}
		}
	}

	kbTop := gridTop + gridH + 18
	for ri, row := range rows {
		x := (width - rowWidth(row)) / 2
		y := kbTop + float64(ri)*(kbKeyH+kbRowGap)
		for _, ch := range row {
			fill := colKeyIdle
			if !v.HideKey {
				if st, ok := v.keyStyle(ch); ok {
					fill = styleColor(st)
				}
			}
			dc.SetColor(fill)
			dc.DrawRoundedRectangle(x, y, keyW, kbKeyH, 5)
			dc.Fill()
			dc.SetFontFace(keyFace)
			dc.SetColor(colText)
			dc.DrawStringAnchored(strings.ToUpper(string(ch)),
				x+keyW/2, y+kbKeyH/2, 0.5, 0.5)
			x += keyW + kbKeyGap
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderBoardText 是图片渲染失败时的纯文本降级：用 emoji 方块表示判定结果。
func renderBoardText(v boardView) string {
	var b strings.Builder
	if len(v.Badges) > 0 {
		b.WriteString("🧩 ")
		b.WriteString(strings.Join(v.Badges, " | "))
		b.WriteByte('\n')
	}
	boards := max(v.Boards, 1)
	fogFrom := 0
	if v.FogRows > 0 && len(v.Guesses) > v.FogRows {
		fogFrom = len(v.Guesses) - v.FogRows
	}
	for bi := range boards {
		if boards > 1 {
			b.WriteString(fmt.Sprintf("WORD %d", bi+1))
			if bi < len(v.Solved) && v.Solved[bi] {
				b.WriteString(" SOLVED")
			}
			b.WriteByte('\n')
		}
		for i := 0; i < v.MaxAttempts; i++ {
			gi := i + v.RowOffset
			if gi >= fogFrom && gi < len(v.Guesses) {
				gg := v.Guesses[gi]
				b.WriteString(strings.ToUpper(gg.Word))
				b.WriteByte('\n')
				styles := v.rowStyles(gi, bi, gg)
				for _, st := range styles {
					b.WriteString(styleEmoji(st))
				}
				if containsStyle(styles, StyleScore) {
					b.WriteString(fmt.Sprintf(" (%d/%d)", correctCount(gg.MarksFor(bi)), len(styles)))
				}
				b.WriteByte('\n')
				continue
			}
			b.WriteString(strings.Repeat("⬜", v.Length))
			b.WriteByte('\n')
		}
		if boards > 1 && bi != boards-1 {
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// ─── 样式 → 颜色 / emoji ──────────────────────────────────────────────────────

// styleColor 把显示样式映射为格子/键盘颜色。
func styleColor(s TileStyle) color.RGBA {
	switch s {
	case StyleCorrect:
		return colCorrect
	case StylePresent:
		return colPresent
	case StyleNear:
		return colNear
	case StyleRepeat:
		return colRepeat
	case StyleUnknown:
		return colUnknown
	default:
		return colAbsent
	}
}

// scoreColor 把绿色数量映射到一条 灰→黄→绿 的渐变（--score-color）。
func scoreColor(count, total int) color.RGBA {
	if total <= 0 {
		return colAbsent
	}
	ratio := float64(count) / float64(total)
	if ratio <= 0.5 {
		return lerpColor(colAbsent, colPresent, ratio/0.5)
	}
	return lerpColor(colPresent, colCorrect, (ratio-0.5)/0.5)
}

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	t = min(max(t, 0), 1)
	ch := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*t)
	}
	return color.RGBA{R: ch(a.R, b.R), G: ch(a.G, b.G), B: ch(a.B, b.B), A: 255}
}

// styleEmoji 把显示样式映射为文本降级用的方块。
func styleEmoji(s TileStyle) string {
	switch s {
	case StyleCorrect:
		return "🟩"
	case StylePresent:
		return "🟨"
	case StyleNear:
		return "🟦"
	case StyleRepeat:
		return "🟪"
	case StyleUnknown:
		return "⬛"
	case StyleScore:
		return "⬛"
	default:
		return "⬜"
	}
}

func containsStyle(styles []TileStyle, want TileStyle) bool {
	return slices.Contains(styles, want)
}

func correctCount(marks []Mark) int {
	n := 0
	for _, m := range marks {
		if m == Correct {
			n++
		}
	}
	return n
}

// rowStyles 返回某行某棋盘的显示样式；缺少显示层时回退到真值。
func (v boardView) rowStyles(r, bi int, gg Guess) []TileStyle {
	if v.Styles != nil && r < len(v.Styles) && bi < len(v.Styles[r]) {
		return v.Styles[r][bi]
	}
	marks := gg.MarksFor(bi)
	out := make([]TileStyle, len(marks))
	for i, m := range marks {
		out[i] = styleFromMark(m)
	}
	return out
}

// tileStyleAt 返回单个格子的显示样式。
func (v boardView) tileStyleAt(r, bi, c int, gg Guess) TileStyle {
	row := v.rowStyles(r, bi, gg)
	if c < len(row) {
		return row[c]
	}
	return StyleAbsent
}

// keyStyle 返回键盘字母的显示样式；优先使用显示层，其次回退到逻辑汇总。
func (v boardView) keyStyle(ch rune) (TileStyle, bool) {
	if v.KeyStyles != nil {
		st, ok := v.KeyStyles[ch]
		return st, ok
	}
	if m, ok := v.KeyStates[ch]; ok {
		return styleFromMark(m), true
	}
	return StyleAbsent, false
}
