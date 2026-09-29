package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestSelectionCacheRespectsCatalogGeneration 锁定"缓存只在目录代数相同时复用"：
// 同一代数下相似话题命中缓存（不再嵌入）；目录成员变化（代数推进）后缓存立即
// 失效并重新计算——这是动态来源接入后仍能控制嵌入成本与工具段稳定的关键。
func TestSelectionCacheRespectsCatalogGeneration(t *testing.T) {
	emb := &mockEmbedder{vec: []float32{1, 0, 0}}
	p := &Plugin{
		cfg: &config.Config{ToolSelectMax: 5, ToolBudget: 8000},
		emb: retrieval.NewTextVectorCache(emb),
		reg: toolkit.NewToolRegistry(),
	}
	for _, tl := range []toolkit.Tool{
		{Name: "get_weather", Description: "查询天气温度湿度"},
		{Name: "roll_dice", Description: "掷骰子检定"},
		{Name: "search_anime", Description: "搜索番剧"},
	} {
		p.registerCatalogTool(tl)
	}

	sess := &session.Session{ID: "gen", UserID: "u", ChatID: "c"}
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "查一下天气怎么样"}}
	evt := platform.NewSyntheticEvent("c2c", "查一下天气怎么样")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	actions := p.catalogSnapshot().Actions()
	p.selectToolsForTurn(ctx, sess, actions)
	first := emb.calls
	if first < 2 {
		t.Fatalf("expected embedding calls on first selection, got %d", first)
	}

	// 相似话题、同一目录代数：复用缓存，不再嵌入。
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "查一下今天的天气怎么样"}}
	p.selectToolsForTurn(ctx, sess, actions)
	if emb.calls != first {
		t.Fatalf("expected cache reuse within same generation, emb calls %d→%d", first, emb.calls)
	}

	// 目录代数推进（来源增删工具）：缓存不再可信，必须重新计算。
	p.catalogGen.Add(1)
	sess.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "查一下今天的天气怎么样"}}
	p.selectToolsForTurn(ctx, sess, actions)
	if emb.calls <= first {
		t.Fatalf("expected cache invalidation after catalog change, emb calls stayed at %d", emb.calls)
	}
}
