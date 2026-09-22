// Package ai freeze_test.go — 现状可观测行为的冻结用例。
//
// 对应 docs/notes/27-ai-freeze-checklist.md 的契约与缺口清单：这些断言锁定的是
// 重构不得改变的结果（选择语义、指标名、权重常量、持久化字段、前缀字节），
// 与实现无关——换写法可以，换结果不行。
package ai

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/decision"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/promptctx"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selectionPlugin 构造只关心选择语义的插件（稳定策略关闭，隔离出选择路径）。
func selectionPlugin(max, budget int) *Plugin {
	return &Plugin{cfg: &config.Config{ToolSelectMax: max, ToolBudget: budget}}
}

// selectionSession 构造带一条用户消息的会话（打分依据是最后一条用户消息）。
func selectionSession(id, query string) *session.Session {
	s := &session.Session{ID: id, UserID: "u", ChatID: "c"}
	s.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: query}}
	return s
}

// selectionCtx 构造合成事件上下文。
func selectionCtx(query string) *eventctx.Context {
	return eventctx.NewContextFromEvent(platform.NewSyntheticEvent("c2c", query), nil)
}

// TestSelectBypassWhenToolsWithinMax 冻结现象：工具总数不超过上限且未启用
// embedding 时，选择器直接原样返回，不排序、不裁剪、不发起嵌入（A1）。
func TestSelectBypassWhenToolsWithinMax(t *testing.T) {
	const max = 5
	p := selectionPlugin(max, 8000)

	tools := make([]toolkit.Tool, 0, max)
	for i := range max {
		tools = append(tools, toolkit.Tool{Name: string(rune('a' + i)), Description: "misc"})
	}
	actions := toolkit.ActionsOf(tools)

	got := p.selectToolsForTurn(selectionCtx("随便问问"), selectionSession("bypass", "随便问问"), actions)

	assert.Equal(t, actions, got, "should bypass selection and return input as-is")
}

// TestMandatoryMayExceedSelectMax 冻结现象：默认保留级别的动作在上限之前追加，
// 数量可以超过 ToolSelectMax（A2/A9）。
func TestMandatoryMayExceedSelectMax(t *testing.T) {
	p := selectionPlugin(1, 8000)

	tools := []toolkit.Tool{
		{Name: "gen_a", Categories: []string{"general"}},
		{Name: "gen_b", Categories: nil},
		{Name: "gen_c", Categories: []string{"general"}},
	}
	for i := range 5 {
		tools = append(tools, toolkit.Tool{Name: "misc_" + string(rune('a'+i)), Categories: []string{"misc"}})
	}

	got := p.selectToolsForTurn(selectionCtx("随便问问"), selectionSession("mandatory", "随便问问"), toolkit.ActionsOf(tools))
	names := map[string]bool{}
	for _, tl := range got {
		names[tl.Spec.Name] = true
	}

	assert.GreaterOrEqual(t, len(got), 3, "mandatory actions must not be capped by ToolSelectMax")
	assert.True(t, names["gen_a"] && names["gen_b"] && names["gen_c"],
		"all baseline actions must survive, got %v", names)
}

// TestBudgetDoesNotDropMandatory 冻结现象：token 预算只约束补充项与稳定补充项，
// 不回收默认保留级别的动作（A7）。
func TestBudgetDoesNotDropMandatory(t *testing.T) {
	p := selectionPlugin(20, 1) // 预算小到任何补充项都放不下

	tools := []toolkit.Tool{
		{Name: "gen_a", Categories: []string{"general"}},
		{Name: "gen_b", Categories: []string{"general"}},
	}
	for i := range 5 {
		tools = append(tools, toolkit.Tool{Name: "misc_" + string(rune('a'+i)), Categories: []string{"misc"}})
	}

	got := p.selectToolsForTurn(selectionCtx("随便问问"), selectionSession("budget", "随便问问"), toolkit.ActionsOf(tools))
	names := map[string]bool{}
	for _, tl := range got {
		names[tl.Spec.Name] = true
	}

	assert.True(t, names["gen_a"] && names["gen_b"], "baseline actions must survive tiny budget, got %v", names)
}

// descStrings 渲染收集器的描述符文本（用于断言指标名与标签集合）。
func descStrings(c prometheus.Collector) string {
	ch := make(chan *prometheus.Desc, 8)
	c.Describe(ch)
	close(ch)
	out := ""
	for d := range ch {
		out += d.String() + "\n"
	}
	return out
}

// TestMetricsDescriptorsRegistered 冻结指标族名与标签集合（G2）。
// 指标改名会让生产监控静默"归零"，因此只允许新增，不允许改名。
func TestMetricsDescriptorsRegistered(t *testing.T) {
	got := descStrings(llmCalls) +
		descStrings(llmLatency) +
		descStrings(llmTokens) +
		descStrings(toolCalls) +
		descStrings(toolSetChanges) +
		descStrings(toolSetSize)

	for _, want := range []string{
		`fqName: "ai_llm_calls_total"`,
		`fqName: "ai_llm_latency_seconds"`,
		`fqName: "ai_llm_tokens_total"`,
		`fqName: "ai_tool_calls_total"`,
		`fqName: "ai_toolset_changes_total"`,
		`fqName: "ai_toolset_size"`,
	} {
		assert.Contains(t, got, want)
	}
	for _, label := range []string{"model", "result", "tool", "reason"} {
		assert.Contains(t, got, label, "label %q must stay registered", label)
	}
}

// TestToolSelectMaxConfigCompat 冻结旧配置键语义（G1）：
// tool_select_max 保持"短路阈值 + 补充项上限"，不得被重新定义为最终硬上限。
func TestToolSelectMaxConfigCompat(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.ToolSelectMax = 3 // 模拟 tool_select_max=3 生效后的配置

	p := &Plugin{cfg: &cfg}
	tools := []toolkit.Tool{
		{Name: "a", Description: "misc"},
		{Name: "b", Description: "misc"},
		{Name: "c", Description: "misc"},
	}

	// 工具数 == 上限：仍走短路，全量返回（说明它不是"最多选 N 个"的硬上限）。
	actions := toolkit.ActionsOf(tools)
	got := p.selectToolsForTurn(selectionCtx("随便问问"), selectionSession("compat", "随便问问"), actions)
	assert.Equal(t, actions, got)
}

// TestRetrievalWeightsFrozen 冻结检索权重与门槛常量（C1）。
// 这些数值直接决定 Top1 / Recall@K / MRR 基线，改动即回归。
func TestRetrievalWeightsFrozen(t *testing.T) {
	assert.Equal(t, 2.0, retrieval.ScoreEmbedW)
	assert.Equal(t, 2, promptctx.KeywordMinScore)
	assert.Equal(t, 0.6, memoryMergeJaccard)
	assert.Equal(t, 0.5, decision.SelectionCacheJaccard)
	assert.Equal(t, 10*time.Minute, decision.SelectionCacheTTL)
}

// TestSessionPersistenceSchemaSnapshot 冻结会话持久化 schema（G3/G4/GAP-10）：
// 字段集合与"当前没有版本字段"在此锁定；round-trip 语义由
// agentstate_test.go 的 TestAgentStatePersistenceRoundTrip 覆盖。
func TestSessionPersistenceSchemaSnapshot(t *testing.T) {
	want := []string{
		"ID", "UserID", "ChatID", "Messages", "CallCount", "ToolCount",
		"Plan", "PendingImages", "CreatedAt", "UpdatedAt",
	}

	typ := reflect.TypeFor[session.Record]()
	got := make([]string, 0, typ.NumField())
	for field := range typ.Fields() {
		got = append(got, field.Name)
	}

	assert.Equal(t, want, got, "persisted field set must stay frozen")
	assert.NotContains(t, got, "SchemaVersion", "no schema version field yet")
}

// toolsRequestBytes 返回工具列表在请求中的序列化字节（协议层视角）。
func toolsRequestBytes(t *testing.T, actions []toolkit.Action) string {
	t.Helper()
	b, err := json.Marshal(toOpenAITools(actions))
	require.NoError(t, err)
	return string(b)
}

// TestToolSetChangeCreatesDistinctToolsBytes 冻结前缀边界（D4/N4）：
// 集合不变时 tools 段字节完全相同；集合变化必须产生不同的字节边界。
// 断言的是请求字节，而不是 Provider 的缓存命中（后者不由本插件保证）。
func TestToolSetChangeCreatesDistinctToolsBytes(t *testing.T) {
	base := []toolkit.Tool{{Name: "a", Description: "d", Parameters: protocol.ToolParamSchema{Type: "object"}}}
	grown := []toolkit.Tool{
		{Name: "a", Description: "d", Parameters: protocol.ToolParamSchema{Type: "object"}},
		{Name: "b", Description: "d", Parameters: protocol.ToolParamSchema{Type: "object"}},
	}

	assert.Equal(t, toolsRequestBytes(t, toolkit.ActionsOf(base)), toolsRequestBytes(t, toolkit.ActionsOf(base)),
		"same tool set must serialize to identical bytes")
	assert.NotEqual(t, toolsRequestBytes(t, toolkit.ActionsOf(base)), toolsRequestBytes(t, toolkit.ActionsOf(grown)),
		"tool set change must produce a distinct prefix boundary")
}

// TestToolSetGenerationStableOnRepeatedCandidate 冻结集合代数（N5/A13）：
// 候选与可用集都不变时，重复收敛不得改变集合、不得自增代数
// （代数自增会被 toolset_changes_total 记成抖动）。
func TestToolSetGenerationStableOnRepeatedCandidate(t *testing.T) {
	p := toolSetPlugin(8, time.Hour)
	sess := &session.Session{ID: "generation"}
	avail := toolSet("alpha", "beta")
	candidate := toolSet("alpha")

	p.stabilizeToolSet(sess, avail, candidate)
	gen := sess.ToolSetState().Generation

	for range 3 {
		p.stabilizeToolSet(sess, avail, candidate)
	}

	assertGeneration(t, sess, gen)
}

// TestEmptyCategoryIsMandatory 冻结必保判据（A3/GAP-4）：Categories 为空与显式
// ["general"] 都算通用（必保），其他类别不算。
func TestEmptyCategoryIsMandatory(t *testing.T) {
	cases := []struct {
		name       string
		categories []string
		want       bool
	}{
		{"nil", nil, true},
		{"empty", []string{}, true},
		{"explicit general", []string{toolkit.CategoryGeneral}, true},
		{"general among others", []string{"web", toolkit.CategoryGeneral}, true},
		{"other category", []string{"web"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, toolkit.IsGeneralTool(toolkit.Tool{Name: "x", Categories: tc.categories}))
		})
	}
}

// TestEmbeddingCacheNoReembed 冻结嵌入缓存（A6）：同一 query 连续两次选择，
// 嵌入调用次数不增加（以调用次数断言，而不是耗时）。
func TestEmbeddingCacheNoReembed(t *testing.T) {
	emb := &mockEmbedder{vec: []float32{1, 0, 0}}
	p := &Plugin{cfg: &config.Config{ToolSelectMax: 5, ToolBudget: 8000}, emb: retrieval.NewTextVectorCache(emb)}
	sess := selectionSession("no-reembed", "查一下天气怎么样")
	ctx := selectionCtx("查一下天气怎么样")
	tools := []toolkit.Tool{
		{Name: "get_weather", Description: "查询天气温度湿度"},
		{Name: "roll_dice", Description: "掷骰子检定"},
		{Name: "search_anime", Description: "搜索番剧"},
	}

	p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))
	first := emb.calls
	require.Equal(t, 2, first, "首次选择应为 1 次工具批量嵌入 + 1 次查询嵌入")

	p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))
	assert.Equal(t, first, emb.calls, "同一 query 再次选择不得重复请求嵌入")
}

// TestToolSetDeterministicOrdering 冻结顺序与字节稳定性（A13/N4）：
// 输入顺序决定输出顺序（不重新按名称排序）；相同输入重复选择结果与序列化
// 字节完全一致；注册表按名称升序输出，因此生产路径的输入顺序是确定的。
func TestToolSetDeterministicOrdering(t *testing.T) {
	reg := toolkit.NewToolRegistry()
	for _, name := range []string{"gamma", "alpha", "beta"} {
		reg.Register(toolkit.Tool{Name: name, Categories: []string{"misc"}, Description: "d"})
	}
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, toolNamesOf(reg.Actions()),
		"注册表必须按名称升序输出，保证跨请求输入顺序稳定")

	tools := []toolkit.Tool{
		{Name: "gamma", Categories: []string{toolkit.CategoryGeneral}, Description: "d"},
		{Name: "alpha", Categories: []string{toolkit.CategoryGeneral}, Description: "d"},
		{Name: "beta", Categories: []string{toolkit.CategoryGeneral}, Description: "d"},
	}
	p := selectionPlugin(1, 8000) // 工具数超过上限，强制走选择路径

	actions := toolkit.ActionsOf(tools)
	first := p.selectToolsForTurn(selectionCtx("随便问问"), selectionSession("order-a", "随便问问"), actions)
	second := p.selectToolsForTurn(selectionCtx("随便问问"), selectionSession("order-b", "随便问问"), actions)

	assert.Equal(t, []string{"gamma", "alpha", "beta"}, toolNamesOf(first),
		"输出顺序跟随输入顺序，不按名称重新排序")
	assert.Equal(t, toolNamesOf(first), toolNamesOf(second), "相同输入必须得到相同顺序")
	assert.Equal(t, toolsRequestBytes(t, first), toolsRequestBytes(t, second), "相同集合的序列化字节必须稳定")
}
