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
	// idLocks 按会话 ID 分片，串行化同一会话的 Load/Save/Delete。
	// 数据库 I/O 不持有全局缓存锁，但清空、回填和收尾保存不能互相越过。
	idLocks [64]sync.Mutex
}

// idLock 返回会话 ID 对应的固定分片锁。哈希冲突只会增加少量串行等待，
// 不会影响会话正确性，也无需为每个历史会话长期保留一把锁。
func (sm *SessionManager) idLock(sessionID string) *sync.Mutex {
	hash := uint32(2166136261)
	for i := 0; i < len(sessionID); i++ {
		hash ^= uint32(sessionID[i])
		hash *= 16777619
	}
	return &sm.idLocks[hash%uint32(len(sm.idLocks))]
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
	// 同一会话的缓存查询、存储加载和插入与 Delete 串行化。若只在 Load 完成
	// 后检查墓碑，先开始的慢查询可能越过 Delete，把旧记录重新插回缓存。
	idLock := sm.idLock(sessionID)
	idLock.Lock()

	// 快路径：命中内存缓存时只在 sm.mu 内触碰会话，不做任何存储 I/O。
	sm.mu.Lock()
	if elem, ok := sm.sessions[sessionID]; ok {
		entry := elem.Value.(*sessionEntry)
		sm.lru.MoveToFront(elem)
		sm.mu.Unlock()
		idLock.Unlock()
		sm.touch(entry.session)
		return entry.session
	}
	sm.mu.Unlock()

	// 慢路径：存储加载在 sm.mu 之外执行。此前在全局写锁内 Load 会让单个慢查询
	// 阻塞所有会话的创建/查询/清理（队头阻塞）。同一 ID 的存储操作由分片锁串行化。
	var loaded *Session
	if sm.storage != nil {
		if stored, err := sm.storage.Load(sessionID); err == nil && stored != nil {
			loaded = stored
		}
	}

	sm.mu.Lock()

	// 二次检查：锁外加载期间可能已有并发 GetOrCreate 创建并登记了同一会话。
	if elem, ok := sm.sessions[sessionID]; ok {
		entry := elem.Value.(*sessionEntry)
		sm.lru.MoveToFront(elem)
		sm.mu.Unlock()
		idLock.Unlock()
		sm.touch(entry.session)
		return entry.session
	}

	session := loaded
	if session == nil {
		session = &Session{
			ID:        sessionID,
			UserID:    userID,
			ChatID:    chatID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	elem := sm.lru.PushFront(&sessionEntry{session: session})
	sm.sessions[sessionID] = elem

	sm.evictLocked()
	sm.mu.Unlock()
	idLock.Unlock()

	if loaded != nil {
		sm.touch(session)
	}
	return session
}

// touch 更新会话活跃时间并按窗口裁剪消息（内部持会话锁）。
func (sm *SessionManager) touch(session *Session) {
	session.Lock()
	session.UpdatedAt = time.Now()
	TrimMessages(session, sm.maxHistory)
	session.Unlock()
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
	idLock := sm.idLock(sessionID)
	idLock.Lock()
	defer idLock.Unlock()

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
// 会话已置删除墓碑（被 Delete）时跳过写库：在途回合的收尾保存不得把删除撤销。
func (sm *SessionManager) SaveLocked(session *Session) {
	idLock := sm.idLock(session.ID)
	idLock.Lock()
	defer idLock.Unlock()

	if session.Deleted() {
		logger.Debugf("[AI] Session %s was deleted, skipping persist", session.ID)
		return
	}
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
//
// 删除前对内存中的会话对象置墓碑：若此刻仍有回合在进行（回合收尾会调用
// SaveSession），墓碑保证收尾保存不会把整条记录重新写回存储——否则
// "/ai clear" 会被静默撤销，消息"删了又回来"。
func (sm *SessionManager) Delete(sessionID string) {
	idLock := sm.idLock(sessionID)
	idLock.Lock()
	defer idLock.Unlock()

	sm.mu.Lock()
	if elem, ok := sm.sessions[sessionID]; ok {
		entry := elem.Value.(*sessionEntry)
		entry.session.MarkDeleted()
		sm.lru.Remove(elem)
		delete(sm.sessions, sessionID)
	}
	sm.mu.Unlock()

	// 存储删除放在全局锁之外：DB I/O 不应阻塞其他会话的创建/查询。
	if sm.storage != nil {
		if err := sm.storage.Delete(sessionID); err != nil {
			logger.Errorf("[AI] Failed to delete session %s from storage: %v", sessionID, err)
		}
	}
}

// CleanupExpired 清理过期的会话。
// 由插件 Setup 中启动的后台 goroutine 定期调用。
// 先复制缓存条目，逐个在会话锁与 ID 分片锁内复查后移出缓存；数据库删除期间
// 不持有全局缓存锁，避免慢 I/O 阻塞其他会话操作。
// 正在执行回合的会话跳过本轮（避免与活跃回合产生双活对象，见 evictLocked）；
// 回合中的会话必然活跃（touch 会刷新 UpdatedAt），自然不会被判过期。
func (sm *SessionManager) CleanupExpired() {
	type candidate struct {
		id      string
		session *Session
	}
	entries := make([]candidate, 0)
	sm.mu.RLock()
	for id, elem := range sm.sessions {
		entries = append(entries, candidate{id: id, session: elem.Value.(*sessionEntry).session})
	}
	sm.mu.RUnlock()

	for _, entry := range entries {
		session := entry.session
		session.Lock()
		expired := sm.ttl > 0 && !session.Deleted() && !session.TurnActive() &&
			time.Now().After(session.UpdatedAt.Add(sm.ttl))
		if !expired {
			session.Unlock()
			continue
		}

		idLock := sm.idLock(entry.id)
		idLock.Lock()
		sm.mu.Lock()
		elem, ok := sm.sessions[entry.id]
		current := ok && elem.Value.(*sessionEntry).session == session
		expired = current && sm.ttl > 0 && !session.Deleted() && !session.TurnActive() &&
			time.Now().After(session.UpdatedAt.Add(sm.ttl))
		if expired {
			session.MarkDeleted()
			sm.lru.Remove(elem)
			delete(sm.sessions, entry.id)
		}
		sm.mu.Unlock()
		session.Unlock()

		if expired && sm.storage != nil {
			if err := sm.storage.Delete(entry.id); err != nil {
				logger.Errorf("[AI] Failed to delete expired session %s from storage: %v", entry.id, err)
			}
		}
		idLock.Unlock()
	}
}

// evictLocked 在持有锁时执行 LRU 淘汰。
func (sm *SessionManager) evictLocked() {
	for sm.lru.Len() > sm.maxSize {
		elem := sm.evictCandidateLocked()
		if elem == nil {
			// 全部会话都有活跃回合：暂不淘汰，等回合结束后再裁。
			return
		}
		entry := elem.Value.(*sessionEntry)
		delete(sm.sessions, entry.session.ID)
		sm.lru.Remove(elem)
	}
}

// evictCandidateLocked 从链表尾部挑选淘汰候选：优先最久未访问的会话，但
// 跳过正在执行回合的会话。淘汰一个活跃回合会话会让同一 sessionID 的下一条
// 消息从存储加载出另一个对象，与进行中回合持有的对象双活——回合收尾的
// 整记录 upsert 会覆盖掉缓存对象已落库的中间消息，且 /ai stop 无法抢断。
// 全部会话都活跃时返回 nil（调用方应放弃本轮淘汰）。
func (sm *SessionManager) evictCandidateLocked() *list.Element {
	for elem := sm.lru.Back(); elem != nil; elem = elem.Prev() {
		entry := elem.Value.(*sessionEntry)
		if !entry.session.TurnActive() {
			return elem
		}
	}
	return nil
}

// TrimLowWaterDivisor / TrimLowWaterMin 批量裁剪的"低水位"规则：超限时让出
// max(TrimLowWaterMin, targetOther/TrimLowWaterDivisor) 条余量，裁后要再累积
// 这么多条才会触发下一次裁剪。
//
// 逐条滑窗（每次都裁到恰好 maxHistory）会让每次请求的历史前缀都向右位移
// 一条消息，LLM 侧的前缀缓存从第一条历史消息起整段失效——即使 System 提示
// 词完全稳定，历史也永远命中不了。批量裁剪牺牲少量保留条数，换来两次裁剪
// 之间前缀字节一致。
//
// 余量必须随窗口放大：max_history=50（生产配置）时固定 4 条只够一个回合
// 的一半——一个带工具调用的回合会一次追加 assistant + N 条 tool 结果，
// 裁剪因此常常落在回合中途，把本回合刚生成的工具结果前缀一起作废。按
// 1/4 窗口让出余量（50 → 12 条 ≈ 4 轮）后，裁剪间隔变长而单次失效的
// 历史更多，缓存命中率整体更高。下限 TrimLowWaterMin 保留小窗口
// （max_history=8 等）的"不逐轮裁剪"收益。
const (
	TrimLowWaterDivisor = 4
	TrimLowWaterMin     = 4
)

// trimHeadroom 返回一次批量裁剪应让出的余量（message 条数）。
// targetOther 为可裁剪（非 system）消息的上限条数。
func trimHeadroom(targetOther int) int {
	if targetOther <= 0 {
		return 0
	}
	return max(TrimLowWaterMin, targetOther/TrimLowWaterDivisor)
}

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
		// 低水位让出 trimHeadroom 条余量：裁后要累积这么多条才再次触发裁剪，
		// 两次裁剪之间的前缀字节才稳定（规则与理由见 TrimLowWaterDivisor）。
		keep := 0
		if targetOther > 0 {
			keep = max(targetOther-trimHeadroom(targetOther), 1)
		}
		start := max(len(otherMsgs)-keep, 0)
		for start < len(otherMsgs) && otherMsgs[start].Role == protocol.RoleTool {
			start++
		}
		otherMsgs = otherMsgs[start:]
	}

	s.Messages = append(systemMsgs, otherMsgs...)
}

// AppendMessage 向会话追加一条消息，自动裁剪上下文窗口并持久化。
// 线程安全。跨回合的即时持久化（如用户消息）用它；一回合内的多条中间消息
// （assistant/tool/反思/重规划）请用 [AppendMessageNoPersist] 追加、回合结束时
// 用 [SaveSession] 一次性落库，避免每条工具结果都触发一次整记录 upsert。
func (sm *SessionManager) AppendMessage(session *Session, msg protocol.Message) {
	sm.AppendMessageNoPersist(session, msg)
	sm.SaveSession(session)
}

// AppendMessageNoPersist 向会话追加一条消息并裁剪窗口，但不写持久化存储。
//
// 供一回合内的多次追加使用：回合内每条 tool 结果各写一次库会把单次 upsert
// 放大成 N 次，改为调用方在回合结束时（processWithTools 的 defer）统一
// [SaveSession]，把 N 次整记录 upsert 降为 1 次。线程安全，持有 session 写锁。
func (sm *SessionManager) AppendMessageNoPersist(session *Session, msg protocol.Message) {
	session.Lock()
	defer session.Unlock()
	session.Messages = append(session.Messages, msg)
	TrimMessages(session, sm.maxHistory)
}
