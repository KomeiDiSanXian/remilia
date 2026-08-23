package knowledgebase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

func TestChunkMarkdown(t *testing.T) {
	md := "# 标题A\n\n第一段内容\n\n## 子标题\n\n第二段内容\n\n# 标题B\n\n第三段内容"
	pieces := chunkMarkdown(md, 20, 0)
	if len(pieces) == 0 {
		t.Fatal("expected pieces")
	}
	// 标题开启新块并作为 heading。
	if pieces[0].heading != "# 标题A" {
		t.Errorf("piece0 heading = %q", pieces[0].heading)
	}
	// 超过 chunkSize 的内容被切分。
	if !strings.Contains(pieces[0].content, "第一段内容") {
		t.Errorf("piece0 content = %q", pieces[0].content)
	}
}

func TestChunkMarkdownOverlap(t *testing.T) {
	long := strings.Repeat("很长的内容用于测试分块。", 50)
	md := "# 标题\n\n" + long
	pieces := chunkMarkdown(md, 30, 10)
	if len(pieces) < 2 {
		t.Fatalf("expected multiple pieces, got %d", len(pieces))
	}
	// 相邻块应有重叠文本。
	if !strings.Contains(pieces[1].content, "内容") {
		t.Errorf("piece1 content = %q", pieces[1].content)
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("如何实现定时任务 scheduler plugin")
	if tokens["如何"] != 1 || tokens["定时"] != 1 {
		t.Errorf("tokens = %v", tokens)
	}
	if tokens["scheduler"] != 1 || tokens["plugin"] != 1 {
		t.Errorf("english tokens missing: %v", tokens)
	}
}

// stubEmbedder 按文本关键词返回可区分的向量（测试用）。
type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		v := make([]float32, 4)
		switch {
		case strings.Contains(text, "命令"):
			v[0] = 1
		case strings.Contains(text, "插件"):
			v[1] = 1
		default:
			v[2] = 1
		}
		out[i] = v
	}
	return out, nil
}

func (stubEmbedder) Model() string { return "stub" }

func newTestPlugin(t *testing.T, index []indexedChunk, embedder ai.Embedder) *Plugin {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "kb.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	p := &Plugin{
		cfg:      defaultConfig(),
		store:    store,
		embedder: embedder,
		index:    index,
	}
	t.Cleanup(func() { _ = store.Close() })
	return p
}

func TestSearchSemantic(t *testing.T) {
	idx := []indexedChunk{
		{Source: "a.md", Heading: "# A", Content: "命令系统使用说明", Vector: []float32{1, 0, 0, 0}},
		{Source: "b.md", Heading: "# B", Content: "插件开发指南", Vector: []float32{0, 1, 0, 0}},
	}
	p := newTestPlugin(t, idx, stubEmbedder{})
	hits, err := p.Search(context.Background(), "命令", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	if hits[0].Source != "a.md" {
		t.Errorf("top hit = %q, want a.md", hits[0].Source)
	}
}

func TestSearchKeywordFallback(t *testing.T) {
	idx := []indexedChunk{
		{Source: "a.md", Heading: "# 命令", Content: "命令系统使用说明"},
		{Source: "b.md", Heading: "# 插件", Content: "插件开发指南"},
	}
	p := newTestPlugin(t, idx, nil) // 无 embedder → 关键词兜底
	hits, err := p.Search(context.Background(), "插件怎么开发", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Source != "b.md" {
		t.Fatalf("hits = %+v, want b.md", hits)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	p := newTestPlugin(t, nil, nil)
	hits, err := p.Search(context.Background(), "   ", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected no hits, got %d", len(hits))
	}
}

func TestStatsText(t *testing.T) {
	idx := []indexedChunk{
		{Source: "a.md", Heading: "# A", Content: "x", Vector: []float32{1, 0}},
	}
	p := newTestPlugin(t, idx, stubEmbedder{})
	p.model = "stub"
	text := p.statsText()
	if !strings.Contains(text, "分块数") || !strings.Contains(text, "stub") {
		t.Errorf("statsText = %q", text)
	}
}

func TestToolDescription(t *testing.T) {
	p := &Plugin{cfg: defaultConfig()}
	if d := p.toolDescription(); strings.Contains(d, "数据源：") {
		t.Errorf("empty source dir should omit 数据源 prefix, got %q", d)
	}
	p.cfg.SourceDir = "docs"
	if d := p.toolDescription(); !strings.Contains(d, "数据源：docs") {
		t.Errorf("default description with source dir = %q", d)
	}
	p.cfg.ToolDescription = "公司内部运维手册知识库"
	if d := p.toolDescription(); d != "公司内部运维手册知识库" {
		t.Errorf("custom description = %q", d)
	}
}

func TestListToolsDisabled(t *testing.T) {
	p := &Plugin{cfg: defaultConfig()} // enabled=false
	if tools := p.ListTools(); tools != nil {
		t.Errorf("expected no tools when disabled, got %+v", tools)
	}
	p.cfg.Enabled = true
	if tools := p.ListTools(); len(tools) != 2 {
		t.Errorf("expected 2 tools when enabled, got %d", len(tools))
	}
}

func TestDefaultConfigMarkdown(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.Markdown {
		t.Error("Markdown should be true by default")
	}
}

func TestScanSourceFilesMissingDir(t *testing.T) {
	files, err := scanSourceFiles(filepath.Join(t.TempDir(), "nope"), nil)
	if err != nil {
		t.Fatalf("scanSourceFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty, got %v", files)
	}
}

func TestScanSourceFilesEmptyDir(t *testing.T) {
	files, err := scanSourceFiles(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("scanSourceFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty, got %v", files)
	}
}

func TestScanSourceFilesExcludesAndNonMarkdown(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.md"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("hello"), 0o644)
	os.Mkdir(filepath.Join(root, "notes"), 0o755)
	os.WriteFile(filepath.Join(root, "notes", "c.md"), []byte("hello"), 0o644)

	files, err := scanSourceFiles(root, []string{"notes"})
	if err != nil {
		t.Fatalf("scanSourceFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %v", files)
	}
	if _, ok := files["a.md"]; !ok {
		t.Errorf("expected a.md, got %v", files)
	}
}

func TestSourceDirExists(t *testing.T) {
	if sourceDirExists("") {
		t.Error("empty dir should not exist")
	}
	if sourceDirExists(filepath.Join(t.TempDir(), "nope")) {
		t.Error("missing dir should not exist")
	}
	if !sourceDirExists(t.TempDir()) {
		t.Error("existing dir should exist")
	}
}

func TestStatsTextMissingSourceDir(t *testing.T) {
	p := newTestPlugin(t, nil, nil)
	p.cfg.SourceDir = filepath.Join(t.TempDir(), "nope")
	text := p.statsText()
	if !strings.Contains(text, "数据源目录不存在") {
		t.Errorf("statsText = %q", text)
	}
}

func TestFormatHitsEmpty(t *testing.T) {
	text := FormatHits("abc", nil, 100)
	if !strings.Contains(text, "没有找到") {
		t.Errorf("FormatHits empty = %q", text)
	}
}

func TestDedupNearDuplicates(t *testing.T) {
	hits := []Hit{
		{Source: "a.md", Heading: "# 命令", Content: "命令系统使用说明与参数解析方式", Score: 0.9},
		{Source: "a.md", Heading: "# 命令", Content: "命令系统使用说明与参数解析方式（重叠尾部）", Score: 0.89},
		{Source: "a.md", Heading: "# 命令", Content: "命令系统高级用法与链式调用", Score: 0.7},
		{Source: "b.md", Heading: "# 插件", Content: "插件开发指南", Score: 0.8},
	}
	out := dedupNearDuplicates(hits)
	if len(out) != 3 {
		t.Fatalf("expected 3 after dedup, got %d: %+v", len(out), out)
	}
	if out[0].Content != "命令系统使用说明与参数解析方式" {
		t.Errorf("kept wrong first hit: %q", out[0].Content)
	}
}
