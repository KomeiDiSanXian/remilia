# FSM 状态机指南

FSM（有限状态机）引擎（`core/fsm/`）用于声明式多步骤对话管理：注册一个带状态的机器，由事件驱动状态迁移，上下文感知地引导用户完成多步流程（如注册、收集信息、技能添加等）。

## 快速上手

```go
import "github.com/KomeiDiSanXian/remilia/core/fsm"

// 1. 创建引擎（nil = 内存存储，跨进程重启会丢失会话）
engine := fsm.NewEngine(nil)

// 2. 注册状态机
signup := &fsm.FSM{
    Name:    "signup",
    Initial: "idle",
    Events: []fsm.Event{
        {
            Name: "start",
            From: "idle", To: "ask_name",
            Match: func(ctx *corectx.Context) bool {
                return ctx.GetMessageContent() == "/signup"
            },
            Action: func(ctx *fsm.FSMContext) error {
                ctx.Reply(platform.TextMessage("请输入你的名字"))
                return nil
            },
        },
        {
            Name: "collect_name",
            From: "ask_name", To: "done",
            Match: func(ctx *corectx.Context) bool {
                return ctx.GetMessageContent() != ""
            },
            Action: func(ctx *fsm.FSMContext) error {
                ctx.Data["name"] = ctx.GetMessageContent()
                ctx.Reply(platform.TextMessage("你好，" + ctx.Data["name"].(string)))
                return nil
            },
        },
    },
}
if err := engine.Register(signup); err != nil {
    return err
}

// 3. 消息处理器中驱动状态机
// 新会话用 StartSession 创建，进行中会话用 TryTransition 推进
sessionID := userID // 按 平台+用户+群 组合的唯一标识
if _, ok, _ := engine.TryTransition(ctx, sessionID); !ok {
    // 无会话：尝试启动
    if err := engine.StartSession(ctx, "signup", sessionID); err != nil {
        // ErrSessionExists 表示已存在会话
    }
}
```

## 核心概念

### `fsm.FSM`

| 字段 | 说明 |
|------|------|
| `Name` | 引擎内唯一标识 |
| `Initial` | 新会话的初始状态 |
| `Events` | 有序迁移规则列表（第一个匹配的事件胜出） |
| `OnEnter` / `OnExit` | 状态进入/离开回调（返回错误会回滚/阻止迁移） |
| `Timeout` | 会话 TTL（从**最后活动**起算；0 = 无超时） |
| `RefreshOnActivity` | true 时每次成功迁移刷新 TTL（会话只要活跃就不过期） |

### `fsm.Event`

- `From`：源状态，`"*"` 匹配任意当前状态（兜底迁移）
- `To`：目标状态；**为空表示终止态**——Action 执行后自动结束会话
- `Match`：判定是否触发（接收原始 `*corectx.Context`）
- `Action`：迁移时执行（接收 `*FSMContext`）

### `fsm.FSMContext`

回调上下文，嵌入 `*corectx.Context`（可直接 `Reply`/`GetMessageContent`），并附带：

- `SessionID`：当前会话唯一标识
- `Current`：回调时的当前状态
- `Data`：会话级键值存储（迁移间持久化）
- `FSM`：所属状态机定义

> **注意**：回调在同一会话的互斥锁内执行；回调内不要对**同一 sessionID** 再次调用
> `TryTransition`/`StartSession`/`GetSession`（会死锁）。跨会话调用是安全的。

### 终止语义

- `To == ""` 且未调用 `EndSession()` → 自动结束会话（推荐：省去 `To` 即终止态）
- `To != ""` 且回调中调用了 `EndSession()` → 终止而非迁移
- 显式 `ctx.EndSession()` 总是结束会话

## 引擎 API

| 方法 | 说明 |
|------|------|
| `NewEngine(storage)` | 创建引擎（`nil` = 内存存储；可传 `fsm.MemoryStorage` 或自定义 `Storage` 接口） |
| `Register(*FSM)` / `Unregister(name)` | 注册/注销状态机（重复注册报错） |
| `StartSession(ctx, fsmName, sessionID)` | 创建新会话并进入 Initial 状态；已存在返回 `ErrSessionExists` |
| `TryTransition(ctx, sessionID)` | 在当前状态中匹配事件并迁移；返回 (新状态, 是否迁移, error) |
| `EndSession(sessionID)` | 显式结束会话 |
| `UpdateSessionData(sessionID, fn)` | 在会话锁内更新 `Data` |
| `GetSession(sessionID)` | 获取会话（**返回副本**，只读使用） |
| `StartCleanup(interval, stop)` | 定期清理过期会话（配合插件生命周期 stop channel） |

## 集成到插件

FSM 引擎由插件自建并生命周期绑定（与框架无关，多插件可各自持有）：

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    p.fsmEngine = fsm.NewEngine(nil)
    p.fsmEngine.Register(signupFSM)
    ctx.Spawn(func(runCtx stdctx.Context) {
        p.fsmEngine.StartCleanup(5*time.Minute, runCtx.Done())
    })
    return p, nil
}
```

参考实现：`builtin/ai` 插件的 `skill_add` 两步注册流程（`registerSkillAddFSM`）。

---

*详细设计见 [notes/12-fsm-engine.md](../notes/12-fsm-engine.md)。*
