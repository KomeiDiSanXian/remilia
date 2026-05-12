# Adaptive Router——策略化的事件分发层

> Bot 接收平台事件后需要决定交给谁处理：Engine（传统匹配器）、FSM（活跃会话）、还是 WASM（第三方插件）。随着新能力接入，Bot 中出现了 if/else 链式判断。Adaptive Router 将分发逻辑抽象为可配置的策略规则链，消除 Bot 与具体分发策略的耦合。

## 问题背景

在引入 FSM 和 WASM 之前，Bot 的事件分发路径很简单：

```
Adapter → Bot.handlePlatformEvent → Engine.ProcessPlatformEvent
```

每个新能力都要求在 `bot.go` 中增加一段硬编码判断：

```go
func (b *Bot) handlePlatformEvent(event platform.Event) {
    // 1. 检查 FSM 活跃会话
    if session := b.fsmManager.GetSession(sessionID); session != nil {
        if changed, _ := b.fsmManager.TryTransition(session, event); changed {
            return
        }
    }
    // 2. 传给 Engine
    b.engine.ProcessPlatformEvent(event, sender)
}
```

问题：
- **硬编码顺序**：FSM 必须在 Engine 之前检查，否则 Engine 会拦截消息
- **不可扩展**：新增分发目标需要修改 Bot 核心代码
- **难测试**：无法单独测试分发逻辑，必须构造完整 Bot

## 核心设计

### Router + RouteRule

```go
type Strategy string

const (
    StrategyEngine Strategy = "engine"
    StrategyFSM    Strategy = "fsm"
    StrategyAgent  Strategy = "agent"
)

type RouteRule struct {
    Name     string
    Strategy Strategy
    Match    func(event platform.Event) bool
    Priority int  // 数值越小优先级越高
}

type Router struct {
    rules      []*RouteRule
    mu         sync.RWMutex
    engine     *engine.Engine
    fsmManager *fsm.Manager
}
```

### 匹配规则工厂

```go
func WithCommandPrefix(prefixes ...string) func(event platform.Event) bool {
    return func(event platform.Event) bool {
        text := extractCommand(event.GetMessage())
        cmd, _ := SplitCommandPattern(text)
        for _, p := range prefixes {
            if strings.HasPrefix(cmd, p) {
                return true
            }
        }
        return false
    }
}

func WithFSMRoute(manager *fsm.Manager) func(event platform.Event) bool {
    return func(event platform.Event) bool {
        session, err := manager.GetSession(makeSessionID(event))
        if err != nil || session == nil {
            return false
        }
        newState, changed, err := manager.TryTransition(session, event)
        if err != nil || !changed {
            return false
        }
        return true
    }
}
```

`WithCommandPrefix` 使用 `extractCommand` + `SplitCommandPattern` 而不是简单的 `strings.HasPrefix`，确保命令前缀在段首匹配，避免误匹配子串。

`WithFSMRoute` 的"fallthrough"行为：尝试执行 TryTransition，只有状态实际改变（`changed == true`）才认为路由命中；否则继续评估后续规则。

### 会话 ID 格式

```
platform:chatID
```

示例：`qq:123456`, `discord:987654321`, `telegram:-1001234567890`

### 分发入口

```go
func (r *Router) Route(event platform.Event, sender platform.Sender) bool {
    r.mu.RLock()
    rules := r.rules
    r.mu.RUnlock()

    for _, rule := range rules {
        if !rule.Match(event) {
            continue
        }
        switch rule.Strategy {
        case StrategyEngine:
            r.engine.ProcessPlatformEvent(event, sender)
            return true
        case StrategyFSM:
            return true  // 已由 WithFSMRoute 内部处理
        case StrategyAgent:
            // 预留：Agent 路由
            return true
        }
    }
    return false
}
```

### Bot 中的三阶段路由

```go
func (b *Bot) handlePlatformEvent(event platform.Event) {
    sessionID := makeSessionID(event)

    // Phase 1: EngineManager 多引擎（per-channel isolation）
    if b.engineManager != nil {
        if eng := b.engineManager.GetOrCreate(sessionID); eng != nil {
            eng.ProcessPlatformEvent(event, b.getSender(event))
            return
        }
    }

    // Phase 2: Router 策略链
    if b.router != nil {
        if b.router.Route(event, b.getSender(event)) {
            return
        }
    }

    // Phase 3: 默认 Engine（兼容旧行为）
    b.engine.ProcessPlatformEvent(event, b.getSender(event))
}
```

## 用法示例

```go
router := router.NewRouter(engine, fsmManager)

// 命令路由到 Engine
router.AddRule(&router.RouteRule{
    Name:     "commands",
    Strategy: router.StrategyEngine,
    Match:    router.WithCommandPrefix("/"),
    Priority: 10,
})

// FSM 活跃会话优先
router.AddRule(&router.RouteRule{
    Name:     "fsm-sessions",
    Strategy: router.StrategyFSM,
    Match:    router.WithFSMRoute(fsmManager),
    Priority: 0,  // 最高优先级
})

bot.SetRouter(router)
```

## 文件清单

```
router/
├── router.go      # Router + RouteRule 核心定义
├── match.go       # 匹配工厂函数（WithCommandPrefix, WithFSMRoute）
├── session.go     # 会话 ID 工具
└── router_test.go
```

## 依赖

- `core/engine`：RouteRule.Match 可调用 Engine.ProcessPlatformEvent
- `core/fsm`：WithFSMRoute 使用 FSM Manager
- `plugin/wasm`（预留）：StrategyAgent 预留

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| 规则顺序 | 显式 Priority 排序 | 避免隐式注册顺序错误 |
| FSM 匹配 | Match 函数内部调用 TryTransition | 保持 Route 接口通用，不暴露 FSM 细节 |
| fallthrough | WithFSMRoute 在未改变状态时返回 false | FSM 未命中时降级到 Engine |
| 会话 ID 格式 | `platform:chatID` | 简单唯一，平台间无冲突 |
| 三阶段路由 | engineManager > router > engine | 隔离 Channel 级引擎 vs 默认引擎 |
