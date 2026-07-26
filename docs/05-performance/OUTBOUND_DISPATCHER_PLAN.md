# OutboundDispatcher 实现方案

> **状态：已实现**（`core/engine/dispatcher.go` + `infra/future`）。
> 本文是当时的设计方案存档；最终实现与演进（含 worker 自删除竞态的修复）
> 见架构笔记 [`../notes/21-outbound-dispatcher.md`](../notes/21-outbound-dispatcher.md)。

## 概述

新增 `OutboundDispatcher` 作为独立的发送调度层，将发送 I/O 与 Handler 执行解耦。
调度单位是 Message Stream（Chat），而不是单个 Task。

---

## 第一步：Future

**`infra/future/future.go`**

```go
package future

import (
    "context"
    "errors"
    "sync"
)

var ErrNotReady = errors.New("future: not ready")

type Future[T any] struct {
    once sync.Once
    done chan struct{}
    val  T
    err  error
}

func New[T any]() *Future[T] {
    return &Future[T]{done: make(chan struct{})}
}

func (f *Future[T]) Resolve(val T, err error) bool {
    var resolved bool
    f.once.Do(func() {
        f.val, f.err = val, err
        close(f.done)
        resolved = true
    })
    return resolved
}

func (f *Future[T]) Wait(ctx context.Context) (T, error) {
    select {
    case <-f.done:
        return f.val, f.err
    case <-ctx.Done():
        var zero T
        return zero, ctx.Err()
    }
}

func (f *Future[T]) Result() (T, error) {
    select {
    case <-f.done:
        return f.val, f.err
    default:
        var zero T
        return zero, ErrNotReady
    }
}

func (f *Future[T]) IsDone() bool {
    select {
    case <-f.done:
        return true
    default:
        return false
    }
}

func (f *Future[T]) MustWait(ctx context.Context) T {
    val, err := f.Wait(ctx)
    if err != nil {
        panic(err)
    }
    return val
}

func (f *Future[T]) Done() <-chan struct{} { return f.done }
```

---

## 第二步：OutboundDispatcher

**`core/engine/dispatcher.go`**

核心语义：按 ChatID 严格 FIFO（Message Stream），跨 Chat 并发。

```go
package engine

import (
    "context"
    "errors"
    "fmt"
    "runtime/debug"
    "sync"
    "sync/atomic"
    "time"
)

var ErrDispatcherClosed = errors.New("dispatcher: closed")
var ErrQueueFull = errors.New("dispatcher: queue full")

type DispatcherConfig struct {
    MaxInflight int           // 最大并发 Worker 数（默认 512）
    QueueSize   int           // 单个 Chat 队列最大长度（默认 64）
    SendTimeout time.Duration // 发送超时（默认 30s）
    Recover     func(any)     // panic 回调，nil 则仅记录
    Hooks       DispatcherHooks
}

type DispatchInfo struct {
    ChatID       string
    QueueLen     int
    QueueLatency time.Duration
    RunLatency   time.Duration
    Err          error
}

type DispatcherHooks struct {
    OnQueued   func(chatID string, queueLen int)
    OnStart    func(chatID string)
    OnDone     func(info DispatchInfo)
    OnRejected func(chatID string, err error)
}

// 经典 RingBuffer：固定容量，head/tail 指针
type ringBuffer struct {
    buf  []queuedTask
    head int // 下一个可读位置
    tail int // 下一个可写位置
    len  int
    cap  int
}

func newRingBuffer(cap int) *ringBuffer {
    return &ringBuffer{buf: make([]queuedTask, cap), cap: cap}
}

func (r *ringBuffer) push(t queuedTask) bool {
    if r.len >= r.cap {
        return false
    }
    r.buf[r.tail] = t
    r.tail = (r.tail + 1) % r.cap
    r.len++
    return true
}

func (r *ringBuffer) pop() (queuedTask, bool) {
    if r.len == 0 {
        return queuedTask{}, false
    }
    t := r.buf[r.head]
    r.head = (r.head + 1) % r.cap
    r.len--
    return t, true
}

type chatQueue struct {
    mu      sync.Mutex
    q       ringBuffer
    running bool
    chatID  string
}

type OutboundDispatcher struct {
    baseCtx  context.Context
    cancel   context.CancelFunc
    config   DispatcherConfig
    sem      chan struct{} // 全局并发令牌
    queues   sync.Map     // map[string]*chatQueue
    stopped  atomic.Bool
    wg       sync.WaitGroup
}

func NewOutboundDispatcher(ctx context.Context, config DispatcherConfig) *OutboundDispatcher {
    if config.MaxInflight <= 0 { config.MaxInflight = 512 }
    if config.QueueSize <= 0   { config.QueueSize = 64 }
    if config.SendTimeout <= 0 { config.SendTimeout = 30 * time.Second }
    bCtx, cancel := context.WithCancel(ctx)
    return &OutboundDispatcher{
        baseCtx: bCtx,
        cancel:  cancel,
        config:  config,
        sem:     make(chan struct{}, config.MaxInflight),
    }
}
```

**Submit**：

```go
func (d *OutboundDispatcher) Submit(chatID string, task func(context.Context) error) error {
    if d.stopped.Load() {
        d.reject(chatID, ErrDispatcherClosed)
        return ErrDispatcherClosed
    }

    actual, _ := d.queues.LoadOrStore(chatID, &chatQueue{
        chatID: chatID,
        q:      *newRingBuffer(d.config.QueueSize),
    })
    q := actual.(*chatQueue)

    q.mu.Lock()
    if !q.q.push(queuedTask{enqueueAt: time.Now(), run: task}) {
        q.mu.Unlock()
        d.reject(chatID, ErrQueueFull)
        return ErrQueueFull
    }
    if h := d.config.Hooks.OnQueued; h != nil {
        h(chatID, q.q.len)
    }
    if !q.running {
        q.running = true
        q.mu.Unlock()
        d.wg.Add(1)
        go d.worker(q)
    } else {
        q.mu.Unlock()
    }
    return nil
}

func (d *OutboundDispatcher) reject(chatID string, err error) {
    if h := d.config.Hooks.OnRejected; h != nil {
        h(chatID, err)
    }
}
```

**Worker**（recover 只清理 queue，不处理 Future）：

```go
func (d *OutboundDispatcher) worker(q *chatQueue) {
    defer d.wg.Done()

    select {
    case d.sem <- struct{}{}:
    case <-d.baseCtx.Done():
        q.mu.Lock()
        q.running = false
        q.mu.Unlock()
        return
    }
    defer func() { <-d.sem }()

    defer func() {
        if r := recover(); r != nil {
            stack := string(debug.Stack())
            err := fmt.Errorf("panic in dispatcher worker [%s]: %v\n%s", q.chatID, r, stack)
            // 丢弃队列中剩余任务（Future 由 Submit 方的 recover 保证）
            q.mu.Lock()
            q.q = *newRingBuffer(d.config.QueueSize)
            q.running = false
            if actual, ok := d.queues.Load(q.chatID); ok && actual == q {
                d.queues.Delete(q.chatID)
            }
            q.mu.Unlock()
            if fn := d.config.Recover; fn != nil {
                fn(r)
            }
            if h := d.config.Hooks.OnDone; h != nil {
                h(DispatchInfo{ChatID: q.chatID, Err: err})
            }
        }
    }()

    for {
        q.mu.Lock()
        if q.q.len == 0 {
            q.running = false
            if actual, ok := d.queues.Load(q.chatID); ok && actual == q {
                d.queues.Delete(q.chatID)
            }
            q.mu.Unlock()
            return
        }
        t, _ := q.q.pop()
        queueLen := q.q.len // 在锁内捕获，避免 data race
        q.mu.Unlock()

        start := time.Now()
        if h := d.config.Hooks.OnStart; h != nil {
            h(q.chatID)
        }

        sendCtx, cancel := context.WithTimeout(d.baseCtx, d.config.SendTimeout)
        err := t.run(sendCtx)
        cancel()

        if h := d.config.Hooks.OnDone; h != nil {
            h(DispatchInfo{
                ChatID:       q.chatID,
                QueueLen:     queueLen, // 无锁读取，无 race
                QueueLatency: start.Sub(t.enqueueAt),
                RunLatency:   time.Since(start),
                Err:          err,
            })
        }
    }
}
```

**Shutdown**：

```go
func (d *OutboundDispatcher) Close()              { d.stopped.Store(true) }
func (d *OutboundDispatcher) Drain(ctx context.Context) error { /* wg.Wait */ }
func (d *OutboundDispatcher) Shutdown(ctx context.Context) error { d.Close(); return d.Drain(ctx) }
func (d *OutboundDispatcher) ForceClose()          { d.stopped.Store(true); d.cancel() }
```

---

## 第三步：Context.Reply() 返回 Future

**`core/context/platform_event.go`**

Future 的 recover 在闭包内完成，Dispatcher 不感知：

```go
func (ctx *Context) Reply(msg platform.OutboundMessage) *future.Future[platform.SendResult] {
    f := future.New[platform.SendResult]()
    req := platform.SendRequest{
        Target:  ctx.platformEvent.Chat(),
        Message: msg,
    }
    sender := ctx.platformSender
    chatID := ctx.platformEvent.Chat().ID
    err := ctx.dispatcher.Submit(chatID, func(sendCtx context.Context) error {
        // Reply 层保证 Future 被 Resolve（即使是 panic）
        defer func() {
            if r := recover(); r != nil {
                f.Resolve(platform.SendResult{}, fmt.Errorf("panic in send: %v", r))
                panic(r) // 让 Dispatcher 的 recovery 继续处理
            }
        }()
        res, err := sender.Send(sendCtx, req)
        f.Resolve(res, err)
        return err
    })
    if err != nil {
        f.Resolve(platform.SendResult{}, err)
    }
    return f
}
```

---

## 第四步：ReplyText/ReplyError/ReplySuccess

**`core/context/reply.go`**

```go
func (ctx *Context) ReplyText(text string) *future.Future[platform.SendResult] {
    return ctx.Reply(platform.TextMessage(text))
}
```

---

## 第五步：SenderDecorator

**`middleware/sender/decorator.go`**

```go
package sender

type SenderDecorator func(platform.Sender) platform.Sender

func Chain(decorators ...SenderDecorator) SenderDecorator {
    return func(s platform.Sender) platform.Sender {
        for i := len(decorators) - 1; i >= 0; i-- {
            s = decorators[i](s)
        }
        return s
    }
}
```

**装饰器顺序（文档规定）：**

```
Metrics → Logging → Retry → RateLimit → Timeout → PlatformSender
```

---

## 第六步：Engine 集成

```go
type Engine struct {
    dispatcher *OutboundDispatcher
}

func WithDispatcherConfig(cfg DispatcherConfig) Option

func (e *Engine) Shutdown(ctx context.Context) error {
    e.shutdown.Store(true)
    if e.internals.execPool != nil { _ = e.internals.execPool.Drain(ctx) }
    if e.dispatcher != nil { _ = e.dispatcher.Shutdown(ctx) }
    return nil
}
```

---

## 第七步：Context 注入 Dispatcher

```go
type Context struct {
    dispatcher *engine.OutboundDispatcher
}

func (e *Engine) processEventContextWithPool(ctx *context.Context) {
    ctx.SetDispatcher(e.dispatcher)
    e.processEventMatchers(ctx, true)
}
```

---

## Reply 语义（框架契约）

```
ctx.Reply(msg)
  → 保证：任务已进入 Dispatcher（队列或 Worker）
  → 不保证：已发送成功
  → 不保证：平台已收到
  → 不保证：已获得 MessageID
  → 保证：即使 Handler 已返回，Dispatcher 中的发送仍会继续执行

需要以上保证时，必须：
  future := ctx.Reply(msg)
  res, err := future.Wait(ctx)
```

---

## 实现顺序

| 步骤 | 文件 | 破坏性 |
|------|------|--------|
| 1 | `infra/future/future.go` | 否 |
| 2 | `core/engine/dispatcher.go` | 否 |
| 3 | `core/context/platform_event.go` | **是**（返回值可忽略） |
| 4 | `core/context/reply.go` | **是**（同上） |
| 5 | `middleware/sender/*.go` | 否 |
| 6 | `core/engine/{engine,config}.go` | 否 |

---

## 设计原则速查

| 问题 | 答案 |
|------|------|
| Dispatcher 知道 Future 吗？ | 不知道。只调度 `func(ctx) error` |
| 同一 Chat 消息顺序？ | RingBuffer + 单 Worker，严格 FIFO |
| 不同 Chat 并发？ | 全局 semaphore 限制 |
| semaphore 在谁那边获取？ | Worker goroutine 内部，不阻塞 Submit |
| Future 不会 Resolve 的情况？ | panic 时闭包内的 defer 保证 Resolve |
| 队列实现？ | 固定容量 RingBuffer，head/tail 指针 |
| Sender 接口改不改？ | 不改 |
| Shutdown 语义分几种？ | Close/Drain/Shutdown/ForceClose |
