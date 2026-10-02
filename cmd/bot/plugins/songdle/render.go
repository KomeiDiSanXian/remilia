package songdle

import (
	"errors"
	"image/color"
	"strconv"
	"strings"
	"unicode"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/textimage"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// 提示板图片尺寸与表格列宽（像素）。
const (
	// boardImageWidth 需容纳 9 列「猜曲目」对比表（列宽合计 1114 + 左右内边距）。
	boardImageWidth = 1166
	// boardInset 与画布水平内边距一致；表格用等宽空单元格实现同样的左右缩进。
	boardInset = 26
	// 线索表列宽：属性 | 当前取值（自适应） | 判定。
	clueLabelW   = 150
	clueVerdictW = 120
	// 记录表列宽：# | 属性 | 取值（自适应） | 判定。
	histIndexW   = 46
	histLabelW   = 120
	histVerdictW = 130
	// 猜曲目对比表列宽：曲名 | 版本 | 类型 | 流派 | 曲师 | BPM | 谱师 | 定数 | 绝赞。
	gridTitleW   = 214
	gridVersionW = 128
	gridTypeW    = 66
	gridGenreW   = 118
	gridArtistW  = 168
	gridBPMW     = 78
	gridCharterW = 168
	gridConstW   = 88
	gridBreakW   = 86
	// gridFontSize 是猜曲目对比表的单元格字号。
	gridFontSize = 17
)

// 提示板图片配色。
var (
	imgBg     = color.RGBA{R: 24, G: 26, B: 32, A: 255}
	imgText   = color.RGBA{R: 236, G: 238, B: 244, A: 255}
	imgDim    = color.RGBA{R: 146, G: 152, B: 164, A: 255}
	imgAccent = color.RGBA{R: 126, G: 170, B: 255, A: 255}
	imgGreen  = color.RGBA{R: 106, G: 170, B: 100, A: 255}
	imgYellow = color.RGBA{R: 201, G: 180, B: 88, A: 255}
	imgGray   = color.RGBA{R: 122, G: 126, B: 134, A: 255}
	// 猜曲目对比表的单元格底色 / 文字色（参考 Wordle 的色块风格）。
	imgCellBgMatch = color.RGBA{R: 45, G: 116, B: 72, A: 255}
	imgCellBgClose = color.RGBA{R: 158, G: 122, B: 36, A: 255}
	imgCellBgMiss  = color.RGBA{R: 38, G: 41, B: 51, A: 255}
	imgCellText    = color.RGBA{R: 240, G: 242, B: 246, A: 255}
	imgCellOnClose = color.RGBA{R: 33, G: 33, B: 36, A: 255}
)

// markColor 把判定映射为图片里的文字颜色。
func markColor(m Mark) color.RGBA {
	switch m {
	case Match:
		return imgGreen
	case Close:
		return imgYellow
	default:
		return imgGray
	}
}

// gridBgColor 返回猜曲目对比表单元格的底色。
func gridBgColor(m Mark) color.RGBA {
	switch m {
	case Match:
		return imgCellBgMatch
	case Close:
		return imgCellBgClose
	default:
		return imgCellBgMiss
	}
}

// gridTextColor 返回猜曲目对比表单元格的文字色：黄底用深色字保证对比度。
func gridTextColor(m Mark) color.RGBA {
	if m == Close {
		return imgCellOnClose
	}
	return imgCellText
}

// gridColWidth 返回某列在猜曲目对比表里的宽度。
func gridColWidth(attr Attribute) int {
	switch attr {
	case AttrTitle:
		return gridTitleW
	case AttrVersion:
		return gridVersionW
	case AttrType:
		return gridTypeW
	case AttrGenre:
		return gridGenreW
	case AttrArtist:
		return gridArtistW
	case AttrBPM:
		return gridBPMW
	case AttrCharter:
		return gridCharterW
	case AttrConst:
		return gridConstW
	case AttrBreak:
		return gridBreakW
	default:
		return gridGenreW
	}
}

// emojiForImage 把文本模板里的 emoji 映射成 CJK 字体普遍覆盖的几何符号，
// 并去掉纯装饰性 emoji。
//
// 图片渲染走的是系统 CJK 字体（Windows 的 Microsoft YaHei、Linux 的
// Noto Sans CJK 等），这些字体基本不含彩色 emoji 字形，直接绘制
// 🟩 / 🟨 / ⬜ / ⬛ / 🎯 等只会得到豆腐块（.notdef）。文本消息由平台
// 自行渲染 emoji，因此只在图片路径做替换。
var emojiForImage = strings.NewReplacer(
	"🟩", "■", "🟨", "■", "🟥", "■", "🟦", "■",
	"⬜", "□", "⬛", "■", "▫", "□", "▪", "■",
	"🎯", "", "🎵", "", "📝", "", "🎉", "", "💀", "",
	"❌", "", "🏳️", "", "🏳", "", "✅", "", "⚠️", "", "⚠", "",
)

// emojiSafe 返回适合光栅化的文本：emoji 已被替换 / 移除。
func emojiSafe(s string) string { return emojiForImage.Replace(s) }

// markStatus 返回判定对应的 i18n 后缀（match / close / miss）。
func markStatus(m Mark) string {
	switch m {
	case Match:
		return "match"
	case Close:
		return "close"
	default:
		return "miss"
	}
}

// markStatusKey 返回判定短语的 i18n key。
func markStatusKey(status string) string { return "songdle.mark." + status }

// clueInfo 是单个属性当前推导出的线索。
type clueInfo struct {
	// Value 是展示用取值：已确认取值 / 数值区间 / 已排除的取值列表；未知时为空。
	Value string
	// Mark 决定表格里的状态颜色。
	Mark Mark
	// Status 是判定短语后缀：match / close / miss / unknown。
	Status string
}

// clueInfo 汇总某个属性已有的探测结果。它是文本提示板与图片表格的共同数据源。
func (p *Plugin) clueInfo(ctx *eventctx.Context, g *Game, attr Attribute) clueInfo {
	if v, ok := g.MatchedValue(attr); ok {
		return clueInfo{Value: v, Mark: Match, Status: "match"}
	}
	if attr.Numeric() {
		if r := g.NumericRange(attr); r.HasLo || r.HasHi {
			return clueInfo{Value: p.rangeText(ctx, attr, r), Mark: Close, Status: "close"}
		}
	}
	if tried := g.TriedValues(attr); len(tried) > 0 {
		if len(tried) > 3 {
			tried = tried[len(tried)-3:]
		}
		return clueInfo{Value: strings.Join(tried, " / "), Mark: Miss, Status: "miss"}
	}
	return clueInfo{Mark: Miss, Status: "unknown"}
}

// renderBoard 渲染纯文本提示板（图片渲染失败时的降级，也是可复制的版本）。
func (p *Plugin) renderBoard(ctx *eventctx.Context, g *Game, notice string) string {
	var b strings.Builder
	if notice != "" {
		b.WriteString(notice)
		b.WriteByte('\n')
	}
	b.WriteString(p.headerLine(ctx, g))
	b.WriteByte('\n')
	b.WriteString(p.maskedLine(ctx, g))
	if len(g.Guesses) > 0 {
		b.WriteByte('\n')
		b.WriteString(p.t(ctx, "songdle.board.guesses_title"))
		for i, tg := range g.Guesses {
			b.WriteByte('\n')
			b.WriteString("  ")
			b.WriteString(strconv.Itoa(i + 1))
			b.WriteString(". ")
			b.WriteString(p.guessLine(ctx, tg))
		}
	}
	if len(g.Probes) == 0 && !g.RevealArtist {
		if len(g.Guesses) > 0 {
			return strings.TrimRight(b.String(), "\n")
		}
		b.WriteByte('\n')
		b.WriteString(p.t(ctx, "songdle.board.clues_title"))
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(p.t(ctx, "songdle.board.no_probes"))
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteByte('\n')
	b.WriteString(p.t(ctx, "songdle.board.clues_title"))
	b.WriteByte('\n')
	for _, attr := range ClueAttributes {
		_, line := p.clueOf(ctx, g, attr)
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(g.Probes) == 0 {
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString(p.t(ctx, "songdle.board.history_title"))
	for i, pr := range g.Probes {
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(p.probeLine(ctx, pr))
	}
	return b.String()
}

// guessLine 渲染一次猜曲目（文本提示板用）：曲名 + 各列取值与判定。
func (p *Plugin) guessLine(ctx *eventctx.Context, tg TrackGuess) string {
	parts := make([]string, 0, len(tg.Cells))
	for _, c := range tg.Cells {
		if c.Attr == AttrTitle {
			continue
		}
		parts = append(parts, c.Mark.Emoji()+" "+p.t(ctx, c.Attr.LabelKey())+" "+
			c.Value+arrowSuffix(c.Attr, c.Dir))
	}
	head := tg.Track.Title
	if c, ok := tg.Cell(AttrTitle); ok {
		head = c.Mark.Emoji() + " " + head
	}
	return head + "\n     " + strings.Join(parts, " · ")
}

// headerLine 返回状态行。
func (p *Plugin) headerLine(ctx *eventctx.Context, g *Game) string {
	return p.t(ctx, "songdle.board.header", map[string]any{
		"Mode":     p.modeLabel(ctx, g.Mode),
		"Attempts": g.Attempts(),
		"Max":      g.MaxAttempts,
	})
}

// maskedLine 返回「完全涂黑」的曲名提示行：开局只给出长度，猜测过程中始终隐藏。
func (p *Plugin) maskedLine(ctx *eventctx.Context, g *Game) string {
	mask := maskTitle(g.Target.Title)
	return p.t(ctx, "songdle.board.masked", map[string]any{
		"Mask": mask,
		"Len":  len([]rune(strings.TrimSpace(g.Target.Title))),
	})
}

// maskTitle 用 ⬛ 涂黑曲名（保留空格），曲名过长时截断，避免刷屏。
func maskTitle(title string) string {
	const maxMask = 24
	runes := []rune(strings.TrimSpace(title))
	var b strings.Builder
	for i, r := range runes {
		if i >= maxMask {
			b.WriteRune('…')
			break
		}
		if unicode.IsSpace(r) {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune('⬛')
	}
	return b.String()
}

// clueOf 返回某个属性的文本线索（判定 + 文案），供文本提示板使用。
func (p *Plugin) clueOf(ctx *eventctx.Context, g *Game, attr Attribute) (Mark, string) {
	inf := p.clueInfo(ctx, g, attr)
	label := p.t(ctx, attr.LabelKey())
	switch inf.Status {
	case "match":
		return Match, p.t(ctx, "songdle.clue.known", map[string]any{
			"Mark": Match.Emoji(), "Label": label, "Value": inf.Value,
		})
	case "close":
		return Close, p.t(ctx, "songdle.clue.range", map[string]any{
			"Mark": Close.Emoji(), "Label": label, "Range": inf.Value,
		})
	case "miss":
		return Miss, p.t(ctx, "songdle.clue.excluded", map[string]any{
			"Mark": Miss.Emoji(), "Label": label, "Values": inf.Value,
		})
	default:
		return Miss, p.t(ctx, "songdle.clue.unknown", map[string]any{"Label": label})
	}
}

// rangeText 把数值区间格式化为易读文本。
func (p *Plugin) rangeText(ctx *eventctx.Context, attr Attribute, r NumRange) string {
	switch {
	case r.HasLo && r.HasHi:
		return p.t(ctx, "songdle.clue.range_both", map[string]any{
			"Lo": formatAttrNumber(attr, r.Lo),
			"Hi": formatAttrNumber(attr, r.Hi),
		})
	case r.HasLo:
		return p.t(ctx, "songdle.clue.range_lo", map[string]any{"Lo": formatAttrNumber(attr, r.Lo)})
	default:
		return p.t(ctx, "songdle.clue.range_hi", map[string]any{"Hi": formatAttrNumber(attr, r.Hi)})
	}
}

// probeLine 渲染一次猜测，如「🟨 BPM 150 ↓」。
func (p *Plugin) probeLine(ctx *eventctx.Context, pr Probe) string {
	arrow := ""
	if pr.Attr.Numeric() {
		arrow = pr.Dir.Arrow()
	}
	return p.t(ctx, "songdle.probe.line", map[string]any{
		"Mark":  pr.Mark.Emoji(),
		"Label": p.t(ctx, pr.Attr.LabelKey()),
		"Value": pr.Value,
		"Arrow": arrow,
	})
}

// formatAttrNumber 按属性格式化数值；定数保留小数，其余取整。
func formatAttrNumber(attr Attribute, v float64) string {
	if attr == AttrConst {
		return formatDS(v)
	}
	return strconv.Itoa(int(v))
}

// boardImageCell 是提示板表格里的一个单元格。
type boardImageCell struct {
	Text  string
	Width int // 0 表示占据本行剩余宽度
	Color color.RGBA
	// Bg 非零值时作为单元格填充色（Alpha > 0 生效），用于 Wordle 式色块。
	Bg   color.RGBA
	Size float64
}

// boardImageRow 是提示板图片里的一行。
//
// Cells 为空时按整行文本渲染；否则按表格行渲染。
type boardImageRow struct {
	Text    string
	Size    float64
	Color   color.RGBA
	Gap     int // 行前额外间距（像素）
	Divider bool
	Cells   []boardImageCell
}

// tableRow 给单元格两侧补上与画布内边距等宽的空单元格，让表格对齐正文缩进。
func tableRow(cells ...boardImageCell) boardImageRow {
	out := make([]boardImageCell, 0, len(cells)+2)
	out = append(out, boardImageCell{Width: boardInset})
	out = append(out, cells...)
	out = append(out, boardImageCell{Width: boardInset})
	return boardImageRow{Cells: out}
}

// clueTableHeader 返回线索表的表头与分隔线。
func (p *Plugin) clueTableHeader(ctx *eventctx.Context) []boardImageRow {
	return []boardImageRow{
		tableRow(
			boardImageCell{Text: p.t(ctx, "songdle.table.attribute"), Width: clueLabelW, Color: imgGray},
			boardImageCell{Text: p.t(ctx, "songdle.table.value"), Color: imgGray},
			boardImageCell{Text: p.t(ctx, "songdle.table.verdict"), Width: clueVerdictW, Color: imgGray},
		),
		{Divider: true},
	}
}

// historyTableHeader 返回记录表的表头与分隔线。
func (p *Plugin) historyTableHeader(ctx *eventctx.Context) []boardImageRow {
	return []boardImageRow{
		tableRow(
			boardImageCell{Text: p.t(ctx, "songdle.table.index"), Width: histIndexW, Color: imgGray},
			boardImageCell{Text: p.t(ctx, "songdle.table.attribute"), Width: histLabelW, Color: imgGray},
			boardImageCell{Text: p.t(ctx, "songdle.table.value"), Color: imgGray},
			boardImageCell{Text: p.t(ctx, "songdle.table.verdict"), Width: histVerdictW, Color: imgGray},
		),
		{Divider: true},
	}
}

// guessTableHeader 返回猜曲目对比表的表头与分隔线。
func (p *Plugin) guessTableHeader(ctx *eventctx.Context) []boardImageRow {
	cells := make([]boardImageCell, 0, len(GuessColumns))
	for _, attr := range GuessColumns {
		cells = append(cells, boardImageCell{
			Text:  p.t(ctx, attr.LabelKey()),
			Width: gridColWidth(attr),
			Color: imgGray,
			Size:  gridFontSize,
		})
	}
	return []boardImageRow{tableRow(cells...), {Divider: true}}
}

// guessTableRow 把一次猜曲目渲染成一行彩色单元格。
func (p *Plugin) guessTableRow(ctx *eventctx.Context, tg TrackGuess) boardImageRow {
	cells := make([]boardImageCell, 0, len(tg.Cells))
	for _, c := range tg.Cells {
		width := gridColWidth(c.Attr)
		text := emojiSafe(strings.TrimSpace(c.Value)) + arrowSuffix(c.Attr, c.Dir)
		cells = append(cells, boardImageCell{
			Text:  ellipsize(text, width-16, gridFontSize),
			Width: width,
			Color: gridTextColor(c.Mark),
			Bg:    gridBgColor(c.Mark),
			Size:  gridFontSize,
		})
	}
	return tableRow(cells...)
}

// arrowSuffix 返回数值 / 版本列的方向箭头；文本属性没有方向。
func arrowSuffix(attr Attribute, d Dir) string {
	if d == DirNone || (!attr.Numeric() && attr != AttrVersion) {
		return ""
	}
	if a := d.Arrow(); a != "" {
		return " " + a
	}
	return ""
}

// ellipsize 按估算宽度截断文本并补省略号，避免单元格换行把表格撑散。
//
// 系统 CJK 字体下全角字符约占一个字号宽、半角约 0.55 个，这里按此粗略估算；
// 只用于排版，不需要精确的字体度量。
func ellipsize(s string, maxPx int, fontSize float64) string {
	if s == "" || maxPx <= 0 {
		return s
	}
	runes := []rune(s)
	limit, width := 0, 0.0
	max := float64(maxPx)
	for i, r := range runes {
		w := runeWidth(r, fontSize)
		if width+w > max {
			break
		}
		width += w
		limit = i + 1
	}
	if limit >= len(runes) {
		return s
	}
	// 截断时给省略号留出位置。
	for limit > 0 && width+fontSize > max {
		limit--
		width -= runeWidth(runes[limit], fontSize)
	}
	if limit == 0 {
		return "…"
	}
	return string(runes[:limit]) + "…"
}

// runeWidth 估算单个字符的显示宽度（像素）。
func runeWidth(r rune, fontSize float64) float64 {
	if r < 0x2E80 { // ASCII 与大部分拉丁字母 / 半角符号
		return fontSize * 0.55
	}
	return fontSize
}

// boardImageRows 汇总图片里的全部行。
//
// 所有文本统一经过 [emojiSafe]：图片用的系统 CJK 字体通常不含彩色 emoji
// 字形，直接绘制会得到豆腐块。集中在这里处理，既保证图片安全，也便于测试。
func (p *Plugin) boardImageRows(ctx *eventctx.Context, g *Game, notice string) []boardImageRow {
	rows := make([]boardImageRow, 0, len(ClueAttributes)+len(g.Probes)+len(g.Guesses)+16)
	if notice != "" {
		rows = append(rows, boardImageRow{Text: strings.TrimSpace(emojiSafe(notice)), Color: imgAccent})
	}
	rows = append(rows,
		boardImageRow{Text: emojiSafe(p.headerLine(ctx, g)), Size: 25, Color: imgText},
		boardImageRow{Text: strings.TrimSpace(emojiSafe(p.maskedLine(ctx, g))), Color: imgAccent},
		boardImageRow{Text: emojiSafe(p.t(ctx, "songdle.board.legend_img")), Size: 16, Color: imgDim},
	)

	// 猜曲目对比表：直接猜整首曲目时的主要视图。
	if len(g.Guesses) > 0 {
		rows = append(rows, boardImageRow{
			Text: emojiSafe(p.t(ctx, "songdle.board.guesses_title")), Color: imgAccent, Gap: 16,
		})
		rows = append(rows, p.guessTableHeader(ctx)...)
		for _, tg := range g.Guesses {
			rows = append(rows, p.guessTableRow(ctx, tg))
		}
	}

	// 属性探测只作补充：没探测过（且没有开局公布的曲师）就不再渲染空的线索表。
	if len(g.Probes) == 0 && !g.RevealArtist {
		if len(g.Guesses) == 0 {
			rows = append(rows,
				boardImageRow{Text: emojiSafe(p.t(ctx, "songdle.board.clues_title")), Color: imgAccent, Gap: 16},
				boardImageRow{Text: " " + emojiSafe(p.t(ctx, "songdle.board.no_probes")), Color: imgDim},
			)
		}
		return rows
	}

	rows = append(rows, boardImageRow{
		Text: emojiSafe(p.t(ctx, "songdle.board.clues_title")), Color: imgAccent, Gap: 16,
	})
	rows = append(rows, p.clueTableHeader(ctx)...)
	for _, attr := range ClueAttributes {
		inf := p.clueInfo(ctx, g, attr)
		value, color := inf.Value, imgText
		if value == "" {
			value, color = "—", imgGray
		}
		rows = append(rows, tableRow(
			boardImageCell{Text: p.t(ctx, attr.LabelKey()), Width: clueLabelW, Color: imgDim},
			boardImageCell{Text: emojiSafe(value), Color: color},
			boardImageCell{Text: p.t(ctx, markStatusKey(inf.Status)), Width: clueVerdictW, Color: markColor(inf.Mark)},
		))
	}

	if len(g.Probes) == 0 {
		return rows
	}
	rows = append(rows, boardImageRow{
		Text: emojiSafe(p.t(ctx, "songdle.board.history_title")), Color: imgAccent, Gap: 18,
	})
	rows = append(rows, p.historyTableHeader(ctx)...)
	for i, pr := range g.Probes {
		verdict := p.t(ctx, markStatusKey(markStatus(pr.Mark)))
		if arrow := pr.Dir.Arrow(); arrow != "" && pr.Attr.Numeric() {
			verdict += " " + arrow
		}
		rows = append(rows, tableRow(
			boardImageCell{Text: strconv.Itoa(i + 1), Width: histIndexW, Color: imgDim},
			boardImageCell{Text: p.t(ctx, pr.Attr.LabelKey()), Width: histLabelW, Color: imgDim},
			boardImageCell{Text: emojiSafe(pr.Value), Color: imgText},
			boardImageCell{Text: verdict, Width: histVerdictW, Color: markColor(pr.Mark)},
		))
	}
	return rows
}

// renderBoardImage 把提示板渲染成图片；失败时由调用方降级为纯文本。
func (p *Plugin) renderBoardImage(ctx *eventctx.Context, g *Game, notice string) ([]byte, error) {
	// 明确探查系统 CJK 字体：WithCJKFont 在缺字体时会静默回退到不含中日韩
	// 字形的 Go Regular，导致整块中文变豆腐块。这里主动报错，交由 replyBoard
	// 降级为文本提示板。
	fontPath := textimage.SystemCJKFontPath()
	if fontPath == "" {
		return nil, errors.New("songdle: 未找到可用的 CJK 字体")
	}
	// PaddingY 设为 0：AddText 每个块都会重复套用纵向内边距，逐行调用会把
	// 图片撑得非常高；改由 boardImageRows 的 Gap 与首尾 Spacer 控制间距。
	c, err := textimage.NewCanvas(boardImageWidth,
		textimage.WithFontPath(fontPath),
		textimage.WithFontSize(21),
		textimage.WithLineHeight(1.45),
		textimage.WithBgColor(imgBg),
		textimage.WithFontColor(imgText),
		textimage.WithPadding(boardInset, 0),
	)
	if err != nil {
		return nil, err
	}
	c.AddSpacer(boardInset)
	for _, row := range p.boardImageRows(ctx, g, notice) {
		if row.Gap > 0 {
			c.AddSpacer(row.Gap)
		}
		if row.Divider {
			c.AddDivider(
				textimage.WithDividerColor(imgGray),
				textimage.WithDividerThickness(1),
				textimage.WithDividerInset(boardInset),
				textimage.WithDividerPadding(6),
			)
			continue
		}
		if len(row.Cells) == 0 {
			opts := []textimage.Option{textimage.WithFontColor(row.Color)}
			if row.Size > 0 {
				opts = append(opts, textimage.WithFontSize(row.Size))
			}
			_ = c.AddText(row.Text, opts...)
			continue
		}
		items := make([]textimage.RowItem, 0, len(row.Cells))
		for _, cell := range row.Cells {
			// 单元格内边距清零，避免继承画布的 26px 水平内边距而挤爆列宽。
			opts := []textimage.Option{textimage.WithFontColor(cell.Color), textimage.WithPadding(0, 0)}
			if cell.Size > 0 {
				opts = append(opts, textimage.WithFontSize(cell.Size))
			}
			if cell.Bg.A > 0 {
				opts = append(opts,
					textimage.WithBgColor(cell.Bg),
					textimage.WithPadding(8, 6),
					textimage.WithAlign(textimage.AlignCenter),
				)
			}
			items = append(items, textimage.RowItem{Width: cell.Width, Text: cell.Text, TextOpts: opts})
		}
		_ = c.AddRow(items...)
	}
	c.AddSpacer(boardInset)
	return c.ResultPNG()
}

// buttons 构建控制按钮。
//
// 每个按钮同时带 ID 与 Command：支持回调的平台用 ID 触发；QQ 等平台使用指令按钮，
// 点击后把命令填入输入框。
func (p *Plugin) buttons(ctx *eventctx.Context, g *Game) []platform.Button {
	if !ctx.GetPlatformCapabilities().Has(platform.CapButtons) {
		return nil
	}
	if g.Finished {
		return []platform.Button{
			{
				ID:      buttonPrefix + "new",
				Label:   p.t(ctx, "songdle.button.new"),
				Command: "/songdle",
				Style:   platform.ButtonStylePrimary,
				Row:     1,
			},
			{
				ID:      buttonPrefix + "board",
				Label:   p.t(ctx, "songdle.button.board"),
				Command: "/songdle board",
				Style:   platform.ButtonStyleSecondary,
				Row:     1,
			},
		}
	}
	return []platform.Button{
		{
			ID:      buttonPrefix + "title",
			Label:   p.t(ctx, "songdle.button.title"),
			Command: "/songdle 猜 ",
			Style:   platform.ButtonStylePrimary,
			Row:     1,
		},
		{
			ID:      buttonPrefix + "bpm",
			Label:   p.t(ctx, "songdle.button.bpm"),
			Command: "/songdle bpm ",
			Style:   platform.ButtonStyleSecondary,
			Row:     1,
		},
		{
			ID:      buttonPrefix + "artist",
			Label:   p.t(ctx, "songdle.button.artist"),
			Command: "/songdle 曲师 ",
			Style:   platform.ButtonStyleSecondary,
			Row:     1,
		},
		{
			ID:      buttonPrefix + "giveup",
			Label:   p.t(ctx, "songdle.button.giveup"),
			Command: "/songdle giveup",
			Style:   platform.ButtonStyleDanger,
			Row:     2,
		},
		{
			ID:      buttonPrefix + "board",
			Label:   p.t(ctx, "songdle.button.board"),
			Command: "/songdle board",
			Style:   platform.ButtonStyleSecondary,
			Row:     2,
		},
	}
}

// replyBoard 渲染并发送提示板：优先发图片，平台支持时把文本作为图注一同发送。
func (p *Plugin) replyBoard(ctx *eventctx.Context, g *Game, notice string) {
	text := p.renderBoard(ctx, g, notice)
	if g.Finished {
		text += "\n" + p.t(ctx, "songdle.answer", map[string]any{"Answer": g.AnswerLine()})
	}
	caps := ctx.GetPlatformCapabilities()

	img, err := p.renderBoardImage(ctx, g, notice)
	if err != nil {
		p.warnf("songdle: 渲染提示板失败: %v", err)
		ctx.ReplyText(text)
		return
	}
	msg := platform.ImageDataMessage(img, "songdle.png", "image/png")
	if btns := p.buttons(ctx, g); len(btns) > 0 {
		msg = msg.WithButtons(btns...)
	}
	switch {
	case caps.Has(platform.CapMarkdown):
		msg.Markdown = text
		ctx.Reply(msg)
	case caps.Has(platform.CapCaption):
		msg.Text = text
		ctx.Reply(msg)
	default:
		ctx.Reply(msg)
		ctx.ReplyText(text)
	}
}

// displayUser 返回展示名，为空时回退到短 ID。
func displayUser(name, id string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return shortID(id)
}

// shortID 截断过长的平台 ID，避免刷屏。
func shortID(id string) string {
	r := []rune(strings.TrimSpace(id))
	if len(r) <= 6 {
		return string(r)
	}
	return string(r[:6])
}
