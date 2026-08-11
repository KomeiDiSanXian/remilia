// Package ai memory.go — 长期事实记忆（自动抽取，LevelDB 持久化）。
//
// 本文件实现 AI 的跨会话长期记忆：
//   - MemoryFact：单条事实（文本 + 更新时间 + 出现次数）
//   - memoryStore：按作用域（user:<id> / group:<id>）存储与检索的 LevelDB 存储，
//     独立目录 data/ai_memory（与群策略的 data/ai 分开，避免 LevelDB 锁冲突）
//   - 去重合并：新事实与已有事实精确相等或关键词 Jaccard ≥ 0.6 时合并
//     （保留原文本、更新次数），避免同一事实反复抽取产生冗余条目
//   - 上限淘汰：每个作用域超过 memory_max_facts 时按次数最少、最旧优先淘汰
//   - 检索：按用户消息关键词对事实打分取 Top-N（复用 select.go 分词器），
//     注入 system prompt 供模型在后续对话中使用
//
// 抽取与注入开关见 memory_enabled（默认关闭，隐私考虑）。
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/kv"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// memoryScopeUser 用户记忆作用域前缀（跨群，键 user:<id>）。
const memoryScopeUser = "user:"

// memoryScopeGroup 群记忆作用域前缀（群内公共事实，键 group:<id>）。
const memoryScopeGroup = "group:"

// memoryMergeJaccard 新事实与已有事实视为同义合并的最小二元组 Jaccard 相似度。
const memoryMergeJaccard = 0.6

// memoryMergeCharRatio 新事实与已有事实视为同义合并的最小字符集合包含度
// （|交集| / min(|A|,|B|)，按去重字符集计算）。
// 短中文句二元组 Jaccard 偏低，字符级包含度作为补充信号：
//
//	"用户喜欢喝咖啡" vs "用户爱喝咖啡" → 6/7 ≈ 0.86（合并）
//	"用户喜欢喝咖啡" vs "用户喜欢玩原神" → 3/7 ≈ 0.43（不合并）
const memoryMergeCharRatio = 0.75

// MemoryFact 一条长期记忆事实。
type MemoryFact struct {
	// Text 事实文本（规范化后的稳定描述）。
	Text string `json:"text"`
	// UpdatedAt 最近一次出现/更新时间。
	UpdatedAt time.Time `json:"updated_at"`
	// Count 累计出现次数（用于排序与淘汰）。
	Count int `json:"count"`
}

// memoryStore 长期记忆存储（内存缓存 + LevelDB 持久化）。
type memoryStore struct {
	mu       sync.RWMutex
	store    *kv.DB
	path     string
	scopes   map[string][]MemoryFact // scope key → 事实列表（时间降序，最新在前）
	maxFacts int

	// emb 文本向量缓存（embedding_base_url 配置时由插件注入）。
	// 启用后 Retrieve 叠加余弦相似度，无关键词重叠也能语义命中；
	// 为 nil 或嵌入失败时维持纯关键词打分。
	emb *textVectorCache

	extractMu   sync.Mutex
	lastExtract map[string]time.Time // scope → 上次抽取时间（节流）
	minInterval time.Duration
}

// OpenMemoryStore 打开长期记忆存储。目录不存在时自动创建。
// 使用独立目录 data/ai_memory，避免与群策略共用 data/ai 触发 LevelDB 锁冲突。
func OpenMemoryStore(dataDir string, maxFacts int, minInterval time.Duration) (*memoryStore, error) {
	dir := filepath.Join(dataDir, "ai_memory")
	db, err := kv.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("open memory kv store: %w", err)
	}
	return &memoryStore{
		store:       db,
		path:        dir,
		scopes:      make(map[string][]MemoryFact),
		maxFacts:    maxFacts,
		lastExtract: make(map[string]time.Time),
		minInterval: minInterval,
	}, nil
}

// SetEmbedder 注入共享文本向量缓存（启用记忆语义检索；nil 或未配置时纯关键词）。
// 与工具选择共用同一实例，相同文本不重复嵌入。
func (m *memoryStore) SetEmbedder(c *textVectorCache) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emb = c
}

// Close 关闭底层存储（插件 Teardown 时调用）。
func (m *memoryStore) Close() {
	if m.store != nil {
		_ = m.store.Close()
	}
}

// Enabled 返回记忆功能是否可用（store 非空）。
func (m *memoryStore) Enabled() bool {
	return m != nil && m.store != nil
}

// userScope 生成用户记忆作用域键。
func userScope(userID string) string { return memoryScopeUser + userID }

// groupScope 生成群记忆作用域键。
func groupScope(chatID string) string { return memoryScopeGroup + chatID }

// load 从存储加载指定作用域的事实列表（懒加载）。
func (m *memoryStore) load(scope string) []MemoryFact {
	m.mu.RLock()
	facts, ok := m.scopes[scope]
	m.mu.RUnlock()
	if ok {
		return facts
	}
	var loaded []MemoryFact
	if m.store != nil {
		if bytes, err := m.store.Get([]byte(scope)); err == nil {
			if err := json.Unmarshal(bytes, &loaded); err != nil {
				logger.WithError(err).Warn("[AI] Failed to unmarshal memory scope " + scope)
				loaded = nil
			}
		}
	}
	m.mu.Lock()
	if existing, ok := m.scopes[scope]; ok {
		m.mu.Unlock()
		return existing
	}
	m.scopes[scope] = loaded
	m.mu.Unlock()
	return loaded
}

// Facts 返回指定作用域的全部事实副本。
func (m *memoryStore) Facts(scope string) []MemoryFact {
	facts := m.load(scope)
	out := make([]MemoryFact, len(facts))
	copy(out, facts)
	return out
}

// mergeSimilar 判断两条事实是否应合并（精确相等 / 二元组 Jaccard / 字符包含度）。
// 相似合并额外要求长度比例 ≥ 0.5：防止长句与短句（仅共享片段）被错误合并。
func mergeSimilar(a, b string) bool {
	if a == b {
		return true
	}
	if !mergeLengthOK(a, b) {
		return false
	}
	if jaccardSimilarity(tokenizeText(a), tokenizeText(b)) >= memoryMergeJaccard {
		return true
	}
	return charSetContainment(a, b) >= memoryMergeCharRatio
}

// mergeLengthOK 检查两条事实的长度比例（短/长 ≥ 0.5）。
func mergeLengthOK(a, b string) bool {
	la, lb := len([]rune(a)), len([]rune(b))
	if la == 0 || lb == 0 {
		return true
	}
	small, big := min(la, lb), max(la, lb)
	return float64(small)/float64(big) >= 0.5
}

// charSetContainment 计算两个文本去重字符集的包含度：|A∩B| / min(|A|,|B|)。
// 任一字符集为空返回 0。
func charSetContainment(a, b string) float64 {
	setA := make(map[rune]struct{})
	for _, r := range a {
		setA[r] = struct{}{}
	}
	setB := make(map[rune]struct{})
	for _, r := range b {
		setB[r] = struct{}{}
	}
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	inter := 0
	for r := range setA {
		if _, ok := setB[r]; ok {
			inter++
		}
	}
	denom := min(len(setA), len(setB))
	return float64(inter) / float64(denom)
}

// Add 添加一条事实到指定作用域，做去重合并与上限淘汰并持久化。
func (m *memoryStore) Add(scope, text string) {
	text = strings.TrimSpace(text)
	if text == "" || scope == "" {
		return
	}

	m.mu.Lock()
	facts := m.loadLocked(scope)

	merged := false
	for i := range facts {
		existing := &facts[i]
		if mergeSimilar(existing.Text, text) {
			existing.Count++
			existing.UpdatedAt = time.Now()
			merged = true
			break
		}
	}
	if !merged {
		facts = append(facts, MemoryFact{Text: text, UpdatedAt: time.Now(), Count: 1})
	}

	// 按更新时间降序（最新在前），便于截断保留最新
	sort.SliceStable(facts, func(i, j int) bool { return facts[i].UpdatedAt.After(facts[j].UpdatedAt) })

	if m.maxFacts > 0 && len(facts) > m.maxFacts {
		facts = m.evictLocked(facts, len(facts)-m.maxFacts)
	}
	m.scopes[scope] = facts
	m.mu.Unlock()

	m.save(scope, facts)
}

// loadLocked 在持有写锁时加载作用域事实（load 的锁内版本）。
func (m *memoryStore) loadLocked(scope string) []MemoryFact {
	if facts, ok := m.scopes[scope]; ok {
		return facts
	}
	var facts []MemoryFact
	if m.store != nil {
		if bytes, err := m.store.Get([]byte(scope)); err == nil {
			_ = json.Unmarshal(bytes, &facts)
		}
	}
	m.scopes[scope] = facts
	return facts
}

// evictLocked 淘汰 n 条事实：优先次数少、更新时间旧的。
// 调用方需持有写锁。
func (m *memoryStore) evictLocked(facts []MemoryFact, n int) []MemoryFact {
	if n <= 0 || len(facts) <= n {
		return facts
	}
	sorted := make([]MemoryFact, len(facts))
	copy(sorted, facts)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Count != sorted[j].Count {
			return sorted[i].Count < sorted[j].Count
		}
		return sorted[i].UpdatedAt.Before(sorted[j].UpdatedAt)
	})
	drop := make(map[string]bool, n)
	for _, f := range sorted[:n] {
		drop[f.Text] = true
	}
	out := make([]MemoryFact, 0, len(facts)-n)
	for _, f := range facts {
		if !drop[f.Text] {
			out = append(out, f)
		}
	}
	return out
}

// save 持久化指定作用域（锁外拷贝）。
func (m *memoryStore) save(scope string, facts []MemoryFact) {
	if m.store == nil {
		return
	}
	bytes, err := json.Marshal(facts)
	if err != nil {
		logger.WithError(err).Warn("[AI] Failed to marshal memory scope " + scope)
		return
	}
	if err := m.store.Set([]byte(scope), bytes); err != nil {
		logger.WithError(err).Warn("[AI] Failed to save memory scope " + scope)
	}
}

// Retrieve 检索与查询相关度最高的至多 limit 条事实。
//
// 打分 = 关键词重叠（复用工具选择分词器）+ 出现次数弱加成；
// 注入共享 embedder 时叠加余弦相似度（权重 scoreEmbedW）——
// 无关键词重叠但语义相近的事实（如"今天想喝点东西"→"用户喜欢喝咖啡"）
// 也能命中；embedder 未配置或嵌入失败自动降级纯关键词。
// 返回 (事实, 得分) 对，按得分降序。
func (m *memoryStore) Retrieve(ctx context.Context, scope, query string, limit int) []MemoryFact {
	if limit <= 0 || strings.TrimSpace(query) == "" {
		return nil
	}
	facts := m.Facts(scope)
	if len(facts) == 0 {
		return nil
	}
	qt := tokenizeText(query)

	// 语义信号：查询向量 + 事实向量（惰性嵌入，缓存缺失才调用）。
	var queryVec []float32
	textVecs := make(map[string][]float32)
	m.mu.RLock()
	emb := m.emb
	m.mu.RUnlock()
	if emb != nil && emb.Enabled() {
		texts := make([]string, 0, len(facts))
		for _, f := range facts {
			texts = append(texts, f.Text)
		}
		embCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if vecs, err := emb.EmbedTexts(embCtx, texts); err == nil {
			textVecs = vecs
			if qv, qerr := emb.EmbedQuery(embCtx, query); qerr == nil {
				queryVec = qv
			} else {
				logger.Debugf("[AI] Memory embedding query failed, keyword-only: %v", qerr)
			}
		} else {
			logger.Debugf("[AI] Memory embedding failed, keyword-only: %v", err)
		}
	}

	scored := make([]struct {
		fact  MemoryFact
		score float64
	}, 0, len(facts))
	for _, f := range facts {
		// 基础信号：关键词重叠 +（可选）语义余弦。
		signal := tokenOverlap(qt, tokenizeText(f.Text))
		if queryVec != nil {
			if fv, ok := textVecs[f.Text]; ok {
				signal += float64(cosineSimilarity(queryVec, fv)) * scoreEmbedW
			}
		}
		// 无任何信号的事实排除（无 embedding 时语义项为 0，维持纯关键词门槛，
		// 防止零重叠噪声事实入选；计数加成不得单独构成入选理由）。
		if signal <= 0 {
			continue
		}
		// 出现次数作为弱加成：更常被确认的事实更可靠
		score := signal + float64(min(f.Count, 5))*0.1
		scored = append(scored, struct {
			fact  MemoryFact
			score float64
		}{f, score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].fact.Text < scored[j].fact.Text
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]MemoryFact, 0, len(scored))
	for _, s := range scored {
		out = append(out, s.fact)
	}
	return out
}

// CanExtract 判断指定作用域是否到达抽取节流间隔（距上次 >= minInterval）。
func (m *memoryStore) CanExtract(scope string) bool {
	if m.minInterval <= 0 {
		return true
	}
	m.extractMu.Lock()
	defer m.extractMu.Unlock()
	last, ok := m.lastExtract[scope]
	if !ok {
		return true
	}
	return time.Since(last) >= m.minInterval
}

// MarkExtracted 记录指定作用域的抽取时间（节流）。
func (m *memoryStore) MarkExtracted(scope string) {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()
	m.lastExtract[scope] = time.Now()
}
