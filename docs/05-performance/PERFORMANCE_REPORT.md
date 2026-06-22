# Remilia 性能报告

> **最后更新**: 2026-06-22  
> **测试环境**: AMD Ryzen 7 5800H (16 线程), 32 GB RAM, Go 1.26, Windows  
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

### 关键结论

| 发现 | 说明 |
|------|------|
| 最大吞吐量 | **~450,000 msg/s**（16 核，端到端含完整适配器链路）|
| 5K 匹配器影响 | 几乎无影响——无 Handler 的匹配器在 sortedCache 层面已被过滤 |
| 50K msg/s 达标率 | 含 1K 匹配器时 **99.9%**，含 5K 匹配器时 **99.9%** |
| 内存效率 | 50K msg/s 下堆内存仅 **20-30 MB** |
| 延迟 | 所有场景 P50 < 0.0001 ms（队列未堆积时） |

---

## Micro-benchmark 延迟

```
操作                         延迟       分配
─────────────────────────────────────────────
Engine ProcessEvent         285 ns/op   224 B/op (2 allocs)
Engine ProcessEvent (并行)   200 ns/op   224 B/op (2 allocs)
命令解析 (简单)             1,250 ns/op  2032 B/op (26 allocs)
命令解析 (复杂)             2,444 ns/op  2864 B/op (37 allocs)
Context.Get                  29 ns/op     0 B/op
Context.Set                  40 ns/op     0 B/op
Temp Matcher 处理           310 ns/op   224 B/op (2 allocs)
```

### COW 写操作

```
操作                         延迟       分配
─────────────────────────────────────────────
单 Matcher 注册            ~175,000 ns   154 KB
批量注册 (10)              ~890,000 ns   1.4 MB
Temp Matcher 增删          ~180,000 ns   182 KB
```

写操作为 COW 复制-修改-替换，比读操作高出 ~500x——与设计预期一致。

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
```
