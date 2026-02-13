package stats

import (
	"sync/atomic"
	"time"
)

// BatchStats 批量处理统计信息
type BatchStats struct {
	TotalBatches    uint64        // 总批次数
	TotalEvents     uint64        // 总事件数
	TotalDuration   time.Duration // 总耗时
	AvgBatchSize    float64       // 平均批量大小
	AvgDuration     time.Duration // 平均每批次耗时
	EventsPerSecond float64       // 吞吐量（事件/秒）
}

// EngineStats engine 统计信息
type EngineStats struct {
	MatcherCount      int   // 匹配器数量
	EventsProcessed   int64 // 已处理事件数
	EventsFailed      int64 // 失败事件数
	ActiveMatchers    int   // 活跃匹配器数
	TempMatchersCount int   // 临时匹配器数量
}

// Counter 原子计数器
type Counter struct {
	value atomic.Int64
}

// NewCounter 创建新的计数器
func NewCounter() *Counter {
	return &Counter{}
}

// Inc 增加计数
func (c *Counter) Inc() {
	c.value.Add(1)
}

// Add 增加指定值
func (c *Counter) Add(delta int64) {
	c.value.Add(delta)
}

// Get 获取当前值
func (c *Counter) Get() int64 {
	return c.value.Load()
}

// Reset 重置计数器
func (c *Counter) Reset() {
	c.value.Store(0)
}

// Gauge 原子计量器
type Gauge struct {
	value atomic.Int64
}

// NewGauge 创建新的计量器
func NewGauge() *Gauge {
	return &Gauge{}
}

// Set 设置值
func (g *Gauge) Set(value int64) {
	g.value.Store(value)
}

// Inc 增加1
func (g *Gauge) Inc() {
	g.value.Add(1)
}

// Dec 减少1
func (g *Gauge) Dec() {
	g.value.Add(-1)
}

// Get 获取当前值
func (g *Gauge) Get() int64 {
	return g.value.Load()
}

// Histogram 直方图统计
type Histogram struct {
	count atomic.Int64
	sum   atomic.Int64
	min   atomic.Int64
	max   atomic.Int64
}

// NewHistogram 创建新的直方图
func NewHistogram() *Histogram {
	h := &Histogram{}
	h.min.Store(int64(^uint64(0) >> 1)) // MaxInt64
	return h
}

// Observe 记录一个观测值
func (h *Histogram) Observe(value int64) {
	h.count.Add(1)
	h.sum.Add(value)

	// 更新最小值
	for {
		oldMin := h.min.Load()
		if value >= oldMin {
			break
		}
		if h.min.CompareAndSwap(oldMin, value) {
			break
		}
	}

	// 更新最大值
	for {
		oldMax := h.max.Load()
		if value <= oldMax {
			break
		}
		if h.max.CompareAndSwap(oldMax, value) {
			break
		}
	}
}

// Count 获取观测次数
func (h *Histogram) Count() int64 {
	return h.count.Load()
}

// Sum 获取总和
func (h *Histogram) Sum() int64 {
	return h.sum.Load()
}

// Min 获取最小值
func (h *Histogram) Min() int64 {
	load := h.min.Load()
	if load == int64(^uint64(0)>>1) {
		return 0
	}
	return load
}

// Max 获取最大值
func (h *Histogram) Max() int64 {
	return h.max.Load()
}

// Avg 获取平均值
func (h *Histogram) Avg() float64 {
	count := h.count.Load()
	if count == 0 {
		return 0
	}
	return float64(h.sum.Load()) / float64(count)
}

// Reset 重置直方图
func (h *Histogram) Reset() {
	h.count.Store(0)
	h.sum.Store(0)
	h.min.Store(int64(^uint64(0) >> 1))
	h.max.Store(0)
}
