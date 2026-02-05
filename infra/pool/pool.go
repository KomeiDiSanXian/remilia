package pool

import (
	"sync"
	"sync/atomic"
)

type Pool interface {
	Get() any
	Put(any)
}

type Stats struct {
	Gets    uint64
	Puts    uint64
	News    uint64
	HitRate float64
}

type InstrumentedPool struct {
	pool    sync.Pool
	gets    atomic.Uint64
	puts    atomic.Uint64
	news    atomic.Uint64
	resetMu sync.Mutex // Protect Reset operation for atomicity
}

func NewInstrumentedPool(newFunc func() any) *InstrumentedPool {
	ip := &InstrumentedPool{}
	ip.pool.New = func() any {
		ip.news.Add(1)
		return newFunc()
	}
	return ip
}

func (ip *InstrumentedPool) Get() any {
	ip.gets.Add(1)
	return ip.pool.Get()
}

func (ip *InstrumentedPool) Put(x any) {
	ip.puts.Add(1)
	ip.pool.Put(x)
}

func (ip *InstrumentedPool) Stats() Stats {
	// Use mutex to ensure we get a consistent snapshot during Reset
	ip.resetMu.Lock()
	gets := ip.gets.Load()
	puts := ip.puts.Load()
	news := ip.news.Load()
	ip.resetMu.Unlock()

	hitRate := 0.0
	if gets > 0 {
		hitRate = float64(gets-news) / float64(gets) * 100
	}

	return Stats{Gets: gets, Puts: puts, News: news, HitRate: hitRate}
}

// Reset atomically resets all statistics counters to zero
// This method is safe to call concurrently with Get/Put operations
func (ip *InstrumentedPool) Reset() {
	ip.resetMu.Lock()
	defer ip.resetMu.Unlock()

	ip.gets.Store(0)
	ip.puts.Store(0)
	ip.news.Store(0)
}
