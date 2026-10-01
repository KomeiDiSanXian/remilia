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
