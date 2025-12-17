package remilia

import (
	"sync"
	"sync/atomic"
)

// Pool 对象池接口
type Pool interface {
	Get() interface{}
	Put(interface{})
}

// PoolStats 对象池统计信息
type PoolStats struct {
	Gets    uint64  // 从池中获取的次数
	Puts    uint64  // 放回池中的次数
	News    uint64  // 创建新对象的次数
	HitRate float64 // 命中率
}

// InstrumentedPool 带统计功能的对象池
type InstrumentedPool struct {
	pool sync.Pool
	gets atomic.Uint64
	puts atomic.Uint64
	news atomic.Uint64
}

// NewInstrumentedPool 创建带统计功能的对象池
func NewInstrumentedPool(newFunc func() interface{}) *InstrumentedPool {
	ip := &InstrumentedPool{}
	ip.pool.New = func() interface{} {
		ip.news.Add(1)
		return newFunc()
	}
	return ip
}

// Get 从池中获取对象
func (ip *InstrumentedPool) Get() interface{} {
	ip.gets.Add(1)
	return ip.pool.Get()
}

// Put 将对象放回池中
func (ip *InstrumentedPool) Put(x interface{}) {
	ip.puts.Add(1)
	ip.pool.Put(x)
}

// Stats 获取统计信息
func (ip *InstrumentedPool) Stats() PoolStats {
	gets := ip.gets.Load()
	puts := ip.puts.Load()
	news := ip.news.Load()

	hitRate := 0.0
	if gets > 0 {
		hitRate = float64(gets-news) / float64(gets) * 100
	}

	return PoolStats{
		Gets:    gets,
		Puts:    puts,
		News:    news,
		HitRate: hitRate,
	}
}

// Reset 重置统计信息
func (ip *InstrumentedPool) Reset() {
	ip.gets.Store(0)
	ip.puts.Store(0)
	ip.news.Store(0)
}
