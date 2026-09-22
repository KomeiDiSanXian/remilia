// Package ai toolset_test.go — 会话级工具集稳定策略的回归测试。
//
// 这些测试锁定的是"提示词前缀缓存"的关键不变量：tools 排在请求最前面，
// 集合一变其后的历史整段失效。因此这里断言的重点不是"选得准不准"，
// 而是"什么时候集合不变、变化是否被抑制成稀疏且确定的"。
package ai

import (
	"slices"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// toolSetPlugin 构造启用稳定策略的插件（测试用参数化配置）。
func toolSetPlugin(stickyMax int, ttl time.Duration) *Plugin {
	return &Plugin{cfg: &config.Config{
		ToolSetSticky:    true,
		ToolSetStickyMax: stickyMax,
		ToolSetTTL:       ttl,
		ToolSelectMax:    20,
		ToolBudget:       8000,
	}}
}

// toolSet 构造只关心名称的动作列表（稳定策略不依赖动作内容）。
func toolSet(names ...string) []toolkit.Action {
	out := make([]toolkit.Action, 0, len(names))
	for _, n := range names {
		out = append(out, toolkit.ActionOf(toolkit.Tool{Name: n}))
	}
	return out
}

// selectedNames 提取动作名列表。
func selectedNames(actions []toolkit.Action) []string {
	names := make([]string, 0, len(actions))
	for _, a := range actions {
		names = append(names, a.Spec.Name)
	}
	return names
}

func assertNames(t *testing.T, got []toolkit.Action, want ...string) {
	t.Helper()
	names := selectedNames(got)
	if !slices.Equal(names, want) {
		t.Errorf("tool set mismatch: got %v want %v", names, want)
	}
}

func assertGeneration(t *testing.T, sess *session.Session, want uint64) {
	t.Helper()
	st := sess.ToolSetState()
	if st == nil {
		t.Fatal("expected session tool set state to be recorded")
	}
	if st.Generation != want {
		t.Errorf("expected generation %d, got %d", want, st.Generation)
	}
}

// TestToolSetStickyGrowsMonotonicallyAndKeepsOnRevisit 验证核心不变量：
// 话题切换时集合只增不减（并集），回到旧话题时集合完全不变。
// 后者正是"话题来回往复不产生缓存失效"的来源。
func TestToolSetStickyGrowsMonotonicallyAndKeepsOnRevisit(t *testing.T) {
	p := toolSetPlugin(8, time.Hour)
	sess := &session.Session{ID: "s"}
	avail := toolSet("a", "b", "c", "d", "e", "f")

	first := p.stabilizeToolSet(sess, avail, toolSet("a", "b", "c"))
	assertNames(t, first, "a", "b", "c")
	assertGeneration(t, sess, 1)

	// 切换到新话题：并集（旧工具保留，新工具加入）——不是替换
	second := p.stabilizeToolSet(sess, avail, toolSet("d", "e"))
	assertNames(t, second, "a", "b", "c", "d", "e")
	assertGeneration(t, sess, 2)

	// 回到旧话题：候选是当前集合的子集 → 集合不变（无缓存失效）
	third := p.stabilizeToolSet(sess, avail, toolSet("a", "b", "c"))
	assertNames(t, third, "a", "b", "c", "d", "e")
	assertGeneration(t, sess, 2)

	// 再次往复同样不变
	fourth := p.stabilizeToolSet(sess, avail, toolSet("d", "e"))
	assertNames(t, fourth, "a", "b", "c", "d", "e")
	assertGeneration(t, sess, 2)
}

// TestToolSetStickyIsByteStableAcrossRepeatTurns 验证集合内容不变时返回值
// 逐字节一致（顺序固定为名称升序），这是 LLM 前缀缓存命中的直接前提。
func TestToolSetStickyIsByteStableAcrossRepeatTurns(t *testing.T) {
	p := toolSetPlugin(8, time.Hour)
	sess := &session.Session{ID: "s"}
	avail := toolSet("zeta", "alpha", "mid")
	candidate := toolSet("zeta", "alpha")

	var prev []string
	for turn := range 5 {
		got := selectedNames(p.stabilizeToolSet(sess, avail, candidate))
		if turn > 0 && !slices.Equal(got, prev) {
			t.Fatalf("turn %d: set changed without new tools: %v vs %v", turn, got, prev)
		}
		prev = got
	}
	assertNames(t, p.stabilizeToolSet(sess, avail, candidate), "alpha", "zeta")
}

// TestToolSetStickyCapsFillersByRecency 验证补充项数量受限，且淘汰按最近使用
// 时间（最陈旧先淘汰），超限时是整批回收而不是逐轮抖动。
func TestToolSetStickyCapsFillersByRecency(t *testing.T) {
	p := toolSetPlugin(2, time.Hour)
	now := time.Now()
	sess := &session.Session{ID: "s"}
	sess.SetToolSetState(&session.ToolSetState{
		Names: []string{"a", "b", "c", "d", "e"},
		LastSeen: map[string]time.Time{
			"a": now.Add(-40 * time.Minute),
			"b": now.Add(-30 * time.Minute),
			"c": now.Add(-20 * time.Minute),
			"d": now.Add(-10 * time.Minute),
			"e": now.Add(-5 * time.Minute),
		},
		Generation: 1,
	})
	avail := toolSet("a", "b", "c", "d", "e", "x", "y")

	got := p.stabilizeToolSet(sess, avail, toolSet("x", "y"))
	// 候选 x,y + 最近使用的 2 个补充项 d,e
	assertNames(t, got, "d", "e", "x", "y")
	assertGeneration(t, sess, 2)
}

// TestToolSetStickyDecaysIdleFillers 验证长期未被候选命中的补充项按 TTL 退场，
// 而未到期的补充项保留（不因一轮没选中就丢）。
func TestToolSetStickyDecaysIdleFillers(t *testing.T) {
	p := toolSetPlugin(8, 20*time.Minute)
	now := time.Now()
	sess := &session.Session{ID: "s"}
	sess.SetToolSetState(&session.ToolSetState{
		Names: []string{"a", "b", "c"},
		LastSeen: map[string]time.Time{
			"a": now.Add(-time.Minute),
			"b": now.Add(-2 * time.Hour), // 过期
			"c": now.Add(-time.Minute),   // 未过期
		},
		Generation: 3,
	})
	avail := toolSet("a", "b", "c")

	got := p.stabilizeToolSet(sess, avail, toolSet("a"))
	assertNames(t, got, "a", "c")
	assertGeneration(t, sess, 4)
}

// TestToolSetStickyNeverExceedsAvailable 验证工具集状态不能成为权限旁路：
// 不在本轮可用集合（群策略白名单 / RBAC 过滤后）中的工具立即被剔除，
// 且不会残留在会话状态里。
func TestToolSetStickyNeverExceedsAvailable(t *testing.T) {
	p := toolSetPlugin(8, time.Hour)
	now := time.Now()
	sess := &session.Session{ID: "s"}
	sess.SetToolSetState(&session.ToolSetState{
		Names:      []string{"a", "b", "restricted"},
		LastSeen:   map[string]time.Time{"a": now, "b": now, "restricted": now},
		Generation: 1,
	})
	// restricted 已被 RBAC / 群策略过滤掉
	avail := toolSet("a", "b")

	got := p.stabilizeToolSet(sess, avail, toolSet("a", "b"))
	assertNames(t, got, "a", "b")

	st := sess.ToolSetState()
	for _, n := range st.Names {
		if n == "restricted" {
			t.Errorf("restricted tool must be pruned from session state, got %v", st.Names)
		}
	}
	if _, ok := st.LastSeen["restricted"]; ok {
		t.Error("restricted tool must be pruned from state timestamps")
	}
}

// TestStabilizeToolSetDisabledKeepsLegacyBehavior 验证关闭开关时完全回退旧行为
// （按候选原样返回、不排序、不写状态），便于 A/B 对比缓存收益。
func TestStabilizeToolSetDisabledKeepsLegacyBehavior(t *testing.T) {
	p := &Plugin{cfg: &config.Config{ToolSelectMax: 20, ToolBudget: 8000}}
	sess := &session.Session{ID: "s"}
	avail := toolSet("b", "a")

	got := p.stabilizeToolSet(sess, avail, toolSet("b", "a"))
	if !slices.Equal(selectedNames(got), []string{"b", "a"}) {
		t.Errorf("disabled stabilization must return candidate as-is, got %v", selectedNames(got))
	}
	if sess.ToolSetState() != nil {
		t.Error("disabled stabilization must not record session tool set state")
	}
}

// TestSelectToolsForTurnStickySurvivesTopicShift 集成验证：经完整选择路径
// （打分 + 会话缓存 + 稳定策略）后，话题切换一次即收敛，随后回到旧话题
// 集合不再变化——即"一次工具集切换只损失一次缓存，而不是每轮都损失"。
func TestSelectToolsForTurnStickySurvivesTopicShift(t *testing.T) {
	p := &Plugin{cfg: &config.Config{
		ToolSetSticky:    true,
		ToolSetStickyMax: 8,
		ToolSetTTL:       time.Hour,
		ToolSelectMax:    4,
		ToolBudget:       8000,
	}}
	sess := &session.Session{ID: "s", UserID: "u", ChatID: "c"}
	tools := []toolkit.Tool{
		{Name: "get_weather", Description: "查询天气温度湿度", Categories: []string{"weather"}},
		{Name: "get_bilibili_live", Description: "查询B站UP主直播状态", Categories: []string{"bilibili"}},
		{Name: "roll_dice", Description: "掷骰子检定", Categories: []string{"game"}},
		{Name: "draw_tarot", Description: "塔罗牌占卜", Categories: []string{"game"}},
		{Name: "search_anime", Description: "搜索番剧信息", Categories: []string{"anime"}},
		{Name: "query_minecraft", Description: "查询MC服务器状态", Categories: []string{"game"}},
	}

	selectFor := func(query string) []string {
		sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: query}}
		evt := platform.NewSyntheticEvent("c2c", query)
		ctx := eventctx.NewContextFromEvent(evt, nil)
		return selectedNames(p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools)))
	}

	first := selectFor("今天天气怎么样")
	if len(first) == 0 {
		t.Fatal("expected tools to be selected")
	}
	second := selectFor("看看B站直播开播了吗")
	if len(second) == 0 {
		t.Fatal("expected tools after topic shift")
	}
	// 话题切换后：旧话题工具仍在集合里（并集）
	contains := func(names []string, want string) bool {
		return slices.Contains(names, want)
	}
	if !contains(second, "get_weather") || !contains(second, "get_bilibili_live") {
		t.Errorf("union must keep both topics' tools, got %v", second)
	}

	// 回到旧话题：集合与上一轮完全一致（无新的缓存失效）
	third := selectFor("今天天气怎么样")
	if !slices.Equal(third, second) {
		t.Errorf("returning to the previous topic must not change the tool set:\ngot  %v\nwant %v", third, second)
	}
}
