// Package stats 提供零依赖的基础统计原语，供框架内部组件使用。
//
// # 与 plugins/stats 的区别
//
// Remilia 统计相关代码分为两个层次：
//
//	stats/         — 基础统计原语（本包），零外部依赖
//	plugins/stats/ — 用户行为统计插件，基于插件系统
//
// ## stats/（本包）
//
// 提供轻量、线程安全的统计数据结构，专为框架**内部组件**设计：
//   - Counter         — 原子计数器（Inc/Add/Get/Reset）
//   - Gauge           — 原子计量器（Set/Inc/Dec/Get）
//   - Histogram       — 简单直方图（Count/Sum/Min/Max/Avg）
//   - QuantileHistogram — 分位数直方图（P50/P90/P95/P99），使用环形缓冲区，O(1) 写入
//
// 典型使用者：middleware/adaptive.go（P99 延迟计算）、engine 内部性能统计
//
// ## plugins/stats/（插件层）
//
// 基于插件系统构建，面向**Bot 业务层**，记录用户行为数据：
//   - 命令调用次数统计（TopCommands）
//   - 活跃用户 UV（按日/周/月）
//   - 可选对接 storage 插件实现持久化
//
// 参见：github.com/KomeiDiSanXian/remilia/plugins/stats
package stats

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// QuantileHistogram 支持分位数查询的直方图
//
// 使用环形缓冲区存储最近的观测值，以计算精确的分位数。
// 适用于延迟统计等需要 P50/P90/P95/P99 的场景。
//
// 改进 #15：使用真正的环形缓冲区（head 指针），消除原来每次写入的 O(N) copy 开销。
// 原实现在超过容量时执行 copy(samples, samples[1:])，每次移动 N 个元素（10000 个时约 80KB）。
// 新实现写入复杂度 O(1)，只在 Quantile 计算时排序一次（懒排序）。
//
// 注意：最大存储 MaxSamples 个样本（默认 10000），超出后环形覆盖最旧的样本。
type QuantileHistogram struct {
	mu         sync.Mutex
	buf        []int64 // 环形缓冲区
	head       int     // 下一个写入位置（环形指针）
	count      int     // 实际有效样本数（≤ maxSamples）
	maxSamples int
	sorted     bool // 标记 buf 中有效样本是否已排序（懒排序，Quantile 调用时才排）
}

const DefaultMaxSamples = 10000

// NewQuantileHistogram 创建分位数直方图
func NewQuantileHistogram() *QuantileHistogram {
	return NewQuantileHistogramWithSize(DefaultMaxSamples)
}

// NewQuantileHistogramWithSize 创建指定最大样本数的分位数直方图
func NewQuantileHistogramWithSize(maxSamples int) *QuantileHistogram {
	if maxSamples <= 0 {
		maxSamples = DefaultMaxSamples
	}
	return &QuantileHistogram{
		buf:        make([]int64, maxSamples), // 预分配固定大小的环形缓冲区
		maxSamples: maxSamples,
	}
}

// Observe 记录一个观测值（纳秒或任意 int64 单位）
//
// 改进 #15：O(1) 写入，使用环形缓冲区 head 指针替换原来的 O(N) copy。
func (qh *QuantileHistogram) Observe(value int64) {
	qh.mu.Lock()
	defer qh.mu.Unlock()

	// 将值写入 head 位置，head 指针环绕
	qh.buf[qh.head] = value
	qh.head = (qh.head + 1) % qh.maxSamples
	if qh.count < qh.maxSamples {
		qh.count++
	}
	qh.sorted = false
}

// Quantile 返回指定分位数的值（q 范围 0.0-1.0）
// 例如 Quantile(0.99) 返回 P99 值
// 如果没有观测数据，返回 0
func (qh *QuantileHistogram) Quantile(q float64) int64 {
	qh.mu.Lock()
	defer qh.mu.Unlock()

	n := qh.count
	if n == 0 {
		return 0
	}

	// 从环形缓冲区提取有效样本（按写入时间顺序）
	// head 指向下一个写入位置，有效数据从 (head - count) 到 (head-1)（环绕）
	samples := make([]int64, n)
	if qh.count < qh.maxSamples {
		// 未满：有效数据在 [0, count)
		copy(samples, qh.buf[:n])
	} else {
		// 已满：有效数据从 head 开始环绕
		start := qh.head
		firstPart := qh.maxSamples - start
		copy(samples, qh.buf[start:])
		copy(samples[firstPart:], qh.buf[:start])
	}

	slices.Sort(samples)

	if q <= 0 {
		return samples[0]
	}
	if q >= 1.0 {
		return samples[n-1]
	}
	idx := int(float64(n-1) * q)
	return samples[idx]
}

// P50 返回第 50 百分位（中位数）
func (qh *QuantileHistogram) P50() int64 { return qh.Quantile(0.50) }

// P90 返回第 90 百分位
func (qh *QuantileHistogram) P90() int64 { return qh.Quantile(0.90) }

// P95 返回第 95 百分位
func (qh *QuantileHistogram) P95() int64 { return qh.Quantile(0.95) }

// P99 返回第 99 百分位
func (qh *QuantileHistogram) P99() int64 { return qh.Quantile(0.99) }

// Count 返回当前样本数
func (qh *QuantileHistogram) Count() int {
	qh.mu.Lock()
	defer qh.mu.Unlock()
	return qh.count
}

// Reset 清空所有样本
func (qh *QuantileHistogram) Reset() {
	qh.mu.Lock()
	defer qh.mu.Unlock()
	qh.head = 0
	qh.count = 0
	qh.sorted = false
}

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

// Min 获取最小观测值
// 如果没有观测数据（Count() == 0），返回 0
func (h *Histogram) Min() int64 {
	if h.count.Load() == 0 {
		return 0
	}
	return h.min.Load()
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
