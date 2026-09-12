package minecraft

import (
	"sync"
	"time"
)

// limiterMaxKeys 限流状态表的容量上限（按会话+用户维度，正常规模远达不到）。
const limiterMaxKeys = 4096

// scopeLimiter 按 key（会话 + 用户）记录上次执行时刻，实现固定间隔限流。
//
// 与框架的 eventctx.OnCooldown 不同：这里由插件自行判定并回复剩余等待时间，
// 避免"规则不匹配即彻底静默"的体验；同时只作用于真正的查询路径，
// /mc add、/mc rm、/mc list 不受影响。
type scopeLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	max      int
	last     map[string]time.Time
}

// newScopeLimiter 创建限流器。interval <= 0 时表示不限制（所有检查直接放行）。
func newScopeLimiter(interval time.Duration, maxKeys int) *scopeLimiter {
	if maxKeys < 1 {
		maxKeys = 1
	}
	return &scopeLimiter{
		interval: interval,
		max:      maxKeys,
		last:     make(map[string]time.Time),
	}
}

// retryAfter 返回 key 距离下次可用还需等待的时长；0 表示可以执行。
// 不修改任何状态。
func (l *scopeLimiter) retryAfter(key string) time.Duration {
	return l.retryAfterAt(key, time.Now())
}

// mark 记录 key 的一次执行。
func (l *scopeLimiter) mark(key string) {
	l.markAt(key, time.Now())
}

func (l *scopeLimiter) retryAfterAt(key string, now time.Time) time.Duration {
	if l == nil || l.interval <= 0 || key == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	last, ok := l.last[key]
	if !ok {
		return 0
	}
	if wait := l.interval - now.Sub(last); wait > 0 {
		return wait
	}
	return 0
}

func (l *scopeLimiter) markAt(key string, now time.Time) {
	if l == nil || l.interval <= 0 || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.last) >= l.max {
		l.evictLocked(now)
	}
	l.last[key] = now
}

// evictLocked 清理已过期的记录；仍满则整体清空
// （限流状态可随时重建，清空的代价只是放开一次窗口）。
func (l *scopeLimiter) evictLocked(now time.Time) {
	for k, t := range l.last {
		if now.Sub(t) >= l.interval {
			delete(l.last, k)
		}
	}
	if len(l.last) >= l.max {
		clear(l.last)
	}
}
