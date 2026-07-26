# 22 — 自适应执行：谁该进池，谁走同步

> 事件引擎最矛盾的需求：快 handler（回一句 "pong"）走 goroutine 池纯属浪费——
> 调度开销比执行本身还贵；慢 handler（AI、外部 API）走同步则会卡住平台的
> 派发 goroutine，一个慢命令拖累整个平台的所有会话。
> `ExecProfile`（判定）+ `ExecPool`（执行）组成的自适应层让**每个 matcher
> 用自己的历史耗时数据决定自己的执行方式**。

## ExecProfile：用 p50 说话

每个 matcher 懒创建一个 `ExecProfile`，记录最近 32 次执行耗时（环形窗口）：

```go
const defaultSlowThreshold = 50 * time.Millisecond

// 判定逻辑（ShouldPool）：
//   promoted           → 池        （已确认慢）
//   demoted            → 同步      （已确认快，跳过统计快路径）
//   样本不足            → 池        （默认怀疑所有 handler 都是慢的）
//   p50 ≥ 50ms         → promote 并入池
//   满窗口 && p50 < 25ms && 最近 10 次都快 → demote 走同步
```

不对称的升降级是刻意的：**一次极慢立即提升**（`Record` 中 elapsed > 100ms 直接
promote，快速隔离故障 handler），**长期稳定快才降级**（满 32 样本 + 连续 10 次
低于阈值一半）。宁可多付一次调度开销，不冒一次卡死派发线程的险。

> v1.21.1 曾修过一个"整个机制失效"级别的缺陷：`OnCommand` 创建的 matcher
> 漏掉了 `execProfile` 字段，而入池判定要求 `profile != nil`——于是所有命令
> handler 恒走同步。慢命令恰恰几乎都是命令。字段可缺省 + 判定静默跳过 = 
> 静默失效的完美配方。

## 决策点：processEventMatchers

```go
blocking := m.isBlocking(channelKey) || blockAll   // ① 先算阻断
if allowPool && profile.ShouldPool() == ExecClassPool {
    pooledCtx := ctx.Clone()                       // ② 入池前必须克隆
    if execPool.TrySubmit(func() {
        pooledCtx.SetMatcher(m)
        e.invokeHandler(pooledCtx, m)              // ③ 池内执行 + Record 耗时
    }) {
        if blocking { break }
        continue
    }
    // ④ 池满 → fallback 同步
}
ctx.SetMatcher(m); e.invokeHandler(ctx, m)         // 同步路径
```

三处顺序都有过血的教训（v1.21.1）：

- **① 阻断先算**：此前入池成功直接 `continue`，跳过了阻断检查——`SetBlock(true)`
  是否生效取决于 handler 当时被判快还是慢，行为不确定；
- **② 克隆再入池**：池 goroutine 与派发循环并发使用同一个 `*Context` 会撕裂
  接口字段写入、互相覆盖 deadline/span（见 [`23-context-design.md`](23-context-design.md)）；
- **④ fallback 不阻塞**：池满退回同步执行，事件不丢、调用方不等。

需要确定性同步语义的场景（AI 插件捕获命令回复、单元测试）用
`ProcessEventSync` / `WithExecPoolDisabled()` 整体关闭入池。

## ExecPool：有界令牌 + 有界队列

```go
type ExecPool struct {
    sem    chan struct{}   // 并发令牌（默认 64）
    queue  chan func()     // 等待队列（默认 128）
    exitMu sync.Mutex      // worker 退出协议（见下）
}
```

不做 work-stealing——池里跑的都是 I/O 密集任务，缓存亲和没有意义，
有界令牌 + 有界队列 + 惰性 worker（有任务才起，空闲即退）已经足够。
每个任务经 `runPoolTask` 包一层 recover：池 goroutine 里逃逸的 panic
会直接终止进程，这层兜底覆盖了 handler 之外的中间件链构造等代码。

### 退出协议（2026-07 修复）

惰性 worker 有一个经典的活性缺陷：worker 完成最终 drain、归还令牌退出的
**间隙**里，一个任务恰好入队——没有任何消费者，任务滞留到下一次 TrySubmit。

修复用一把只覆盖边界动作的小锁（exitMu）建立不变式：

```
worker 退出前：  exitMu 内做最终非阻塞收队 → 空才归还令牌
TrySubmit 入队后：exitMu 内尝试抢令牌 → 抢到就起 drain worker
```

两个临界区互斥，于是"入队成功"之后只有两种世界：仍有 worker 持有令牌
（它的退出检查必然看到这个任务），或者能立刻抢到令牌起新 worker。
临界区内全是非阻塞操作，直接执行的快路径完全不碰这把锁。

## 池的归属

`WithSharedExecPool(pool)` 允许多个 Engine 复用一个池。共享池的生命周期归
调用方：`Engine.Shutdown` 不会 Drain 它（否则先关闭的 Engine 会拔掉其他
Engine 的执行器）。

> 2026-07 复查发现该选项自诞生起就不生效——NewEngine 在应用完选项后
> 无条件 `execPool = NewExecPool(cfg)` 覆盖了注入的共享池，且零测试覆盖。
> "没有测试守护的配置项等于不存在"（[`20-core-review-lessons.md`](20-core-review-lessons.md) 模式七）。

## 与三级调用路径的关系

自适应层决定"在哪个 goroutine 执行"；进入 `invokeHandler` 后的
编译链快路径（compiledVersion 比对）决定"以多低的开销执行"——
后者见 [`04-middleware-chain.md`](04-middleware-chain.md)。两层正交。
