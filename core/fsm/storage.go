package fsm

import (
	"maps"
	"sync"
)

// Session 表示单个 FSM 会话的运行时状态。
//
// 在迁移调用之间通过 [Storage] 接口持久化。
// [Data] map 在迁移之间携带用户定义的键值对。
type Session struct {
	// ID 唯一标识此会话（例如 "platform:chatID"）。
	ID string
	// FSMName 是此会话所属的 FSM 定义名称。
	FSMName string
	// Current 是此会话的当前状态。
	Current State
	// Data 是用户定义的键值存储，在迁移之间持久化。
	Data map[string]any
	// ExpireAt 是此会话被认为过期的 Unix 时间戳。零值表示无过期。
	ExpireAt int64
	// OwnerID 是会话归属者（启动会话的用户 ID），仅在 [FSM.OwnerScoped]
	// 为 true 时写入。
	//
	// 非空时 [Engine.TryTransition] 只接受归属者推进，避免群聊里其它成员
	// 接管他人的多步流程。空值表示未启用归属校验。
	OwnerID string
	// CreatedAt 是会话创建的 Unix 时间戳。
	CreatedAt int64
	// UpdatedAt 是最近一次迁移的 Unix 时间戳。
	UpdatedAt int64
}

// Storage 是 FSM 会话的持久化接口。实现必须是并发安全的。
// 默认实现是 [MemoryStorage]。
//
// # 所有权契约
//
//   - Get 返回的 *Session 归调用方临时持有：引擎在**会话锁内**原地改写
//     Current/UpdatedAt/Data 后调用 Save 回写，因此实现不得在 Get 返回后
//     复用该对象（Load/Save 语义）。
//   - 只读枚举路径（如 [Engine.ListSessions]）必须使用快照：如果后端支持
//     List，返回的会话必须是深拷贝（含 Data），不得暴露存储内部的活对象，
//     否则枚举会与并发迁移竞争（数据竞争 / concurrent map read and write）。
//   - 实现必须保证 Get 返回的会话 Data 非 nil（[Engine] 会直接写入）。
type Storage interface {
	// Get 按 ID 检索会话。未找到或已过期时返回 nil。
	Get(sessionID string) *Session
	// Save 持久化一个会话（创建或更新）。
	Save(session *Session)
	// Delete 按 ID 删除一个会话。
	Delete(sessionID string)
	// Cleanup 删除所有 ExpireAt <= before 的会话。返回被删除的会话数量。
	Cleanup(before int64) int
}

// MemoryStorage 是 [Storage] 的内存实现。
//
// 它使用 sync.RWMutex 保证并发安全，自动将过期会话视为未找到。
//
// 注意：Get 只是把过期会话视为不存在，并不会删除条目——
// 过期条目的真正回收依赖 [Engine.StartCleanup]。长期运行且设置了
// Timeout 的场景请务必启动清理，否则 sessions map 只增不减。
type MemoryStorage struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewMemoryStorage 创建一个空的内存 FSM 会话存储。
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		sessions: make(map[string]*Session),
	}
}

// Get 按 ID 检索会话。如果会话不存在或已过期（ExpireAt 在过去），返回 nil。
func (s *MemoryStorage) Get(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sess, ok := s.sessions[id]; ok {
		if sess.ExpireAt > 0 && sess.ExpireAt <= nowUnix() {
			return nil
		}
		return sess
	}
	return nil
}

// Save 创建或更新一个会话。
func (s *MemoryStorage) Save(session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
}

// List 返回当前所有会话（过期会话同样返回，由调用方自行过滤）。
//
// 返回的是存储内部会话的引用（与 Get 一致）：字段与 Data 的读取必须由
// 调用方在会话锁内进行——引擎在迁移时于会话锁内原地改写
// Current/UpdatedAt/Data，任何锁外读取都会与迁移竞争。
// [Engine.ListSessions] 正是在会话锁内深拷贝出快照的。
func (s *MemoryStorage) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if sess == nil {
			continue
		}
		out = append(out, sess)
	}
	return out
}

// snapshot 深拷贝会话（含 Data），调用方必须持有该会话的会话锁。
func (s *Session) snapshot() Session {
	cp := *s
	if s.Data != nil {
		cp.Data = make(map[string]any, len(s.Data))
		maps.Copy(cp.Data, s.Data)
	}
	return cp
}

// Delete 按 ID 删除一个会话。如果会话不存在则为空操作。
func (s *MemoryStorage) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// Cleanup 删除所有 ExpireAt <= before 的会话，并返回被删除的会话数量。
func (s *MemoryStorage) Cleanup(before int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for id, sess := range s.sessions {
		if sess.ExpireAt > 0 && sess.ExpireAt <= before {
			delete(s.sessions, id)
			count++
		}
	}
	return count
}
