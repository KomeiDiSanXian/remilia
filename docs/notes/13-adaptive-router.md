# Adaptive Router——优先级驱动的路由分发层

> Bot 接收平台事件后需要决定交给谁处理：FSM（活跃会话/启动事件）、Engine（命令/消息）、Agent（预留）。
> Router 将所有路由逻辑抽象为按 Priority 排序的 RouteRule 链，FSM 作为内建规则始终优先。

## 核心设计

### RouteRule

```go
type RouteRule struct {
    Name     string                           // 调试标识
    Priority int                              // 越小越优先
    Match    func(ctx *corectx.Context) bool  // 判断是否适用
    Handle   func(ctx *corectx.Context) bool  // 执行路由，true=已处理
}
```

Handle 约定：
- `Handle` 返回 `true` → 事件被消费，后续规则停止
- `Handle` 返回 `false` → 继续评估下一规则
- `Handle` 为 `nil` → `Route()` 自动设为 `dispatchToEngine`

### Router

```go
type Router struct {
    engine        *engine.Engine
    engineManager *engine.EngineManager
    fsmEngine     *fsm.Engine
    rules         []*RouteRule
}
```

## Dispatch 流程

```
Dispatch(ctx)
  ├── FSM Check (内建, Priority=-1000)
  │   ├── TryTransition (活跃会话) → true → return
  │   ├── TryStartSession (启动事件) → true → return
  │   └── false → fallthrough
  ├── User Rules (按 Priority 排序)
  │   ├── WithCommandPrefix (Priority=0) → Match → Handle dispatchToEngine → return
  │   └── 其他规则按序评估
  └── Fallback → dispatchToEngine
```

FSM 是内建的一级路由，不受用户规则声明顺序影响。每次 Dispatch 都会先检查 FSM。

### 路由优先级

| 规则 | Priority | 说明 |
|------|----------|------|
| FSM（内建） | -1000 | 活跃会话迁移 / 启动事件 |
| WithCommandPrefix | 0 | / 前缀命令路由到 Engine |
| WithCustom | 100 | 用户自定义规则 |
| Fallback | ∞ | 无规则匹配时走 dispatchToEngine |

## 命名规则工厂

### WithCommandPrefix

```go
func WithCommandPrefix() *RouteRule
```

匹配带有命令前缀的消息（如 `/help`, `!!admin`）。
使用 `extractCommand` + `SplitCommandPattern` 而非简单 `strings.HasPrefix`：

- `/help` → prefix="/" → 匹配
- `!!admin` → prefix="!!" → 匹配
- `hello` → prefix="" → 不匹配
- `帮助` → prefix="" → 不匹配

Handle 为 nil，由 Route() 自动设为 dispatchToEngine。

### WithCustom

```go
func WithCustom(name string, match func(ctx) bool, handle func(ctx) bool) *RouteRule
```

完全自定义的匹配 + 处理。

## 文件清单

```
router/
├── router.go      — Router、RouteRule、Dispatch、handleFSM、dispatchToEngine
├── options.go     — WithCommandPrefix、WithCustom
└── router_test.go — 10 tests
```

## 引擎分发

dispatchToEngine 接收两个路径：

1. **engineManager 非 nil** → `engineManager.Dispatch(ctx)`（per-channel 隔离）
2. **engineManager 为 nil** → `engine.ProcessEvent(ctx)`（单引擎）

## 回退行为

所有规则均未匹配时，Router 调用 dispatchToEngine 保证事件不被丢弃。

## 异步执行语义（v1.21.1 起）

路由到 Engine 只保证事件**进入**引擎，不保证 handler 已执行完毕：
命令处理器经 ExecPool 自适应调度（见 [22 — 自适应执行](22-adaptive-execution.md)），
慢命令会在池中异步执行。`Dispatch` 返回 ≠ 副作用可见。

对测试的影响：路由后立刻断言 handler 副作用会产生 flaky——
需要先 `eng.WaitForAsyncHandlers()` 等待在途异步 handler 收敛：

```go
router.Dispatch(ctx)
eng.WaitForAsyncHandlers()   // 等待异步命令处理器完成
require.True(t, handled.Load())
```

router_test.go 中所有断言 handler 执行结果的用例都遵循此模式。

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| 规则排序 | Priority 排序 | 声明顺序无关，新增规则只需指定 Priority |
| FSM 集成 | 内建规则，非用户声明 | 无需开发者关心路由顺序 |
| Handle 自动补全 | Handle=nil → dispatchToEngine | 减少样板代码 |
| WithCommandPrefix | Handle=nil | 匹配即路由到 Engine，无需显式处理器 |
| dispatchToEngine | engineManager > engine | 通道隔离 vs 单引擎回退 |
