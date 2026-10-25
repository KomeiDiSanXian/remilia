package engine

import (
	"slices"
	"sync"
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
//
// 同一份窗口数据还用于准入控制的成本分类（见 P50/Samples）：
// 那里关心的是「这条 handler 会占多久槽位」，与「是否该走池」是两套阈值。
type ExecProfile struct {
	mu       sync.Mutex
	results  [execProfileWindowSize]time.Duration
	idx      atomic.Uint64
	promoted atomic.Bool
	demoted  atomic.Bool // 已确认为快 handler，跳过排序

	// 供准入控制读取的 p50 快照：Record 后标记 dirty，P50 首次读取时重算。
	// 避免每个事件在 ShouldPool 之外再做一次排序。
	cachedP50 atomic.Int64
	p50Dirty  atomic.Bool
}

func newExecProfile() *ExecProfile {
	return &ExecProfile{}
}

// ShouldPool 基于历史数据决定当前 handler 是否应走 ExecPool。
//
// 判定逻辑：
//   - promoted → 走池
//   - demoted → 走同步（快速路径，已确认为快 handler）
//   - 数据不足 → 走池（默认怀疑所有 handler 都是慢的）
//   - p50 > threshold → 提升并走池
//   - 数据充足且 p50 < threshold/2 且连续快 → 降级并走同步
func (p *ExecProfile) ShouldPool() ExecClass {
	if p.promoted.Load() {
		return ExecClassPool
	}
	if p.demoted.Load() {
		return ExecClassDirect
	}

	p.mu.Lock()
	n := min(p.idx.Load(), execProfileWindowSize)
	if n < execProfilePromoteAfter+1 {
		p.mu.Unlock()
		return ExecClassPool // 数据太少，默认走池
	}

	// 计算 p50：栈分配固定数组，避免多 goroutine 并发排序的数据竞争
	var snapshot [execProfileWindowSize]time.Duration
	for i := range n {
		snapshot[i] = p.results[i%execProfileWindowSize]
	}
	snapSlice := snapshot[:n]

	var recentFast bool
	if n >= execProfileWindowSize {
		recentFast = true
		idx := p.idx.Load()
		start := idx - execProfileDemoteAfterFast
		for i := start; i < idx; i++ {
			if p.results[i%execProfileWindowSize] > defaultSlowThreshold/2 {
				recentFast = false
				break
			}
		}
	}
	p.mu.Unlock()

	slices.Sort(snapSlice)
	p50 := snapSlice[n/2]

	if p50 >= defaultSlowThreshold {
		p.promoted.Store(true)
		return ExecClassPool
	}

	if n >= execProfileWindowSize && p50 < defaultSlowThreshold/2 && recentFast {
		p.promoted.Store(false)
		p.demoted.Store(true)
		return ExecClassDirect
	}

	return ExecClassPool
}

// Record 记录一次 handler 执行耗时，供后续 ShouldPool 决策使用。
func (p *ExecProfile) Record(elapsed time.Duration) {
	idx := p.idx.Add(1) - 1
	p.mu.Lock()
	p.results[idx%execProfileWindowSize] = elapsed
	p.mu.Unlock()
	p.p50Dirty.Store(true)

	// 单次极慢也直接提升（快速隔离故障）
	if elapsed > defaultSlowThreshold*2 {
		p.promoted.Store(true)
		p.demoted.Store(false)
	}
}

// Samples 返回窗口中已有的样本数（上限为窗口大小）。
func (p *ExecProfile) Samples() int {
	return int(min(p.idx.Load(), execProfileWindowSize))
}

// P50 返回最近窗口内执行耗时的中位数。
//
// 无样本时返回 0。结果按需重算并缓存：Record 只置 dirty 标记，
// 真正的排序发生在第一次读取（每个事件每个 matcher 至多一次）。
func (p *ExecProfile) P50() time.Duration {
	if !p.p50Dirty.Load() {
		return time.Duration(p.cachedP50.Load())
	}

	p.mu.Lock()
	n := int(min(p.idx.Load(), execProfileWindowSize))
	var snapshot [execProfileWindowSize]time.Duration
	copy(snapshot[:], p.results[:n])
	p.mu.Unlock()

	var p50 time.Duration
	if n > 0 {
		slices.Sort(snapshot[:n])
		p50 = snapshot[n/2]
	}
	p.cachedP50.Store(int64(p50))
	p.p50Dirty.Store(false)
	return p50
}

// IsPromoted 返回当前 handler 是否被标记为"慢"。
func (p *ExecProfile) IsPromoted() bool {
	return p.promoted.Load()
}

// Reset 重置 profile，下次走 ExecDirect（向后兼容）。
func (p *ExecProfile) Reset() {
	p.promoted.Store(false)
	p.demoted.Store(false)
	p.idx.Store(0)
	p.mu.Lock()
	for i := range p.results {
		p.results[i] = 0
	}
	p.mu.Unlock()
}
