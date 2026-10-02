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
	if !strings.Contains(out, "还没有任何探测") {
		t.Errorf("空提示板应引导玩家探测，实际输出:\n%s", out)
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
