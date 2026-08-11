package ai

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func newTestMemoryStore(t *testing.T, maxFacts int, minInterval time.Duration) *memoryStore {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ai_memory")
	m, err := OpenMemoryStore(dir, maxFacts, minInterval)
	if err != nil {
		t.Fatalf("OpenMemoryStore failed: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}

func TestMemoryStoreAddAndFacts(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	m.Add(userScope("u1"), "用户喜欢喝咖啡")
	m.Add(userScope("u1"), "用户养了一只猫")

	facts := m.Facts(userScope("u1"))
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}
	if facts[0].Count != 1 {
		t.Errorf("expected count 1, got %d", facts[0].Count)
	}
}

func TestMemoryStoreMergeDuplicate(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	m.Add(userScope("u1"), "用户喜欢喝咖啡")
	m.Add(userScope("u1"), "用户喜欢喝咖啡")

	facts := m.Facts(userScope("u1"))
	if len(facts) != 1 {
		t.Fatalf("duplicate should merge into 1 fact, got %d", len(facts))
	}
	if facts[0].Count != 2 {
		t.Errorf("expected count 2 after merge, got %d", facts[0].Count)
	}
}

func TestMemoryStoreMergeSimilar(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	m.Add(userScope("u1"), "用户喜欢喝咖啡")
	// 语义接近（关键词 Jaccard ≥ 0.6）应合并
	m.Add(userScope("u1"), "用户爱喝咖啡")

	facts := m.Facts(userScope("u1"))
	if len(facts) != 1 {
		t.Fatalf("similar facts should merge, got %d: %v", len(facts), facts)
	}
	if facts[0].Count != 2 {
		t.Errorf("expected count 2, got %d", facts[0].Count)
	}
}

func TestMemoryStoreEvict(t *testing.T) {
	m := newTestMemoryStore(t, 3, time.Minute)
	for i, text := range []string{"事实一", "事实二", "事实三", "事实四"} {
		m.Add(userScope("u1"), text)
		if i == 0 {
			// 让"事实一"再出现一次，提高其优先级
			m.Add(userScope("u1"), "事实一")
		}
	}
	facts := m.Facts(userScope("u1"))
	if len(facts) != 3 {
		t.Fatalf("expected 3 facts after eviction, got %d: %v", len(facts), facts)
	}
	for _, f := range facts {
		if f.Text == "事实四" {
			t.Logf("note: latest fact kept: %v", facts)
		}
	}
}

func TestMemoryStoreRetrieve(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	scope := userScope("u1")
	m.Add(scope, "用户喜欢喝咖啡")
	m.Add(scope, "原神游戏很好玩")
	m.Add(scope, "程序员喜欢写代码")

	hits := m.Retrieve(context.Background(), scope, "今天喝什么咖啡好", 2)
	if len(hits) == 0 {
		t.Fatal("expected retrieval hits")
	}
	if hits[0].Text != "用户喜欢喝咖啡" {
		t.Errorf("expected coffee fact first, got %q", hits[0].Text)
	}

	if got := m.Retrieve(context.Background(), scope, "完全无关的话题讨论", 2); len(got) > 0 {
		t.Errorf("unrelated query should return nothing, got %v", got)
	}
}

func TestMemoryStorePersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ai_memory")
	m1, err := OpenMemoryStore(dir, 50, time.Minute)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	m1.Add(userScope("u1"), "用户喜欢喝咖啡")
	m1.Close()

	m2, err := OpenMemoryStore(dir, 50, time.Minute)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer m2.Close()
	facts := m2.Facts(userScope("u1"))
	if len(facts) != 1 || facts[0].Text != "用户喜欢喝咖啡" {
		t.Fatalf("persisted facts mismatch: %v", facts)
	}
}

func TestMemoryThrottle(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Hour)
	scope := userScope("u1")
	if !m.CanExtract(scope) {
		t.Fatal("first extract should be allowed")
	}
	m.MarkExtracted(scope)
	if m.CanExtract(scope) {
		t.Error("extract within interval should be throttled")
	}
	// 越过间隔后可再次抽取
	m.lastExtract[scope] = time.Now().Add(-2 * time.Hour)
	if !m.CanExtract(scope) {
		t.Error("extract after interval should be allowed")
	}
}

func TestParseExtractedFacts(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{`[{"text":"用户喜欢喝咖啡","scope":"user"}]`, 1},
		{`好的，这是提取结果：` + "```json\n" + `[{"text":"a","scope":"user"},{"text":"b","scope":"group"}]` + "\n```", 2},
		{`[]`, 0},
		{`没有可提取内容`, 0},
		{`[{"text":"","scope":"user"}]`, 0},
		{`[{bad json]`, 0},
	}
	for _, tt := range cases {
		if got := len(parseExtractedFacts(tt.in)); got != tt.want {
			t.Errorf("parseExtractedFacts(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestLastRoundForMemory(t *testing.T) {
	s := &Session{}
	s.Messages = []Message{
		{Role: RoleSystem, Content: "system"},
		{Role: RoleUser, Content: "我喜欢喝咖啡"},
		{Role: RoleAssistant, Content: "好的，记住了"},
		{Role: RoleUser, Content: "["},
	}
	s.Messages[3] = Message{Role: RoleUser, ContentParts: []ContentPart{{Type: ContentPartText, Text: "今天天气怎么样"}}}
	s.Messages = append(s.Messages, Message{Role: RoleAssistant, Content: "今天晴朗"})

	conv := lastRoundForMemory(s)
	if !strings.Contains(conv, "今天天气怎么样") || !strings.Contains(conv, "今天晴朗") {
		t.Errorf("expected last user+assistant round, got %q", conv)
	}
	if strings.Contains(conv, "system") || strings.Contains(conv, "我喜欢喝咖啡") {
		t.Errorf("should only contain latest round, got %q", conv)
	}
}

func TestExtractAndStore(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	p := &Plugin{
		cfg:    &Config{},
		memory: m,
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
				return &ChatResponse{Content: `[{"text":"用户喜欢喝咖啡","scope":"user"},{"text":"本群都是FGO玩家","scope":"group"}]`}, nil
			},
		},
		lifecycleCtx: context.Background(),
	}
	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{
		{Role: RoleUser, Content: "我喜欢喝咖啡，群里大家都玩FGO"},
		{Role: RoleAssistant, Content: "好的"},
	}
	chat := platform.ChatInfo{ID: "g1", IsGroup: true}

	if err := p.extractAndStore(userScope("u1"), "u1", chat, session); err != nil {
		t.Fatalf("extractAndStore failed: %v", err)
	}
	if facts := m.Facts(userScope("u1")); len(facts) != 1 {
		t.Errorf("expected 1 user fact, got %d", len(facts))
	}
	if facts := m.Facts(groupScope("g1")); len(facts) != 1 {
		t.Errorf("expected 1 group fact, got %d", len(facts))
	}
}

func TestMaybeExtractMemoryDisabled(t *testing.T) {
	// memory 为 nil：不抽取、不 panic
	p := &Plugin{cfg: &Config{}}
	evt := platform.NewSyntheticEvent("c2c", "hello")
	ctx := eventctx.NewContextFromEvent(evt, nil)
	p.maybeExtractMemory(ctx, &Session{})
}

func TestMaybeExtractMemoryPrivateChatForcesUserScope(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	p := &Plugin{
		cfg:    &Config{},
		memory: m,
		prov: &mockProvider{
			chatFn: func(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
				return &ChatResponse{Content: `[{"text":"用户喜欢喝茶","scope":"group"}]`}, nil
			},
		},
		lifecycleCtx: context.Background(),
	}
	// 私聊中 group 事实应归入 user 作用域
	session := &Session{ID: "s1", UserID: "u1", ChatID: "c1"}
	session.Messages = []Message{{Role: RoleUser, Content: "我喜欢喝茶"}}
	chat := platform.ChatInfo{ID: "c1", IsGroup: false}

	if err := p.extractAndStore(userScope("u1"), "u1", chat, session); err != nil {
		t.Fatalf("extractAndStore failed: %v", err)
	}
	if facts := m.Facts(userScope("u1")); len(facts) != 1 {
		t.Errorf("private chat group fact should fall back to user scope, got %v", m.Facts(userScope("u1")))
	}
}

func TestMemoryRetrieveWithEmbedding(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	emb := &mockEmbedder{vec: []float32{1, 0, 0}}
	m.SetEmbedder(newTextVectorCache(emb))
	scope := userScope("u1")
	m.Add(scope, "用户喜欢喝咖啡")
	m.Add(scope, "原神游戏很好玩")

	// 语义命中：查询与事实无关键词重叠，但（mock）向量相似 → 入选
	hits := m.Retrieve(context.Background(), scope, "今天想喝点东西", 5)
	if len(hits) == 0 {
		t.Fatal("expected semantic hit via embedding")
	}
	// 1 次事实批量嵌入 + 1 次查询嵌入
	if emb.calls != 2 {
		t.Errorf("expected 2 embed calls, got %d", emb.calls)
	}

	// 事实向量已缓存：再次检索只嵌入查询
	m.Retrieve(context.Background(), scope, "今天想喝点东西", 5)
	if emb.calls != 3 {
		t.Errorf("expected 3 embed calls (facts cached), got %d", emb.calls)
	}
}

func TestMemoryRetrieveEmbeddingFailureFallback(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	emb := &mockEmbedder{vec: []float32{1, 0, 0}, err: context.DeadlineExceeded}
	m.SetEmbedder(newTextVectorCache(emb))
	scope := userScope("u1")
	m.Add(scope, "用户喜欢喝咖啡")
	m.Add(scope, "原神游戏很好玩")

	// embedding 失败 → 降级纯关键词：零重叠的无关查询不返回
	if hits := m.Retrieve(context.Background(), scope, "今天想喝点东西", 5); len(hits) > 0 {
		t.Errorf("embedding failure should fall back to keyword-only, got %v", hits)
	}
	if hits := m.Retrieve(context.Background(), scope, "今天喝什么咖啡", 5); len(hits) == 0 {
		t.Error("keyword retrieval should still work after embedding failure")
	}
}

func TestMemorySharedEmbedCacheWithTools(t *testing.T) {
	emb := &mockEmbedder{vec: []float32{1, 0, 0}}
	cache := newTextVectorCache(emb)

	// 工具文本嵌入
	texts := []string{toolEmbeddingText(Tool{Name: "get_weather", Description: "查询天气"})}
	if _, err := cache.EmbedTexts(context.Background(), texts); err != nil {
		t.Fatalf("EmbedTexts failed: %v", err)
	}
	if emb.calls != 1 {
		t.Errorf("expected 1 embed call, got %d", emb.calls)
	}

	// 记忆注入同一缓存实例：事实文本与工具嵌入文本相同 → 事实批量嵌入零调用
	m := newTestMemoryStore(t, 50, time.Minute)
	m.SetEmbedder(cache)
	scope := userScope("u1")
	m.Add(scope, texts[0])
	hits := m.Retrieve(context.Background(), scope, "查询天气", 5)
	if len(hits) == 0 {
		t.Fatal("expected retrieval hit")
	}
	// 事实向量复用缓存（0 调用）+ 查询嵌入（1 调用）
	if emb.calls != 2 {
		t.Errorf("expected 2 embed calls (facts reused from cache), got %d", emb.calls)
	}
}

func TestBuildMemoryContextInjection(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	m.Add(userScope("u1"), "用户喜欢喝咖啡")
	m.Add(userScope("u1"), "原神游戏很好玩")
	m.Add(groupScope("g1"), "本群成员都爱喝咖啡")

	p := &Plugin{
		cfg:    &Config{MemoryInjectMax: 8},
		memory: m,
	}
	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: "今天喝什么咖啡"}}

	evt := platform.NewSyntheticEvent("c2c", "今天喝什么咖啡",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "小明"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)

	memCtx := p.buildMemoryContext(ctx, session)
	if !strings.Contains(memCtx, "用户喜欢喝咖啡") {
		t.Errorf("expected coffee fact injected, got %q", memCtx)
	}
	if !strings.Contains(memCtx, "群相关记忆") {
		t.Errorf("expected group facts injected, got %q", memCtx)
	}
	// 无关事实（原神）不应注入
	if strings.Contains(memCtx, "原神") {
		t.Errorf("irrelevant facts should not be injected: %q", memCtx)
	}
}
