package wasm

import (
	"sync"
	"time"
)

// Sandbox 为 WASM 模块提供资源限制和安全隔离。
type Sandbox struct {
	mu          sync.Mutex //nolint:unused
	callLimiter *TokenBucket
	memoryPages uint32

	// 安全阈值
	CallTimeout     time.Duration
	InitTimeout     time.Duration
	ResponseSizeMax uint32
	WasmSizeMax     uint32
	ImportsMax      int
}

// TokenBucket 令牌桶限流器。
type TokenBucket struct {
	mu       sync.Mutex
	rate     int64
	capacity int64
	tokens   int64
	lastTime time.Time
}

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

func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime)
	tb.lastTime = now

	tb.tokens += int64(float64(elapsed.Nanoseconds()) * float64(tb.rate) / float64(time.Second.Nanoseconds()))
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	if tb.tokens > 0 {
		tb.tokens--
		return true
	}
	return false
}

// NewSandbox 根据 ResourceLimit 创建沙箱，零值使用默认值。
func NewSandbox(rl ResourceLimit) *Sandbox {
	return &Sandbox{
		callLimiter: NewTokenBucket(rl.MaxCallPerSec, rl.MaxCallPerSec),
		memoryPages: rl.MemoryPages,

		CallTimeout:     rl.CallTimeout,
		InitTimeout:     rl.InitTimeout,
		ResponseSizeMax: rl.ResponseSizeMax,
		WasmSizeMax:     rl.WasmSizeMax,
		ImportsMax:      rl.ImportsMax,
	}
}

func (s *Sandbox) AllowCall() bool          { return s.callLimiter.Allow() }
func (s *Sandbox) MemoryLimitBytes() uint32 { return s.memoryPages * 64 * 1024 }
