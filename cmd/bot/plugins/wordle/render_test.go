package wordle

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
)

func TestRenderBoard(t *testing.T) {
	guesses := []Guess{
		{Word: "crate", Marks: Evaluate("crane", "crate")},
		{Word: "eaten", Marks: Evaluate("crane", "eaten")},
	}
	v := boardView{
		Title:       "WORDLE 5x6",
		Length:      5,
		MaxAttempts: 6,
		Guesses:     guesses,
		KeyStates:   KeyStates(guesses, -1),
	}
	data, err := renderBoard(v)
	if err != nil {
		t.Fatalf("renderBoard: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("生成的图片不是合法 PNG: %v", err)
	}
	if img.Bounds().Dx() < 200 || img.Bounds().Dy() < 200 {
		t.Fatalf("图片尺寸过小: %v", img.Bounds())
	}
}

func TestRenderBoard_Invalid(t *testing.T) {
	if _, err := renderBoard(boardView{Length: 0, MaxAttempts: 6}); err == nil {
		t.Fatal("非法尺寸应返回错误")
	}
	if _, err := renderBoard(boardView{Length: 5, MaxAttempts: 0}); err == nil {
		t.Fatal("非法次数应返回错误")
	}
}

func TestBoardRows_Window(t *testing.T) {
	finite := &Game{Length: 5, MaxAttempts: 6}
	if rows, off := boardRows(finite); rows != 6 || off != 0 {
		t.Fatalf("有限机会 rows=%d offset=%d, 期望 6/0", rows, off)
	}
	empty := &Game{Length: 5, Unlimited: true}
	if rows, off := boardRows(empty); rows != minUnlimitedRows || off != 0 {
		t.Fatalf("空无限局 rows=%d offset=%d, 期望 %d/0", rows, off, minUnlimitedRows)
	}
	many := &Game{Length: 5, Unlimited: true, Guesses: make([]Guess, 40)}
	rows, off := boardRows(many)
	if rows != maxUnlimitedRows {
		t.Fatalf("无限局行数上限应为 %d，实际 %d", maxUnlimitedRows, rows)
	}
	if off+rows != len(many.Guesses)+1 {
		t.Fatalf("窗口应覆盖最新一行: offset=%d rows=%d", off, rows)
	}
}

// TestRenderBoardText_UnlimitedWindow 验证无限机会只绘制最近的滑动窗口，
// 且最新一行始终可见、窗口外的旧行不再绘制。
func TestRenderBoardText_UnlimitedWindow(t *testing.T) {
	g := &Game{
		ID:        "u1",
		Length:    5,
		Unlimited: true,
		Answers:   []string{"crane"},
		Solved:    []bool{false},
	}
	for i := range 20 {
		w := strings.Repeat(string(rune('a'+i)), 5)
		if i == 19 {
			w = "crane"
		}
		g.Guesses = append(g.Guesses, Guess{Word: w, Marks: Evaluate("crane", w)})
	}
	v := gameBoardView(g)
	if v.MaxAttempts != maxUnlimitedRows || v.RowOffset == 0 {
		t.Fatalf("无限局应使用滑动窗口: rows=%d offset=%d", v.MaxAttempts, v.RowOffset)
	}
	if !strings.Contains(v.Title, "INF") {
		t.Fatalf("无限局标题应含 INF，实际 %q", v.Title)
	}
	text := renderBoardText(v)
	if !strings.Contains(text, "CRANE") {
		t.Fatalf("最新一行应可见:\n%s", text)
	}
	if strings.Contains(text, "AAAAA") {
		t.Fatalf("窗口外的旧行不应绘制:\n%s", text)
	}
}

func TestRenderBoard_MultiBoard(t *testing.T) {
	guesses := []Guess{
		{
			Word:  "crate",
			Marks: Evaluate("crane", "crate"),
			Boards: [][]Mark{
				Evaluate("crane", "crate"),
				Evaluate("slice", "crate"),
				Evaluate("adore", "crate"),
				Evaluate("brine", "crate"),
			},
		},
	}
	v := boardView{
		Title:       "WORDLE 5x6",
		Length:      5,
		MaxAttempts: 6,
		Guesses:     guesses,
		Boards:      4,
		Solved:      []bool{false, false, true, false},
		KeyStates:   KeyStates(guesses, -1),
	}
	data, err := renderBoard(v)
	if err != nil {
		t.Fatalf("renderBoard 多谜底: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("生成的图片不是合法 PNG: %v", err)
	}
	if img.Bounds().Dy() < 400 {
		t.Fatalf("四谜底图片应更高，实际 %v", img.Bounds())
	}

	text := renderBoardText(v)
	if !strings.Contains(text, "WORD 2") || !strings.Contains(text, "WORD 3 SOLVED") {
		t.Errorf("文本降级应标注多棋盘与已解出状态，实际:\n%s", text)
	}
}

func TestRenderBoard_Fog(t *testing.T) {
	guesses := []Guess{
		{Word: "crate", Marks: Evaluate("crane", "crate")},
		{Word: "eaten", Marks: Evaluate("crane", "eaten")},
	}
	v := boardView{
		Length:      5,
		MaxAttempts: 6,
		Guesses:     guesses,
		FogRows:     1,
		KeyStates:   KeyStates(guesses[len(guesses)-1:], -1),
	}
	text := renderBoardText(v)
	if strings.Contains(text, "CRATE") {
		t.Errorf("迷雾模式不应显示被遮住的旧行，实际:\n%s", text)
	}
	if !strings.Contains(text, "EATEN") {
		t.Errorf("迷雾模式应显示最近一行，实际:\n%s", text)
	}
}

func TestRenderBoard_ColorStyles(t *testing.T) {
	v := boardView{
		Title:       "WORDLE 5x6",
		Length:      5,
		MaxAttempts: 6,
		Guesses:     []Guess{{Word: "crate", Marks: Evaluate("crane", "crate")}},
		Styles: [][][]TileStyle{{
			{StyleNear, StyleRepeat, StyleUnknown, StyleAbsent, StyleCorrect},
		}},
		KeyStyles: map[rune]TileStyle{'c': StyleNear, 'r': StyleRepeat, 't': StyleUnknown},
	}
	if _, err := renderBoard(v); err != nil {
		t.Fatalf("renderBoard 颜色样式: %v", err)
	}
	text := renderBoardText(v)
	for _, want := range []string{"🟦", "🟪", "⬛"} {
		if !strings.Contains(text, want) {
			t.Errorf("文本降级应包含 %s，实际:\n%s", want, text)
		}
	}

	// 计分色：整行同色，文本附加 绿数/长度。
	v.Styles = [][][]TileStyle{{{StyleScore, StyleScore, StyleScore, StyleScore, StyleScore}}}
	if _, err := renderBoard(v); err != nil {
		t.Fatalf("renderBoard 计分色: %v", err)
	}
	if got := renderBoardText(v); !strings.Contains(got, "(4/5)") {
		t.Errorf("计分色文本应包含 (4/5)，实际:\n%s", got)
	}
}

func TestRenderBoard_HideKey(t *testing.T) {
	v := boardView{
		Title:       "WORDLE 5x6",
		Length:      5,
		MaxAttempts: 6,
		Guesses:     []Guess{{Word: "crate", Marks: Evaluate("crane", "crate")}},
		HideKey:     true,
		KeyStyles:   map[rune]TileStyle{'c': StyleCorrect},
	}
	if _, err := renderBoard(v); err != nil {
		t.Fatalf("renderBoard 隐藏键盘: %v", err)
	}
}

func TestRenderBoardText(t *testing.T) {
	v := boardView{
		Length:      5,
		MaxAttempts: 2,
		Guesses:     []Guess{{Word: "crate", Marks: Evaluate("crane", "crate")}},
	}
	got := renderBoardText(v)
	if !strings.Contains(got, "CRATE") {
		t.Errorf("文本降级应包含大写单词，实际:\n%s", got)
	}
	if !strings.Contains(got, "🟩") || !strings.Contains(got, "⬜") {
		t.Errorf("文本降级应包含 emoji 方块，实际:\n%s", got)
	}
}
