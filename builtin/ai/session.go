// Package ai session.go — 会话管理：Session 定义、LRU 缓存、消息裁剪、GORM 持久化记录。
//
// 本文件实现 AI 对话会话的完整生命周期管理：
//   - Session: 单个对话会话的数据结构
//   - SessionManager: LRU 缓存 + TTL 过期 + 可选持久化的会话管理器
//   - SessionStore: 持久化存储接口
//   - trimMessages: 上下文窗口裁剪（保留 System 消息 + 最近 maxHistory 条）
//   - sessionRecord: GORM 数据库记录结构体及转换方法
package ai

import (
	"container/list"
	"encoding/json"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Session 表示一个 AI 对话会话，按 platform:chatID:userID 维度隔离。
//
// 字段说明：
//   - ID: 会话唯一标识，格式 "{platform}:{chatID}:{userID}"
//   - UserID: 用户 ID
//   - ChatID: 群组/频道 ID
//   - Messages: 对话消息列表（受 mu 保护）
//   - CreatedAt: 会话创建时间
//   - UpdatedAt: 会话最后活跃时间（用于 TTL 过期判断）
//   - CallCount: 本轮对话中 LLM API 的累计调用次数
//   - ToolCount: 本轮对话中工具调用的累计次数
//   - contentCache: 附件二进制内容的内存缓存（按 URL key，限本轮会话有效，不持久化）
type Session struct {
	mu        sync.Mutex
	ID        string
	UserID    string
	ChatID    string
	Messages  []Message
	CreatedAt time.Time
	UpdatedAt time.Time
	CallCount int
	ToolCount int

	contentCache map[string]*cachedContent `json:"-"`
}

// Lock 锁定会话，禁止并发访问 Messages 等可变字段。
func (s *Session) Lock() { s.mu.Lock() }

// Unlock 解锁会话。
func (s *Session) Unlock() { s.mu.Unlock() }

// getCachedContent 从内存缓存中获取附件内容。已过期或不存在返回 nil。
// 线程安全，持有 session 读锁。
func (s *Session) getCachedContent(url string) *cachedContent {
	s.Lock()
	defer s.Unlock()
	if s.contentCache == nil {
		return nil
	}
	c, ok := s.contentCache[url]
	if !ok {
		return nil
	}
	if time.Now().After(c.ExpireAt) {
		delete(s.contentCache, url)
		return nil
	}
	return c
}

// setCachedContent 将附件内容存入内存缓存，TTL 默认 10 分钟。
// 线程安全，持有 session 写锁。
func (s *Session) setCachedContent(url string, data []byte, mimeType, audioFormat string) {
	s.Lock()
	defer s.Unlock()
	if s.contentCache == nil {
		s.contentCache = make(map[string]*cachedContent)
	}
	s.contentCache[url] = &cachedContent{
		Data:        data,
		MimeType:    mimeType,
		AudioFormat: audioFormat,
		ExpireAt:    time.Now().Add(10 * time.Minute),
	}
}

// SessionManager 管理 AI 对话会话，使用 LRU 淘汰策略。
//
// 功能：
//   - GetOrCreate: 自动创建或获取会话（LRU 缓存 → 持久化存储 → 新建）
//   - Save: 持久化保存会话
//   - Delete: 删除会话（LRU + 持久化）
//   - AppendMessage: 追加消息并自动裁剪上下文窗口
//   - CleanupExpired: 清理超过 TTL 未活跃的会话
//   - evictLocked: LRU 淘汰（达到 maxSize 时淘汰最久未访问的）
type SessionManager struct {
	mu         sync.RWMutex
	sessions   map[string]*list.Element
	lru        *list.List
	maxSize    int
	maxHistory int
	ttl        time.Duration
	storage    SessionStore
}

// sessionEntry 包装 Session，作为 LRU 链表的节点值。
type sessionEntry struct {
	session *Session
}

// SessionStore 会话持久化存储接口。
//
// 实现此接口可将会话存储到不同后端（数据库、Redis 等）。
// 内置实现：gormSessionStore（基于 GORM）、noopSessionStore（空实现）。
type SessionStore interface {
	Load(sessionID string) (*Session, error)
	Save(session *Session) error
	Delete(sessionID string) error
}

// NewSessionManager 创建会话管理器。
//
// 参数：
//   - maxSize: LRU 缓存最大会话数，<= 0 时使用默认值 1000
//   - maxHistory: 保留的最大消息条数，<= 0 时使用默认值 20
//   - ttl: 会话 TTL，超过此时间未活跃的会话将被 CleanupExpired 清理
//   - storage: 持久化存储实现，为 nil 时不持久化
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

// Save 持久化保存会话到存储后端。调用方应已持有 session 锁。
//
// 如果调用方未持有锁，请使用 [SaveSession] 替代。
func (sm *SessionManager) saveNoLock(session *Session) {
	session.UpdatedAt = time.Now()
	trimMessages(session, sm.maxHistory)

	if sm.storage != nil {
		if err := sm.storage.Save(session); err != nil {
			logger.Errorf("[AI] Failed to persist session %s: %v", session.ID, err)
		}
	}
}

// SaveSession 线程安全地持久化保存会话，自动加锁。
func (sm *SessionManager) SaveSession(session *Session) {
	session.Lock()
	defer session.Unlock()
	sm.saveNoLock(session)
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

// AppendMessage 向会话追加一条消息，自动裁剪上下文窗口并持久化。
// 线程安全，持有 session 写锁。
func (sm *SessionManager) AppendMessage(session *Session, msg Message) {
	session.Lock()
	defer session.Unlock()
	session.Messages = append(session.Messages, msg)
	sm.saveNoLock(session)
}

// --- GORM 持久化记录 ---

// sessionRecord 对应数据库表，用于 GORM 持久化。
type sessionRecord struct {
	ID        string `gorm:"primaryKey"`
	UserID    string `gorm:"index"`
	ChatID    string `gorm:"index"`
	Messages  string `gorm:"type:text"`
	CallCount int    `gorm:"default:0"`
	ToolCount int    `gorm:"default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// toRecord 将 Session 转换为数据库记录。
func (s *Session) toRecord() *sessionRecord {
	data, err := json.Marshal(s.Messages)
	if err != nil {
		logger.Errorf("[AI] Failed to marshal session messages for %s: %v", s.ID, err)
		data = []byte("[]")
	}
	return &sessionRecord{
		ID:        s.ID,
		UserID:    s.UserID,
		ChatID:    s.ChatID,
		Messages:  string(data),
		CallCount: s.CallCount,
		ToolCount: s.ToolCount,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// toSession 将数据库记录还原为 Session。
func (r *sessionRecord) toSession() *Session {
	var msgs []Message
	if err := json.Unmarshal([]byte(r.Messages), &msgs); err != nil {
		logger.Warnf("[AI] Failed to unmarshal session messages (corrupted?): %v", err)
	}
	return &Session{
		ID:        r.ID,
		UserID:    r.UserID,
		ChatID:    r.ChatID,
		Messages:  msgs,
		CallCount: r.CallCount,
		ToolCount: r.ToolCount,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}
