// Package ai embedding.go — Embedding 客户端、余弦相似度与文本向量缓存。
//
// 本文件实现语义检索的通用基础设施（工具选择与记忆检索共用）：
//   - Embedder 接口：统一嵌入调用抽象
//   - openAIEmbedder：OpenAI 兼容 /embeddings 端点实现（DeepSeek/Ollama/SiliconFlow 等兼容）
//   - textVectorCache：按文本键缓存的嵌入向量（相同文本只嵌入一次，
//     工具描述与记忆事实共用同一缓存实例）
//   - cosineSimilarity：余弦相似度计算
//
// 未配置 embedding_base_url 或请求失败时，调用方自动降级为纯关键词打分，
// 不影响对话主流程。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Embedder 将文本列表转换为向量。实现需保证返回顺序与输入一致。
type Embedder interface {
	// Embed 批量嵌入文本，返回与输入等长的向量切片。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Model 返回当前使用的嵌入模型名。
	Model() string
}

// openAIEmbedder 调用 OpenAI 兼容的 /embeddings 端点。
// 兼容 OpenAI、DeepSeek（无）、SiliconFlow、Ollama、vLLM、Jina 等实现该协议的服务。
type openAIEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
	breaker *embeddingBreaker
	dims    atomic.Int32 // 已确认的向量维度；0 = 未知。维度漂移（服务端换模型）即报错。
}

// newOpenAIEmbedder 创建 OpenAI 兼容的嵌入客户端。
// baseURL 需为 API 根地址（如 https://api.openai.com/v1），
// /embeddings 会在其后拼接。为空时返回 nil（不启用语义检索）。
func newOpenAIEmbedder(baseURL, apiKey, model string) Embedder {
	return newOpenAIEmbedderWithBreaker(baseURL, apiKey, model, newEmbeddingBreaker())
}

// NewOpenAIEmbedder 创建 OpenAI 兼容的嵌入客户端（对外导出）。
//
// 供其他插件（如知识库检索）复用同一套嵌入实现：包含熔断器、
// 维度漂移检测与 30s 超时。baseURL 为空时返回 nil。
func NewOpenAIEmbedder(baseURL, apiKey, model string) Embedder {
	return newOpenAIEmbedder(baseURL, apiKey, model)
}

// newOpenAIEmbedderWithBreaker 同上，但允许注入自定义熔断器（测试用）。
func newOpenAIEmbedderWithBreaker(baseURL, apiKey, model string, breaker *embeddingBreaker) Embedder {
	return newOpenAIEmbedderWithClient(baseURL, apiKey, model, breaker, &http.Client{Timeout: 30 * time.Second})
}

// newOpenAIEmbedderWithClient 同上，允许注入自定义 HTTP 客户端（测试 timeout 分支用）。
func newOpenAIEmbedderWithClient(baseURL, apiKey, model string, breaker *embeddingBreaker, client *http.Client) Embedder {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &openAIEmbedder{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		breaker: breaker,
		client:  client,
	}
}

// 熔断器参数：连续失败达到阈值后进入冷却期，冷却期内直接拒绝请求
// （调用方降级纯关键词），到期放行一次探活，成功即恢复。
const (
	embeddingBreakerThreshold = 3
	embeddingBreakerCooldown  = 30 * time.Second
)

// errEmbeddingCooldown 熔断冷却期内拒绝请求的哨兵错误。
var errEmbeddingCooldown = errors.New("embedding service in cooldown")

// embeddingBreaker 轻量熔断器：连续失败计数 + 冷却期。
// 防止 embedding 服务挂掉后每条消息都产生无意义的失败请求。
type embeddingBreaker struct {
	mu                  sync.Mutex
	threshold           int
	cooldown            time.Duration
	consecutiveFailures int
	openUntil           time.Time // 冷却截止时间；zero 表示未熔断
}

func newEmbeddingBreaker() *embeddingBreaker {
	return &embeddingBreaker{
		threshold: embeddingBreakerThreshold,
		cooldown:  embeddingBreakerCooldown,
	}
}

// Allow 返回当前是否允许发起 embedding 请求。
// 冷却期结束后的第一次调用放行（探活），调用方成功则恢复、失败则重新熔断。
func (b *embeddingBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() {
		return true
	}
	if time.Now().After(b.openUntil) {
		b.openUntil = time.Time{}
		return true
	}
	return false
}

// RecordSuccess 记录一次成功，重置失败计数并解除熔断。
func (b *embeddingBreaker) RecordSuccess() {
	b.mu.Lock()
	failures := b.consecutiveFailures
	b.consecutiveFailures = 0
	b.openUntil = time.Time{}
	b.mu.Unlock()
	if failures > 0 {
		logger.Infof("[AI] Embedding breaker recovered after %d failures", failures)
	}
}

// RecordFailure 记录一次失败；达到阈值时进入冷却期。
func (b *embeddingBreaker) RecordFailure() {
	b.mu.Lock()
	b.consecutiveFailures++
	if b.consecutiveFailures >= b.threshold {
		b.openUntil = time.Now().Add(b.cooldown)
		n := b.consecutiveFailures
		b.consecutiveFailures = 0
		b.mu.Unlock()
		logger.Warnf("[AI] Embedding breaker opened after %d consecutive failures, cooldown %v", n, b.cooldown)
		return
	}
	b.mu.Unlock()
}

// embedRequest OpenAI 兼容嵌入请求体。
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse OpenAI 兼容嵌入响应体。
type embedResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Embed 批量嵌入文本。结果按输入顺序返回。
func (e *openAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if !e.breaker.Allow() {
		return nil, errEmbeddingCooldown
	}
	body, err := json.Marshal(embedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		// 上下文主动取消不算服务故障，避免误熔断。
		if ctx.Err() == nil {
			e.breaker.RecordFailure()
		}
		return nil, fmt.Errorf("embed: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("embed: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		e.breaker.RecordFailure()
		return nil, fmt.Errorf("embed: status %d: %s", resp.StatusCode, truncateBytes(raw, 200))
	}

	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		e.breaker.RecordFailure()
		return nil, fmt.Errorf("embed: parse response: %w", err)
	}
	if parsed.Error != nil {
		e.breaker.RecordFailure()
		return nil, fmt.Errorf("embed: api error: %s", parsed.Error.Message)
	}

	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	for i := range out {
		if out[i] == nil {
			e.breaker.RecordFailure()
			return nil, fmt.Errorf("embed: missing vector for index %d", i)
		}
	}
	// 数值防御：维度一致性 + 有限值校验。垃圾响应视为失败（计入熔断），
	// 防止 NaN/Inf 或维度漂移产生 score=NaN 的诡异排序。
	dim := len(out[0])
	if dim == 0 {
		e.breaker.RecordFailure()
		return nil, fmt.Errorf("embed: empty vector for index 0")
	}
	if prev := e.dims.Load(); prev != 0 && prev != int32(dim) {
		e.breaker.RecordFailure()
		return nil, fmt.Errorf("embed: embedding dimension changed from %d to %d (model switched?)", prev, dim)
	}
	e.dims.CompareAndSwap(0, int32(dim))
	for i, v := range out {
		if err := validateVector(v, dim); err != nil {
			e.breaker.RecordFailure()
			return nil, fmt.Errorf("embed: invalid vector at index %d: %w", i, err)
		}
	}
	e.breaker.RecordSuccess()
	return out, nil
}

func (e *openAIEmbedder) Model() string { return e.model }

// validateVector 校验单个向量的数值合法性：非空、长度与期望维度一致、全部元素有限
// （拒绝 NaN / ±Inf）。供 embedding 响应边界防御使用。
func validateVector(v []float32, expectedDim int) error {
	if len(v) == 0 {
		return errors.New("empty vector")
	}
	if expectedDim > 0 && len(v) != expectedDim {
		return fmt.Errorf("dimension mismatch: got %d, want %d", len(v), expectedDim)
	}
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return errors.New("vector contains NaN or Inf")
		}
	}
	return nil
}

// truncateBytes 截断响应正文用于错误提示，避免泄露完整 API 错误体。
func truncateBytes(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// cosineSimilarity 计算两个向量的余弦相似度。
// 任一向量为零向量时返回 0。
func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// CosineSimilarity 计算两个向量的余弦相似度（对外导出）。
// 任一向量为空或维度不一致时返回 0。
func CosineSimilarity(a, b []float32) float32 {
	return cosineSimilarity(a, b)
}

// textVectorCache 按文本键缓存的嵌入向量。
// 工具描述、记忆事实等静态文本只嵌入一次；相同文本复用同一向量。
type textVectorCache struct {
	mu       sync.Mutex
	embedder Embedder
	vectors  map[string][]float32 // model\x00text → vector
}

// newTextVectorCache 创建文本向量缓存。
func newTextVectorCache(e Embedder) *textVectorCache {
	return &textVectorCache{embedder: e, vectors: make(map[string][]float32)}
}

// Enabled 返回是否配置了可用的嵌入器。
func (c *textVectorCache) Enabled() bool {
	return c != nil && c.embedder != nil
}

// cacheKey 生成缓存键：模型名 + 文本。
// 切换 embedding 模型/维度后旧向量不复用，避免同文本复用不同模型的向量。
func (c *textVectorCache) cacheKey(text string) string {
	if c.embedder == nil {
		return text
	}
	return c.embedder.Model() + "\x00" + text
}

// EmbedQuery 嵌入单条查询文本（查询随消息变化，不缓存）。
func (c *textVectorCache) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vecs, err := c.embedder.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// EmbedTexts 返回给定文本集的向量，缺失的按需嵌入并缓存。
// 返回 text → vector 映射；embedder 出错时返回错误（调用方整体跳过
// embedding 加权并降级纯关键词）。
func (c *textVectorCache) EmbedTexts(ctx context.Context, texts []string) (map[string][]float32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var need []string
	for _, t := range texts {
		if _, ok := c.vectors[c.cacheKey(t)]; !ok {
			need = append(need, t)
		}
	}
	if len(need) > 0 {
		vecs, err := c.embedder.Embed(ctx, need)
		if err != nil {
			return nil, err
		}
		for i, t := range need {
			c.vectors[c.cacheKey(t)] = vecs[i]
		}
	}

	out := make(map[string][]float32, len(texts))
	for _, t := range texts {
		if v, ok := c.vectors[c.cacheKey(t)]; ok {
			out[t] = v
		}
	}
	return out, nil
}

// toolEmbeddingText 构建单个工具用于嵌入的文本。
func toolEmbeddingText(t Tool) string {
	desc := t.Description
	if desc == "" {
		desc = "执行命令 " + t.Name
	}
	cats := strings.Join(t.Categories, " ")
	return fmt.Sprintf("tool %s categories: %s. %s", t.Name, cats, desc)
}
