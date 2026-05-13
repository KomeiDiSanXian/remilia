package fsm

import "sync"

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
	// CreatedAt 是会话创建的 Unix 时间戳。
	CreatedAt int64
	// UpdatedAt 是最近一次迁移的 Unix 时间戳。
	UpdatedAt int64
}

// Storage 是 FSM 会话的持久化接口。实现必须是并发安全的。
// 默认实现是 [MemoryStorage]。
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
