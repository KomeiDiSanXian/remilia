// replyjudge_test.go — "judge" 档的判定 prompt/解析纯函数测试。
package runtime

import (
	"strings"
	"testing"
)

func TestBuildReplyJudgeMessage(t *testing.T) {
	msg := BuildReplyJudgeMessage("甲: 今晚吃啥\n机器人: 我建议酸菜鱼", "我的快递单号是啥", "乙")
	if !strings.Contains(msg, "甲: 今晚吃啥") ||
		!strings.Contains(msg, "机器人: 我建议酸菜鱼") ||
		!strings.Contains(msg, "【新消息】乙：我的快递单号是啥") {
		t.Fatalf("judge message shape wrong: %q", msg)
	}
}

func TestBuildReplyJudgeMessageNoWindow(t *testing.T) {
	msg := BuildReplyJudgeMessage("", "有人吗", "阿丙")
	if !strings.Contains(msg, "【新消息】阿丙：有人吗") {
		t.Fatalf("empty window message wrong: %q", msg)
	}
}

func TestParseReplyVerdictJSON(t *testing.T) {
	cases := []struct {
		text       string
		want       bool // reply?
		wantParsed bool
	}{
		{`{"verdict": "reply", "reason": "需要帮忙查快递"}`, true, true},
		{`{"verdict": "silent", "reason": "群友互聊"}`, false, true},
		{"前置文本 {\"verdict\":\"reply\"} 后缀", true, true},
		{`{"verdict": "不知道"}`, false, false}, // 非法值 → 不可解析
	}
	for i, tc := range cases {
		got, ok := ParseReplyVerdict(tc.text)
		if ok != tc.wantParsed || (ok && got.Reply != tc.want) {
			t.Errorf("case %d: ParseReplyVerdict(%q) = %+v,%v; want reply=%v parsed=%v",
				i, tc.text, got, ok, tc.want, tc.wantParsed)
		}
	}
}

func TestParseReplyVerdictKeywords(t *testing.T) {
	if v, ok := ParseReplyVerdict("reply"); !ok || !v.Reply {
		t.Errorf("bare reply should parse: %+v %v", v, ok)
	}
	if v, ok := ParseReplyVerdict("silent"); !ok || v.Reply {
		t.Errorf("bare silent should parse: %+v %v", v, ok)
	}
	// "不回复" 以不回关键词命中（"不回" 是词表内前缀）；ok=true、Reply=false。
	if v, ok := ParseReplyVerdict("不回复"); !ok || v.Reply {
		t.Errorf("chinese negate '不回复' should be silent, got %+v %v", v, ok)
	}
	if _, ok := ParseReplyVerdict("完全无关的输出"); ok {
		t.Error("unparsable output must be treated as unusable")
	}
	if _, ok := ParseReplyVerdict(""); ok {
		t.Error("empty output must be unusable")
	}
}

func TestBuildReplyJudgeWindowOrderAndBotFlag(t *testing.T) {
	// 输入与 QueryChat 一致（最新在前）；输出按时间顺序、最新挂末尾。
	window := BuildReplyJudgeWindow([]ChatWindowEntry{
		{Content: "早上好各位", Bot: true}, // newest
		{SenderName: "乙", Content: "早啊"},
		{SenderName: "甲", Content: "早"}, // oldest
	}, 0)
	lines := strings.Split(window, "\n")
	if len(lines) != 3 || lines[0] != "甲: 早" || lines[2] != "机器人: 早上好各位" {
		t.Fatalf("window must render chronological with bot prefix last, got %q", window)
	}
}

func TestBuildReplyJudgeWindowTruncate(t *testing.T) {
	window := BuildReplyJudgeWindow([]ChatWindowEntry{{SenderName: "甲", Content: strings.Repeat("字", 2000)}}, 10)
	if got := len([]rune(window)); got > 40 {
		t.Errorf("window must be truncated (rune mark included), got %d", got)
	}
	if !strings.Contains(window, "已截断") {
		t.Errorf("truncation marker missing: %q", window)
	}
}
