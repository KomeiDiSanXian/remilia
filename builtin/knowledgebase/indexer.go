package knowledgebase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// embedBatchSize 单次嵌入请求的文本条数上限（避免请求体过大）。
const embedBatchSize = 32

// chunkPiece 一个 Markdown 分块。
type chunkPiece struct {
	heading string // 所属标题（最近一个 # 标题行）
	content string
}

// loadIndex 从 SQLite 加载全部索引到内存。
func (p *Plugin) loadIndex() error {
	if p.store == nil {
		return nil
	}
	idx, err := p.store.LoadIndex()
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.index = idx
	p.mu.Unlock()
	p.logf("knowledgebase: loaded %d chunks from index", len(idx))
	return nil
}

// rebuildIfChanged 启动后台增量构建：磁盘文件 hash 与已索引不一致的部分重建。
func (p *Plugin) rebuildIfChanged(ctx context.Context) {
	if p.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := p.Build(ctx, false); err != nil {
		p.setLastErr(err)
		logger.Warnf("knowledgebase: initial index build failed: %v", err)
		return
	}
	p.logf("knowledgebase: index ready (%d chunks)", p.chunkCount())
}

// Build 扫描数据源并增量更新索引。
// full=true 时清空后全量重建（/kb rebuild）；否则只重建 hash 变化的文件。
func (p *Plugin) Build(ctx context.Context, full bool) error {
	if p.store == nil {
		return nil
	}
	p.building.Store(true)
	defer p.building.Store(false)

	disk, err := scanSourceFiles(p.cfg.SourceDir, p.cfg.ExcludeDirs)
	if err != nil {
		return err
	}
	existing, err := p.store.SourceFiles()
	if err != nil {
		return err
	}
	existingMap := make(map[string]SourceFile, len(existing))
	for _, f := range existing {
		existingMap[f.Source] = f
	}

	if full {
		if err := p.store.Clear(); err != nil {
			return err
		}
		existingMap = nil
	}

	paths := make([]string, 0, len(disk))
	for path := range disk {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	added := 0
	for _, path := range paths {
		hash := disk[path]
		if existingMap != nil {
			if old, ok := existingMap[path]; ok && old.Hash == hash {
				continue // 未变化，跳过
			}
		}
		if err := p.buildFile(ctx, path, hash); err != nil {
			logger.Warnf("knowledgebase: index %s failed: %v", path, err)
			continue
		}
		added++
	}

	// 删除磁盘上已不存在的源文件索引。
	removed := 0
	for path := range existingMap {
		if _, ok := disk[path]; !ok {
			if err := p.store.RemoveSource(path); err != nil {
				logger.Warnf("knowledgebase: remove stale source %s failed: %v", path, err)
				continue
			}
			removed++
		}
	}

	if err := p.loadIndex(); err != nil {
		return err
	}
	p.mu.Lock()
	p.builtAt = time.Now()
	p.mu.Unlock()
	p.setLastErr(nil)
	p.logf("knowledgebase: build done (added %d, removed %d, total %d chunks)", added, removed, p.chunkCount())
	return nil
}

// buildFile 读取单个 Markdown 文件，分块并嵌入后写入索引。
func (p *Plugin) buildFile(ctx context.Context, path, hash string) error {
	data, err := os.ReadFile(filepath.Join(p.cfg.SourceDir, path))
	if err != nil {
		return err
	}
	pieces := chunkMarkdown(string(data), p.cfg.ChunkSize, p.cfg.ChunkOverlap)
	if len(pieces) == 0 {
		// 空文件也记录 hash，避免每次启动重复处理。
		return p.store.ReplaceSource(path, hash, nil)
	}

	texts := make([]string, len(pieces))
	for i, pc := range pieces {
		if pc.heading != "" {
			texts[i] = pc.heading + "\n" + pc.content
		} else {
			texts[i] = pc.content
		}
	}

	chunks := make([]Chunk, 0, len(pieces))
	if p.embedder != nil {
		vecs, err := p.embedBatch(ctx, texts)
		if err != nil {
			// 嵌入失败：降级为纯文本索引（关键词检索仍可用），不阻塞建索引。
			logger.Warnf("knowledgebase: embed %s failed: %v, indexing without vectors", path, err)
			for i, pc := range pieces {
				chunks = append(chunks, Chunk{
					Source: path, Heading: pc.heading, Content: pc.content, Hash: hashOf(texts[i]),
				})
			}
		} else {
			for i, pc := range pieces {
				chunks = append(chunks, Chunk{
					Source: path, Heading: pc.heading, Content: pc.content, Hash: hashOf(texts[i]),
					Vector: encodeVector(vecs[i]), Dim: len(vecs[i]), Model: p.model,
				})
			}
		}
	} else {
		for i, pc := range pieces {
			chunks = append(chunks, Chunk{
				Source: path, Heading: pc.heading, Content: pc.content, Hash: hashOf(texts[i]),
			})
		}
	}
	return p.store.ReplaceSource(path, hash, chunks)
}

// embedBatch 分批嵌入文本（控制单次请求体大小）。
func (p *Plugin) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	var out [][]float32
	for i := 0; i < len(texts); i += embedBatchSize {
		end := min(i+embedBatchSize, len(texts))
		vecs, err := p.embedder.Embed(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

// chunkCount 返回内存索引分块数。
func (p *Plugin) chunkCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.index)
}

// scanSourceFiles 扫描数据源目录中的 .md 文件，返回相对路径 → 内容 hash。
func scanSourceFiles(root string, excludeDirs []string) (map[string]string, error) {
	excluded := make(map[string]bool, len(excludeDirs))
	for _, d := range excludeDirs {
		excluded[filepath.Clean(d)] = true
	}
	out := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 单个目录/文件不可读时跳过并告警，不中断整个构建。
			if d != nil && d.IsDir() {
				logger.Warnf("knowledgebase: skip unreadable dir %s: %v", path, err)
				return fs.SkipDir
			}
			logger.Warnf("knowledgebase: skip unreadable entry %s: %v", path, err)
			return nil
		}
		if d.IsDir() {
			if path != root {
				rel, relErr := filepath.Rel(root, path)
				if relErr == nil && excluded[rel] {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			logger.Warnf("knowledgebase: read %s failed: %v", path, err)
			return nil
		}
		out[filepath.ToSlash(rel)] = hashOfBytes(data)
		return nil
	})
	if os.IsNotExist(err) {
		logger.Warnf("knowledgebase: source dir %s not found, index empty", root)
		return map[string]string{}, nil
	}
	return out, err
}

// sourceDirExists 判断数据源目录是否存在。
func sourceDirExists(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// chunkMarkdown 将 Markdown 按标题与长度限制切块。
// 标题行开启新块并作为该块的 heading；内容超过 chunkSize 时按行切分，
// 相邻块保留 overlap rune 的尾部文本保证语义连续性。
func chunkMarkdown(md string, chunkSize, overlap int) []chunkPiece {
	if chunkSize <= 0 {
		chunkSize = 600
	}
	if overlap < 0 {
		overlap = 0
	}
	var pieces []chunkPiece
	var buf []string
	var curLen int
	curHeading := ""

	flush := func() {
		text := strings.TrimSpace(strings.Join(buf, "\n"))
		if text != "" {
			pieces = append(pieces, chunkPiece{heading: curHeading, content: text})
		}
		buf = nil
		curLen = 0
	}

	for line := range strings.SplitSeq(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if isMarkdownHeading(trimmed) {
			flush()
			curHeading = trimmed
			continue
		}
		if trimmed == "" {
			if len(buf) > 0 {
				buf = append(buf, "")
			}
			continue
		}
		// 单行超长（无换行的大段落/代码）：按 rune 直接切成多块。
		if utf8.RuneCountInString(line) > chunkSize {
			flush()
			for _, seg := range splitLongLine(line, chunkSize, overlap) {
				pieces = append(pieces, chunkPiece{heading: curHeading, content: seg})
			}
			continue
		}
		lineLen := utf8.RuneCountInString(line)
		if len(buf) > 0 && curLen+lineLen > chunkSize {
			prev := strings.Join(buf, "\n")
			flush()
			if overlap > 0 && utf8.RuneCountInString(prev) > overlap {
				tail := lastNRunes(prev, overlap)
				buf = append(buf, tail)
				curLen = utf8.RuneCountInString(tail)
			}
		}
		buf = append(buf, line)
		curLen += lineLen
	}
	flush()
	return pieces
}

// splitLongLine 将单行文本按 rune 长度切块，相邻块保留 overlap 长度重叠。
func splitLongLine(line string, size, overlap int) []string {
	runes := []rune(line)
	var segs []string
	start := 0
	for start < len(runes) {
		end := min(start+size, len(runes))
		segs = append(segs, string(runes[start:end]))
		if end == len(runes) {
			break
		}
		next := end - overlap
		if next <= start {
			next = start + 1
		}
		start = next
	}
	return segs
}

// isMarkdownHeading 判断是否为 Markdown 标题行（# 开头）。
func isMarkdownHeading(line string) bool {
	if line == "" || line[0] != '#' {
		return false
	}
	for _, r := range line {
		if r != '#' {
			return r == ' ' || r == '\t'
		}
	}
	return false
}

// lastNRunes 返回字符串末尾最多 n 个 rune。
func lastNRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

func hashOf(s string) string {
	return hashOfBytes([]byte(s))
}

func hashOfBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
