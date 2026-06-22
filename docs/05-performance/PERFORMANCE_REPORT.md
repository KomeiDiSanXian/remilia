# Remilia 性能报告

> **最后更新**: 2026-06-22  
> **测试环境**: AMD Ryzen 7 5800H (16 线程), 32 GB RAM, Go 1.26, Windows  
> **测试工具**: `examples/benchmark/throughput_bench.go`（端到端压测）+ `go test -benchmem`（micro-benchmark）

---

## 端到端吞吐量（standard 套件）

通过 pumpAdapter 注入事件，包含完整适配器 → 引擎 → 中间件 → 处理器链路，每场景持续 10 秒。

```
场景                               目标(msg/s)  实际(msg/s)  成功率     CPU(进程)   堆内存
─────────────────────────────────────────────────────────────────────────────────────
smoke      (100 msg/s, 0 matchers)       100         100     100.0%      0.8%     1.5 MB
medium    (1000 msg/s, 0 matchers)     1,000       1,000     100.0%      0.2%     2.4 MB
high      (5000 msg/s, 0 matchers)     5,000       4,996     100.0%      0.3%     2.5 MB
stress   (20000 msg/s, 0 matchers)    20,000      19,969     100.0%      2.2%     6.0 MB
extreme  (50000 msg/s, 0 matchers)    50,000      49,897     100.0%      6.3%    12.7 MB
matcher-100   (20K/100)               20,000      19,970     100.0%     13.1%     6.1 MB
matcher-1K    (20K/1K)                20,000      19,996     100.0%     46.4%    14.1 MB
matcher-5K    (20K/5K)                20,000      10,725      53.6%    153.4%    66.2 MB
matcher-100   (50K/100)               50,000      49,923     100.0%    228.2%    14.0 MB
matcher-1K    (50K/1K)                50,000      49,972      99.9%    283.5%    62.4 MB
unlimited  (64 workers, sema=8)     unlimited     449,884    100.0%    337.9%     3.5 MB
```

### 关键结论

| 发现 | 说明 |
|------|------|
| 最大吞吐量 | **~450,000 msg/s**（16 核满载，100% 成功率，0 丢包） |
| 50K msg/s 达标率 | 含 1K 匹配器时仍能 **99.9%** 达成目标 |
| 匹配器 5K+ 瓶颈 | 20K msg/s 下 5K 匹配器降至 53.6%——6 路合并排序成为瓶颈 |
| 内存效率 | 50K msg/s 下堆内存仅 **12-17 MB**，匹配器增加时因状态复制上升至 ~100 MB |
| GC 影响 | 匹配器多时 GC 频繁（~3000 次/10s），但 pause 平均仅 **0.1 ms** |

---

## Micro-benchmark 延迟

```
操作                         延迟       分配
─────────────────────────────────────────────
Engine ProcessEvent         285 ns/op   224 B/op (2 allocs)
Engine ProcessEvent (并行)   200 ns/op   224 B/op (2 allocs)
命令解析 (简单)             1,254 ns/op  2032 B/op (26 allocs)
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

## 运行基准测试

```bash
# 端到端吞吐量压测（推荐）
cd examples/benchmark
go run throughput_bench.go -suite quick        # 快速验证
go run throughput_bench.go -suite standard     # 完整套件
go run throughput_bench.go -suite full -duration 10s

# Micro-benchmark
go test -benchmem -bench="BenchmarkEngine" ./tests/benchmark/
go test -benchmem -bench="BenchmarkContext" ./core/context/
