# Remilia 架构升级 TODO

## 概览

四个特性按依赖顺序排列：

```
Phase 1: State Machine      ✅ 已完成 (core/fsm/)
Phase 2: Adaptive Router    ✅ 已完成 (router/)
Phase 3: WASM Plugin        ✅ 已完成 (plugin/wasm/)
Phase 4: Per-Channel Engine ✅ 已完成 (core/engine/)
```

---

## Phase 1: State Machine

### 目标

为复杂多步骤对话（抢购、表单、问卷调查）提供声明式 FSM，替代 handler 内部手写 `if state == x`。

### 新增文件 ✅

```
plugin/fsm/
├── fsm.go              — FSM 引擎核心 (388 行)
├── fsm_test.go         — 核心测试 (24 tests)
├── descriptor.go       — FSMDescriptor 定义
├── context.go          — FSMContext（handler 拿到的上下文）
├── manager.go          — FSM 实例管理器（生命周期）
├── storage.go          — Storage 接口 + MemoryStorage 实现
└── examples_test.go    — 使用示例 (3 步表单 Go Example)
```

---

## Phase 2: Adaptive Router

### 目标

在 Engine 之上加一层策略路由层，将事件分流到 Engine / FSM / LLM Agent，互不干扰。

### 新增文件 ✅

```
router/
├── router.go           — Router 核心 + Dispatch + FSM 处理 (139 行)
├── router_test.go      — 12 tests
└── options.go          — WithCommandPrefix/WithFSMRoute/WithCustom (61 行)
```

engine_handler、fsm_handler、agent_handler 未独立成文件——Handler 逻辑内联在 Dispatch 中（~200 行）。

### 修改文件 ✅

```
bot.go                  — 加 router 字段、UseRouter()、handlePlatformEvent 路由分流 (bot.go:337-348)
bot_builder.go          — 暂未改动（Router 通过 bot.UseRouter(r) 注入）
```

### 实现步骤

#### Step 2.1 — 定义 Router 核心 (`router.go`)

```go
package router

type Strategy int

const (
    StrategyEngine Strategy = iota  // 默认，走 Engine
    StrategyFSM                     // 走状态机
    StrategyAgent                   // 走 LLM Agent（可选）
)

// RouteRule 路由规则
type RouteRule struct {
    Name     string
    Strategy Strategy
    Match    func(ctx *corectx.Context) bool  // 匹配条件
    Priority int                              // 匹配优先级
}

type Router struct {
    engine    *engine.Engine
    fsmEngine *fsm.Engine
    rules     []*RouteRule
}

func New(e *engine.Engine, fsm *fsm.Engine) *Router

// Route 路由单条规则
func (r *Router) Route(rule *RouteRule)

// Dispatch 分发事件到对应的策略
func (r *Router) Dispatch(ctx *corectx.Context) {
    for _, rule := range r.rules {
        if rule.Match(ctx) {
            switch rule.Strategy {
            case StrategyEngine:
                r.engine.ProcessEvent(ctx)
            case StrategyFSM:
                sessionID := ctx.GetChatID()
                transitioned, _ := r.fsmEngine.TryTransition(ctx, sessionID)
                if !transitioned {
                    // FSM 未命中，fallback 到下一规则
                    continue
                }
            case StrategyAgent:
                r.handleAgent(ctx)
            }
            return
        }
    }
    // 没有任何规则命中 → fallback 到 Engine（保证现有行为不变）
    r.engine.ProcessEvent(ctx)
}
```

#### Step 2.2 — 实现各 Handler

`engine_handler.go` — 直接调 `Engine.ProcessEvent()`，约 20 行

`fsm_handler.go` — 调 `fsm.Engine.TryTransition()`，如果没命中就 fallback，约 30 行

`agent_handler.go` — LLM Agent 处理（可选），约 200 行

#### Step 2.3 — 预置路由规则 (`options.go`)

```go
// WithFSMRoute 创建一条 FSM 路由规则
func WithFSMRoute(name string, match func(ctx *corectx.Context) bool) RouteRule {
    return RouteRule{
        Name:     name,
        Strategy: StrategyFSM,
        Match:    match,
    }
}

// WithCommandPrefix 将 / 开头的消息路由到 Engine（默认规则）
//
// 使用 [context.SplitCommandPattern] 判断首个 token 是否包含非字母数字前缀：
//   - "/help"   → prefix="/", name="help"   → 匹配
//   - "!!admin" → prefix="!!", name="admin" → 匹配
//   - "hello"   → prefix="", name="hello"   → 不匹配
//   - "帮助"     → prefix="", name="帮助"     → 不匹配
//
// 提取首个 token 参考 [engine.extractCommand]（取首个空白前的单词）。
func WithCommandPrefix() RouteRule {
    return RouteRule{
        Name:     "command_prefix",
        Strategy: StrategyEngine,
        Match: func(ctx *corectx.Context) bool {
            content := ctx.GetMessageContent()
            // 取首个 token（同 engine.extractCommand）
            trimmed := strings.TrimSpace(content)
            idx := strings.IndexFunc(trimmed, unicode.IsSpace)
            var firstWord string
            if idx == -1 {
                firstWord = trimmed
            } else {
                firstWord = trimmed[:idx]
            }
            if firstWord == "" {
                return false
            }
            // 用 SplitCommandPattern 判断是否有命令前缀
            prefix, _ := context.SplitCommandPattern(firstWord)
            return prefix != ""
        },
    }
}
```

#### Step 2.4 — 集成到 Bot (`bot.go`)

```go
// bot.go

type Bot struct {
    // ... 原有字段 ...

    // 新增
    router *router.Router
}

// 构造时传入 Router（可选，没有则走原有路径）
func (b *Bot) UseRouter(r *router.Router) *Bot {
    b.router = r
    return b
}

// handlePlatformEvent 改道
func (b *Bot) handlePlatformEvent(event platform.Event) {
    // ... 原有解析 sender, caps, botID 不变 ...

    ctx := context.NewContextFromEvent(event, sender)
    // ... 注入 caps, botID ...

    if b.router != nil {
        b.router.Dispatch(ctx)  // 走 Router
    } else {
        b.engine.ProcessEvent(ctx)  // 兼容旧路径
    }
}
```

#### Step 2.5 — 默认路由规则

```go
// BotBuilder 中自动注入默认规则：
func (bb *BotBuilder) Build() (*Bot, error) {
    // ... 原有构建逻辑 ...

    if bb.fsmEnabled {
        rules := []router.RouteRule{
            router.WithCommandPrefix(),          // /help → Engine
            router.WithFSMRoute("fsm",           // 其他消息 → FSM
                func(ctx *corectx.Context) bool { return true }),
        }
        r := router.New(bb.engine, bb.fsmEngine)
        for _, rule := range rules {
            r.Route(rule)
        }
        bot.UseRouter(r)
    }

    return bot, nil
}
```

**注意**：`WithFSMRoute` 规则放在最后作为 fallback。`TryTransition` 内部会先判断会话是否存在，不存在时 `continue` 到 Engine。

#### Step 2.6 — 测试

```go
// router_test.go
func TestRouter_EnginePath
func TestRouter_FSMHitFallthroughToEngine
func TestRouter_FSMNoSessionFallthroughToEngine
func TestRouter_PriorityOrder
func TestRouter_MultipleRules
func TestRouter_NoRulesFallbackToEngine
```

---

## Phase 3: WASM Plugin ✅

### 目标

第三方插件以 `.wasm` 模块运行在沙箱中，崩溃不拖垮主进程，安全隔离。

### 新增文件 ✅

```
plugin/wasm/
├── abi.go              — ABI 常量 & 结果编解码
├── runtime.go          — wazero Runtime 封装（NewRuntime/LoadModule/CallInit/CallHandle）
├── host.go             — HostFuncRegistry + RegisterDefaultHostFuncs
├── bridge.go           — Bridge: WASM Module → Engine Matcher
├── descriptor.go       — WASMDescriptor + ResourceLimit
├── sandbox.go          — TokenBucket 限流 + Sandbox
├── manager.go          — Manager: WASM 插件生命周期管理
└── wasm_test.go        — 12 tests (descriptor, host, sandbox, bridge, encode/decode)
```

### 修改文件 ✅

```
go.mod                  — + github.com/tetratelabs/wazero v1.11.0
```

plugin/manager.go 和 plugin/descriptor.go 未修改——WASM 通过独立的 [wasm.Manager] 管理生命周期。

### 实现步骤

#### Step 3.1 — 定义 WASM ABI

先在 `plugin/wasm/abi.go` 中定义 Host 和 Plugin 之间的接口约定：

```
Plugin Exports（插件导出，Host 调用）:
  plugin_init()              → i32 (0=成功, 非0=错误)
  plugin_handle(event_ptr, event_len)  → response_ptr, response_len

Host Imports（Host 导出，插件调用）:
  remilia:host/
    log(level, msg_ptr, msg_len)
    register_command(json_ptr, json_len)  → handler_id (i32)
    register_matcher(json_ptr, json_len)  → handler_id (i32)
    send_message(json_ptr, json_len)      → i32
    get_config(key_ptr, key_len)          → value_ptr, value_len
    storage_get(key_ptr, key_len)         → value_ptr, value_len
    storage_set(key_ptr, key_len, val_ptr, val_len) → i32
    http_request(json_ptr, json_len)      → resp_ptr, resp_len
    call_host_func(name_ptr, name_len, args_ptr, args_len) → result_ptr, result_len
```

数据通过 WASM 线性内存传递，JSON 序列化。

#### Step 3.2 — WASM Runtime 封装 (`runtime.go`)

```go
type Runtime struct {
    wazeroRuntime  wazero.Runtime
    hostFunctions  *HostFuncRegistry
    resourceLimit  ResourceLimit
}

func NewRuntime(opts ...RuntimeOption) (*Runtime, error)

// LoadModule 加载 .wasm 文件
func (r *Runtime) LoadModule(path string) (*Module, error)

// Module 表示一个已加载的 WASM 模块实例
type Module struct {
    compiled wazero.CompiledModule
    instance wazero.Module
    name     string
    callCount atomic.Int64
    createdAt  time.Time
}

func (m *Module) CallInit() error
func (m *Module) CallHandle(event []byte) ([]byte, error)
func (m *Module) Close() error
```

#### Step 3.3 — Host Function 注册表 (`host.go`)

```go
type HostFuncRegistry struct {
    funcs map[string]func(args json.RawMessage) json.RawMessage
}

func NewHostFuncRegistry() *HostFuncRegistry

// Register 注册一个 Host Function（由原生插件调用）
func (r *HostFuncRegistry) Register(name string, fn func(json.RawMessage) json.RawMessage)

func (r *HostFuncRegistry) BuildImports(ctx context.Context, rt wazero.Runtime) api.Module
```

#### Step 3.4 — WASMDescriptor (`descriptor.go`)

```go
type WASMDescriptor struct {
    Name       string
    Version    string
    Path       string                // .wasm 文件路径
    Config     map[string]any
    ResourceLimit *ResourceLimit     // 可选
    Permissions []string             // ["http", "storage"]
}

type ResourceLimit struct {
    MemoryPages  uint32  // 默认 2 (128KB)
    CPUMillicores uint64 // 默认 100 (10%)
    MaxCallPerSec int64  // 默认 1000
}
```

#### Step 3.5 — Bridge (`bridge.go`)

WASM 插件在 `plugin_init` 中通过 `host_register_command` 注册 Matcher。Bridge 负责在 Host 端创建对应的 `engine.Matcher`：

```go
type Bridge struct {
    wasmModule   *Module
    engine       engine.MatcherWriter
}

// BuildMatcher 为 WASM 插件的注册请求创建 Engine Matcher
func (b *Bridge) BuildMatcher(regReq RegistrationRequest) *engine.Matcher {
    matcher := b.engine.OnCommand(regReq.EventType, regReq.Command)
    matcher.Handle(func(ctx *corectx.Context) {
        // 序列化事件 → 调用 WASM → 发回复
        eventJSON := marshalEvent(ctx)
        respJSON, err := b.wasmModule.CallHandle(eventJSON)
        if err != nil {
            ctx.Reply("插件执行错误")
            return
        }
        resp := unmarshalResponse(respJSON)
        if resp.Reply != "" {
            ctx.Reply(resp.Reply)
        }
    })
    matcher.Block(false)  // WASM 插件不阻塞其他插件
    return matcher
}
```

#### Step 3.6 — 集成到 Manager (`plugin/manager.go`)

```go
func (pm *Manager) RegisterWASM(desc *WASMDescriptor) error {
    // 1. 加载 WASM 模块
    module, err := pm.wasmRuntime.LoadModule(desc.Path)
    // 2. 注册 Host Functions
    runtime.RegisterHostFuncs(pm.hostFuncs)
    // 3. 调用 plugin_init()
    err = module.CallInit()
    // 4. 记录 WASM 实例
    pm.wasmPlugins[desc.Name] = &wasmInstance{module: module, desc: desc}
    return nil
}

func (pm *Manager) UnregisterWASM(name string) error {
    inst := pm.wasmPlugins[name]
    // 1. 删除该 WASM 插件注册的所有 Matcher
    // 2. 关闭 WASM 模块
    inst.module.Close()
    // 3. 从 map 中移除
    delete(pm.wasmPlugins, name)
    return nil
}
```

#### Step 3.7 — 资源沙箱 (`sandbox.go`)

```go
type ResourceLimiter struct {
    memoryLimit   uint32
    cpuQuota      uint64
    callLimiter   *rate.Limiter
}

// 通过 wazero 的 api.WithMemoryLimitConfig 限制内存
// 通过 rate.Limiter 限制调用频率
// CPU 时间通过 wazero 的编译配置做 gas meter（可选）
```

#### Step 3.8 — 测试

```go
// 测试策略：
// 1. 用 TinyGo 编译一个简单的 test plugin 到 .wasm
// 2. 集成测试 RegisterWASM → CallInit → Dispatch Event → CallHandle
// 3. 测试资源限制：内存超标、频率超标
// 4. 测试崩溃隔离：插件 panic → 主进程不受影响
// 5. 测试 Host Function：HTTP、Storage
```

---

## Phase 4: Per-Channel Engine

### 目标

每个群/用户拥有独立的 Engine 实例，matcher 列表隔离，Block() 互不影响，慢 handler 不互相拖累。

### 新增文件 ✅

```
core/engine/
├── fork.go              — ForkFrom / syncTemplates / Version / bumpVersion / touch / IsFork
├── manager.go           — EngineManager（Dispatch / GetChannel / Stats / StartGC / Close）
├── manager_test.go      — 6 tests
└── channel.go           — ChannelKey / MakeChannelKey
```

### 修改文件 ✅

```
bot.go                          — engineManager 字段 + UseEngineManager + handlePlatformEvent 三路路由
core/engine/engine.go           — templateVer + fork 字段（写操作5个 bumpVersion 埋点）
```

### 实现步骤

#### Step 4.1 — ChannelKey 类型 (`plugin/channel.go`)

```go
type ChannelKey string

func MakeChannelKey(platform, chatID string) ChannelKey {
    return ChannelKey(platform + ":" + chatID)
}

type ChannelInfo struct {
    Platform string
    ChatID   string
    BotID    string
    // 可选：该 channel 的配置（已启用插件列表等）
}
```

#### Step 4.2 — Engine 增加 Fork 能力 (`core/engine/fork.go`)

```go
// fork.go

// forkState 记录 fork 来源信息
type forkState struct {
    template      *Engine
    templateVer   int64
    channelKey    ChannelKey
}

// ForkOption fork 配置
type ForkOption func(*Engine)

// ForkFrom 从模板引擎创建一个新的 channel engine
// 返回的 Engine 初始为空，syncTemplates 时从模板复制 matcher
func (e *Engine) ForkFrom(template *Engine, channelKey ChannelKey, opts ...ForkOption) {
    e.fork = &forkState{
        template:    template,
        templateVer: template.Version(),
        channelKey:  channelKey,
    }
    e.syncTemplates()
}

// syncTemplates 从模板同步所有 matcher
// 只同步 Scope 匹配该 channel 的 matcher（或全局 matcher）
func (e *Engine) syncTemplates() {
    if e.fork == nil {
        return
    }

    tmplState := e.fork.template.state.Load()
    childState := e.state.Load()

    // 遍历模板的 matcher
    for _, m := range tmplState.matchers {
        if e.isMatcherForChannel(m) {
            // 克隆 matcher 到本地 state
            childState.withAddedMatcher(m.Clone())
        }
    }

    // 更新模板版本
    e.fork.templateVer = e.fork.template.Version()
}
```

#### Step 4.3 — 版本号比较 (`core/engine/engine.go`)

```go
// engine.go — 新增

// templateVer atomic 版本号（整个 Engine 的 matcher 版本）
type Engine struct {
    // ... 原有字段 ...
    templateVer atomic.Int64  // 新增：用于 fork 懒同步
    fork        *forkState    // 新增：nil = 全局 Engine
}

// Version 返回当前模板版本号
func (e *Engine) Version() int64 {
    return e.templateVer.Load()
}

// bumpVersion 模板版本号 +1（每次 Matcher 增删改后调用）
func (e *Engine) bumpVersion() {
    e.templateVer.Add(1)
}
```

在 `withAddedMatcher` / `withRemovedGroup` / `withDeletedMatcher` 等写操作完成后调用 `bumpVersion()`。

#### Step 4.4 — EngineManager (`core/engine/manager.go`)

```go
type EngineManager struct {
    template  *Engine          // 全局模板引擎
    instances sync.Map         // map[ChannelKey]*Engine
    maxIdle   time.Duration    // 空闲淘汰时间
    stopCh    chan struct{}
}

func NewEngineManager(template *Engine, opts ...ManagerOption) *EngineManager

// Dispatch 分发事件到对应 channel 的 Engine
func (em *EngineManager) Dispatch(ctx *corectx.Context, event platform.Event) {
    channelKey := MakeChannelKey(event.Platform(), event.GetChatID())

    actual, loaded := em.instances.LoadOrStore(channelKey, em.newChannelEngine(channelKey))
    chEngine := actual.(*Engine)

    // 懒同步：检查模板版本是否变化
    if chEngine.Version() < em.template.Version() {
        chEngine.syncTemplates()
    }

    chEngine.ProcessEvent(ctx)
}

// newChannelEngine 从模板创建新 Engine
func (em *EngineManager) newChannelEngine(key ChannelKey) *Engine {
    child := NewEngine()
    child.ForkFrom(em.template, key)
    return child
}

// GC 清理空闲 Engine（后台 goroutine）
func (em *EngineManager) startGC(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    for {
        select {
        case <-ticker.C:
            em.instances.Range(func(key, value any) bool {
                chEngine := value.(*Engine)
                if time.Since(chEngine.lastEventAt) > em.maxIdle {
                    em.instances.Delete(key)
                }
                return true
            })
        case <-ctx.Done():
            return
        }
    }
}
```

#### Step 4.5 — 集成到 Bot (`bot.go`)

```go
// bot.go

type Bot struct {
    // ... 原有字段 ...
    engineManager *engine.EngineManager  // 新增
}

// UseEngineManager 注入 EngineManager
func (b *Bot) UseEngineManager(em *engine.EngineManager) *Bot {
    b.engineManager = em
    return b
}

// handlePlatformEvent 改道
func (b *Bot) handlePlatformEvent(event platform.Event) {
    // ... 原有解析 sender, caps, botID 不变 ...

    ctx := context.NewContextFromEvent(event, sender)
    // ... 注入 caps, botID ...

    if b.engineManager != nil {
        b.engineManager.Dispatch(ctx, event)
    } else if b.router != nil {
        b.router.Dispatch(ctx)
    } else {
        b.engine.ProcessEvent(ctx)
    }
}
```

**分层优先级**：`engineManager > router > engine`
- 有 EngineManager → 走 per-channel 隔离
- 没有 EngineManager 但有 Router → 走策略路由
- 只有 Engine → 兼容旧路径

#### Step 4.6 — 插件注册传播

在 `plugin/manager.go` 中，注册新插件时更新模板版本：

```go
func (pm *Manager) Register(desc *Descriptor) error {
    // ... 原有逻辑 ...

    // 所有 Matcher 注册完成后，递增模板版本号
    pm.coordinator.(*engine.Engine).bumpVersion()

    // 注：per-channel engine 会在下次事件时懒同步
}
```

#### Step 4.7 — 测试

```go
// manager_test.go
func TestEngineManager_CreateOnFirstEvent
func TestEngineManager_Isolation_BlockNotLeaking
func TestEngineManager_SyncOnTemplateChange
func TestEngineManager_LazyGC
func TestEngineManager_MemoryLimit
```

---

## 构建顺序与依赖图

```
State Machine (.fsm)     WASM Plugin (.wasm)
       │                        │
       └─────────┬──────────────┘
                 ▼
         Adaptive Router
                 │
                 ▼
       Per-Channel Engine
```

| 阶段 | 特性 | 预估时长 | 依赖 |
|------|------|---------|------|
| **1** | State Machine | 1 周 | 无 |
| **2** | WASM Plugin | 2 周 | 无 |
| **3** | Adaptive Router | 3 天 | Phase 1 |
| **4** | Per-Channel Engine | 2-3 周 | Phase 3 |

**建议**：Phase 1 和 Phase 2 可以两个人并行；Phase 3 一个人做；Phase 4 一个人做。

---

## 每个阶段的验收标准

### Phase 1 — State Machine ✅

- [x] `fsm.Engine.TryTransition` 能正确匹配 Event 并迁移状态
- [x] `OnEnter` / `OnExit` 回调正确触发
- [x] `FSMContext` 嵌入 `*corectx.Context`，`Reply()` 可用
- [x] `MemoryStorage` 的 Get/Save/Delete/Cleanup 通过测试
- [x] 示例：用 FSM 实现一个"输入姓名→输入年龄→确认"的 3 步表单 (examples_test.go)
- [x] 超时后自动重置状态
- [x] 并发安全（100 goroutine 同时操作不同 session）

### Phase 2 — Adaptive Router ✅

- [x] `/` 开头消息 → Engine（使用 `extractCommand` + `SplitCommandPattern` 判断——不是简单 `strings.HasPrefix("/")`）
- [x] 有 FSM 会话的消息 → FSM
- [x] FSM 未命中 → fallback 到 Engine
- [x] 无 Router 时完全兼容旧路径（`handlePlatformEvent` 中的 `if b.router != nil` 守卫）
- [x] 通过 `bot.UseRouter(r)` 注入
- [x] 测试覆盖三种分支（12 tests: Engine/FSM/Fallback/Priority/Custom/Nil/Chinese）

### Phase 3 — WASM Plugin ✅

- [x] `wasm.Runtime.NewRuntime` + `LoadModule` + `CallInit` + `CallHandle` (runtime.go)
- [x] `wasm.HostFuncRegistry` 宿主函数注册表 (host.go)
- [x] `wasm.Bridge` 将 WASM 注册请求转换为 Engine Matcher (bridge.go)
- [x] Host Function: `remilia_host_log`, `remilia_host_get_config` (host.go)
- [x] `wasm.Sandbox` + `TokenBucket` 令牌桶限流 (sandbox.go)
- [x] `wasm.WASMDescriptor` + `ResourceLimit` (descriptor.go)
- [x] `wasm.Manager` 独立生命周期管理 (manager.go, 不修改 plugin/manager.go)
- [x] wazero v1.11.0 依赖 (go.mod)
- [x] ABI 常量定义 + encode/decode 结果编解码 (abi.go)
- [x] 12 tests (decscriptor, host, sandbox, bridge, encode/decode)

### Phase 4 — Per-Channel Engine ✅

- [x] 群 A 的消息只进入群 A 的 Engine（EngineManager.Dispatch → MakeChannelKey → LoadOrStore → ProcessEvent）
- [x] 群 A 的慢 handler 不阻塞群 B（每个 channel 独立的 ProcessEvent + ExecPool）
- [x] 插件注册后，已有 channel 的 Engine 在下次事件时自动同步（Version + processEventGuard 懒检查）
- [x] 空闲 Engine 被 GC 回收（StartGC / evictIdle / maxIdle）
- [x] 通过 `bot.UseEngineManager(em)` 注入（优先于 router）
- [x] ForkFrom / syncTemplates / Version / bumpVersion / touch / IsFork
- [x] 6 个单元测试（创建/隔离/多channel/同步/fork/stats）
