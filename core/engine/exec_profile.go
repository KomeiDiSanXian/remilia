package engine

import (
	"sort"
	"sync/atomic"
	"time"
)

// ExecClass 表示 handler 的执行方式。
type ExecClass int

const (
	// ExecClassPool 表示 handler 应在 ExecPool 的 goroutine 中执行，
	// 避免阻塞 adapter 的 worker。
	ExecClassPool ExecClass = iota
	// ExecClassDirect 表示 handler 应在当前 goroutine 中直接执行，
	// 零额外开销（适用于已证明足够快的 handler）。
	ExecClassDirect
)

// defaultSlowThreshold 是 handler 被判定为"慢"的阈值。
const defaultSlowThreshold = 50 * time.Millisecond

// windowSize 是滑动窗口大小。
const execProfileWindowSize = 32

// execProfilePromoteAfter 是在确认 handler 慢之前所需的连续慢次数。
const execProfilePromoteAfter = 1

// execProfileDemoteAfterFast 是降级所需的连续快次数。
const execProfileDemoteAfterFast = 10

// ExecProfile 跟踪单个 matcher 的执行耗时，使用滑动窗口 + p50 判定
// handler 该走同步还是走池。
//
// 设计原则：
//   - ShouldPool 基于历史数据做决策，Record 记录本次耗时供下次使用
//   - 默认怀疑慢（前几次走池），用事实证明自己快后才降级
//   - 一次慢立刻提升（promote），长时间快才降级（demote）
type ExecProfile struct {
	results  [execProfileWindowSize]time.Duration
	idx      atomic.Uint64
	promoted atomic.Bool
}

// ShouldPool 基于历史数据决定当前 handler 是否应走 ExecPool。
//
// 判定逻辑：
//   - promoted → 走池
//   - 数据不足 → 走池（默认怀疑所有 handler 都是慢的）
//   - p50 > threshold → 提升并走池
//   - p50 >= threshold → 提升并走池
//   - 数据充足且 p50 < threshold/2 且连续快 → 降级并走同步
//   - 其他 → 保持当前状态
func (p *ExecProfile) ShouldPool() ExecClass {
	if p.promoted.Load() {
		return ExecClassPool
	}

	n := min(p.idx.Load(), execProfileWindowSize)
	if n < execProfilePromoteAfter+1 {
		return ExecClassPool // 数据太少，默认走池
	}

	// 计算 p50
	sorted := make([]time.Duration, n)
	for i := uint64(0); i < n; i++ {
		sorted[i] = p.results[i%execProfileWindowSize]
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p50 := sorted[n/2]

	threshold := defaultSlowThreshold

	if p50 > threshold || p50 >= threshold {
		p.promoted.Store(true)
		return ExecClassPool
	}

	if n >= execProfileWindowSize && p50 < threshold/2 {
		allFast := true
		idx := p.idx.Load()
		start := idx - execProfileDemoteAfterFast
		for i := start; i < idx; i++ {
			if p.results[i%execProfileWindowSize] > threshold/2 {
				allFast = false
				break
			}
		}
		if allFast {
			p.promoted.Store(false)
			return ExecClassDirect
		}
	}

	if p.promoted.Load() {
		return ExecClassPool
	}
	return ExecClassPool
}

// Record 记录一次 handler 执行耗时，供后续 ShouldPool 决策使用。
func (p *ExecProfile) Record(elapsed time.Duration) {
	idx := p.idx.Add(1) - 1
	p.results[idx%execProfileWindowSize] = elapsed

	// 单次极慢也直接提升（快速隔离故障）
	if elapsed > defaultSlowThreshold*2 {
		p.promoted.Store(true)
	}
}

// IsPromoted 返回当前 handler 是否被标记为"慢"。
func (p *ExecProfile) IsPromoted() bool {
	return p.promoted.Load()
}

// Reset 重置 profile，下次走 ExecDirect（向后兼容）。
func (p *ExecProfile) Reset() {
	p.promoted.Store(false)
	p.idx.Store(0)
	for i := range p.results {
		p.results[i] = 0
	}
}
