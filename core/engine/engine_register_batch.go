package engine

// engine_register_batch.go — 注册批处理会话
//
// 插件 Setup 等"启动期集中注册"场景下，每次 On/OnCommand/SetMatcherGroup/Handle
// 都会触发一次 COW 全量复制（commandIndex 复制随命令数线性增长），
// 1000 个命令顺序注册实测约 800ms。RegisterBatch 将会话期间的注册与
// 链式索引维护全部收集，Flush 时以一次 withBatchMatchers 提交（~2ms）。
//
// 语义与约束：
//   - 会话期间注册的 matcher 在 Flush 前对事件不可见（不可路由）；
//     适用于插件 Setup 等无事件派发的启动期
//   - 会话是引擎级单例：已有活跃会话时 Begin 返回 nil，调用方退化为
//     逐条注册（并发加载重叠时功能正确、仅失去批量收益）
//   - 会话本身在单个加载 goroutine 内使用；Begin 与 Flush 之间的
//     链式写操作降级收集由 writeMu 保护
//
// 批内 matcher 的链式写操作（SetMatcherGroup / UpdateMatcherIndex /
// UpdateCommandCache / InvalidateSortedCache）自动降级为收集，
// Flush 统一重建，最终状态与逐条注册完全等价。

// RegisterBatch 是一次注册批处理会话（见 [Engine.BeginRegisterBatch]）。
type RegisterBatch struct {
	e        *Engine
	matchers []*Matcher
	members  map[*Matcher]struct{}
	temps    []*Matcher
	dirty    map[EventType]struct{}
}

// contains 报告 m 是否属于本会话（批内 matcher 的链式写操作降级为收集）。
func (b *RegisterBatch) contains(m *Matcher) bool {
	if b == nil || m == nil {
		return false
	}
	_, ok := b.members[m]
	return ok
}

// Flush 提交会话期间收集的所有注册：
//   - 常规 matcher：一次 withBatchMatchers COW 提交（含排序/分组/命令缓存重建）
//   - 临时 matcher：统一入 TempManager
//   - 批期脏事件类型：排序缓存重建
//
// 幂等：已结束的会话重复 Flush 为空操作。
func (b *RegisterBatch) Flush() {
	if b == nil || b.e == nil {
		return
	}
	e := b.e
	e.writeMu.Lock()
	if e.registerBatch != b {
		e.writeMu.Unlock()
		return
	}
	e.registerBatch = nil
	e.writeMu.Unlock()

	if len(b.matchers) > 0 {
		e.BatchRegisterMatchers(b.matchers)
	}
	for _, m := range b.temps {
		e.internals.tempManager.Add(m)
		e.rebuildMatcherChainCOW(m)
	}
	for et := range b.dirty {
		e.InvalidateSortedCache(et)
	}
}

// RegisterBatchStarter 是注册批处理会话的入口（Engine 实现）。
//
// plugin.Manager 通过类型断言此接口在插件 Setup 周围开启批量注册；
// 未实现该接口的协调器（如测试桩）自动退化为逐条注册。
type RegisterBatchStarter interface {
	BeginRegisterBatch() *RegisterBatch
}

// BeginRegisterBatch 开启注册批处理会话（见 [RegisterBatch] 文档）。
//
// 会话期间所有注册（On/OnCommand/OnTemp）与批内 matcher 的链式索引维护
// 被收集，直到 [RegisterBatch.Flush] 一次提交。
//
// 返回 nil 表示已有活跃会话（并发加载重叠，如不同插件并发 Reload）：
// 调用方应退化为逐条注册——功能正确，仅失去批量收益。
// 引擎级单例决定了同一时刻只有一个批处理会话（避免两个 Flush 相互终结）。
func (e *Engine) BeginRegisterBatch() *RegisterBatch {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	if e.registerBatch != nil {
		return nil
	}
	b := &RegisterBatch{
		e:       e,
		members: make(map[*Matcher]struct{}),
		dirty:   make(map[EventType]struct{}),
	}
	e.registerBatch = b
	return b
}
