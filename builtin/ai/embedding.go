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
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
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
}

// newOpenAIEmbedder 创建 OpenAI 兼容的嵌入客户端。
// baseURL 需为 API 根地址（如 https://api.openai.com/v1），
// /embeddings 会在其后拼接。为空时返回 nil（不启用语义检索）。
func newOpenAIEmbedder(baseURL, apiKey, model string) Embedder {
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
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
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
		return nil, fmt.Errorf("embed: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("embed: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: status %d: %s", resp.StatusCode, truncateBytes(raw, 200))
	}

	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("embed: parse response: %w", err)
	}
	if parsed.Error != nil {
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
			return nil, fmt.Errorf("embed: missing vector for index %d", i)
		}
	}
	return out, nil
}

func (e *openAIEmbedder) Model() string { return e.model }

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

// textVectorCache 按文本键缓存的嵌入向量。
// 工具描述、记忆事实等静态文本只嵌入一次；相同文本复用同一向量。
type textVectorCache struct {
	mu       sync.Mutex
	embedder Embedder
	vectors  map[string][]float32 // text → vector
}

// newTextVectorCache 创建文本向量缓存。
func newTextVectorCache(e Embedder) *textVectorCache {
	return &textVectorCache{embedder: e, vectors: make(map[string][]float32)}
}

// Enabled 返回是否配置了可用的嵌入器。
func (c *textVectorCache) Enabled() bool {
	return c != nil && c.embedder != nil
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
		if _, ok := c.vectors[t]; !ok {
			need = append(need, t)
		}
	}
	if len(need) > 0 {
		vecs, err := c.embedder.Embed(ctx, need)
		if err != nil {
			return nil, err
		}
		for i, t := range need {
			c.vectors[t] = vecs[i]
		}
	}

	out := make(map[string][]float32, len(texts))
	for _, t := range texts {
		if v, ok := c.vectors[t]; ok {
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
