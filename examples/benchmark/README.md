# Remilia 引擎吞吐量基准测试

该基准测试程序测量 Remilia 框架在高并发消息处理场景下的极限吞吐能力。

## 快速运行

```bash
cd examples/benchmark
go run throughput_bench.go                      # 标准套件（~3 分钟）
go run throughput_bench.go -suite quick          # 快速验证（~2 分钟）
go run throughput_bench.go -suite full           # 完整测试（~5 分钟）
```

### 可用参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-duration` | `10s` | 每个场景的测试时长 |
| `-suite` | `standard` | 测试套件：`quick` / `standard` / `full` |
| `-middleware` | `true` | 是否挂载 Recover 中间件 |
| `-output` | `""` | JSON 结果输出路径（可选） |
| `-gcpercent` | `100` | GOGC 值 |

## 架构

```
┌────────────────────────┐
│  Producer Goroutines   │  ── 按速率注入事件 ──▶
│  (N workers, ticker)   │
└────────────────────────┘
        │
        ▼
┌────────────────────────┐
│    pumpAdapter         │  ── 批量分发 ──▶
│    chan *dto.Payload   │     32 consumer workers
└────────────────────────┘
        │
        ▼
┌────────────────────────┐
│  remilia.Bot           │
│  → Engine.ProcessEvent  │  (COW 并发模型)
└────────────────────────┘
        │
        ▼
┌────────────────────────┐
│  Handler (计数)        │  ← 原子计数 + 延迟记录
└────────────────────────┘
```

## 测试场景

### 标准套件（standard）

| 场景 | Workers | 每 Worker 速率 | 目标速率 | 说明 |
|------|---------|---------------|---------|------|
| smoke | 10 | 10/s | 100 msg/s | 最低负载验证 |
| medium | 50 | 20/s | 1,000 msg/s | 中等负载 |
| high | 100 | 50/s | 5,000 msg/s | 较高负载 |
| stress | 400 | 50/s | 20,000 msg/s | 压力测试 |
| extreme | 1,000 | 50/s | 50,000 msg/s | 极限测试 |
| unlimited | CPU×4 | 不限 | 无上限 | 找系统极限 |

## 注意事项

1. **无真实网络请求** — 使用 pumpAdapter 和 mock 处理，引擎内循环
2. **结果受硬件影响** — CPU 核心数、内存带宽、Go 版本都会影响结果
3. **GC 影响** — unlimited 场景下 GC 暂停占总时间的 ~4-6%，吞吐量会有波动
4. **Handler 为空** — 仅做原子计数，不包含业务逻辑（如 API 调用、消息处理等）
