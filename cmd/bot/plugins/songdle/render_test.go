package songdle

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

func TestRenderBoardShowsClues(t *testing.T) {
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	target := Track{
		ID: "1", Title: "Future", Artist: "sasakure.UK", Genre: "流行&动漫",
		Type: "SD", Version: "maimai", BPM: 130, MasDS: 10.7, MasBreak: 9,
	}
	g := &Game{Target: target, MaxAttempts: 10, Mode: ModeRandom}
	g.Submit(mustProbe(t, target, AttrBPM, "100"), time.Now())
	g.Submit(mustProbe(t, target, AttrConst, "10.5"), time.Now())

	out := p.renderBoard(ctx, g, "")
	for _, want := range []string{"猜音游曲目", "⬛", "曲师", "BPM", "定数", "🟨", "⬜", "↑"} {
		if !strings.Contains(out, want) {
			t.Errorf("提示板应包含 %q，实际输出:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Future") {
		t.Errorf("提示板不应泄露谜底曲名，实际输出:\n%s", out)
	}
}

func TestRenderBoardEmpty(t *testing.T) {
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	g := &Game{Target: Track{Title: "Future"}, MaxAttempts: 10}
	out := p.renderBoard(ctx, g, "")
	if !strings.Contains(out, "还没有开始") {
		t.Errorf("空提示板应引导玩家开始，实际输出:\n%s", out)
	}
}

func TestRenderBoardRevealArtist(t *testing.T) {
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	g := &Game{Target: Track{Title: "Future", Artist: "sasakure.UK"}, MaxAttempts: 10, RevealArtist: true}
	out := p.renderBoard(ctx, g, "")
	if !strings.Contains(out, "sasakure.UK") {
		t.Errorf("--artist 应公布曲师，实际输出:\n%s", out)
	}
}

func TestMaskTitle(t *testing.T) {
	if got := maskTitle("Future"); got != "⬛⬛⬛⬛⬛⬛" {
		t.Errorf("maskTitle(Future) = %q", got)
	}
	if got := maskTitle("A B"); got != "⬛ ⬛" {
		t.Errorf("maskTitle 应保留空格，实际 %q", got)
	}
	long := strings.Repeat("字", 30)
	if got := maskTitle(long); !strings.Contains(got, "…") {
		t.Errorf("超长曲名应截断，实际 %q", got)
	}
}

func TestEmojiSafe(t *testing.T) {
	cases := map[string]string{
		"🟩 曲师":           "■ 曲师",
		"🟨 BPM 150↓":     "■ BPM 150↓",
		"⬜ 流派":           "□ 流派",
		"🎯 谜底：⬛⬛":        " 谜底：■■",
		"📝 已记录 · 还剩 4 次": " 已记录 · 还剩 4 次",
	}
	for in, want := range cases {
		if got := emojiSafe(in); got != want {
			t.Errorf("emojiSafe(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// isUnsupportedGraphic 报告该字符是否属于 CJK 字体通常缺失的 emoji / 图形符号。
func isUnsupportedGraphic(r rune) bool {
	switch {
	case r >= 0x1F000: // 各种 emoji 与补充符号
		return true
	case r >= 0x2600 && r <= 0x27BF: // 杂项符号与装饰符（❌ ✅ ⚠ 等）
		return true
	case r == 0x2B1B || r == 0x2B1C: // ⬛ ⬜
		return true
	case r == 0xFE0E || r == 0xFE0F: // 变体选择符
		return true
	default:
		return false
	}
}

// TestBoardImageRowsAreFontSafe 保证图片路径里不会混入 CJK 字体缺失的 emoji。
func TestBoardImageRowsAreFontSafe(t *testing.T) {
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	target := Track{
		ID: "1", Title: "Future", Artist: "sasakure.UK", Genre: "流行&动漫",
		Type: "SD", Version: "maimai", BPM: 130, MasDS: 10.7, MasBreak: 9,
	}
	g := &Game{Target: target, MaxAttempts: 12, Mode: ModeRandom}
	g.Submit(mustProbe(t, target, AttrBPM, "100"), time.Now())
	g.Submit(mustProbe(t, target, AttrArtist, "sasakure.UK"), time.Now())
	g.Submit(mustProbe(t, target, AttrTitle, "fut"), time.Now())

	var joined strings.Builder
	var sb strings.Builder
	for _, row := range p.boardImageRows(ctx, g, "📝 已记录 · 还剩 8 次") {
		sb.Reset()
		sb.WriteString(row.Text)
		for _, cell := range row.Cells {
			sb.WriteString(cell.Text)
		}
		line := sb.String()
		for _, r := range line {
			if isUnsupportedGraphic(r) {
				t.Errorf("图片文本含字体可能缺失的字符 U+%04X %q：%q", r, r, line)
			}
		}
		joined.WriteString(line)
		joined.WriteByte('\n')
	}
	out := joined.String()
	for _, want := range []string{"■", "已记录", "sasakure.UK", "属性", "取值", "判定", "命中", "未探测"} {
		if !strings.Contains(out, want) {
			t.Errorf("图片文本应包含 %q，实际：\n%s", want, out)
		}
	}
}

// TestBoardImageUsesTable 验证图片按表格组织：线索与记录都以多单元格行呈现。
func TestBoardImageUsesTable(t *testing.T) {
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	target := Track{Title: "Future", Artist: "sasakure.UK", BPM: 130}
	g := &Game{Target: target, MaxAttempts: 12, Mode: ModeRandom}
	g.Submit(mustProbe(t, target, AttrBPM, "100"), time.Now())

	rows := p.boardImageRows(ctx, g, "")
	var tableRows int
	for _, row := range rows {
		if len(row.Cells) >= 3 {
			tableRows++
		}
	}
	// 线索表头 + 分隔线外的 7 行 + 记录表头 + 记录行（至少 1）≥ 10。
	if tableRows < 10 {
		t.Fatalf("表格行数 = %d, 期望 >= 10（线索 7 项 + 表头 + 记录）", tableRows)
	}
}

func TestRenderBoardImageProducesPNG(t *testing.T) {
	if textimage.SystemCJKFontPath() == "" {
		t.Skip("系统未安装 CJK 字体，跳过图片渲染测试")
	}
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	g := &Game{Target: Track{Title: "Future", Artist: "X"}, MaxAttempts: 12}
	img, err := p.renderBoardImage(ctx, g, "")
	if err != nil {
		t.Fatalf("renderBoardImage: %v", err)
	}
	if !bytes.HasPrefix(img, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("输出应为 PNG，实际前 8 字节：% x", img[:min(8, len(img))])
	}
}

func TestEllipsize(t *testing.T) {
	if got := ellipsize("Future", 100, gridFontSize); got != "Future" {
		t.Errorf("短文本不应截断，实际 %q", got)
	}
	if got := ellipsize("", 100, gridFontSize); got != "" {
		t.Errorf("空文本应原样返回，实际 %q", got)
	}
	if got := ellipsize("abcdefghijklmnop", 40, gridFontSize); !strings.HasSuffix(got, "…") {
		t.Errorf("超宽 ASCII 应截断并补省略号，实际 %q", got)
	}
	if got := ellipsize("这一首曲名相当长需要被截断显示", 60, gridFontSize); !strings.HasSuffix(got, "…") {
		t.Errorf("超宽中文应截断并补省略号，实际 %q", got)
	}
	if got := ellipsize("abc", 0, gridFontSize); got != "abc" {
		t.Errorf("maxPx<=0 应原样返回，实际 %q", got)
	}
}

func TestArrowSuffix(t *testing.T) {
	cases := []struct {
		attr Attribute
		dir  Dir
		want string
	}{
		{AttrBPM, DirUp, " ↑"},
		{AttrConst, DirDown, " ↓"},
		{AttrVersion, DirUp, " ↑"},
		{AttrArtist, DirUp, ""}, // 文本列不带方向
		{AttrBPM, DirNone, ""},
	}
	for _, c := range cases {
		if got := arrowSuffix(c.attr, c.dir); got != c.want {
			t.Errorf("arrowSuffix(%v,%v) = %q, 期望 %q", c.attr, c.dir, got, c.want)
		}
	}
}

// gridTarget / gridGuess 是猜曲目对比表渲染测试用的固定谜底与猜测。
var (
	gridTarget = Track{
		ID: "1", Title: "Future", Artist: "sasakure.UK", Genre: "流行&动漫",
		Type: "SD", Version: "舞萌DX2024", BPM: 130, MasCharter: "Q", MasDS: 10.7, MasBreak: 9,
	}
	gridGuess = Track{
		ID: "2", Title: "Other", Artist: "X", Genre: "舞萌", Type: "DX",
		Version: "舞萌DX2025", BPM: 100, MasCharter: "Y", MasDS: 10.2, MasBreak: 4,
	}
)

func TestGuessTableRow(t *testing.T) {
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	row := p.guessTableRow(ctx, TrackGuess{Track: gridGuess, Cells: Compare(gridTarget, gridGuess)})
	if len(row.Cells) != len(GuessColumns)+2 {
		t.Fatalf("单元行应含 %d 列（含两侧缩进），实际 %d", len(GuessColumns)+2, len(row.Cells))
	}
	if row.Cells[0].Text != "" || row.Cells[len(row.Cells)-1].Text != "" {
		t.Error("首尾应为缩进空单元格")
	}
	for i, attr := range GuessColumns {
		cell := row.Cells[i+1]
		if cell.Width != gridColWidth(attr) {
			t.Errorf("%v 列宽 = %d, 期望 %d", attr, cell.Width, gridColWidth(attr))
		}
		if cell.Bg.A == 0 {
			t.Errorf("%v 列应带色块底色", attr)
		}
		if !cell.Center {
			t.Errorf("%v 列应居中，才能与表头对齐", attr)
		}
	}
	if got := row.Cells[1].Text; got != "Other" {
		t.Errorf("曲名列应展示猜测曲名，实际 %q", got)
	}
	// BPM 列索引：缩进 + Title/Version/Type/Genre/Artist 之后。
	bpm := row.Cells[6].Text
	if !strings.Contains(bpm, "100") || !strings.HasSuffix(bpm, "↑") {
		t.Errorf("BPM 猜低应展示数值并带 ↑，实际 %q", bpm)
	}
}

func TestBoardImageRowsWithGuesses(t *testing.T) {
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	g := &Game{Target: gridTarget, MaxAttempts: 10, Mode: ModeRandom}
	g.SubmitGuess(TrackGuess{Track: gridGuess, Cells: Compare(gridTarget, gridGuess)}, time.Now())

	rows := p.boardImageRows(ctx, g, "")
	var (
		sawHeader bool
		sawGuess  bool
		tableRows int
		joined    strings.Builder
	)
	for _, row := range rows {
		line := row.Text
		for _, cell := range row.Cells {
			line += cell.Text
			if cell.Text == "Other" {
				sawGuess = true
			}
		}
		if len(row.Cells) >= 3 {
			tableRows++
		}
		if strings.Contains(line, "曲名") && strings.Contains(line, "BPM") {
			sawHeader = true
		}
		joined.WriteString(line)
	}
	if !sawHeader {
		t.Error("应渲染猜曲目对比表表头")
	}
	if !sawGuess {
		t.Error("应渲染本次猜测的曲名单元格")
	}
	if tableRows < 2 {
		t.Errorf("应有表头与数据行，实际 %d 行", tableRows)
	}
	if strings.Contains(joined.String(), "未探测") {
		t.Error("已有猜曲目、未探测时不应再渲染空的线索表")
	}
	for _, r := range joined.String() {
		if isUnsupportedGraphic(r) {
			t.Errorf("图片文本含字体可能缺失的字符 U+%04X %q", r, r)
		}
	}
}

// TestGuessTableRowEllipsizesTitle 验证过长的曲名不会撑破单元格。
func TestGuessTableRowEllipsizesTitle(t *testing.T) {
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	target := Track{ID: "1", Title: "Future", Version: "舞萌DX2024", Type: "SD"}
	long := Track{
		ID: "9", Title: strings.Repeat("很长的曲名", 8),
		Version: "舞萌DX2024", Type: "SD",
	}
	row := p.guessTableRow(ctx, TrackGuess{Track: long, Cells: Compare(target, long)})
	if title := row.Cells[1].Text; !strings.HasSuffix(title, "…") {
		t.Errorf("超长曲名应截断，实际 %q", title)
	}
}

// TestGuessTableHeaderCentered 保证表头与数据单元格一样居中、列宽一致，避免错位。
func TestGuessTableHeaderCentered(t *testing.T) {
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	rows := p.guessTableHeader(ctx)
	if len(rows) == 0 || len(rows[0].Cells) != len(GuessColumns)+2 {
		t.Fatalf("表头行数或列数异常: %+v", rows)
	}
	for i, attr := range GuessColumns {
		cell := rows[0].Cells[i+1]
		if !cell.Center {
			t.Errorf("表头 %v 列应居中，才能与数据单元格对齐", attr)
		}
		if cell.Width != gridColWidth(attr) {
			t.Errorf("表头 %v 列宽 = %d, 期望 %d", attr, cell.Width, gridColWidth(attr))
		}
	}
}

func TestRenderBoardImageWithGuessesProducesPNG(t *testing.T) {
	if textimage.SystemCJKFontPath() == "" {
		t.Skip("系统未安装 CJK 字体，跳过图片渲染测试")
	}
	p := &Plugin{i18n: newI18nPlugin(t)}
	ctx := newCmdCtx("/songdle", "chat1", "u1")
	g := &Game{Target: gridTarget, MaxAttempts: 10, Mode: ModeRandom}
	g.SubmitGuess(TrackGuess{Track: gridGuess, Cells: Compare(gridTarget, gridGuess)}, time.Now())
	g.SubmitGuess(TrackGuess{Track: gridTarget, Cells: Compare(gridTarget, gridTarget)}, time.Now())

	img, err := p.renderBoardImage(ctx, g, "❌ 不是这首 · 还剩 8 次")
	if err != nil {
		t.Fatalf("renderBoardImage: %v", err)
	}
	if !bytes.HasPrefix(img, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("输出应为 PNG，实际前 8 字节： % x", img[:min(8, len(img))])
	}
}
