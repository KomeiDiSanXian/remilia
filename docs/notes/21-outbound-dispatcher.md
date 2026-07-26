# 21 — OutboundDispatcher：以会话为单位的出站调度

> Handler 里一句 `ctx.Reply(...)` 背后是慢速网络 I/O。若同步执行，一次 2s 的平台
> API 调用就占住一个事件处理 goroutine；若无脑 `go func` 异步，同一会话的两条回复
> 可能乱序到达。`OutboundDispatcher`（core/engine/dispatcher.go）的答案是：
> **调度单位不是消息，而是会话（Chat）**——同一 Chat 严格 FIFO，不同 Chat 之间受
> 全局并发上限约束地并行。
>
> 设计方案见 [`../05-performance/OUTBOUND_DISPATCHER_PLAN.md`](../05-performance/OUTBOUND_DISPATCHER_PLAN.md)，本文记录最终实现与踩过的坑。

## 核心结构

```go
type OutboundDispatcher struct {
    baseCtx context.Context   // ForceClose 时统一取消
    sem     chan struct{}     // 全局并发令牌（MaxInflight，默认 512）
    queues  sync.Map          // map[chatID]*chatQueue
    stopped atomic.Bool
    wg      sync.WaitGroup
}

type chatQueue struct {
    mu      sync.Mutex
    q       ringBuffer        // 固定容量环形缓冲（QueueSize，默认 64）
    running bool              // 是否已有 worker 在消费
    chatID  string
}
```

三层限流各司其职：

| 层 | 机制 | 满了会怎样 |
|----|------|-----------|
| 单 Chat 队列 | 环形缓冲（QueueSize） | `Submit` 返回 `ErrQueueFull`（背压给调用方） |
| 全局并发 | sem 令牌（MaxInflight） | worker 等待令牌，队列继续积压 |
| 单任务 | `context.WithTimeout(SendTimeout)` | 该次发送收到超时取消 |

## Submit 与 worker 的生命周期舞蹈

worker 是**按需存在**的：每个 Chat 只在有积压时才有一个 worker goroutine，
队列清空后 worker 把自己（连同 chatQueue）从 `queues` 中摘除并退出——
空闲 Chat 零常驻成本。

```
Submit(chatID, task)                     worker(q)
────────────────────                     ─────────────────
Load/LoadOrStore 拿到 q                   抢全局令牌
q.mu.Lock                                循环：q.mu 下 pop → 锁外执行 task
  复查 queues[chatID] == q  ←──┐          队列空：running=false
  push；running==false 则起 worker        └── 从 queues 删除 q（q.mu 内）→ 退出
q.mu.Unlock
```

### 踩坑实录：自删除与提交的竞态（2026-07 修复）

worker 摘除 `q` 与 `Submit` 拿到 `q` 是并发的。此前的时序缺陷：

1. `Submit` 从 map 里 `Load` 到 q，等待 `q.mu`；
2. worker 在 `q.mu` 内发现队列空 → 从 map 删除 q → 释放锁退出；
3. `Submit` 获得锁，向**孤儿 q** 推任务并重启 worker；
4. 下一个 `Submit` 在 map 里找不到 q → 创建 q2 + worker2。

同一 Chat 出现两个并存队列和 worker——"严格 FIFO"的文档保证被打破。
修复是在 `q.mu` 内**复查映射**：`queues[chatID]` 已不是这个 q 就整体重试。
worker 的删除同样发生在 `q.mu` 临界区内，因此该检查无竞争窗口。
顺带把 `LoadOrStore` 改为先 `Load`——此前热路径上每次 Submit 都白白分配
一个 chatQueue + 环形缓冲。

## 与 Reply/Future 的集成

`ctx.Reply` 提交任务后立即返回 `*future.Future[SendResult]`：

```go
f := ctx.Reply(platform.TextMessage("pong"))   // 已入队即返回
res, err := f.Wait(ctx.Context())              // 需要结果时才等待
```

保证的是"任务已进入 Dispatcher、Handler 返回后仍会执行"；
不保证"已发送成功"。Future 的 Resolve 有双层兜底：

- Reply 层：任务闭包 `defer recover` → 先 Resolve(err) 再重新 panic；
- worker 层：recover 后清空该 Chat 队列、摘除 chatQueue、回调 `Recover` 钩子——
  一个 panic 的发送任务不会拖死整个调度器，代价是该 Chat 当时积压的任务被丢弃
  （通过 `OnDone` 钩子可观测）。

## 生命周期

| 方法 | 语义 |
|------|------|
| `Close()` | 拒绝新任务，存量继续执行 |
| `Drain(ctx)` | 等待所有 worker 完成（不拒新，测试用 `WaitForDispatcher`） |
| `Shutdown(ctx)` | Close + Drain（`Engine.Shutdown` 调用） |
| `ForceClose()` | 取消 baseCtx——在途发送立即收到 `context.Canceled` |

## 可观测性

`DispatcherHooks` 提供 OnQueued/OnStart/OnDone/OnRejected 四个观察点，
`DispatchInfo` 携带排队延迟与执行延迟——builtin/sendqueue 的队列监控即基于此。

## 设计权衡

- **为什么不用一个全局 channel + worker 池？** 全局队列无法表达"同 Chat 有序、
  跨 Chat 并行"；per-chat 队列把顺序约束编码进了数据结构本身。
- **为什么 worker 自删除而不常驻？** 长尾 Chat 极多（每个私聊都是一个 Chat），
  常驻 worker 是 O(活跃会话数) 的 goroutine 泄漏。
- **为什么队列满返回错误而不阻塞？** Handler 不应被出站背压卡住；错误交给
  调用方决定（丢弃/降级/提示用户）。
