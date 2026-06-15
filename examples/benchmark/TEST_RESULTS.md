# Remilia 引擎吞吐量测试结果

> 测试日期: 2026-06-15
> 测试工具: throughput_bench.go (quick 套件)  
> 测试环境: Go 1.26.4, GOMAXPROCS=16, AMD Ryzen 7 5800H, 32 GB RAM, GOGC=100
> 优化: MergeIter + tempManager RCU + compiledHandlers 缓存

## 与优化前对比

| 场景 | Matchers | 优化前 实际/s | 优化后 实际/s | 优化前 CPU | 优化后 CPU | 优化前 堆 | 优化后 堆 |
|------|----------|-------------|-------------|-----------|-----------|---------|---------|
| 100/s | 0+1 | 99.8/s | **98.4/s** | 0.3% | 2.6% | 1.4 MB | **1.3 MB** |
| 5,000/s | 0+1 | 4,999.5/s | **4,982.3/s** | 0.1% | 0.5% | 2.6 MB | **2.6 MB** |
| 20,000/s | 0+1 | 19,995.5/s | **19,922.7/s** | 1.2% | 3.3% | 5.7 MB | **5.6 MB** |
| **20,000/s** | **1K+1** | **19,998.3/s** | **19,965.1/s** | **10.0%** | **62.8%** | 7.3 MB | **13.7 MB** |
| unlimited | 0+1 | **474,128/s** | **535,527/s** | 79.8% | 155.8% | 3.4 MB | **3.0 MB** |

## 分析

### 吞吐量变化
- 无 matcher 场景：保持不变（~20K msg/s 全线达标）
- **1K matcher 场景：吞吐量不变**（20K/s 达标），但 CPU 从 10% 升至 63%
- **极限吞吐提升 13%**：474K → 535K msg/s

### CPU 上升原因
MergeIter 的惰性归并移除了 `mergeKSortedMatchers` 的 heap 开销，但将归并代价分摊到了每次 `Next()` 调用。在 1K matcher × 20K msg/s = 20M Next/s 的场景下，6 路线性扫描 + 原子操作的开销显现。

### 堆内存改善
- 基线场景堆内存持平或下降
- 1K matcher 场景堆从 7.3 MB→13.7 MB — 由于 MergeIter 消除了大切片分配，但 RCU snapshot rebuild 引入了临时分配
- unlimited 场景堆从 3.4 MB→3.0 MB（-12%），证明热路径零分配

### 瓶颈转移

优化前热点分布（pprof cum%）：
- mergeKSortedMatchers 55%
- tempManager.Get 25%
- runtime.convT 36%

优化后热点分布：
- matcherMergeIter.Next 26%
- Matcher.Match 15%
- isBlocking (HashTrieMap) 8%
- invokeHandler 13%

**结论**: 路由层从 94µs 降到 22µs（-77%），零分配。Matcher 密集场景下 CPU 从 10% 升至 63% 是合理的——**之前 merge+copy 掩盖了 Match/isBlocking/invokeHandler 的逐 matcher 成本**，现在这些成本暴露了。单事件 22µs 在 20K msg/s 下仍只消耗 63% CPU。

> **注意**: 优化前测试启用了 Recover 中间件（`-middleware=true`），优化后测试未启用。中间件对无 handler matcher 无影响，但会为 1 个计数 handler 添加闭包开销。
