// manager.go — 会话管理器：LRU 缓存、TTL 过期、消息窗口裁剪与可选持久化。
package session

import (
	"container/list"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// SessionManager 管理 AI 对话会话，使用 LRU 淘汰策略。
//
// 功能：
//   - GetOrCreate: 自动创建或获取会话（LRU 缓存 → 持久化存储 → 新建）
//   - Peek: 仅内存查找（不创建、不访问存储）
//   - PeekOrLoad: 只读查找（LRU → 持久化存储，不创建、不写库、不动 LRU）
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
// 内置实现：GormStore（基于 GORM）、NoopStore（空实现）。
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
		entry.session.Lock()
		entry.session.UpdatedAt = time.Now()
		TrimMessages(entry.session, sm.maxHistory)
		entry.session.Unlock()
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
			session.Lock()
			session.UpdatedAt = time.Now()
			TrimMessages(session, sm.maxHistory)
			session.Unlock()
		}
	}

	elem := sm.lru.PushFront(&sessionEntry{session: session})
	sm.sessions[sessionID] = elem

	sm.evictLocked()

	return session
}

// Peek 仅从内存缓存查找会话，不创建、不触碰持久化存储。
// 用于只需判断"会话是否存在/当前是否活跃"的场景（如 QQ 按钮回调忙时预检，
// 不应因误点旧消息的按钮而凭空创建一个空会话）；未命中返回 nil。
func (sm *SessionManager) Peek(sessionID string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if elem, ok := sm.sessions[sessionID]; ok {
		return elem.Value.(*sessionEntry).session
	}
	return nil
}

// PeekOrLoad 只读地查找会话：先查内存 LRU，未命中再查持久化存储。
//
// 与 GetOrCreate 的区别：不创建会话、不写库、不 trim、不插入或重排 LRU，
// 因此不会为从未与 AI 交互过的用户凭空造出一个会话，也不影响缓存淘汰顺序。
// 用于管理员查询他人使用状态这类只读场景。未找到返回 nil。
//
// 返回的会话可能是缓存中的活跃对象（会被并发回合修改），因此调用方读取
// 可变字段（Messages/CallCount/ToolCount 等）时需自行加锁。
func (sm *SessionManager) PeekOrLoad(sessionID string) *Session {
	sm.mu.RLock()
	if elem, ok := sm.sessions[sessionID]; ok {
		session := elem.Value.(*sessionEntry).session
		sm.mu.RUnlock()
		return session
	}
	sm.mu.RUnlock()

	if sm.storage == nil {
		return nil
	}
	stored, err := sm.storage.Load(sessionID)
	if err != nil {
		logger.Errorf("[AI] Failed to load session %s: %v", sessionID, err)
		return nil
	}
	return stored
}

// SaveLocked 持久化保存会话到存储后端（调用方须已持有会话锁）；需要自动加锁时用 SaveSession。
//
// 如果调用方未持有锁，请使用 [SaveSession] 替代。
func (sm *SessionManager) SaveLocked(session *Session) {
	session.UpdatedAt = time.Now()
	TrimMessages(session, sm.maxHistory)

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
	sm.SaveLocked(session)
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
// 先持锁收集过期会话并移出缓存，释放锁后再执行持久化删除，避免 DB I/O 阻塞其他会话操作。
func (sm *SessionManager) CleanupExpired() {
	var expiredIDs []string

	sm.mu.Lock()
	now := time.Now()
	for id, elem := range sm.sessions {
		entry := elem.Value.(*sessionEntry)
		entry.session.Lock()
		expired := sm.ttl > 0 && now.After(entry.session.UpdatedAt.Add(sm.ttl))
		entry.session.Unlock()
		if expired {
			sm.lru.Remove(elem)
			delete(sm.sessions, id)
			expiredIDs = append(expiredIDs, id)
		}
	}
	sm.mu.Unlock()

	if sm.storage != nil {
		for _, id := range expiredIDs {
			_ = sm.storage.Delete(id)
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

// TrimLowWaterDivisor 批量裁剪的"低水位"除数：超限时一次性多裁
// usable/TrimLowWaterDivisor 条，裁后要累积这么多条才会再次触发裁剪。
//
// 逐条滑窗（每次都裁到恰好 maxHistory）会让每次请求的历史前缀都向右位移
// 一条消息，LLM 侧的前缀缓存从第一条历史消息起整段失效——即使 System 提示
// 词完全稳定，历史也永远命中不了。批量裁剪牺牲少量保留条数，换来两次裁剪
// 之间前缀字节一致（默认 20 条约可稳定复用 4 轮左右）。
const TrimLowWaterDivisor = 4

// TrimMessages 裁剪消息列表，保留最近的 maxHistory 条消息。
// System 消息优先保留。超限时一次裁到低水位（见 TrimLowWaterDivisor），
// 避免历史前缀逐轮位移。
//
// 裁剪边界不会落在 tool 消息上：tool 消息必须紧邻其前的
// assistant(tool_calls)（OpenAI/Anthropic API 硬性约束），若 assistant
// 被裁掉而 tool 消息残留，会产生孤儿 tool 消息，下一次请求会被 400 拒绝。
// 因此起点会向前推进，把残留的 tool 响应连同其 assistant 一起裁掉。
func TrimMessages(s *Session, maxHistory int) {
	if maxHistory <= 0 {
		return
	}

	usable := maxHistory
	if usable > len(s.Messages) {
		return
	}

	var systemMsgs []protocol.Message
	var otherMsgs []protocol.Message
	for _, m := range s.Messages {
		if m.Role == protocol.RoleSystem {
			systemMsgs = append(systemMsgs, m)
		} else {
			otherMsgs = append(otherMsgs, m)
		}
	}

	targetOther := max(usable-len(systemMsgs), 0)
	if len(otherMsgs) > targetOther {
		keep := max(targetOther-max(targetOther/TrimLowWaterDivisor, 1), 1)
		start := max(len(otherMsgs)-keep, 0)
		for start < len(otherMsgs) && otherMsgs[start].Role == protocol.RoleTool {
			start++
		}
		otherMsgs = otherMsgs[start:]
	}

	s.Messages = append(systemMsgs, otherMsgs...)
}

// AppendMessage 向会话追加一条消息，自动裁剪上下文窗口并持久化。
// 线程安全，持有 session 写锁。
func (sm *SessionManager) AppendMessage(session *Session, msg protocol.Message) {
	session.Lock()
	defer session.Unlock()
	session.Messages = append(session.Messages, msg)
	sm.SaveLocked(session)
}
