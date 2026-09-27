package promptctx

import (
	"strings"
	"testing"
)

// TestBuildWindowedBudgetIsAuthoritative 冻结装配入口的路径选择语义：
// window > 0 时预算路径是权威的——即便预算装不下任何动态节，也绝不回退到
// 非预算路径（回退会恰在预算最紧时突破窗口）。
func TestBuildWindowedBudgetIsAuthoritative(t *testing.T) {
	sources := []Source{{
		Header: "节",
		Limit:  100,
		Body:   func(int) string { return strings.Repeat("内容", 50) },
	}}

	if got := BuildWindowed(sources, 100000, ""); got == "" {
		t.Fatal("large window should fit the section")
	}

	// 稳定系统提示词已吃掉全部预算 → 动态节丢弃且不回退。
	if got := BuildWindowed(sources, 10, strings.Repeat("系统", 50)); got != "" {
		t.Fatalf("tiny window must drop all sections without falling back, got %q", got)
	}

	// window <= 0 → 非预算路径按配置上限装配。
	if got := BuildWindowed(sources, 0, "ignored"); got == "" {
		t.Fatal("non-budget path should include the section")
	}
}

// TestBuildWindowedSharesParticipationConditions 冻结：两条路径共用同一套
// 参与条件（Enabled == false 的节在两条路径上都不出现）。
func TestBuildWindowedSharesParticipationConditions(t *testing.T) {
	off := false
	sources := []Source{{
		Header:  "节",
		Enabled: func() bool { return off },
		Limit:   10,
		Body:    func(int) string { return "内容" },
	}}
	if got := BuildWindowed(sources, 100000, ""); got != "" {
		t.Fatalf("disabled section must not appear on budget path, got %q", got)
	}
	if got := BuildWindowed(sources, 0, ""); got != "" {
		t.Fatalf("disabled section must not appear on non-budget path, got %q", got)
	}
}
