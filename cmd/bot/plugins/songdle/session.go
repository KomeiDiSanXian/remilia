package songdle

import (
	"sync"
	"time"
)

// SessionKey 生成会话键：user 维度附加用户 ID，group 维度忽略用户。
func SessionKey(platform, chatID, userID string, scope Scope) string {
	if scope == ScopeUser {
		return platform + "|" + chatID + "|" + userID
	}
	return platform + "|" + chatID
}

// SessionStore 内存会话表：按会话键隔离对局，并按空闲时间回收。
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

// Get 按会话键取对局。
func (s *SessionStore) Get(key string) (*Game, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.games[key]
	return g, ok
}

// Find 按平台 / 会话 / 用户查找对局：优先用户维度，其次群维度；都优先未结束的对局。
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
// 未找到任何对局时返回 false。
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
	// 兜底：只剩已结束的对局时也允许 fn 读取，用于给出提示。
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

// Sweep 清理空闲超时的对局，并返回其中「尚未结束」的对局。
//
// 未结束就被回收意味着玩家弃局：调用方需要把它们结算为失败，否则只要挂机
// 就能逃掉一次败场。已结束的对局直接丢弃即可。
func (s *SessionStore) Sweep() []*Game {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-s.ttl)
	var abandoned []*Game
	for k, g := range s.games {
		if !g.UpdatedAt.Before(cutoff) {
			continue
		}
		delete(s.games, k)
		if !g.Finished {
			abandoned = append(abandoned, g)
		}
	}
	return abandoned
}
