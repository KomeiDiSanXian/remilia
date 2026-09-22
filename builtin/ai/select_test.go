package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/decision"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestTokenizeText(t *testing.T) {
	tokens := retrieval.TokenizeText("B站 直播 今天天气如何 Weather API")
	// CJK 二元组
	if tokens["天气"] != 1 {
		t.Errorf("expected 天气 bigram, got %v", tokens)
	}
	if tokens["直播"] != 1 {
		t.Errorf("expected 直播 bigram, got %v", tokens)
	}
	// 中英混合脚本：孤立汉字单独成 token
	if tokens["站"] != 1 {
		t.Errorf("expected isolated Han 站 token, got %v", tokens)
	}
	// 英文小写单词
	if tokens["weather"] != 1 || tokens["api"] != 1 {
		t.Errorf("expected lowercase words, got %v", tokens)
	}
}

func TestTokenOverlapMinBias(t *testing.T) {
	a := map[string]float64{"天气": 5, "weather": 1}
	b := map[string]float64{"天气": 2}
	if got := retrieval.TokenOverlap(a, b); got != 2 {
		t.Errorf("expected min overlap 2, got %v", got)
	}
}

func TestJaccardSimilarity(t *testing.T) {
	a := map[string]float64{"a": 1, "b": 1}
	b := map[string]float64{"a": 1, "c": 1}
	if got := retrieval.JaccardSimilarity(a, b); got != 1.0/3.0 {
		t.Errorf("expected 1/3, got %v", got)
	}
	if got := retrieval.JaccardSimilarity(map[string]float64{}, b); got != 0 {
		t.Errorf("expected 0 for empty set, got %v", got)
	}
}

func TestEstimateToolTokens(t *testing.T) {
	tool := toolkit.Tool{
		Name:        "get_weather",
		Description: "查询指定城市的天气情况，返回温度、湿度与风力",
		Parameters: protocol.ToolParamSchema{
			Type: "object",
			Properties: map[string]protocol.ToolParamSchema{
				"city": {Type: "string", Description: "城市名", Enum: []string{"北京", "上海"}},
			},
		},
	}
	if got := decision.EstimateToolTokens(toolkit.ActionOf(tool)); got <= 0 {
		t.Errorf("expected positive estimate, got %d", got)
	}
}

func TestIsGeneralTool(t *testing.T) {
	if !toolkit.IsGeneralTool(toolkit.Tool{}) {
		t.Error("empty categories should be general")
	}
	if !toolkit.IsGeneralTool(toolkit.Tool{Categories: []string{"general"}}) {
		t.Error("explicit general should be general")
	}
	if toolkit.IsGeneralTool(toolkit.Tool{Categories: []string{"weather"}}) {
		t.Error("weather should not be general")
	}
}

func TestScoreToolSignals(t *testing.T) {
	query := "今天天气怎么样"
	qt := retrieval.TokenizeText(query)

	weather := toolkit.Tool{Name: "get_weather", Description: "查询天气温度湿度"}
	dice := toolkit.Tool{Name: "roll_dice", Description: "掷骰子检定"}
	_, _, ws := decision.ScoreToolParts(query, qt, toolkit.ActionOf(weather), false, nil, nil)
	_, _, ds := decision.ScoreToolParts(query, qt, toolkit.ActionOf(dice), false, nil, nil)
	if ws <= ds {
		t.Errorf("weather tool should outscore dice tool: weather=%v dice=%v", ws, ds)
	}

	// 会话热用加成
	_, _, wsUsed := decision.ScoreToolParts(query, qt, toolkit.ActionOf(dice), true, nil, nil)
	if wsUsed <= ds {
		t.Errorf("used tool should get bonus: used=%v unused=%v", wsUsed, ds)
	}
}

func TestSelectToolsForTurnTrimsToMax(t *testing.T) {
	p := &Plugin{cfg: &config.Config{ToolSelectMax: 5, ToolBudget: 8000}}
	tools := make([]toolkit.Tool, 0, 30)
	for i := range 30 {
		tools = append(tools, toolkit.Tool{
			Name:        fmt.Sprintf("tool_%02d", i),
			Description: "misc data provider",
			Categories:  []string{"misc"},
		})
	}
	sess := &session.Session{ID: "s1", UserID: "u", ChatID: "c"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "查询一下天气"}}

	evt := platform.NewSyntheticEvent("c2c", "查询一下天气")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	selected := p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))
	if len(selected) > 5 {
		t.Errorf("expected at most 5 tools, got %d", len(selected))
	}
}

func TestSelectToolsForTurnAlwaysKeepsGeneralAndUsed(t *testing.T) {
	p := &Plugin{cfg: &config.Config{ToolSelectMax: 3, ToolBudget: 8000}}
	sess := &session.Session{ID: "s2", UserID: "u", ChatID: "c"}
	sess.Messages = []protocol.Message{
		{Role: protocol.RoleAssistant, ToolCalls: []protocol.ToolCall{{Name: "roll_dice"}}},
		{Role: protocol.RoleUser, Content: "查天气"},
	}
	tools := []toolkit.Tool{
		{Name: "general_query", Description: "通用查询", Categories: []string{"general"}},
		{Name: "get_weather", Description: "查询天气温度湿度"},
		{Name: "roll_dice", Description: "掷骰子"},
		{Name: "search_anime", Description: "搜索番剧"},
		{Name: "query_minecraft", Description: "查询MC服务器"},
	}

	evt := platform.NewSyntheticEvent("c2c", "查天气")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	selected := p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))
	names := map[string]bool{}
	for _, t := range selected {
		names[t.Spec.Name] = true
	}
	// 必保集：general 工具与会话已用工具
	if !names["general_query"] {
		t.Errorf("general tool should always be kept, got %v", names)
	}
	if !names["roll_dice"] {
		t.Errorf("session-used tool should always be kept, got %v", names)
	}
	// 相关工具
	if !names["get_weather"] {
		t.Errorf("relevant tool get_weather should be selected, got %v", names)
	}
}

func TestSelectToolsForTurnCrossDomain(t *testing.T) {
	p := &Plugin{cfg: &config.Config{ToolSelectMax: 10, ToolBudget: 8000}}
	sess := &session.Session{ID: "s3", UserID: "u", ChatID: "c"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "查一下B站直播和今天天气"}}
	tools := []toolkit.Tool{
		{Name: "get_bilibili_live_status", Description: "查询B站UP主直播状态"},
		{Name: "get_weather", Description: "查询天气温度湿度"},
		{Name: "roll_dice", Description: "掷骰子检定"},
		{Name: "draw_tarot", Description: "塔罗牌占卜"},
	}

	evt := platform.NewSyntheticEvent("c2c", "查一下B站直播和今天天气")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	selected := p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))
	names := map[string]bool{}
	for _, t := range selected {
		names[t.Spec.Name] = true
	}
	// 跨域：B站与天气两个域的工具都被选中
	if !names["get_bilibili_live_status"] || !names["get_weather"] {
		t.Errorf("cross-domain tools should both be selected, got %v", names)
	}
}

func TestSelectToolsForTurnBudget(t *testing.T) {
	p := &Plugin{cfg: &config.Config{ToolSelectMax: 5, ToolBudget: 60}}
	tools := make([]toolkit.Tool, 0, 10)
	for i := range 10 {
		desc := strings.Repeat("描述信息内容 ", 10) // 每条描述 ~190 字节
		tools = append(tools, toolkit.Tool{
			Name:        fmt.Sprintf("tool_extra_desc_%02d", i),
			Description: desc,
			Categories:  []string{"misc"},
		})
	}
	sess := &session.Session{ID: "s4", UserID: "u", ChatID: "c"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "随便查点什么"}}

	evt := platform.NewSyntheticEvent("c2c", "随便查点什么")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	selected := p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))
	if len(selected) >= len(tools) {
		t.Errorf("budget should cap tool count, got %d of %d", len(selected), len(tools))
	}
}

// mockEmbedder 计数调用次数的假嵌入器，用于缓存命中验证。
type mockEmbedder struct {
	calls int
	vec   []float32
	err   error
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = m.vec
	}
	return out, nil
}

func (m *mockEmbedder) Model() string { return "mock" }

func TestSelectToolsForTurnCacheHit(t *testing.T) {
	emb := &mockEmbedder{vec: []float32{1, 0, 0}}
	p := &Plugin{
		cfg: &config.Config{ToolSelectMax: 5, ToolBudget: 8000},
		emb: retrieval.NewTextVectorCache(emb),
	}
	sess := &session.Session{ID: "s5", UserID: "u", ChatID: "c"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "查一下天气怎么样"}}
	tools := []toolkit.Tool{
		{Name: "get_weather", Description: "查询天气温度湿度"},
		{Name: "roll_dice", Description: "掷骰子检定"},
		{Name: "search_anime", Description: "搜索番剧"},
	}

	evt := platform.NewSyntheticEvent("c2c", "查一下天气怎么样")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	first := p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))
	if emb.calls != 2 { // 1 次工具批量嵌入 + 1 次查询嵌入
		t.Errorf("expected 2 embed calls on first selection, got %d", emb.calls)
	}

	// 相似话题（Jaccard 命中）：复用缓存，不再发起嵌入
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "查一下今天的天气怎么样"}}
	second := p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))
	if emb.calls != 2 {
		t.Errorf("expected cache hit (no new embed calls), got %d calls", emb.calls)
	}
	if len(first) != len(second) {
		t.Errorf("cache result length mismatch: %d vs %d", len(first), len(second))
	}
}

func TestSelectToolsForTurnCacheMissOnTopicShift(t *testing.T) {
	emb := &mockEmbedder{vec: []float32{1, 0, 0}}
	p := &Plugin{
		cfg: &config.Config{ToolSelectMax: 5, ToolBudget: 8000},
		emb: retrieval.NewTextVectorCache(emb),
	}
	sess := &session.Session{ID: "s6", UserID: "u", ChatID: "c"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "B站直播开播了吗"}}
	tools := []toolkit.Tool{
		{Name: "get_bilibili_live_status", Description: "查询B站UP主直播状态"},
		{Name: "roll_dice", Description: "掷骰子检定"},
	}

	evt := platform.NewSyntheticEvent("c2c", "B站直播开播了吗")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))

	// 完全切换话题：缓存未命中，重新嵌入查询
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "给我掷个骰子检定一下"}}
	evt2 := platform.NewSyntheticEvent("c2c", "给我掷个骰子检定一下")
	ctx2 := eventctx.NewContextFromEvent(evt2, nil)
	p.selectToolsForTurn(ctx2, sess, toolkit.ActionsOf(tools))
	if emb.calls < 3 {
		t.Errorf("expected re-embed on topic shift, got %d calls", emb.calls)
	}
}

func TestSelectToolsForTurnEmbeddingFallback(t *testing.T) {
	emb := &mockEmbedder{vec: []float32{1, 0, 0}, err: context.DeadlineExceeded}
	p := &Plugin{
		cfg: &config.Config{ToolSelectMax: 5, ToolBudget: 8000},
		emb: retrieval.NewTextVectorCache(emb),
	}
	sess := &session.Session{ID: "s7", UserID: "u", ChatID: "c"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "查一下天气"}}
	tools := []toolkit.Tool{
		{Name: "get_weather", Description: "查询天气温度湿度"},
		{Name: "roll_dice", Description: "掷骰子检定"},
	}

	evt := platform.NewSyntheticEvent("c2c", "查一下天气")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	// embedding 失败不应中断选择，关键词打分仍返回结果
	selected := p.selectToolsForTurn(ctx, sess, toolkit.ActionsOf(tools))
	if len(selected) == 0 {
		t.Fatal("expected keyword fallback selection despite embedding failure")
	}
}

func TestToolEmbeddingText(t *testing.T) {
	text := decision.ToolEmbeddingText(toolkit.ActionOf(toolkit.Tool{Name: "get_weather", Description: "查询天气", Categories: []string{"weather"}}))
	if !strings.Contains(text, "get_weather") || !strings.Contains(text, "查询天气") || !strings.Contains(text, "weather") {
		t.Errorf("embedding text should include name/desc/categories: %q", text)
	}
}
