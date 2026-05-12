package wasm

import (
	"sync"
	"time"
)

// Sandbox 为 WASM 模块提供资源限制和并发控制。
type Sandbox struct {
	mu          sync.Mutex
	callLimiter *TokenBucket
	memoryPages uint32
}

// TokenBucket 是一个简单的令牌桶限流器。
type TokenBucket struct {
	mu       sync.Mutex
	rate     int64
	capacity int64
	tokens   int64
	lastTime time.Time
}

// NewTokenBucket 创建一个令牌桶。
// rate 是每秒生成的令牌数，capacity 是桶容量。
func NewTokenBucket(rate, capacity int64) *TokenBucket {
	if rate <= 0 {
		rate = DefaultMaxCallPerSec
	}
	if capacity <= 0 {
		capacity = rate
	}
	return &TokenBucket{
		rate:     rate,
		capacity: capacity,
		tokens:   capacity,
		lastTime: time.Now(),
	}
}

// Allow 尝试消耗一个令牌，成功返回 true，限流返回 false。
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime)
	tb.lastTime = now

	// 补充令牌
	tb.tokens += int64(elapsed.Seconds()) * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	if tb.tokens > 0 {
		tb.tokens--
		return true
	}
	return false
}

// NewSandbox 创建一个沙箱实例。
// memoryPages 是最大内存页数（每页 64KB），0 表示使用默认值。
func NewSandbox(memoryPages uint32, maxCallPerSec int64) *Sandbox {
	if memoryPages == 0 {
		memoryPages = DefaultMemoryPages
	}
	if maxCallPerSec <= 0 {
		maxCallPerSec = DefaultMaxCallPerSec
	}
	return &Sandbox{
		callLimiter: NewTokenBucket(maxCallPerSec, maxCallPerSec),
		memoryPages: memoryPages,
	}
}

// AllowCall 检查是否允许一次调用。限流时返回 false。
func (s *Sandbox) AllowCall() bool {
	return s.callLimiter.Allow()
}

// MemoryLimitBytes 返回内存限制的字节数。
func (s *Sandbox) MemoryLimitBytes() uint32 {
	return s.memoryPages * 64 * 1024
}


