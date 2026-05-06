# Terminal Bot 示例

使用 `platform/terminal` 适配器配合内置插件系统在本地命令行中调试 Bot，
无需连接任何外部平台即可测试命令和插件功能。

## 功能

### 插件命令

| 插件 | 命令 | 说明 |
|------|------|------|
| help | `/help` | 查看所有可用命令 |
| help | `/help <命令>` | 查看命令详情 |
| help | `/help plugins` | 查看已加载的插件 |
| debug | `/debug` | 查看所有调试子命令 |
| debug | `/debug event` | 查看当前事件详情 |
| debug | `/debug ctx` | 查看上下文信息 |
| debug | `/debug commands` | 查看所有注册的命令 |
| debug | `/debug plugins` | 查看插件状态 |
| debug | `/debug runtime` | 查看运行时信息 |
| permission | `/acl list <用户>` | 查看用户权限 |

### 示例命令

| 命令 | 说明 |
|------|------|
| `/ping` | 测试 Bot 是否在线 |
| `/echo <消息>` | 回显输入的消息 |
| `/info` | 查看当前事件信息 |
| `/caps` | 查看平台能力声明 |

## 运行

```bash
go run ./examples/terminal-bot
```

启动后在终端直接输入命令即可交互：

```
=== 终端 Bot 控制台 ===
输入命令与 Bot 交互，输入 quit 或 exit 退出。

User> /ping
[Bot Reply] Pong! 🏓
User> /help
[Bot Reply] 可用命令:
  /ping         - 测试 Bot 是否在线
  /echo <消息>  - 回显输入的消息
  /info         - 查看当前事件信息
  /caps         - 查看平台能力声明
  ...
User> /debug plugins
[Bot Reply] 插件状态: ...
User> quit
```

## 代码结构

```
examples/terminal-bot/
├── main.go   — 入口，创建 Engine + 加载插件 + 启动终端适配器
└── README.md
```

### 核心流程

1. 创建 `engine.Engine` 并挂载中间件
2. 创建 `command.NewCommandRegistry()` 并注入引擎（启用命令补全）
3. 创建 `plugin.Manager` 加载插件（permission → debug → help）
4. 注册示例命令
5. 创建 `terminal.NewAdapter()`，传入 `WithCompletionFunc(fn)` 绑定命令补全
6. 通过 `ProcessPlatformEventEx` 连接 adapter 与 engine
7. 启动输入循环等待用户输入

### 终端特性

- **上下方向键** — 浏览历史命令（默认保存最近 100 条）
- **Tab 补全** — 输入 `/` 后按 Tab 查看所有可用命令，输入部分命令名后按 Tab 自动补全
- **行编辑** — 退格、光标移动等标准终端编辑功能
- **原始模式** — 自动切换 stdin 为 raw mode，退出时恢复

### 命令补全

```go
reg := command.NewCommandRegistry()
eng.SetCommandRegistry(reg)

adapter := terminal.NewAdapter(
    terminal.WithCompletionFunc(func(prefix string) []string {
        if !strings.HasPrefix(prefix, "/") {
            return nil
        }
        metas := reg.Complete(prefix)
        names := make([]string, len(metas))
        for i, m := range metas {
            names[i] = m.Name
        }
        return names
    }),
)
```

### 关键点

- `adapter.Sender()` 返回 adapter 自身（实现了 `platform.Sender`）
- `ProcessPlatformEventEx` 自动注入 botID，使 `ctx.IsFromSelf()` 生效
- 插件系统自动处理依赖顺序：permission 先加载，debug 后加载

### 支持的插件接口

终端适配器实现了 14 个接口，所有通过 `platform.Adapter.Sender()` 类型断言获取
的可选接口均可用，详情见 [platform/terminal](../../platform/terminal/README.md)。
