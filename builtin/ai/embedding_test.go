package ai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if got := cosineSimilarity(a, b); math.Abs(float64(got)) > 1e-6 {
		t.Errorf("orthogonal vectors should score ~0, got %v", got)
	}
	if got := cosineSimilarity([]float32{1, 2}, []float32{1, 2}); math.Abs(float64(got)-1) > 1e-6 {
		t.Errorf("identical vectors should score ~1, got %v", got)
	}
	if got := cosineSimilarity(nil, b); got != 0 {
		t.Errorf("nil vector should score 0, got %v", got)
	}
}

func TestOpenAIEmbedder(t *testing.T) {
	var gotModel string
	var gotInput []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing authorization header")
		}
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotModel = req.Model
		gotInput = req.Input
		w.Header().Set("Content-Type", "application/json")
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": []float32{0.1, 0.2}}
		}
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	defer srv.Close()

	emb := newOpenAIEmbedder(srv.URL, "test-key", "my-model")
	vecs, err := emb.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if gotModel != "my-model" {
		t.Errorf("expected model my-model, got %q", gotModel)
	}
	if len(gotInput) != 2 || gotInput[0] != "a" || gotInput[1] != "b" {
		t.Errorf("unexpected input: %v", gotInput)
	}
	if len(vecs) != 2 {
		t.Errorf("expected 2 vectors, got %d", len(vecs))
	}
	if emb.Model() != "my-model" {
		t.Errorf("Model() mismatch")
	}
}

func TestOpenAIEmbedderErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"rate limited","type":"rate_limit_error"}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	emb := newOpenAIEmbedder(srv.URL, "", "")
	if _, err := emb.Embed(context.Background(), []string{"x"}); err == nil {
		t.Error("expected error on non-200 status")
	}
}

func TestTextVectorCacheLazyEmbedAndCache(t *testing.T) {
	emb := &mockEmbedder{vec: []float32{1, 0, 0}}
	cache := newTextVectorCache(emb)
	texts := []string{"alpha tool", "beta tool"}

	// 首次：一次批量嵌入
	vecs, err := cache.EmbedTexts(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedTexts failed: %v", err)
	}
	if emb.calls != 1 {
		t.Errorf("expected 1 embed call for 2 uncached texts, got %d", emb.calls)
	}
	if len(vecs) != 2 {
		t.Errorf("expected 2 vectors, got %d", len(vecs))
	}
	if _, ok := vecs["alpha tool"]; !ok {
		t.Error("expected vector keyed by text")
	}

	// 第二次：全部命中缓存，不再调用
	vecs2, err := cache.EmbedTexts(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedTexts (cached) failed: %v", err)
	}
	if emb.calls != 1 {
		t.Errorf("expected cache hit (no embed calls), got %d", emb.calls)
	}
	if len(vecs2) != 2 {
		t.Errorf("expected 2 cached vectors, got %d", len(vecs2))
	}

	// 查询嵌入：独立调用，不写入缓存
	qv, err := cache.EmbedQuery(context.Background(), "query")
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}
	if len(qv) == 0 {
		t.Error("expected query vector")
	}
	if emb.calls != 2 {
		t.Errorf("expected 2nd call for query embed, got %d", emb.calls)
	}
}

func TestTextVectorCacheDisabled(t *testing.T) {
	if (*textVectorCache)(nil).Enabled() {
		t.Error("nil cache should be disabled")
	}
	cache := newTextVectorCache(nil)
	if cache.Enabled() {
		t.Error("cache without embedder should be disabled")
	}
}

func TestToolEmbeddingText(t *testing.T) {
	text := toolEmbeddingText(Tool{Name: "get_weather", Description: "查询天气", Categories: []string{"weather"}})
	if !strings.Contains(text, "get_weather") || !strings.Contains(text, "查询天气") || !strings.Contains(text, "weather") {
		t.Errorf("embedding text should include name/desc/categories: %q", text)
	}
}
