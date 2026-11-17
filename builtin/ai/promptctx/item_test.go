// item_test.go — 上下文条目、通道与快照的契约。
//
// 这里冻结三件事：
//   - 信任边界是由类型保证的（Persistent 只能来自 EnvironmentFacts）；
//   - 条目顺序即注入顺序，Persistent 在前；
//   - Render 是唯一的最终渲染实现，节之间以空行分隔、空正文不产生节。
package promptctx

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPersistentItemsOnlyFromFacts 验证 Persistent 项只能由框架事实构造，
// 且正文为空时不产生空节。
func TestPersistentItemsOnlyFromFacts(t *testing.T) {
	f := EnvironmentFacts{
		Now:      time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC),
		Platform: "qq",
	}
	opts := ContextOptions{RuntimeFields: []string{"time", "platform"}}
	items := f.PersistentItems(opts)
	require.Len(t, items, 1)
	assert.Equal(t, Persistent, items[0].Channel())
	assert.Equal(t, RuntimeContextSection, items[0].Section())
	assert.Contains(t, items[0].Body(), "平台: qq")

	// 白名单排除全部字段 → 无条目（不产生只有标题的空节）。
	empty := f.PersistentItems(ContextOptions{RuntimeFields: []string{"nope"}})
	assert.Empty(t, empty)
}

// TestSourceItemsAreAlwaysEphemeral 验证 Source 产出的条目一律是 Ephemeral：
// 群聊窗口/记忆/RAG 不能因为被放进 Builder 就获得系统级权限。
func TestSourceItemsAreAlwaysEphemeral(t *testing.T) {
	src := Source{Header: "群聊最近消息", Body: func(int) string { return "小明: 你好" }}
	snap := Collect([]Source{src}, nil)

	require.Len(t, snap.Items(), 1)
	assert.Equal(t, Ephemeral, snap.Items()[0].Channel())
	assert.Empty(t, snap.Persistent())
	assert.Len(t, snap.Ephemeral(), 1)
}

// TestSnapshotFingerprintIsStable 验证指纹对相同内容稳定、对不同内容不同，
// 且不会因字段边界歧义而碰撞（("ab","c") 与 ("a","bc")）。
func TestSnapshotFingerprintIsStable(t *testing.T) {
	a := newSnapshot([]Item{
		{section: "x", channel: Ephemeral, body: "ab"},
		{section: "x", channel: Ephemeral, body: "c"},
	})
	b := newSnapshot([]Item{
		{section: "x", channel: Ephemeral, body: "ab"},
		{section: "x", channel: Ephemeral, body: "c"},
	})
	assert.Equal(t, a.Fingerprint(), b.Fingerprint(), "相同内容必须得到同一指纹")
	assert.Equal(t, a.Fingerprint(), newSnapshot(a.Items()).Fingerprint(), "重算指纹必须稳定")

	c := newSnapshot([]Item{
		{section: "x", channel: Ephemeral, body: "a"},
		{section: "x", channel: Ephemeral, body: "bc"},
	})
	assert.NotEqual(t, a.Fingerprint(), c.Fingerprint(), "不同切分不得碰撞")

	// 通道不同也算不同内容。
	d := newSnapshot([]Item{{section: "x", channel: Persistent, body: "ab"}})
	e := newSnapshot([]Item{{section: "x", channel: Ephemeral, body: "ab"}})
	assert.NotEqual(t, d.Fingerprint(), e.Fingerprint(), "通道是内容的一部分")
}

// TestSnapshotTokensCountsBodiesOnly 验证 token 用量只统计正文（不含节标题）。
func TestSnapshotTokensCountsBodiesOnly(t *testing.T) {
	body := strings.Repeat("一", 30) // 90 字节 → EstimateTokens = 31
	snap := newSnapshot([]Item{{section: "很长的标题很长的标题", channel: Ephemeral, body: body}})
	assert.Equal(t, EstimateTokens(body), snap.Tokens(), "标题不计入正文用量")
}

// TestSnapshotEmptyAndClone 验证空快照与 Items 的防御性拷贝。
func TestSnapshotEmptyAndClone(t *testing.T) {
	var zero Snapshot
	assert.True(t, zero.Empty())
	assert.Empty(t, zero.Items())
	assert.Equal(t, "", zero.Fingerprint())
	assert.Zero(t, zero.Tokens())

	snap := newSnapshot([]Item{{section: "s", channel: Ephemeral, body: "b"}})
	items := snap.Items()
	items[0].body = "changed"
	assert.Equal(t, "b", snap.Items()[0].Body(), "Items 必须返回副本")
}

// TestRenderJoinsSections 验证渲染：节之间空行分隔、空正文不产生节、顺序不变。
func TestRenderJoinsSections(t *testing.T) {
	items := []Item{
		{section: "甲", channel: Persistent, body: "1"},
		{section: "乙", channel: Ephemeral, body: ""},
		{section: "丙", channel: Ephemeral, body: "3"},
	}
	got := Render(items)
	want := "===== 甲 =====\n1\n\n===== 丙 =====\n3"
	assert.Equal(t, want, got)
}

// TestCollectBudgetedSharesBudgetWithPersistent 验证 Persistent 条目与各节
// 共享同一份窗口预算：加上环境事实不会悄悄突破 context_window。
func TestCollectBudgetedSharesBudgetWithPersistent(t *testing.T) {
	bigBody := strings.Repeat("历", 300) // 900 字节 ≈ 301 token
	src := Source{Header: "相关历史消息", Body: func(int) string { return bigBody }}
	persistent := []Item{{section: RuntimeContextSection, channel: Persistent, body: bigBody}}

	// 窗口 1000：预留 512 输出余量后剩 488，环境事实占掉 301，
	// 历史节只剩 187，必然装不下（会被截断或丢弃）。
	snap := CollectBudgeted([]Source{src}, persistent, 1000, "")
	require.False(t, snap.Empty())
	assert.Len(t, snap.Persistent(), 1, "环境事实优先占用预算")
	if got := snap.Tokens(); got > 1000 {
		t.Errorf("总用量 %d 超出窗口 1000", got)
	}
}

// TestBuildWindowedSnapshotMatchesStringEntry 验证快照入口与字符串入口
// 产出同样的字节（两条公开入口不得分叉）。
func TestBuildWindowedSnapshotMatchesStringEntry(t *testing.T) {
	sources := []Source{
		{Header: "运行时上下文", Body: func(int) string { return "当前时间: 2026-11-17 12:00:00" }},
		{Header: "群聊最近消息", Body: func(int) string { return "小明: 你好" }},
	}
	for _, window := range []int{0, 100000, 50} {
		snapText := BuildWindowedSnapshot(sources, nil, window, "system").Render()
		strText := BuildWindowed(sources, window, "system")
		assert.Equal(t, strText, snapText, "window=%d", window)
	}
}

// TestBuildWindowedSnapshotKeepsPersistentFirst 验证 Persistent 条目排在
// 全部 Ephemeral 节之前（它们随历史回放，必须紧邻对应的 user 消息）。
func TestBuildWindowedSnapshotKeepsPersistentFirst(t *testing.T) {
	sources := []Source{
		{Header: "群聊最近消息", Body: func(int) string { return "小明: 你好" }},
	}
	persistent := []Item{{section: RuntimeContextSection, channel: Persistent, body: "平台: qq"}}
	snap := BuildWindowedSnapshot(sources, persistent, 0, "")

	items := snap.Items()
	require.Len(t, items, 2)
	assert.Equal(t, Persistent, items[0].Channel(), "Persistent 必须排在前面")
	assert.Equal(t, Ephemeral, items[1].Channel())
}
