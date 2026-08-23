package knowledgebase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

// TestBuildAndSearchSmoke 端到端冒烟测试：对真实 docs 目录建索引并检索。
//
// 设置 REMILIA_KB_EMBED_URL（OpenAI 兼容 /embeddings 端点根地址）后执行；
// 未设置时自动跳过，不影响 CI。
func TestBuildAndSearchSmoke(t *testing.T) {
	embedURL := os.Getenv("REMILIA_KB_EMBED_URL")
	if embedURL == "" {
		t.Skip("REMILIA_KB_EMBED_URL not set, skipping smoke test")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	cfg := defaultConfig()
	cfg.SourceDir = filepath.Join(repoRoot, "docs")
	cfg.DBPath = filepath.Join(t.TempDir(), "kb.db")

	store, err := OpenStore(cfg.DBPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	p := &Plugin{
		cfg:      cfg,
		store:    store,
		embedder: ai.NewOpenAIEmbedder(embedURL, "", "Qwen3-Embedding-0.6B-Q8_0.gguf"),
	}
	p.model = p.embedder.Model()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := p.Build(ctx, false); err != nil {
		t.Fatalf("Build: %v", err)
	}

	queries := []string{
		"如何实现一个定时任务插件",
		"命令系统如何定义命令",
		"FSM 状态机怎么集成到插件",
		"配置如何热更新",
		"如何实现一个简单的 AI Tool",
	}
	for _, q := range queries {
		hits, err := p.Search(ctx, q, 3)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		t.Logf("query=%q hits=%d", q, len(hits))
		if len(hits) == 0 {
			t.Errorf("query %q returned no hits", q)
			continue
		}
		for i, h := range hits {
			t.Logf("  %d. %s [%.3f] %s", i+1, h.Source, h.Score, h.Heading)
		}
		if hits[0].Score <= 0 {
			t.Errorf("query %q top score = %f", q, hits[0].Score)
		}
	}

	stats := p.statsText()
	if !strings.Contains(stats, "分块数") {
		t.Errorf("statsText = %q", stats)
	}
	fmt.Println(stats)
}
