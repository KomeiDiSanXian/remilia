# FSM 引擎——声明式多步对话状态机

> 多步对话（如问卷、向导、多轮确认）需要跨消息维护会话状态。传统做法是在插件内手动管理状态映射表和过期逻辑——每个插件都造一个轮子。FSM 引擎提供声明式的有限状态机抽象，统一处理状态转换、会话过期和持久化。

## 问题背景

在机器人对话中，许多场景需要跨多条消息维护上下文：

1. **多步表单**：用户输入需要分步收集，每步依赖上一步的结果
2. **确认流程**：先问"确定吗？"，再根据回答执行或取消
3. **向导式交互**：一系列步骤，用户可前进/后退/取消
4. **临时会话**：超时后自动清理，释放资源

手动实现每个这样的场景都需要重复以下工作：
- 状态管理：用 map 或 sync.Map 跟踪用户当前状态
- 状态转换：if/else 或 switch 判断当前状态决定下一步
- 过期清理：启动 goroutine 定期扫描过期会话
- 持久化：自行对接存储层

FSM 引擎将所有这些抽象为声明的状态机描述，开发者只需定义状态和事件。

## 核心设计

### 包结构

```
core/fsm/
├── fsm.go          # 核心类型：State, Event, FSM, FSMContext, Transition
├── engine.go       # Engine: 状态机引擎，TryTransition 核心逻辑
├── descriptor.go   # FSMDescriptor: 状态机描述符注册
├── manager.go      # Manager: 包装 Engine + 注册的描述符
├── session.go      # Session: 会话状态 + 过期
├── storage.go      # Storage 接口 + MemoryStorage 默认实现
└── errors.go       # 错误类型
```

### 类型体系

```go
type State string
type Event string

type Transition struct {
    From    State           // 起始状态（"*" 表示匹配任意状态）
    Match   func(ctx FSMContext) bool  // 谓词匹配
    Action  func(ctx FSMContext) (State, error)  // 执行动作，返回下一状态
    To      State           // 目标状态（Action 返回空时使用）
}

type FSM struct {
    Initial     State
    States      []State
    Transitions []Transition
    OnEnter     map[State]func(ctx FSMContext) error
    OnExit      map[State]func(ctx FSMContext) error
}

type FSMContext struct {
    Session *Session
    Event   platform.Event
    Storage Storage
    Data    map[string]any  // 会话数据
}
```

### 核心引擎：TryTransition

```go
func (e *Engine) TryTransition(session *Session, event platform.Event) (newState State, changed bool, err error) {
    current := session.CurrentState

    for _, t := range e.fsm.Transitions {
        if t.From != "*" && t.From != current {
            continue
        }
        ctx := newFSMContext(session, event, e.storage)
        if t.Match != nil && !t.Match(ctx) {
            continue
        }

        if exitFn, ok := e.fsm.OnExit[current]; ok {
            ctx := newFSMContext(session, event, e.storage)
            if err := exitFn(ctx); err != nil {
                return current, false, err  // rollback: 保持原状态
            }
        }

        var next State
        if t.Action != nil {
            next, err = t.Action(ctx)
            if err != nil {
                return current, false, err
            }
        } else {
            next = t.To
        }

        session.CurrentState = next
        session.UpdatedAt = time.Now()

        if enterFn, ok := e.fsm.OnEnter[next]; ok {
            ctx := newFSMContext(session, event, e.storage)
            if err := enterFn(ctx); err != nil {
                session.CurrentState = current  // rollback
                return current, false, err
            }
        }

        return next, true, nil
    }

    return current, false, nil
}
```

执行顺序：
1. 遍历所有 Transition，按注册顺序匹配
2. `From` 匹配当前状态（`"*"` 通配）
3. `Match` 谓词过滤
4. 执行当前状态的 `OnExit`（失败则回滚）
5. 执行 `Action` 或使用 `To` 确定下一状态
6. 执行下一状态的 `OnEnter`（失败则完全回滚）
7. 更新会话状态

### 会话管理

```go
type Session struct {
    Key          string    // 唯一标识 "platform:chatID"
    CurrentState State
    Data         map[string]any
    CreatedAt    time.Time
    UpdatedAt    time.Time
    Timeout      time.Duration
}

func (s *Session) IsExpired() bool {
    if s.Timeout <= 0 {
        return false
    }
    return time.Since(s.UpdatedAt) > s.Timeout
}
```

Manager 启动后台 Cleanup goroutine，定期扫描过期会话：

```go
func (m *Manager) startCleanup(ctx context.Context, interval time.Duration) {
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                m.cleanup()
            }
        }
    }()
}

func (m *Manager) cleanup() {
    expired := m.storage.ListExpired(time.Now())
    for _, key := range expired {
        m.storage.Delete(key)
    }
}
```

### Storage 接口

```go
type Storage interface {
    Get(key string) (*Session, error)
    Set(session *Session) error
    Delete(key string) error
    ListExpired(now time.Time) ([]string, error)
}
```

`MemoryStorage` 使用 `sync.Map` 实现，适用于单实例部署。生产环境可对接 Redis。

### FSMDescriptor 注册

```go
type FSMDescriptor struct {
    Name        string
    Version     string
    FSM         *FSM
    Config      map[string]any
}
```

Manager 统一管理注册和取消：

```go
type Manager struct {
    engine       *Engine
    descriptors  map[string]*FSMDescriptor
    mu           sync.RWMutex
    storage      Storage
    cleanupCtx   context.Context
    cleanupCancel context.CancelFunc
}
```

## 用法示例

```go
// 定义状态
const (
    StateIdle      fsm.State = "idle"
    StateAwaitName fsm.State = "await_name"
    StateAwaitAge  fsm.State = "await_age"
    StateConfirm   fsm.State = "confirm"
)

// 注册 FSM 描述符
descriptor := &fsm.FSMDescriptor{
    Name:    "registration",
    Version: "1.0.0",
    FSM: &fsm.FSM{
        Initial: StateIdle,
        States:  []fsm.State{StateIdle, StateAwaitName, StateAwaitAge, StateConfirm},
        Transitions: []fsm.Transition{
            {From: StateIdle, Match: cmdMatch("/register"), Action: startRegistration},
            {From: StateAwaitName, Action: collectName},
            {From: StateAwaitAge, Action: collectAge},
            {From: StateConfirm, Match: confirmMatch, To: StateIdle},
            {From: "*", Match: cmdMatch("/cancel"), Action: cancelRegistration},
        },
        OnEnter: map[State]func(ctx fsm.FSMContext) error{
            StateAwaitName: sendPrompt("请输入姓名："),
            StateAwaitAge:  sendPrompt("请输入年龄："),
            StateConfirm:   sendPrompt("确认提交？(y/n)"),
        },
    },
}

manager.Register(descriptor)
```

## 依赖

- `core/engine`：Bot 通过 Engine 处理事件，TryTransition 在事件处理流程中调用
- `platform`：FSMContext 携带 `platform.Event`

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| 状态表示 | 字符串常量 State | 可读、可序列化、便于调试 |
| 通配符 From | `"*"` 字符串通配 | 简化通用 handler（如 /cancel）注册 |
| Action vs To | Action 函数 + To 字段 | Action 返回动态下一状态，To 提供静态短路 |
| OnEnter/OnExit 回滚 | 失败时 revert CurrentState | 保证状态一致性 |
| 过期清理 | 独立 goroutine 定期扫描 | 简单可靠，适合毫秒级精度要求不高的场景 |
| 存储 | Storage 接口 + MemoryStorage 默认 | 开箱即用，可扩展 |
