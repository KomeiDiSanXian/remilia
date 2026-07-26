# FSM 引擎——声明式多步对话状态机

> 多步对话（如问卷、注册表单、多轮确认）需要跨消息维护会话状态。
> `core/fsm/` 提供声明式的有限状态机抽象，统一处理状态迁移、会话过期、终态清理和 Router 自动启动。
> FSM 是 Router 的内建一级路由（Priority=-1000），无需手动声明路由规则。

## 问题背景

在机器人对话中，许多场景需要跨多条消息维护上下文：

1. **多步表单**：收集姓名→邮箱→确认，每步依赖上一步
2. **确认流程**：先问"确定吗？"，再根据回答执行或取消
3. **临时会话**：超时后自动清理，释放资源

手动实现需要重复造轮子：状态映射表、if/else 判断、过期清理 goroutine、存储对接。
FSM 引擎将这一切抽象为声明的状态机描述。

## 核心设计

### 包结构

```
core/fsm/
├── fsm.go          # State, Event, FSM, Engine, TryTransition/TryStartSession/StartSession
├── context.go      # FSMContext（嵌入 *corectx.Context + EndSession + ended 标记）
├── descriptor.go   # FSMDescriptor
├── manager.go      # Manager 包装 Engine + 注册描述符
├── storage.go      # Storage 接口 + MemoryStorage
```

### 类型体系

```go
type State string

type Event struct {
    Name   string
    From   State                       // 源状态（"*" 匹配任意）
    To     State                       // 目标状态（空=终态，自动结束会话）
    Match  func(ctx *corectx.Context) bool
    Action func(ctx *FSMContext) error
}

type FSM struct {
    Name    string
    Initial State
    Events  []Event                    // 按顺序匹配，第一个胜出
    OnEnter map[State]func(ctx *FSMContext) error
    OnExit  map[State]func(ctx *FSMContext) error
    Timeout time.Duration              // TTL：默认自会话创建起计
    RefreshOnActivity bool             // true=滑动 TTL，每次成功迁移续期
}

type FSMContext struct {
    *corectx.Context                   // 嵌入，Reply() 等方法直接可用
    SessionID string
    Current   State
    Data      map[string]any
    FSM       *FSM
    engine    *Engine                  // 用于 EndSession
    ended     bool                     // Action 中调 EndSession 后置 true
}

func (ctx *FSMContext) EndSession()   // 无论 event.To 是否为空，结束会话
```

### 终态规则

To 为空 = 终态。Action 返回后框架自动结束会话：

| To  | 调了 EndSession? | 结果 |
|-----|-----------------|------|
| 空  | 否 | 框架自动 Delete |
| 空  | 是 | 双保险，无冲突 |
| 非空 | 是 | ended=true → 框架跳过 Save |
| 非空 | 否 | 正常迁移到 To |

### TryTransition

```go
func (e *Engine) TryTransition(ctx *corectx.Context, sessionID string) (State, bool, error)
```

遍历事件的执行顺序：
1. 检查 From 是否匹配当前状态（`"*"` 通配）
2. Match 谓词过滤
3. OnExit[当前状态]（失败回滚）
4. Action（失败回滚）
5. 检查 `fsmCtx.ended` + `event.To == ""` → 终态则 Delete 并返回
6. OnEnter[下一状态]（失败回滚）
7. 更新并 Save session

### TryStartSession

```go
func (e *Engine) TryStartSession(ctx *corectx.Context, sessionID string) (bool, error)
```

由 Router 自动调用。遍历所有已注册 FSM，检查 Initial 状态事件：
1. 若已有会话，检查 `canTransitionFrom(Current)`——卡死则清理后重新开始
2. 查找 From=Initial 或 From="*" 且 Match 返回 true 的事件
3. 创建 session + 执行完整迁移（OnExit → Action → 终态检查 → OnEnter）

### 存储接口

```go
type Storage interface {
    Get(sessionID string) *Session
    Save(session *Session)
    Delete(sessionID string)
    Cleanup(before int64) int
}
```

`MemoryStorage` 使用 `sync.RWMutex`，自动忽略过期会话。注意 `Get` 只是把过期会话
视为不存在、不做删除——真正回收依赖 `StartCleanup`，长期运行务必启动清理。

### 并发模型与会话安全（2026-07 重构）

Storage 的锁只保护 map 本身，保护不了 `Session.Current`/`Session.Data` 的内容。
复查发现同一 sessionID 的并发事件（用户连发两条消息）会对 Data（map）产生
无锁读写——并发 map 写可直接 panic。现行并发模型：

- **per-session 互斥锁**：`sync.Map[sessionID]*sync.Mutex`（条目常驻，bot 场景基数=
  会话数，成本可忽略）。TryTransition/TryStartSession/StartSession 全程持有该锁，
  同一会话严格串行，不同会话完全并发。会话在锁内重新 Get，check-act 原子。
- **回调重入约束**：Action/OnEnter/OnExit 在会话锁内执行。回调里调
  `FSMContext.EndSession` 安全（不取会话锁）；对**同一** sessionID 再调
  TryTransition/StartSession/GetSession 会自死锁；对其他 sessionID 安全。
- **锁外执行回调的例外**：`TryStartSession` 此前整个遍历持有 `e.mu.RLock`，
  回调内 Register/Unregister 直接死锁——现在先在锁内快照 FSM 列表，释放 e.mu
  后再遍历执行（会话锁仍持有）。
- **GetSession 返回副本**（含 Data 浅拷贝），与文档一致；外部写入 Data 的唯一
  受支持方式是 `UpdateSessionData(sessionID, fn)`——在会话锁内应用变更并 Save。
  builtin/ai 曾依赖旧的活指针行为（注释里自己承认"配合内存 Storage 修改即时
  生效"），已迁移。
- **孤儿会话清理**：FSM 被 Unregister 后，遗留会话曾让该 sessionID 永远无法
  开启新会话（无 Timeout 时）；TryStartSession 现在检测到关联 FSM 不存在时
  自动删除孤儿会话。
- **TTL 语义**：默认自创建起计、活跃不续期；`RefreshOnActivity: true` 启用滑动
  TTL（每次成功迁移重置为 now+Timeout）。

## 用法示例

```go
fsmMgr := fsm.NewManager(nil)
signupFSM := &fsm.FSM{
    Name: "signup", Initial: "idle", Timeout: 3 * time.Minute,
    Events: []fsm.Event{
        {Name: "cancel", From: "*",
            Match: func(ctx *eventctx.Context) bool {
                return strings.TrimSpace(ctx.GetMessageContent()) == "/cancel"
            },
            Action: func(ctx *fsm.FSMContext) error {
                _, e := ctx.Reply(platform.TextMessage("已取消"))
                ctx.EndSession()
                return e
            }},
        {Name: "start", From: "idle", To: "ask_name",
            Match: func(ctx *eventctx.Context) bool {
                return strings.TrimSpace(ctx.GetMessageContent()) == "/signup"
            },
            Action: func(ctx *fsm.FSMContext) error {
                _, e := ctx.Reply(platform.TextMessage("请输入昵称："))
                return e
            }},
        {Name: "input_name", From: "ask_name", To: "ask_age",
            Match: func(ctx *eventctx.Context) bool { return strings.TrimSpace(ctx.GetMessageContent()) != "" },
            Action: func(ctx *fsm.FSMContext) error {
                ctx.Data["name"] = strings.TrimSpace(ctx.GetMessageContent())
                _, e := ctx.Reply(platform.TextMessage("请输入年龄："))
                return e
            }},
        {Name: "input_age", From: "ask_age",
            // To 为空 → 终态，Action 后自动 EndSession
            Match: func(ctx *eventctx.Context) bool { return strings.TrimSpace(ctx.GetMessageContent()) != "" },
            Action: func(ctx *fsm.FSMContext) error {
                ctx.Data["age"] = strings.TrimSpace(ctx.GetMessageContent())
                _, e := ctx.Reply(platform.TextMessage("注册成功！"))
                ctx.EndSession()
                return e
            }},
    },
}
fsmMgr.Register(&fsm.FSMDescriptor{Name: "signup", FSM: signupFSM})
rtr := router.New(eng, fsmMgr.Engine())
// FSM 是内建规则，无需 WithFSMRoute()
```

## FSM 与 Router 的集成

- Router.New 传入 fsmEngine → 自动注册 Priority=-1000 的内建 FSM 规则
- 每次事件：先 TryTransition（活跃会话），再 TryStartSession（启动事件）
- FSM 优先于所有用户规则，不受声明顺序影响
- 匹配引擎规则的命令（如 `/help`）在无 FSM 会话时正常通过

## 文件清单

```
core/fsm/
├── fsm.go          — FSM 核心类型 + Engine（TryTransition/TryStartSession/StartSession/EndSession）
├── context.go      — FSMContext（嵌入 Context + EndSession + ended 标记）
├── descriptor.go   — FSMDescriptor + Validate
├── manager.go      — Manager（Register/Unregister/GetEngine/ListDescriptors）
└── storage.go      — Session + Storage 接口 + MemoryStorage
```

## 依赖

- `core/context` — FSMContext 嵌入，Reply/GetMessageContent 等方法
- `router` — 内建 FSM 规则在 router.New 中自动注册

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| 状态表示 | `type State string` | 可读、可序列化、类型安全 |
| 通配符 From | `"*"` | 简化 /cancel 等全局处理 |
| 终态 | To 为空 + ended 标记 | 避免 EndSession 被 Save 覆盖 |
| Action 签名 | `func(ctx *FSMContext) error` | 通过 ctx 读写会话数据，无返回值回传状态 |
| 事件顺序 | 列表顺序，第一个胜出 | 通配符 cancel 放首位避免被具体事件拦截 |
| 会话清理 | 持久化 userLocales map | 独立 goroutine 定期扫描 |
