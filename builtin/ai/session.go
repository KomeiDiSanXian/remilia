package ai

import (
	"container/list"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Session 表示一个 AI 对话会话。
// 按 platform:chatID:userID 维度进行隔离。
type Session struct {
	ID        string
	UserID    string
	ChatID    string
	Messages  []Message
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionManager 管理 AI 对话会话，使用 LRU 淘汰策略。
//
// 功能：
//   - 自动创建/获取会话（GetOrCreate）
//   - 消息追加时自动裁剪上下文窗口
//   - 可选持久化存储（通过 SessionStore 接口）
//   - 定期清理过期会话
//   - LRU 淘汰（达到 maxSize 时淘汰最久未访问的）
type SessionManager struct {
	mu         sync.RWMutex
	sessions   map[string]*list.Element
	lru        *list.List
	maxSize    int
	maxHistory int
	ttl        time.Duration
	storage    SessionStore
}

// sessionEntry 包装 Session，存储在 LRU 链表中。
type sessionEntry struct {
	session *Session
}

// SessionStore 会话持久化存储接口。
// 实现此接口可将会话存储到不同后端。
type SessionStore interface {
	Load(sessionID string) (*Session, error)
	Save(session *Session) error
	Delete(sessionID string) error
}

func NewSessionManager(maxSize, maxHistory int, ttl time.Duration, storage SessionStore) *SessionManager {
	sm := &SessionManager{
		sessions:   make(map[string]*list.Element),
		lru:        list.New(),
		maxSize:    maxSize,
		maxHistory: maxHistory,
		ttl:        ttl,
		storage:    storage,
	}
	if sm.maxSize <= 0 {
		sm.maxSize = 1000
	}
	if sm.maxHistory <= 0 {
		sm.maxHistory = 20
	}
	return sm
}

// GetOrCreate 获取或创建会话。
// 先从 LRU 缓存查找，未命中时尝试从持久化存储加载，
// 都未找到时创建全新的会话。
func (sm *SessionManager) GetOrCreate(sessionID, userID, chatID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if elem, ok := sm.sessions[sessionID]; ok {
		entry := elem.Value.(*sessionEntry)
		sm.lru.MoveToFront(elem)
		entry.session.UpdatedAt = time.Now()
		trimMessages(entry.session, sm.maxHistory)
		return entry.session
	}

	session := &Session{
		ID:        sessionID,
		UserID:    userID,
		ChatID:    chatID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if sm.storage != nil {
		if stored, err := sm.storage.Load(sessionID); err == nil && stored != nil {
			session = stored
			session.UpdatedAt = time.Now()
			trimMessages(session, sm.maxHistory)
		}
	}

	elem := sm.lru.PushFront(&sessionEntry{session: session})
	sm.sessions[sessionID] = elem

	sm.evictLocked()

	return session
}

// Save 持久化保存会话到存储后端。
func (sm *SessionManager) Save(session *Session) {
	session.UpdatedAt = time.Now()
	trimMessages(session, sm.maxHistory)

	if sm.storage != nil {
		if err := sm.storage.Save(session); err != nil {
			fmt.Printf("ai: session persist error: %v\n", err)
		}
	}
}

// Delete 删除指定会话（从 LRU 和持久化存储中）。
func (sm *SessionManager) Delete(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if elem, ok := sm.sessions[sessionID]; ok {
		sm.lru.Remove(elem)
		delete(sm.sessions, sessionID)
	}

	if sm.storage != nil {
		_ = sm.storage.Delete(sessionID)
	}
}

// CleanupExpired 清理过期的会话。
// 由插件 Setup 中启动的后台 goroutine 定期调用。
func (sm *SessionManager) CleanupExpired() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for id, elem := range sm.sessions {
		entry := elem.Value.(*sessionEntry)
		if sm.ttl > 0 && now.After(entry.session.UpdatedAt.Add(sm.ttl)) {
			sm.lru.Remove(elem)
			delete(sm.sessions, id)
			if sm.storage != nil {
				_ = sm.storage.Delete(id)
			}
		}
	}
}

// evictLocked 在持有锁时执行 LRU 淘汰。
func (sm *SessionManager) evictLocked() {
	for sm.lru.Len() > sm.maxSize {
		elem := sm.lru.Back()
		if elem == nil {
			break
		}
		entry := elem.Value.(*sessionEntry)
		delete(sm.sessions, entry.session.ID)
		sm.lru.Remove(elem)
	}
}

// trimMessages 裁剪消息列表，保留最近的 maxHistory 条消息。
// System 消息优先保留。
func trimMessages(s *Session, maxHistory int) {
	if maxHistory <= 0 {
		return
	}

	usable := maxHistory
	if usable > len(s.Messages) {
		return
	}

	var systemMsgs []Message
	var otherMsgs []Message
	for _, m := range s.Messages {
		if m.Role == RoleSystem {
			systemMsgs = append(systemMsgs, m)
		} else {
			otherMsgs = append(otherMsgs, m)
		}
	}

	targetOther := max(usable-len(systemMsgs), 0)
	if len(otherMsgs) > targetOther {
		start := max(len(otherMsgs)-targetOther, 0)
		otherMsgs = otherMsgs[start:]
	}

	s.Messages = append(systemMsgs, otherMsgs...)
}

// AppendMessage 追加消息并持久化。
func (sm *SessionManager) AppendMessage(session *Session, msg Message) {
	session.Messages = append(session.Messages, msg)
	trimMessages(session, sm.maxHistory)
	sm.Save(session)
}

// --- GORM 持久化记录 ---

// sessionRecord 对应数据库表，用于 GORM 持久化。
type sessionRecord struct {
	ID        string `gorm:"primaryKey"`
	UserID    string `gorm:"index"`
	ChatID    string `gorm:"index"`
	Messages  string `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// toRecord 将 Session 转换为数据库记录。
func (s *Session) toRecord() *sessionRecord {
	data, _ := json.Marshal(s.Messages)
	return &sessionRecord{
		ID:        s.ID,
		UserID:    s.UserID,
		ChatID:    s.ChatID,
		Messages:  string(data),
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// toSession 将数据库记录还原为 Session。
func (r *sessionRecord) toSession() *Session {
	var msgs []Message
	_ = json.Unmarshal([]byte(r.Messages), &msgs)
	return &Session{
		ID:        r.ID,
		UserID:    r.UserID,
		ChatID:    r.ChatID,
		Messages:  msgs,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}
