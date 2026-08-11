package ai

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"gorm.io/gorm"
)

// newRAGTestLogger 创建带 SQLite 的测试 messagelog。
func newRAGTestLogger(t *testing.T) (*messagelog.Logger, *gorm.DB) {
	t.Helper()
	db, err := messagelog.OpenDB(filepath.Join(t.TempDir(), "ml.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	l := messagelog.New(10)
	l.UseDB(db)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return l, db
}

// insertMessage 直插一条历史消息到 SQLite（时间窗口内、仅入站）。
// age 为消息距今的时长（如 2*time.Hour）。
func insertMessage(t *testing.T, db *gorm.DB, chatID, userID, content string, age time.Duration, eventID string) {
	t.Helper()
	if eventID == "" {
		eventID = chatID + "_" + content
	}
	rec := messagelog.MessageRecord{
		Platform:  "qq",
		Kind:      "GROUP_MESSAGE",
		EventID:   eventID,
		ChatID:    chatID,
		UserID:    userID,
		UserName:  userID,
		Content:   content,
		Timestamp: time.Now().Add(-age).UnixNano(),
		IsGroup:   true,
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("insert message failed: %v", err)
	}
}

// ragBaseCfg 返回启用了 RAG 的默认配置（测试用）。
func ragBaseCfg() Config {
	return Config{
		ContextRAGMessages:   3,
		ContextRAGDays:       7,
		ContextRAGCandidates: 500,
		ContextRAGInjectMax:  3,
	}
}

// newRAGPlugin 构造测试插件（RAG 启用与否由 cfg 决定，不强制覆盖）。
func newRAGPlugin(t *testing.T, history *messagelog.Logger, cfg Config) *Plugin {
	if cfg.ContextRAGDays <= 0 {
		cfg.ContextRAGDays = 7
	}
	if cfg.ContextRAGInjectMax <= 0 {
		cfg.ContextRAGInjectMax = 3
	}
	return &Plugin{cfg: &cfg, history: history}
}

func TestFormatRAGHitsKeywordPrefilter(t *testing.T) {
	l, db := newRAGTestLogger(t)
	insertMessage(t, db, "g1", "张三", "上次我们讨论了服务器方案选型", 2*time.Hour, "")
	insertMessage(t, db, "g1", "李四", "今天食堂吃什么", 2*time.Hour, "")

	p := newRAGPlugin(t, l, ragBaseCfg())
	evt := platform.NewSyntheticEvent("c2c", "服务器方案是什么",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: "服务器方案是什么"}}

	text := p.buildRAGContext(ctx, session)
	if !strings.Contains(text, "服务器方案选型") {
		t.Errorf("expected matching historical message, got %q", text)
	}
	if strings.Contains(text, "食堂") {
		t.Errorf("unrelated message should be filtered out, got %q", text)
	}
	if !strings.Contains(text, "张三") || !strings.Contains(text, "[") {
		t.Errorf("expected formatted nickname and timestamp, got %q", text)
	}
}

func TestBuildRAGContextDisabled(t *testing.T) {
	l, db := newRAGTestLogger(t)
	insertMessage(t, db, "g1", "张三", "服务器方案选型讨论", time.Hour, "")

	p := newRAGPlugin(t, l, Config{ContextRAGMessages: 0})
	evt := platform.NewSyntheticEvent("c2c", "服务器方案是什么",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: "服务器方案是什么"}}

	if text := p.buildRAGContext(ctx, session); text != "" {
		t.Errorf("disabled RAG should return empty, got %q", text)
	}
}

func TestBuildRAGContextNoKeywordHit(t *testing.T) {
	l, db := newRAGTestLogger(t)
	insertMessage(t, db, "g1", "张三", "今天食堂吃什么", time.Hour, "")

	p := newRAGPlugin(t, l, ragBaseCfg())
	evt := platform.NewSyntheticEvent("c2c", "服务器方案是什么",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: "服务器方案是什么"}}

	if text := p.buildRAGContext(ctx, session); text != "" {
		t.Errorf("no keyword hit should return empty, got %q", text)
	}
}

func TestBuildRAGContextDedupRecentWindow(t *testing.T) {
	l, db := newRAGTestLogger(t)
	// 匹配但较早的历史消息（id=1，不在最近 3 条窗口内）
	insertMessage(t, db, "g1", "张三", "服务器方案选型讨论", 6*time.Hour, "old_match")
	// 4 条较新的无关消息（占据最近窗口的 3 个名额）
	for i := range 4 {
		insertMessage(t, db, "g1", "李四", "食堂今日推荐菜品"+string(rune('0'+i)), time.Duration(i+1)*time.Hour, "")
	}
	// 窗口内的匹配消息（ring 热缓存）：应被去重
	l.Record(messagelog.RecordEntry{
		ChatID: "g1", UserID: "王五", Content: "服务器运维手册在这",
		Timestamp: time.Now().Add(-30 * time.Minute), EventID: "in_window",
	})

	p := newRAGPlugin(t, l, Config{ContextRAGMessages: 3, ContextGroupMessages: 3})
	evt := platform.NewSyntheticEvent("c2c", "服务器方案",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: "服务器方案"}}

	text := p.buildRAGContext(ctx, session)
	// 窗口内的"运维手册"被去重（窗口已注入）
	if strings.Contains(text, "运维手册") {
		t.Errorf("entry in recent window should be deduped, got %q", text)
	}
	// 窗口外的匹配历史仍被检索注入
	if !strings.Contains(text, "选型讨论") {
		t.Errorf("older matching history should be retrieved, got %q", text)
	}
}

// mapEmbedder 按文本返回不同向量的假嵌入器（用于精排顺序测试）。
type mapEmbedder struct {
	calls int
	vecs  map[string][]float32
	err   error
}

func (m *mapEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := m.vecs[t]; ok {
			out[i] = v
		} else {
			out[i] = []float32{0.5, 0.5}
		}
	}
	return out, nil
}

func (m *mapEmbedder) Model() string { return "map" }

func TestBuildRAGContextEmbeddingRerank(t *testing.T) {
	l, db := newRAGTestLogger(t)
	insertMessage(t, db, "g1", "张三", "服务器选型讨论会纪要", time.Hour, "e1")
	insertMessage(t, db, "g1", "李四", "服务器这边最后怎么定的", 2*time.Hour, "e2")

	query := "服务器方案是什么"
	emb := &mapEmbedder{vecs: map[string][]float32{
		query:         {1, 0},
		"服务器选型讨论会纪要":  {0, 1}, // 关键词分高但语义无关
		"服务器这边最后怎么定的": {1, 0}, // 关键词分低但语义相关
	}}
	p := newRAGPlugin(t, l, ragBaseCfg())
	p.emb = newTextVectorCache(emb)

	evt := platform.NewSyntheticEvent("c2c", query,
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: query}}

	text := p.buildRAGContext(ctx, session)
	// 语义精排后：关键词分相同但语义相关的"最后怎么定的"应排在前面
	idxFinal := strings.Index(text, "最后怎么定的")
	idxMemo := strings.Index(text, "选型讨论会纪要")
	if idxFinal < 0 || idxMemo < 0 {
		t.Fatalf("expected both messages in output, got %q", text)
	}
	if idxFinal > idxMemo {
		t.Errorf("semantic rerank should put relevant message first, got %q", text)
	}
	if emb.calls < 1 {
		t.Error("expected embedding calls for rerank")
	}
}

func TestBuildRAGContextCacheHit(t *testing.T) {
	l, db := newRAGTestLogger(t)
	insertMessage(t, db, "g1", "张三", "服务器方案选型讨论", time.Hour, "")

	emb := &mapEmbedder{vecs: map[string][]float32{}}
	p := newRAGPlugin(t, l, ragBaseCfg())
	p.emb = newTextVectorCache(emb)

	evt := platform.NewSyntheticEvent("c2c", "服务器方案",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: "服务器方案"}}

	first := p.buildRAGContext(ctx, session)
	if first == "" {
		t.Fatal("expected first retrieval hit")
	}
	callsAfterFirst := emb.calls

	// 相似查询（Jaccard 命中）：复用缓存，零 embedding 调用
	session.Messages = []Message{{Role: RoleUser, Content: "服务器方案是啥"}}
	second := p.buildRAGContext(ctx, session)
	if second != first {
		t.Errorf("cache hit should return same text: %q vs %q", first, second)
	}
	if emb.calls != callsAfterFirst {
		t.Errorf("cache hit should not embed again, calls %d → %d", callsAfterFirst, emb.calls)
	}
}

func TestBuildRAGContextEmbeddingFailureFallback(t *testing.T) {
	l, db := newRAGTestLogger(t)
	insertMessage(t, db, "g1", "张三", "服务器方案选型讨论", time.Hour, "")

	emb := &mapEmbedder{vecs: map[string][]float32{}, err: context.DeadlineExceeded}
	p := newRAGPlugin(t, l, ragBaseCfg())
	p.emb = newTextVectorCache(emb)

	evt := platform.NewSyntheticEvent("c2c", "服务器方案",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: "服务器方案"}}

	// embedding 失败不阻塞检索，关键词排序仍返回结果
	if text := p.buildRAGContext(ctx, session); !strings.Contains(text, "选型讨论") {
		t.Errorf("expected keyword fallback result, got %q", text)
	}
}

func TestBuildRAGContextLimitInjection(t *testing.T) {
	l, db := newRAGTestLogger(t)
	for i := range 5 {
		insertMessage(t, db, "g1", "张三", "服务器方案备选"+string(rune('0'+i)), time.Duration(i+1)*time.Hour, "")
	}
	p := newRAGPlugin(t, l, Config{ContextRAGMessages: 3, ContextRAGInjectMax: 2})
	evt := platform.NewSyntheticEvent("c2c", "服务器方案",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	session := &Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	session.Messages = []Message{{Role: RoleUser, Content: "服务器方案"}}

	text := p.buildRAGContext(ctx, session)
	if strings.Count(text, "\n[") < 2 {
		t.Errorf("expected at most 2 injected messages, got %q", text)
	}
}
