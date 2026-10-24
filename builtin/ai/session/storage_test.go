// storage_test.go — GORM 会话存储的集成契约（真实 SQLite，不走 mock）。
//
// 这些用例覆盖的是"最容易在换驱动/换版本时静默坏掉"的部分：表名、单语句
// upsert 的更新语义、计数归零、大字段、损坏历史的错误传播、历史表迁移与
// 库侧过期清理。会话历史是整套 AI 插件里最贵的数据，没有集成测试就没有
// 任何东西保证 Save→Load 真的能往返。
package session

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openSessionTestDB 为每个用例打开独立的内存 SQLite。
//
// NewMemory 使用 file::memory:?cache=shared，同进程内多个连接会看到彼此的表；
// 表名固定，因此按用例名构造独立 DSN，避免用例相互污染。
func openSessionTestDB(t *testing.T) *infrastorage.Plugin {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	client, err := infrastorage.Open(infrastorage.WithDSN("file:" + name + "?mode=memory&cache=shared"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if sqlDB, err := client.DB().DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	return client
}

func newSessionTestStore(t *testing.T) (*infrastorage.Plugin, SessionStore) {
	t.Helper()
	client := openSessionTestDB(t)
	store, err := NewGormSessionStore(client)
	if err != nil {
		t.Fatalf("NewGormSessionStore: %v", err)
	}
	return client, store
}

func TestGormSessionStoreTableName(t *testing.T) {
	// 表名必须显式声明：GORM 默认会把 Record 复数化成通用的 records，
	// 与同库其它插件的记录表混淆（历史版本正是如此，靠迁移改正）。
	if name := (Record{}).TableName(); name != SessionRecordTable {
		t.Fatalf("TableName() = %q, want %q", name, SessionRecordTable)
	}
	if SessionRecordTable == "records" {
		t.Fatal("session table must not stay at the generic legacy name")
	}
}

func TestGormSessionStoreMigratesLegacyTable(t *testing.T) {
	client := openSessionTestDB(t)

	// 模拟历史版本：数据落在 records 表，且 user_id/chat_id 上带着旧索引。
	legacy := `CREATE TABLE records (
		id text PRIMARY KEY,
		user_id text,
		chat_id text,
		messages mediumtext,
		call_count integer DEFAULT 0,
		tool_count integer DEFAULT 0,
		plan text,
		pending_images text,
		created_at datetime,
		updated_at datetime
	)`
	require.NoError(t, client.DB().Exec(legacy).Error)
	require.NoError(t, client.DB().Exec(`CREATE INDEX idx_records_user_id ON records(user_id)`).Error)
	require.NoError(t, client.DB().Exec(`CREATE INDEX idx_records_chat_id ON records(chat_id)`).Error)
	require.NoError(t, client.DB().Exec(
		`INSERT INTO records (id, user_id, chat_id, messages, call_count, tool_count, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"p:c:u", "u", "c", `[{"role":"user","content":"旧历史"}]`, 7, 3, time.Now(),
	).Error)

	store, err := NewGormSessionStore(client)
	require.NoError(t, err)

	// 旧数据必须原地改到新表且可读，而不是留在 records 里被忽略。
	assert.False(t, client.DB().Migrator().HasTable(legacyRecordTable),
		"legacy table should have been renamed away")
	assert.True(t, client.DB().Migrator().HasTable(SessionRecordTable))

	loaded, err := store.Load("p:c:u")
	require.NoError(t, err)
	require.NotNil(t, loaded, "history must survive the table rename")
	require.Len(t, loaded.Messages, 1)
	assert.Equal(t, "旧历史", loaded.Messages[0].Content)
	assert.Equal(t, 7, loaded.CallCount)

	// 旧索引（名字沿用旧表名）应被删掉，保留期清理用的 updated_at 索引要建出来。
	naming := client.DB().NamingStrategy
	assert.False(t, client.DB().Migrator().HasIndex(&Record{}, naming.IndexName(legacyRecordTable, "user_id")),
		"legacy user_id index should be dropped")
	assert.True(t, client.DB().Migrator().HasIndex(&Record{}, naming.IndexName(SessionRecordTable, "updated_at")),
		"updated_at index is required by retention cleanup")
}

func TestGormSessionStoreRoundTrip(t *testing.T) {
	_, store := newSessionTestStore(t)

	now := time.Now().Truncate(time.Second)
	src := &Session{
		ID:     "qq:group:1:user:2",
		UserID: "2",
		ChatID: "1",
		Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: "你好"},
			{Role: protocol.RoleAssistant, Content: "在的", ToolCalls: []protocol.ToolCall{
				{ID: "call-1", Name: "get_weather", Arguments: map[string]any{"city": "上海"}},
			}},
			{Role: protocol.RoleTool, ToolCallID: "call-1", Content: "晴"},
		},
		CallCount: 3,
		ToolCount: 1,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now,
	}
	src.SetPlan(&Plan{
		Task:   "查天气",
		Active: true,
		Steps:  []PlanStep{{ID: "s1", Description: "第一步", Status: PlanInProgress}},
	})
	src.pendingImage = &PendingImageState{
		Parts:     []PendingImageRef{{ChatID: "1", PlatformMsgID: "m1", URL: "https://example.invalid/1.png"}},
		Extra:     1,
		Timestamp: now,
	}

	require.NoError(t, store.Save(src))

	loaded, err := store.Load(src.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, src.ID, loaded.ID)
	assert.Equal(t, src.UserID, loaded.UserID)
	assert.Equal(t, src.ChatID, loaded.ChatID)
	assert.Equal(t, src.Messages, loaded.Messages)
	assert.Equal(t, src.CallCount, loaded.CallCount)
	assert.Equal(t, src.ToolCount, loaded.ToolCount)
	assert.True(t, src.CreatedAt.Equal(loaded.CreatedAt))
	assert.True(t, src.UpdatedAt.Equal(loaded.UpdatedAt))

	plan := loaded.PlanSnapshot()
	require.NotNil(t, plan)
	assert.Equal(t, "查天气", plan.Task)
	assert.True(t, plan.Active)

	refs, extra := loaded.ConsumePendingImage(time.Hour, now)
	require.Len(t, refs, 1)
	assert.Equal(t, "m1", refs[0].PlatformMsgID)
	assert.Equal(t, 1, extra)
}

// TestGormSessionStoreUpsertUpdatesInPlace 二次保存必须是"更新"而不是"插入"，
// 且归零的计数要能写回（GORM 的 default:0 + 零值组合很容易把归零吞掉）。
func TestGormSessionStoreUpsertUpdatesInPlace(t *testing.T) {
	client, store := newSessionTestStore(t)

	session := &Session{
		ID:        "p:c:u",
		UserID:    "u",
		ChatID:    "c",
		Messages:  []protocol.Message{{Role: protocol.RoleUser, Content: "第一句"}},
		CallCount: 5,
		ToolCount: 2,
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, store.Save(session))

	session.Messages = append(session.Messages, protocol.Message{Role: protocol.RoleAssistant, Content: "第二句"})
	session.CallCount = 0
	session.ToolCount = 0
	require.NoError(t, store.Save(session))

	loaded, err := store.Load(session.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Len(t, loaded.Messages, 2, "second save must update, not duplicate")
	assert.Equal(t, "第二句", loaded.Messages[1].Content)
	assert.Equal(t, 0, loaded.CallCount, "zeroed counters must be persisted")
	assert.Equal(t, 0, loaded.ToolCount)

	var rows int64
	require.NoError(t, client.DB().Model(&Record{}).Count(&rows).Error)
	assert.Equal(t, int64(1), rows, "upsert must not leave duplicate rows")
}

func TestGormSessionStoreDelete(t *testing.T) {
	_, store := newSessionTestStore(t)

	src := &Session{ID: "p:c:u", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}}}
	require.NoError(t, store.Save(src))

	require.NoError(t, store.Delete(src.ID))
	loaded, err := store.Load(src.ID)
	require.NoError(t, err)
	assert.Nil(t, loaded, "deleted session must not be loadable")

	// 删除不存在的行不是错误（幂等）。
	assert.NoError(t, store.Delete("missing"))
}

func TestGormSessionStoreLoadCorruptedHistoryReturnsError(t *testing.T) {
	client, store := newSessionTestStore(t)

	require.NoError(t, client.DB().Exec(
		`INSERT INTO ai_session_records (id, user_id, chat_id, messages, call_count, tool_count, updated_at)
		 VALUES (?, ?, ?, ?, 0, 0, ?)`,
		"broken", "u", "c", "{not json", time.Now(),
	).Error)

	loaded, err := store.Load("broken")
	require.Error(t, err, "corrupted history must surface as an error, not empty history")
	assert.Nil(t, loaded)
}

func TestGormSessionStoreSaveNil(t *testing.T) {
	_, store := newSessionTestStore(t)
	err := store.Save(nil)
	assert.True(t, errors.Is(err, ErrNilSession), "Save(nil) error = %v, want ErrNilSession", err)
}

// TestGormSessionStoreLargeMessages 验证 mediumtext 不只是标签：中文工具结果
// 动辄几十 KB，超过 MySQL TEXT 的 64KB 上限就该由列类型兜住。
func TestGormSessionStoreLargeMessages(t *testing.T) {
	_, store := newSessionTestStore(t)

	// 每条 40KB 中文（UTF-8 下 3 字节/字），20 条远超 64KB。
	chunk := strings.Repeat("工具结果", 4000)
	msgs := make([]protocol.Message, 0, 20)
	for i := range 20 {
		msgs = append(msgs, protocol.Message{Role: protocol.RoleTool, ToolCallID: fmt.Sprintf("c%d", i), Content: chunk})
	}
	src := &Session{ID: "big", Messages: msgs, UpdatedAt: time.Now()}
	require.NoError(t, store.Save(src))

	loaded, err := store.Load("big")
	require.NoError(t, err)
	require.Len(t, loaded.Messages, 20)
	assert.Equal(t, chunk, loaded.Messages[0].Content)
}

func TestNewGormSessionStoreNilClient(t *testing.T) {
	store, err := NewGormSessionStore(nil)
	assert.True(t, errors.Is(err, ErrNilStorageClient), "error = %v, want ErrNilStorageClient", err)
	assert.Nil(t, store)
}

func TestGormSessionStorePurgeExpired(t *testing.T) {
	_, store := newSessionTestStore(t)

	purger, ok := store.(ExpiredPurger)
	require.True(t, ok, "GormStore must implement ExpiredPurger")

	old := &Session{ID: "old", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "旧"}}, UpdatedAt: time.Now().Add(-48 * time.Hour)}
	fresh := &Session{ID: "fresh", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "新"}}, UpdatedAt: time.Now()}
	require.NoError(t, store.Save(old))
	require.NoError(t, store.Save(fresh))

	removed, err := purger.PurgeExpired(time.Now().Add(-24 * time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	gone, err := store.Load("old")
	require.NoError(t, err)
	assert.Nil(t, gone)

	kept, err := store.Load("fresh")
	require.NoError(t, err)
	assert.NotNil(t, kept)
}

// TestSaveAfterDeleteSkipsPersist 冻结删除墓碑契约：/ai clear 之后，仍在进行
// 的回合（手里还握着同一个会话对象）收尾保存时不得把删除撤销。
func TestSaveAfterDeleteSkipsPersist(t *testing.T) {
	store := &tombstoneStore{}
	sm := NewSessionManager(10, 20, time.Hour, store)

	session := sm.GetOrCreate("p:c:u", "u", "c")
	sm.SaveSession(session)
	require.Equal(t, 1, store.saves)

	sm.Delete("p:c:u")
	// 模拟在途回合：删除之后继续往旧对象里追加并收尾保存。
	sm.AppendMessage(session, protocol.Message{Role: protocol.RoleUser, Content: "晚到的消息"})

	assert.Equal(t, 1, store.saves, "deleted session must not be written back")
}

type tombstoneStore struct {
	saves int
}

func (s *tombstoneStore) Load(string) (*Session, error) { return nil, nil }
func (s *tombstoneStore) Save(*Session) error           { s.saves++; return nil }
func (s *tombstoneStore) Delete(string) error           { return nil }

// --- 管理器与存储协作的降级/清理契约 ---

// flakyStore 前 failTimes 次 Load 失败，之后正常返回已落库的会话。
type flakyStore struct {
	mu        sync.Mutex
	saved     *Session
	failTimes int
	loadCalls int
	saves     int
	deletes   int
}

func (f *flakyStore) Load(string) (*Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadCalls++
	if f.loadCalls <= f.failTimes {
		return nil, errors.New("transient storage failure")
	}
	return f.saved, nil
}

func (f *flakyStore) Save(s *Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	f.saved = s
	return nil
}

func (f *flakyStore) Delete(string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	f.saved = nil
	return nil
}

// TestLoadFailureDoesNotOverwriteStoredHistory 冻结最重要的一条数据安全契约：
// 读取失败后本轮照常对话，但落库前必须重试加载并把旧历史合并回去，
// 绝不能用"空会话"覆盖库里那份读不出来的历史。
func TestLoadFailureDoesNotOverwriteStoredHistory(t *testing.T) {
	store := &flakyStore{
		failTimes: 1,
		saved: &Session{
			ID:        "p:c:u",
			Messages:  []protocol.Message{{Role: protocol.RoleUser, Content: "旧历史"}},
			CallCount: 4,
			ToolCount: 2,
		},
	}
	sm := NewSessionManager(10, 20, time.Hour, store)

	session := sm.GetOrCreate("p:c:u", "u", "c")
	require.True(t, session.LoadFailed(), "load error must put the session into degraded mode")
	assert.Empty(t, session.SnapshotMessages())

	sm.AppendMessage(session, protocol.Message{Role: protocol.RoleUser, Content: "新问题"})

	require.NotNil(t, store.saved)
	require.Len(t, store.saved.Messages, 2, "stored history must be recovered and merged")
	assert.Equal(t, "旧历史", store.saved.Messages[0].Content)
	assert.Equal(t, "新问题", store.saved.Messages[1].Content)
	assert.Equal(t, 4, store.saved.CallCount, "counters must not regress")
	assert.False(t, session.LoadFailed(), "flag should clear once recovery succeeds")
}

// TestLoadFailureSkipsPersistWhileStorageIsDown 重试仍失败时宁可这一轮不落库，
// 也不能写出一条空历史。
func TestLoadFailureSkipsPersistWhileStorageIsDown(t *testing.T) {
	store := &flakyStore{
		failTimes: 100,
		saved: &Session{
			ID:       "p:c:u",
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "旧历史"}},
		},
	}
	sm := NewSessionManager(10, 20, time.Hour, store)

	session := sm.GetOrCreate("p:c:u", "u", "c")
	sm.AppendMessage(session, protocol.Message{Role: protocol.RoleUser, Content: "新问题"})

	assert.Equal(t, 0, store.saves, "no write should happen while the history cannot be read")
	require.NotNil(t, store.saved)
	assert.Equal(t, "旧历史", store.saved.Messages[0].Content, "stored history must stay untouched")
	assert.True(t, session.LoadFailed(), "flag stays set until recovery succeeds")
}

// TestCleanupExpiredPurgesStorage 验证库侧清理：按保留期（retention）删除
// 不再驻留内存的过期行，否则数据库会随运行时间单调增长。
func TestCleanupExpiredPurgesStorage(t *testing.T) {
	store := &purgeRecorder{}
	sm := NewSessionManager(10, 20, time.Hour, store)
	sm.SetRetention(30 * 24 * time.Hour)
	sm.GetOrCreate("p:c:u", "u", "c")

	sm.CleanupExpired()

	require.Equal(t, 1, store.purgeCalls, "CleanupExpired should ask the store to purge expired rows")
	assert.WithinDuration(t, time.Now().Add(-30*24*time.Hour), store.purgedBefore, time.Minute,
		"purge cutoff must follow retention, not the memory TTL")
	assert.Equal(t, 0, store.deleteCalls, "row deletion is retention's job, not memory eviction's")
}

// TestRetentionZeroKeepsHistoryForever 保留期 <= 0 时不做库侧时间清理：
// 只有显式 Delete（/ai reset）才会删行。
func TestRetentionZeroKeepsHistoryForever(t *testing.T) {
	store := &purgeRecorder{}
	sm := NewSessionManager(10, 20, time.Hour, store)
	sm.GetOrCreate("p:c:u", "u", "c")

	sm.CleanupExpired()

	assert.Equal(t, 0, store.purgeCalls)
	assert.Equal(t, 0, store.deleteCalls)
}

// TestMemoryEvictionKeepsPersistedHistory 内存过期只回收缓存，绝不删库：
// 这是"缓存语义"与"数据保留"拆开后的核心契约——用户闲置一天后再回来，
// 历史应该还在。
func TestMemoryEvictionKeepsPersistedHistory(t *testing.T) {
	store := &purgeRecorder{}
	sm := NewSessionManager(10, 20, 10*time.Millisecond, store)
	sm.SetRetention(30 * 24 * time.Hour)
	sm.GetOrCreate("p:c:u", "u", "c")

	time.Sleep(30 * time.Millisecond)
	sm.CleanupExpired()

	assert.Nil(t, sm.Peek("p:c:u"), "idle session should leave the memory cache")
	assert.Equal(t, 0, store.deleteCalls, "memory eviction must not delete the stored row")
}

type purgeRecorder struct {
	purgeCalls   int
	deleteCalls  int
	purgedBefore time.Time
}

func (p *purgeRecorder) Load(string) (*Session, error) { return nil, nil }
func (p *purgeRecorder) Save(*Session) error           { return nil }
func (p *purgeRecorder) Delete(string) error {
	p.deleteCalls++
	return nil
}
func (p *purgeRecorder) PurgeExpired(before time.Time) (int64, error) {
	p.purgeCalls++
	p.purgedBefore = before
	return 0, nil
}

// TestSaveSessionSerializesConcurrentSnapshots 并发追加 + 落库时，最终记录必须
// 覆盖全部消息：快照在 idLock 内取，晚到的写入者看到的是完整状态，
// 不会被先前较旧的快照覆盖回去。
func TestSaveSessionSerializesConcurrentSnapshots(t *testing.T) {
	store := &orderStore{}
	sm := NewSessionManager(64, 64, time.Hour, store)
	session := sm.GetOrCreate("p:c:u", "u", "c")

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sm.AppendMessageNoPersist(session, protocol.Message{Role: protocol.RoleUser, Content: fmt.Sprintf("m%d", i)})
			sm.SaveSession(session)
		}(i)
	}
	wg.Wait()

	last := store.snapshot()
	require.Len(t, last, 20, "final record must contain every appended message")
	seen := make(map[string]bool, len(last))
	for _, m := range last {
		seen[m.Content] = true
	}
	for i := range 20 {
		assert.True(t, seen[fmt.Sprintf("m%d", i)], "message m%d lost", i)
	}
}

type orderStore struct {
	mu   sync.Mutex
	last *Session
}

func (o *orderStore) Load(string) (*Session, error) { return nil, nil }

func (o *orderStore) Save(s *Session) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.last = s
	return nil
}

func (o *orderStore) Delete(string) error { return nil }

func (o *orderStore) snapshot() []protocol.Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.last == nil {
		return nil
	}
	return o.last.Messages
}
