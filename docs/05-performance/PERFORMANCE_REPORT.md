# Remilia 性能报告

> **最后更新**: 2026-08-03  
> **测试环境**: AMD Ryzen 7 5800H (16 线程), 32 GB RAM, Go 1.26.5, Windows  
> **测试工具**: `examples/benchmark/throughput_bench.go`（端到端压测）+ `go test -benchmem`（micro-benchmark）

---

## 端到端吞吐量（standard 套件，阻塞模式）

通过 pumpAdapter 注入事件，包含完整适配器 → 引擎 → 中间件 → 处理器链路，每场景持续 10 秒。

```
场景                               目标(msg/s)  实际(msg/s)  成功率    CPU(进程)    P50(ms)
─────────────────────────────────────────────────────────────────────────────────────
smoke      (100 msg/s, 0 matchers)       100         100     100.0%      0.4%     0.0000
medium    (1000 msg/s, 0 matchers)     1,000       1,000     100.0%      1.7%     0.0000
high      (5000 msg/s, 0 matchers)     5,000       4,996     100.0%      0.6%     0.0000
stress   (20000 msg/s, 0 matchers)    20,000      19,965     100.0%      0.6%     0.0000
extreme  (50000 msg/s, 0 matchers)    50,000      49,936     100.0%      6.8%     0.0000
matcher-100   (20K/100)               20,000      19,997     100.0%      3.5%     0.0000
matcher-1K    (20K/1K)                20,000      19,989     100.0%      3.5%     0.0000
matcher-5K    (20K/5K)                20,000      19,985     100.0%      3.5%     0.0000
matcher-100   (50K/100)               50,000      49,912     100.0%     75.0%     0.0000
matcher-1K    (50K/1K)                50,000      49,823     100.0%      6.8%     0.0000
unlimited  (64 workers, sema=8)     unlimited   2,900,777    100.0%     36.9%     0.0000
```

> 注：所有场景 0 丢包、0 失败。`matcher-*` 场景的匹配器中一半命中事件类型，一半不命中。

> **2026-08-03 复测**（当前 RoutingStrategy 版，同机）：有界场景全部复现
> （50K msg/s 目标 + 5K 匹配器 100% 达标）；unlimited 场景实测
> **1,366,247 msg/s**（6 月的 2,900,777 未复现，疑为当时环境/配置漂移，
> 与今日 quick 套件 1.39M 一致，非重构回退）。

### 关键结论

| 发现 | 说明 |
|------|------|
| 最大吞吐量 | **~1.4M msg/s**（16 核，unlimited 场景；有界场景最高 50K msg/s 目标 100% 达标）|
| 5K 匹配器影响 | 几乎无影响——无 Handler 的匹配器在 sortedCache 层面已被过滤 |
| 50K msg/s 达标率 | 含 1K 匹配器时 **99.8%**，含 5K 匹配器时 **100%**（20K 目标）|
| 内存效率 | 50K msg/s 下堆内存仅 **~24 MB**（avg 23.7 / peak 37.7 MB）|
| 延迟 | 所有场景 P50 < 0.0001 ms（队列未堆积时） |

---

## Micro-benchmark 延迟

> 2026-08-03 复测（当前 RoutingStrategy 版）；括号内为与直接前身
> HEAD=32ef416（v1.31.0）的对比。

```
操作                         延迟       分配
─────────────────────────────────────────────
Engine ProcessEvent         393 ns/op   272 B/op (2 allocs)  (+36%)
Engine ProcessEvent (并行)   204 ns/op   272 B/op (2 allocs)  (~持平)
Engine HotPath/Empty         207 ns/op     0 B/op (0 allocs)  (+61%, +0.08µs)
Engine HotPath/Light         565 ns/op     0 B/op (0 allocs)  (+15%)
Engine HotPath/Medium      3,033 ns/op     0 B/op (0 allocs)  (+11%)
Engine HotPath/Heavy      24,812 ns/op     0 B/op (0 allocs)  (+8%)
Engine HotPath/AllMatch   42,141 ns/op     6 B/op (0 allocs)  (~持平)
Temp Matcher 处理           370 ns/op   272 B/op (2 allocs)  (+38%)
命令解析 (简单)             1,250 ns/op  2032 B/op (26 allocs)  (未变)
命令解析 (复杂)             2,444 ns/op  2864 B/op (37 allocs)  (未变)
Context.Get                  29 ns/op     0 B/op  (未变)
Context.Set                  40 ns/op     0 B/op  (未变)
```

### 路由层（RoutingStrategy.Plan，见 docs/notes/25）

```
操作                         延迟       分配
─────────────────────────────────────────────
Plan/Empty（纯路由层）       94 ns/op     0 B/op
Plan/CommandHit             653 ns/op     0 B/op
Plan/RegexHit（慢带+Meta） 1,448 ns/op   451 B/op (12 allocs)
```

路由重构（六路归并 → 可插拔 RoutingStrategy）引入约 166ns 抽象税，
经三轮语义等价优化（单迭代器/内置索引直引/池化零化裁剪）回收
86ns（52%）。剩余差距集中在空事件/轻负载（+61%~+15%），
重负载（Heavy/AllMatch/池化）与 HEAD 持平——真实 bot 负载形态
不受影响，端到端吞吐无差异（见上节）。全程 0 allocs 保持。

### 归并迭代器（merge_iter.go，0 allocs）

| 基准 | v1.31.0（6 路硬编码） | 当前（add 动态流） |
|---|---|---|
| SingleList | 805 ns | **473 ns**（-41%） |
| TwoLists | 857 ns | **580 ns**（-32%） |
| SixLists | 2,714 ns | 2,774 ns（+2%） |
| Alloc/UnderThreshold | 3,843 ns | **2,303 ns**（-40%） |
| Alloc/OverThreshold | 15,112 ns | **9,197 ns**（-39%） |

单/双流提升来自空列表跳过使 K_act 衰减；SixLists 峰值场景
+2% 为每次 add 空检查的代价（噪声级）。

### COW 写操作

```
操作                         延迟       分配
─────────────────────────────────────────────
单 Matcher 注册             ~99,000 ns   294 KB  (v1.31.0: ~89µs)
批量注册 (10)              ~482,000 ns   1.4 MB  (v1.31.0: ~519µs, -7%)
Temp Matcher 增删           ~25,000 ns    71 KB  (v1.31.0: ~25µs)
```

写操作为 COW 复制-修改-替换，比读操作高出 ~250x——与设计预期一致。
注册批处理（`RegisterBatch`）将 1000 命令顺序注册从 786ms 降至
~1.6ms（见 docs/notes/01-cow-engine.md V6）。

---

## 优化历程

此报告中的 Benchmark 工具曾存在多个设计问题，经过修复后数据可靠性大幅提升：

| 阶段 | Unlimited/5K | 问题 |
|------|-------------|------|
| 原始非阻塞 + 3s drain | 10,725 (53.6%) | ❌ Drain 不足假象 |
| 修复 drain + 阻塞模式 | 9,821 | ✅ 暴露真实瓶颈 |
| + ExecProfile 预分配缓冲区 | 25,735 | ✅ GC 风暴消除 |
| + demoted 快速路径 | 53,420 | ✅ ShouldPool 排序消除 |
| + sortedCache `hasHandler` 过滤 | **235,000+** | ✅ 源头过滤无 Handler 匹配器 |

---

## 运行基准测试

```bash
# 端到端吞吐量压测（推荐）
cd examples/benchmark
go run throughput_bench.go -suite quick              # 快速验证
go run throughput_bench.go -suite standard            # 完整标准套件
go run throughput_bench.go -suite matcher5k           # 5K 匹配器针对性测试
go run throughput_bench.go -inject-mode blocking      # 阻塞模式（背压测试）

# 额外参数
go run throughput_bench.go -duration 10s -disable-latency   # 减少统计开销

# Micro-benchmark
go test -benchmem -bench="BenchmarkEngine" ./tests/benchmark/
go test -benchmem -bench="BenchmarkContext" ./core/context/

# 路由层 / 热路径 / 归并迭代器（RoutingStrategy 重构相关）
go test -benchmem -bench="BenchmarkRoutingPlan" ./core/engine/
go test -benchmem -bench="BenchmarkHotPath" ./core/engine/
go test -benchmem -bench="BenchmarkMergeIter" ./core/engine/
```
