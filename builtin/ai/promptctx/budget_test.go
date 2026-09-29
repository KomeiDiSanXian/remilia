// budget_test.go — 预算编排的缩量与截断契约。
//
// 冻结：预算缩减不再沿"计数减半"反复重建（每次重建都要重跑一遍
// 检索/嵌入），而是按比例缩量重建一次、仍装不下才截断，而不是整节丢弃。
package promptctx

import (
	"fmt"
	"strings"
	"testing"
)

// TestShrinkSectionRebuildsAtMostOnce 预算缩量最多重建一次，超预算时截断保留。
func TestShrinkSectionRebuildsAtMostOnce(t *testing.T) {
	calls := 0
	// 正文与条数无关（模拟 RAG 会话缓存命中：命中直接返回整段文本）。
	body := strings.Repeat("历史行内容\n", 40)
	sources := []Source{{
		Header: "相关历史消息",
		Limit:  20,
		Shrink: true,
		Body: func(int) string {
			calls++
			return body
		},
	}}

	got := BuildWindowed(sources, 60, "")
	if got == "" {
		t.Fatal("超预算的节应截断保留，而不是整节丢弃")
	}
	if calls > 2 {
		t.Fatalf("缩量应最多重建一次（总构建次数 ≤ 2），实际 %d 次", calls)
	}
	if !strings.Contains(got, "===== 相关历史消息 =====") {
		t.Fatalf("节标题应保留: %q", got)
	}
	// window=60 → 预留后剩余额度 = 60/2 = 30；截断后正文仍须落在额度内。
	trimmed := strings.TrimPrefix(got, "===== 相关历史消息 =====\n")
	if est := EstimateTokens(trimmed); est > 30 {
		t.Fatalf("截断后正文估算 %d 应 ≤ 剩余额度 30（正文 %q）", est, trimmed)
	}
}

// TestShrinkKeepsTailForTimeOrderedSection KeepTail 节超预算时保留末尾（最新）。
func TestShrinkKeepsTailForTimeOrderedSection(t *testing.T) {
	lines := make([]string, 0, 40)
	for i := 1; i <= 40; i++ {
		lines = append(lines, fmt.Sprintf("msg%02d", i))
	}
	// 正文与条数无关，强制走截断兜底。
	body := strings.Join(lines, "\n")
	sources := []Source{{
		Header:   "群聊最近消息",
		Limit:    40,
		Shrink:   true,
		KeepTail: true,
		Body:     func(int) string { return body },
	}}

	got := BuildWindowed(sources, 60, "")
	if !strings.Contains(got, "msg40") {
		t.Fatalf("KeepTail 节应保留最新内容 msg40: %q", got)
	}
	if strings.Contains(got, "msg01") {
		t.Fatalf("KeepTail 节应截掉最旧内容 msg01: %q", got)
	}
}

// TestShrinkDropsWhenBudgetHopeless 预算连一行都放不下时整节丢弃，不产生空节。
func TestShrinkDropsWhenBudgetHopeless(t *testing.T) {
	sources := []Source{{
		Header: "节",
		Limit:  10,
		Shrink: true,
		Body:   func(int) string { return strings.Repeat("长内容", 100) },
	}}
	if got := BuildWindowed(sources, 1, ""); got != "" {
		t.Fatalf("预算耗尽应返回空串，得到 %q", got)
	}
}
