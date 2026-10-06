package dealornodeal

import (
	"strings"
	"sync"
	"time"
)

// Scope 对局隔离维度。
type Scope uint8

const (
	// ScopeUser 单人局：每人独立一局，同群内互不影响（默认）。
	ScopeUser Scope = iota
	// ScopeGroup 群投票局：全群共享一局，任何人可开箱、对报价投票。
	ScopeGroup
)

// String 返回配置/展示用的维度名。
func (s Scope) String() string {
	if s == ScopeGroup {
		return "group"
	}
	return "user"
}

// ParseScope 解析隔离维度字符串，无法识别时 ok 为 false。
func ParseScope(s string) (Scope, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "user", "private", "personal", "single", "solo":
		return ScopeUser, true
	case "group", "chat", "shared", "vote":
		return ScopeGroup, true
	default:
		return ScopeUser, false
	}
}

// SessionStore 是按会话键保存对局的内存表，带空闲回收。
type SessionStore struct {
	mu    sync.Mutex
	games map[string]*Game
	ttl   time.Duration
	now   func() time.Time
}

// NewSessionStore 创建会话表；ttl <= 0 时使用默认 30 分钟。
func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &SessionStore{games: make(map[string]*Game), ttl: ttl, now: time.Now}
}

// SessionKey 生成会话键：user 维度附加用户 ID，group 维度忽略用户。
func SessionKey(platform, chatID, userID string, scope Scope) string {
	if scope == ScopeGroup {
		return platform + "|" + chatID
	}
	return platform + "|" + chatID + "|" + userID
}

// Get 按会话键取对局。
func (s *SessionStore) Get(key string) (*Game, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.games[key]
	return g, ok
}

// Find 按平台/会话/用户查找对局：优先用户维度、其次群维度，且优先未结束的对局。
func (s *SessionStore) Find(platform, chatID, userID string) (*Game, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	userKey := SessionKey(platform, chatID, userID, ScopeUser)
	groupKey := SessionKey(platform, chatID, "", ScopeGroup)
	u, uok := s.games[userKey]
	g, gok := s.games[groupKey]
	if uok && !u.Finished {
		return u, true
	}
	if gok && !g.Finished {
		return g, true
	}
	if uok {
		return u, true
	}
	if gok {
		return g, true
	}
	return nil, false
}

// UpdateFound 在锁内查找并修改对局，保证同一对局的并发操作串行执行。
//
// 查找顺序与 [SessionStore.Find] 一致；fn 在持锁状态下执行，不应阻塞。
func (s *SessionStore) UpdateFound(platform, chatID, userID string, fn func(*Game)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	userKey := SessionKey(platform, chatID, userID, ScopeUser)
	groupKey := SessionKey(platform, chatID, "", ScopeGroup)

	apply := func(key string, requireActive bool) bool {
		g, ok := s.games[key]
		if !ok || (requireActive && g.Finished) {
			return false
		}
		fn(g)
		return true
	}
	if apply(userKey, true) || apply(groupKey, true) {
		return true
	}
	return apply(userKey, false) || apply(groupKey, false)
}

// Put 写入对局。
func (s *SessionStore) Put(key string, g *Game) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.games[key] = g
}

// Delete 删除对局。
func (s *SessionStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.games, key)
}

// Len 返回当前对局数。
func (s *SessionStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.games)
}

// Sweep 回收空闲超时的对局，返回回收数量。
func (s *SessionStore) Sweep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-s.ttl)
	removed := 0
	for k, g := range s.games {
		if g.UpdatedAt.Before(cutoff) {
			delete(s.games, k)
			removed++
		}
	}
	return removed
}
