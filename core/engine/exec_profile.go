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

// execProfileWindowSize 是滑动窗口大小。
const execProfileWindowSize = 32

// execProfilePromoteAfter 是在确认 handler 慢之前所需的连续慢次数。
const execProfilePromoteAfter = 1

// execProfileDemoteAfterFast 是降级所需的连续快次数。
const execProfileDemoteAfterFast = 10

// execProfileReevaluateAfter 是两次窗口重算之间所需的最少新样本数。
//
// ShouldPool / P50 每个事件每个候选都会被调用，而它们都要对本窗口排序
// （32 元素）。此前 Record 每次都标记 dirty，等于每个事件每个命中 matcher
// 都排一次序。按 N 个样本做一次重算，把这块开销摊薄到 1/N；
// promote / demote 仍由原子标志即时生效（见 Record），不受该阈值影响。
const execProfileReevaluateAfter = 8

// execProfileSlowStreak 是 demote 之后判定"又变慢了"所需的连续慢样本数。
//
// 必须有这条回滞：demoted 是粘滞标志，此前只由单次 >2×阈值（100ms）
// 的极慢样本清除。一个曾经很快、后来稳定在 60ms 的 handler 会永久留在
// Direct 分类，阻塞事件派发 goroutine。
const execProfileSlowStreak = 2

// decisionUnknown 表示窗口决策尚未计算。
const decisionUnknown int32 = -1

// ExecProfile 跟踪单个 matcher 的执行耗时，使用滑动窗口 + p50 判定
// handler 该走同步还是走池。
//
// 设计原则：
//   - ShouldPool 基于历史数据做决策，Record 记录本次耗时供下次使用
//   - 默认怀疑慢（前几次走池），用事实证明自己快后才降级
//   - 一次慢立刻提升（promote），长时间快才降级（demote）
//   - 降级不是终态：连续慢样本（execProfileSlowStreak）会重新提升
//
// 同一份窗口数据还用于准入控制的成本分类（见 P50/Samples）：
// 那里关心的是「这条 handler 会占多久槽位」，与「是否该走池」是两套阈值。
type ExecProfile struct {
	mu       sync.Mutex
	results  [execProfileWindowSize]time.Duration
	idx      atomic.Uint64
	promoted atomic.Bool
	demoted  atomic.Bool // 已确认为快 handler，跳过排序

	// slowStreak 统计「连续慢样本」数（>= defaultSlowThreshold）。
	// demote 之后靠它把重新变慢的 handler 拉回池，见 execProfileSlowStreak。
	slowStreak atomic.Int32

	// 窗口决策缓存：每 execProfileReevaluateAfter 个样本重算一次，
	// 其余调用直接读 cachedDecision（-1 表示未计算）。
	cachedDecision atomic.Int32
	decisionIdx    atomic.Uint64

	// 供准入控制读取的 p50 快照：同样按样本数节流重算。
	cachedP50 atomic.Int64
	p50Idx    atomic.Uint64
	p50Init   atomic.Bool
}

func newExecProfile() *ExecProfile {
	p := &ExecProfile{}
	p.cachedDecision.Store(decisionUnknown)
	return p
}

// ShouldPool 基于历史数据决定当前 handler 是否应走 ExecPool。
//
// 判定逻辑：
//   - promoted → 走池（一次慢即提升，Record 内立即生效）
//   - demoted → 走同步（Record 内的连续慢回滞会把它撤销）
//   - 数据不足 → 走池（默认怀疑所有 handler 都是慢的）
//   - p50 >= threshold → 提升并走池
//   - 数据充足且 p50 < threshold/2 且连续快 → 降级并走同步
//
// 窗口重算按样本数节流（execProfileReevaluateAfter）：两次重算之间返回
// 上一次的决策，避免每个事件都排序窗口。
func (p *ExecProfile) ShouldPool() ExecClass {
	if p.promoted.Load() {
		return ExecClassPool
	}
	if p.demoted.Load() {
		return ExecClassDirect
	}

	idx := p.idx.Load()
	n := min(idx, execProfileWindowSize)

	// 缓存只在窗口填满后生效：窗口未满时每个新样本都会改变中位数
	// （样本数少、中位数跳变明显），继续逐次重算；填满后窗口内容变化
	// 相对平滑，按 execProfileReevaluateAfter 节流即可。
	if n >= execProfileWindowSize {
		if d := p.cachedDecision.Load(); d != decisionUnknown {
			if last := p.decisionIdx.Load(); idx >= last && idx-last < execProfileReevaluateAfter {
				if d == int32(ExecClassDirect) {
					return ExecClassDirect
				}
				return ExecClassPool
			}
		}
	}

	p.mu.Lock()
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

	cls := ExecClassPool
	if p50 >= defaultSlowThreshold {
		p.promoted.Store(true)
	} else if n >= execProfileWindowSize && p50 < defaultSlowThreshold/2 && recentFast {
		p.promoted.Store(false)
		p.demoted.Store(true)
		cls = ExecClassDirect
	}

	// 缓存本次决策（promote/demote 标志优先于缓存，见函数开头）。
	p.cachedDecision.Store(int32(cls))
	p.decisionIdx.Store(idx)
	return cls
}

// Record 记录一次 handler 执行耗时，供后续 ShouldPool 决策使用。
func (p *ExecProfile) Record(elapsed time.Duration) {
	idx := p.idx.Add(1) - 1
	p.mu.Lock()
	p.results[idx%execProfileWindowSize] = elapsed
	p.mu.Unlock()

	// 单次极慢也直接提升（快速隔离故障）
	if elapsed > defaultSlowThreshold*2 {
		p.promoted.Store(true)
		p.demoted.Store(false)
		p.slowStreak.Store(0)
		return
	}

	// 连续慢样本：把"曾经快、现在慢"的 handler 重新拉回池。
	// 单次越过阈值不算（避免抖动），连续 execProfileSlowStreak 次才生效。
	if elapsed >= defaultSlowThreshold {
		if p.slowStreak.Add(1) >= execProfileSlowStreak {
			p.promoted.Store(true)
			p.demoted.Store(false)
			p.slowStreak.Store(0)
		}
		return
	}
	p.slowStreak.Store(0)
}

// Samples 返回窗口中已有的样本数（上限为窗口大小）。
func (p *ExecProfile) Samples() int {
	return int(min(p.idx.Load(), execProfileWindowSize))
}

// P50 返回最近窗口内执行耗时的中位数。
//
// 无样本时返回 0。结果按需重算并缓存：窗口填满后按样本数节流
// （execProfileReevaluateAfter），避免每个事件每个候选都排序一次窗口；
// 窗口未满时每次重算（样本少，中位数变化剧烈，缓存会给出误导值）。
func (p *ExecProfile) P50() time.Duration {
	idx := p.idx.Load()
	if p.p50Init.Load() && idx >= execProfileWindowSize {
		if last := p.p50Idx.Load(); idx >= last && idx-last < execProfileReevaluateAfter {
			return time.Duration(p.cachedP50.Load())
		}
	}

	p.mu.Lock()
	n := int(min(idx, execProfileWindowSize))
	var snapshot [execProfileWindowSize]time.Duration
	copy(snapshot[:], p.results[:n])
	p.mu.Unlock()

	var p50 time.Duration
	if n > 0 {
		slices.Sort(snapshot[:n])
		p50 = snapshot[n/2]
	}
	p.cachedP50.Store(int64(p50))
	p.p50Idx.Store(idx)
	p.p50Init.Store(true)
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
	p.slowStreak.Store(0)
	p.cachedDecision.Store(decisionUnknown)
	p.decisionIdx.Store(0)
	p.p50Init.Store(false)
	p.p50Idx.Store(0)
	p.cachedP50.Store(0)
	p.idx.Store(0)
	p.mu.Lock()
	for i := range p.results {
		p.results[i] = 0
	}
	p.mu.Unlock()
}
