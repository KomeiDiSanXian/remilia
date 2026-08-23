// 检索契约测试（env-gated）：固定数据集 + 真实 embedding 服务回归。
//
// 设置 REMILIA_EMBED_TEST_URL（如 http://10.0.0.20:8080/v1）后运行，
// 输出工具选择 / RAG / 记忆三类检索的指标（Top1 / Recall@k / MRR），
// 并断言"embedding 不劣于纯关键词"。未设置环境变量时自动跳过，
// 不影响常规 CI。
package ai

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// retrievalFixture 检索契约数据集（testdata/retrieval_cases.json）。
type retrievalFixture struct {
	Tools []struct {
		Name        string `json:"name"`
		Category    string `json:"category"`
		Description string `json:"description"`
	} `json:"tools"`
	ToolCases []struct {
		Query    string   `json:"query"`
		Expected []string `json:"expected"`
	} `json:"tool_cases"`
	RAGCorpus []string `json:"rag_corpus"`
	RAGCases  []struct {
		Query    string   `json:"query"`
		Expected []string `json:"expected"`
	} `json:"rag_cases"`
	MemoryFacts []string `json:"memory_facts"`
	MemoryCases []struct {
		Query    string   `json:"query"`
		Expected []string `json:"expected"`
	} `json:"memory_cases"`
}

func loadRetrievalFixture(t *testing.T) *retrievalFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/retrieval_cases.json")
	if err != nil {
		t.Fatalf("load retrieval fixture: %v", err)
	}
	var fx retrievalFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse retrieval fixture: %v", err)
	}
	return &fx
}

// rankMetrics 从排名列表计算 Top1 / Recall@k / MRR。
// correct[i] 为第 i 条查询的正确条目下标（-1 表示无）。
func rankMetrics(ranks [][]int, correct []int) (top1 float64, recall map[int]float64, mrr float64) {
	recall = map[int]float64{3: 0, 5: 0, 10: 0}
	n := len(ranks)
	for i, r := range ranks {
		ci := correct[i]
		if ci < 0 {
			continue
		}
		pos := -1
		for j, id := range r {
			if id == ci {
				pos = j
				break
			}
		}
		if pos == 0 {
			top1++
		}
		if pos >= 0 {
			mrr += 1.0 / float64(pos+1)
		}
		for k := range recall {
			if pos >= 0 && pos < k {
				recall[k]++
			}
		}
	}
	top1 /= float64(n)
	mrr /= float64(n)
	for k := range recall {
		recall[k] /= float64(n)
	}
	return
}

// rankToolsByWeight 用给定 embedding 权重对全部查询打分配序。
func rankToolsByWeight(fx *retrievalFixture, toolTexts []string, toolVecs [][]float32,
	queryVecs [][]float32, weight float64) (ranks [][]int, correct []int) {
	nameIdx := make(map[string]int, len(fx.Tools))
	for i, t := range fx.Tools {
		nameIdx[t.Name] = i
	}
	for qi, tc := range fx.ToolCases {
		qt := tokenizeText(tc.Query)
		scored := make([]struct {
			id   int
			name string
			kw   float64
			cos  float64
		}, 0, len(fx.Tools))
		for ti, t := range fx.Tools {
			kw, cos, _ := scoreToolParts(tc.Query, qt, Tool{
				Name: t.Name, Description: t.Description, Categories: []string{t.Category},
			}, false, queryVecs[qi], toolVecs[ti])
			scored = append(scored, struct {
				id   int
				name string
				kw   float64
				cos  float64
			}{ti, t.Name, kw, cos})
		}
		sort.Slice(scored, func(i, j int) bool {
			si := scored[i].kw + weight*scored[i].cos
			sj := scored[j].kw + weight*scored[j].cos
			if si != sj {
				return si > sj
			}
			return scored[i].name < scored[j].name
		})
		row := make([]int, len(scored))
		for i, s := range scored {
			row[i] = s.id
		}
		ranks = append(ranks, row)
		correct = append(correct, nameIdx[tc.Expected[0]])
	}
	return
}

func TestRetrievalContractLive(t *testing.T) {
	baseURL := os.Getenv("REMILIA_EMBED_TEST_URL")
	if baseURL == "" {
		t.Skip("REMILIA_EMBED_TEST_URL not set; skipping live retrieval contract test")
	}
	model := os.Getenv("REMILIA_EMBED_TEST_MODEL")
	if model == "" {
		model = "Qwen3-Embedding-0.6B-Q8_0.gguf"
	}
	fx := loadRetrievalFixture(t)
	emb := newOpenAIEmbedder(baseURL, "", model)
	if emb == nil {
		t.Fatalf("invalid embedding base url %q", baseURL)
	}
	ctx := context.Background()

	// ---- 工具选择 ----
	toolTexts := make([]string, len(fx.Tools))
	for i, t := range fx.Tools {
		toolTexts[i] = toolEmbeddingText(Tool{
			Name: t.Name, Description: t.Description, Categories: []string{t.Category},
		})
	}
	toolVecs, err := emb.Embed(ctx, toolTexts)
	if err != nil {
		t.Fatalf("embed tool texts: %v", err)
	}
	queries := make([]string, len(fx.ToolCases))
	for i, tc := range fx.ToolCases {
		queries[i] = tc.Query
	}
	queryVecs, err := emb.Embed(ctx, queries)
	if err != nil {
		t.Fatalf("embed queries: %v", err)
	}

	var kwTop1, kwR5 float64
	t.Logf("== 工具选择指标（%d 条查询 × %d 工具）==", len(fx.ToolCases), len(fx.Tools))
	for _, w := range []float64{0, 0.5, 1, 1.5, 2, 3} {
		ranks, correct := rankToolsByWeight(fx, toolTexts, toolVecs, queryVecs, w)
		top1, rec, mrr := rankMetrics(ranks, correct)
		if w == 0 {
			kwTop1, kwR5 = top1, rec[5]
		}
		t.Logf("w=%-4.1f  Top1=%.1f%%  R@3=%.1f%%  R@5=%.1f%%  R@10=%.1f%%  MRR=%.3f",
			w, top1*100, rec[3]*100, rec[5]*100, rec[10]*100, mrr)
	}
	ranks, correct := rankToolsByWeight(fx, toolTexts, toolVecs, queryVecs, 2.0)
	top1, rec, _ := rankMetrics(ranks, correct)
	if top1 < kwTop1-0.001 || rec[5] < kwR5-0.001 {
		t.Errorf("embedding(w=2) degraded tool metrics: top1 kw=%.3f emb=%.3f, r5 kw=%.3f emb=%.3f",
			kwTop1, top1, kwR5, rec[5])
	}

	// ---- RAG 预筛 + 语义兜底 ----
	ragVecs, err := emb.Embed(ctx, fx.RAGCorpus)
	if err != nil {
		t.Fatalf("embed rag corpus: %v", err)
	}
	ragQueryVecs, err := emb.Embed(ctx, ragQueries(fx))
	if err != nil {
		t.Fatalf("embed rag queries: %v", err)
	}
	prefilterKept, droppedRecovered, droppedTotal := 0, 0, 0
	for qi, rc := range fx.RAGCases {
		qt := tokenizeText(rc.Query)
		kept := false
		for _, m := range fx.RAGCorpus {
			if tokenOverlap(qt, tokenizeText(m)) >= ragKeywordMinScore && m == rc.Expected[0] {
				kept = true
			}
		}
		if kept {
			prefilterKept++
			continue
		}
		droppedTotal++
		// 语义兜底：全部候选按 关键词重叠 + 2×cosine 精排，检查正确文档是否回 Top-3。
		type cand struct {
			text  string
			score float64
		}
		all := make([]cand, 0, len(fx.RAGCorpus))
		for mi, m := range fx.RAGCorpus {
			all = append(all, cand{m, tokenOverlap(qt, tokenizeText(m)) + 2*float64(cosineSimilarity(ragQueryVecs[qi], ragVecs[mi]))})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
		for i := 0; i < len(all) && i < 3; i++ {
			if all[i].text == rc.Expected[0] {
				droppedRecovered++
				break
			}
		}
	}
	t.Logf("== RAG（%d 条查询）==", len(fx.RAGCases))
	t.Logf("关键词预筛保留正确文档: %d/%d；预筛丢弃后语义兜底找回: %d/%d",
		prefilterKept, len(fx.RAGCases), droppedRecovered, droppedTotal)

	// ---- 记忆检索 ----
	memVecs, err := emb.Embed(ctx, fx.MemoryFacts)
	if err != nil {
		t.Fatalf("embed memory facts: %v", err)
	}
	memQueryVecs, err := emb.Embed(ctx, memQueries(fx))
	if err != nil {
		t.Fatalf("embed memory queries: %v", err)
	}
	memKwHit, memEmbHit := 0, 0
	for qi, mc := range fx.MemoryCases {
		qt := tokenizeText(mc.Query)
		found := func(useEmbed bool) bool {
			type cand struct {
				text  string
				score float64
			}
			all := make([]cand, 0, len(fx.MemoryFacts))
			for fi, f := range fx.MemoryFacts {
				sig := tokenOverlap(qt, tokenizeText(f))
				if useEmbed {
					sig += 2 * float64(cosineSimilarity(memQueryVecs[qi], memVecs[fi]))
				}
				if sig <= 0 {
					continue
				}
				all = append(all, cand{f, sig})
			}
			sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
			for i := 0; i < len(all) && i < 3; i++ {
				if all[i].text == mc.Expected[0] {
					return true
				}
			}
			return false
		}
		if found(false) {
			memKwHit++
		}
		if found(true) {
			memEmbHit++
		}
	}
	t.Logf("== 记忆（%d 条查询）==", len(fx.MemoryCases))
	t.Logf("正确事实入 Top-3: 纯关键词 %d/%d，+embedding %d/%d", memKwHit, len(fx.MemoryCases), memEmbHit, len(fx.MemoryCases))
	if memEmbHit < memKwHit {
		t.Errorf("embedding degraded memory retrieval: kw=%d emb=%d", memKwHit, memEmbHit)
	}
}

func ragQueries(fx *retrievalFixture) []string {
	out := make([]string, len(fx.RAGCases))
	for i, c := range fx.RAGCases {
		out[i] = c.Query
	}
	return out
}

func memQueries(fx *retrievalFixture) []string {
	out := make([]string, len(fx.MemoryCases))
	for i, c := range fx.MemoryCases {
		out[i] = c.Query
	}
	return out
}
