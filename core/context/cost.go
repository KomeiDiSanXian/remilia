package context

import "time"

// CostClass 表示「处理这起事件的那条 handler 会占用多久」的成本类别。
//
// 它用于**准入控制**（每类独立并发预算），不是系统负载信号：
// 降级看 CPU/内存，准入看槽位占用时间。
type CostClass uint8

const (
	// CostUnknown 未分类：由引擎实测 p50 自动判定。
	CostUnknown CostClass = iota
	// CostLocal 本地/廉价处理：CPU 秒级以内，等待不含外部 I/O。
	CostLocal
	// CostIO 外部 I/O 型处理：会长时间等待第三方 API（AI、搜索、天气等）。
	CostIO
)

// String 返回便于日志阅读的名称。
func (c CostClass) String() string {
	switch c {
	case CostLocal:
		return "local"
	case CostIO:
		return "io"
	default:
		return "unknown"
	}
}

// MatcherCost 是引擎在调用 handler 前注入的、当前 matcher 的成本快照。
//
// 它只含**数据**，不含策略：阈值（多少毫秒算慢）与预算由中间件决定。
type MatcherCost struct {
	// Declared 是插件/调用方显式声明的类别（[Matcher.SetCost] 或
	// [plugin.Descriptor.Cost]）；CostUnknown 表示未声明，交给观测值判定。
	Declared CostClass
	// P50 是该 matcher 最近执行耗时的中位数；Samples 是有效样本数。
	// Samples 为 0 时表示冷启动，调用方不应据此判定为 I/O 型。
	P50     time.Duration
	Samples int
}

// Resolve 按「显式声明 > 观测 > 兜底」得出成本类别。
//
// 观测判定：样本数 ≥ minSamples 且 p50 ≥ slowThreshold 时为 I/O 型。
// 冷启动/样本不足一律返回 CostLocal —— 宁可先放行几个重请求，
// 也不要把新插件的第一条消息排进队列。
func (mc MatcherCost) Resolve(slowThreshold time.Duration, minSamples int) CostClass {
	if mc.Declared != CostUnknown {
		return mc.Declared
	}
	if minSamples <= 0 {
		minSamples = 3
	}
	if mc.Samples < minSamples {
		return CostLocal
	}
	if slowThreshold > 0 && mc.P50 >= slowThreshold {
		return CostIO
	}
	return CostLocal
}

// SetMatcherCost 注入当前 matcher 的成本快照（框架内部，由 Engine 调用）。
func (ctx *Context) SetMatcherCost(c MatcherCost) {
	if ctx == nil {
		return
	}
	ctx.matcherCost = c
}

// MatcherCost 返回当前 matcher 的成本快照。
//
// 未注入（或手动构造的 Context）时返回零值：Samples=0、Declared=Unknown，
// 按 [MatcherCost.Resolve] 的口径即 CostLocal。
func (ctx *Context) MatcherCost() MatcherCost {
	if ctx == nil {
		return MatcherCost{}
	}
	return ctx.matcherCost
}
