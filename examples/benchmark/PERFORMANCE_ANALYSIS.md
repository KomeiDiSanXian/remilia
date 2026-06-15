# Remilia Framework — 引擎吞吐量分析报告

> 测试日期: 2026-06-15
> 测试工具: throughput_bench.go (quick 套件, GOGC=100)
> 硬件: AMD Ryzen 7 5800H, 16 CPU, 32 GB RAM, Go 1.26.4, GOMAXPROCS=16

---

## 测试结果总览

| 场景 | Matchers | 目标 | 实际 | 成功率 | CPU | 堆(avg) |
|------|----------|------|------|--------|-----|---------|
| low (100/s) | 0+1 | 100/s | 98.4/s | 100% | 2.6% | 1.3 MB |
| mid (5K/s) | 0+1 | 5,000/s | 4,982.3/s | 100% | 0.5% | 2.6 MB |
| high (20K/s) | 0+1 | 20,000/s | 19,922.7/s | 100% | 3.3% | 5.6 MB |
| **matcher-1K (20K/s)** | **1K+1** | **20,000/s** | **19,965.1/s** | **100%** | **62.8%** | **13.7 MB** |
| unlimited | 0+1 | — | 535,527.1/s | 100% | 155.8% | 3.0 MB |

---

## 优化历程

### PR1: MergeIter — 零分配惰性归并

**问题**: 旧 `mergeKSortedMatchers` 使用 `container/heap` + `interface{}` 装箱

| 旧热点 | 占比 |
|--------|------|
| mergeKSortedMatchers | 55% CPU |
| runtime.convT (heapItem→any) | 36% CPU |
| container/heap.Pop | 25% CPU |
| mallogc | 28% CPU |

**解决**:
- 手写 `matcherMergeIter` + 内联 6 路线性扫描
- 边消费边归并，支持 `isBlocking` 提前终止
- 零 alloc，无 heap.Interface 装箱

### PR2: tempManager RCU — O(1) 读路径

**问题**: 旧 `Get()` 每事件执行 8×RLock + copy + merge

| 旧热点 | 占比 |
|--------|------|
| tempManager.Get | 25% CPU |
| tempManager.Get | 42% Heap (alloc_space) |

**解决**: `atomic.Pointer[TempSnapshot]` 只读快照，写入后 atomically replace

### PR3: compiledHandlers 缓存修复

**问题**: 无 middleware 时 `getOrBuildIterChain` 直接返回 handler，不缓存到 `compiledHandlers`，导致 `invokeHandler` 每事件都跑慢路径

**解决**: 无 middleware 时也写入 `compiledHandlers`

## 性能总提升 (Heavy: 1K matchers)

| 指标 | 优化前 | 优化后 | 提升 |
|------|--------|--------|------|
| 单事件延迟 | 94,398 ns | 22,074 ns | **-77%** |
| B/op | 24,467 | **0** | **-100%** |
| allocs/op | 1,408 | **0** | **-100%** |
| 极限吞吐 | 474K/s | **535K/s** | **+13%** |

## 热点转移

| 函数 | 优化前 cum | 优化后 flat% | 说明 |
|------|-----------|-------------|------|
| mergeKSortedMatchers | 55% | — | 已消除 |
| tempManager.Get | 25% | — | 已消除 |
| runtime.convT | 36% | — | 已消除 |
| runtime.mallocgc | 28% | — | 已消除 |
| matcherMergeIter.Next | — | **26%** | 新#1 热点 |
| Matcher.Match | 5% | **15%** | 新#2 热点 |
| isBlocking (HashTrieMap) | 1% | **8%** | 新#3 热点 |
| invokeHandler | 4% | 13% cum | 大幅下降 |

## Matcher 缩放特性

1K matcher + 20K msg/s 下 100% 成功。**每事件路由延迟从 94µs 降到 22µs**，但由于 Match() 和 isBlocking 是 O(N)，matcher 数量增长会线性增加 CPU 消耗。

---

## 架构说明

| 组件 | 说明 |
|------|------|
| **COW 引擎** | `ProcessEvent` 完全无锁，`atomic.Load()` 获取快照，写时复制 |
| **MergeIter** | 6 路惰性归并，零分配，边消费边归并 |
| **TempManager RCU** | `atomic.Pointer[TempSnapshot]` 只读快照，写入后原子替换 |
| **compiledHandlers 缓存** | `atomic.Value` + 版本号，稳定态 0 锁 0 alloc |
| **批量分发** | pumpAdapter 32 个 worker，每批 drain 64 个事件 |
| **对象池** | benchEvent 通过 sync.Pool 复用 |

---

## 生产环境指导

| 场景 | 建议容量 | 路由耗时 |
|------|---------|---------|
| 纯转发, 0-100 matchers | <200K msg/s | ~1µs |
| 命令 Bot, 100-1K matchers | <50K msg/s | ~22µs |
| 复杂业务, 1K-5K matchers | <10K msg/s | ~100µs |
| 多媒体, 任意 matcher | <200 msg/s | 图片/AI 为主 |
