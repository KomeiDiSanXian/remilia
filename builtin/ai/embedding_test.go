package ai

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestEmbeddingBreaker(t *testing.T) {
	b := &embeddingBreaker{threshold: 3, cooldown: 40 * time.Millisecond}
	if !b.Allow() {
		t.Fatal("breaker should allow requests initially")
	}
	b.RecordFailure()
	b.RecordFailure()
	if !b.Allow() {
		t.Error("breaker should stay closed below failure threshold")
	}
	b.RecordFailure()
	if b.Allow() {
		t.Error("breaker should open after threshold failures")
	}
	if b.Allow() {
		t.Error("breaker should reject requests during cooldown")
	}
	// 冷却结束后放行一次探活。
	time.Sleep(60 * time.Millisecond)
	if !b.Allow() {
		t.Error("breaker should allow a probe request after cooldown")
	}
	b.RecordSuccess()
	if !b.Allow() {
		t.Error("breaker should reset after a successful probe")
	}
}

func TestOpenAIEmbedderBreakerCooldown(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":{"message":"boom","type":"server_error"}}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	// 大冷却期：验证熔断打开后不再向服务发起无意义请求。
	brk := &embeddingBreaker{threshold: 3, cooldown: time.Hour}
	emb := newOpenAIEmbedderWithBreaker(srv.URL, "", "test-model", brk)
	for i := range 3 {
		if _, err := emb.Embed(context.Background(), []string{"x"}); err == nil {
			t.Fatalf("call %d should fail", i+1)
		}
	}
	if _, err := emb.Embed(context.Background(), []string{"x"}); !errors.Is(err, errEmbeddingCooldown) {
		t.Fatalf("expected cooldown error after breaker opens, got %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 server calls before cooldown, got %d", got)
	}
}

func TestValidateVector(t *testing.T) {
	if err := validateVector(nil, 2); err == nil {
		t.Error("empty vector should be rejected")
	}
	if err := validateVector([]float32{1, 2}, 3); err == nil {
		t.Error("dimension mismatch should be rejected")
	}
	if err := validateVector([]float32{float32(math.NaN()), 1}, 2); err == nil {
		t.Error("NaN should be rejected")
	}
	if err := validateVector([]float32{float32(math.Inf(1)), 1}, 2); err == nil {
		t.Error("+Inf should be rejected")
	}
	if err := validateVector([]float32{float32(math.Inf(-1)), 1}, 2); err == nil {
		t.Error("-Inf should be rejected")
	}
	if err := validateVector([]float32{0.1, 0.2}, 2); err != nil {
		t.Errorf("valid vector should pass: %v", err)
	}
}

func TestOpenAIEmbedderInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()
	emb := newOpenAIEmbedder(srv.URL, "", "m")
	if _, err := emb.Embed(context.Background(), []string{"x"}); err == nil {
		t.Error("expected parse error on invalid JSON")
	}
}

func TestOpenAIEmbedderEmptyVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": []float32{}}},
		})
	}))
	defer srv.Close()
	emb := newOpenAIEmbedder(srv.URL, "", "m")
	if _, err := emb.Embed(context.Background(), []string{"x"}); err == nil {
		t.Error("expected error on empty vector")
	}
}

func TestOpenAIEmbedderInconsistentBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "index": 0, "embedding": []float32{0.1, 0.2}},
				{"object": "embedding", "index": 1, "embedding": []float32{0.1, 0.2, 0.3}},
			},
		})
	}))
	defer srv.Close()
	emb := newOpenAIEmbedder(srv.URL, "", "m")
	if _, err := emb.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Error("expected error on inconsistent batch dimensions")
	}
}

func TestOpenAIEmbedderDimensionChanged(t *testing.T) {
	var call atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		dim := 2
		if call.Add(1) >= 2 {
			dim = 3 // 模拟服务端切换模型导致维度漂移
		}
		vec := make([]float32, dim)
		for i := range vec {
			vec[i] = 0.1
		}
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": vec}},
		})
	}))
	defer srv.Close()
	emb := newOpenAIEmbedder(srv.URL, "", "m")
	if _, err := emb.Embed(context.Background(), []string{"x"}); err != nil {
		t.Fatalf("first call should succeed: %v", err)
	}
	if _, err := emb.Embed(context.Background(), []string{"x"}); err == nil ||
		!strings.Contains(err.Error(), "dimension changed") {
		t.Fatalf("expected dimension changed error, got %v", err)
	}
}

func TestOpenAIEmbedderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	brk := &embeddingBreaker{threshold: 3, cooldown: time.Hour}
	emb := newOpenAIEmbedderWithClient(srv.URL, "", "m", brk, &http.Client{Timeout: 50 * time.Millisecond})
	if _, err := emb.Embed(context.Background(), []string{"x"}); err == nil {
		t.Error("expected timeout error")
	}
	if brk.consecutiveFailures != 1 {
		t.Errorf("timeout should count as a failure, got %d", brk.consecutiveFailures)
	}
}

func TestOpenAIEmbedderConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close() // 端口已关闭 → connection refused
	emb := newOpenAIEmbedder(addr, "", "m")
	if _, err := emb.Embed(context.Background(), []string{"x"}); err == nil {
		t.Error("expected connection refused error")
	}
}

func TestOpenAIEmbedderRecovery(t *testing.T) {
	var calls atomic.Int32
	var healthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if healthy.Load() {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": []float32{0.1, 0.2}}},
			})
			return
		}
		http.Error(w, `{"error":{"message":"boom","type":"server_error"}}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	brk := &embeddingBreaker{threshold: 3, cooldown: 50 * time.Millisecond}
	emb := newOpenAIEmbedderWithBreaker(srv.URL, "", "m", brk)
	for i := 0; i < 3; i++ {
		if _, err := emb.Embed(context.Background(), []string{"x"}); err == nil {
			t.Fatalf("call %d should fail while unhealthy", i+1)
		}
	}
	if _, err := emb.Embed(context.Background(), []string{"x"}); !errors.Is(err, errEmbeddingCooldown) {
		t.Fatalf("expected cooldown error while breaker open, got %v", err)
	}

	// 服务恢复 + 冷却结束 → 探活成功 → 熔断恢复。
	healthy.Store(true)
	time.Sleep(80 * time.Millisecond)
	vecs, err := emb.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("probe after recovery should succeed: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 2 {
		t.Errorf("unexpected recovered vector: %v", vecs)
	}
	if _, err := emb.Embed(context.Background(), []string{"x"}); err != nil {
		t.Errorf("breaker should stay closed after recovery: %v", err)
	}
}

func TestOpenAIEmbedderConcurrentFailureNoStorm(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":{"message":"boom","type":"server_error"}}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	brk := &embeddingBreaker{threshold: 3, cooldown: time.Hour}
	emb := newOpenAIEmbedderWithBreaker(srv.URL, "", "m", brk)
	for i := range 3 {
		if _, err := emb.Embed(context.Background(), []string{"x"}); err == nil {
			t.Fatalf("call %d should fail", i+1)
		}
	}

	// 熔断已打开：100 个并发请求应全部快速失败，且不再打到服务（无 retry storm）。
	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, 100)
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = emb.Embed(context.Background(), []string{"x"})
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	if got := calls.Load(); got != 3 {
		t.Errorf("breaker should prevent further server calls, got %d (want 3)", got)
	}
	if elapsed > 3*time.Second {
		t.Errorf("concurrent cooldown failures should fail fast, took %v", elapsed)
	}
	for i, err := range errs {
		if !errors.Is(err, errEmbeddingCooldown) {
			t.Fatalf("goroutine %d: expected cooldown error, got %v", i, err)
		}
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
