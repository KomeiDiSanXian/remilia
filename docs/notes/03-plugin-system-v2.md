# 插件系统 v2——函数式、依赖注入、蓝绿部署

> **ZeroBot 基因**：ZeroBot 的"插件"通过 `init()` + `StoreMatcher()` 全局注册实现，没有生命周期、依赖注入或热重载。Remilia v1 继承此模式，v2 才彻底重写为 Descriptor 模式。参阅 [`11-zerobot-inspiration.md`](11-zerobot-inspiration.md#35-关键分叉点-④插件系统)。

## 设计哲学

v1 插件系统采用经典的面相对象继承模式：`BasePlugin` 基类 + 子类覆写生命周期方法。这种方式虽然直观，但随着功能增长暴露出一系列问题：

| 问题 | 影响 |
|------|------|
| 继承强耦合 | 插件与框架硬绑定，难以独立测试 |
| 样板代码多 | 每个插件都要重复写 `BasePlugin` 结构 |
| 依赖管理隐式 | `Deps` 字段手动声明，容易遗漏或弄错 |
| 权限模糊 | 插件持有 Engine 引用，可做任何写操作 |
| 热重载不灵活 | 只有"卸载-加载"一种策略 |

v2 弃用继承，采用**纯函数式描述符（Descriptor）**：

```go
type Descriptor struct {
    Name        string
    Version     string
    Meta        *Metadata
    Deps        []string      // 手动声明依赖（Smart 注册时可省略）
    Setup       func(*SetupContext) (any, error)
    Teardown    func(*TeardownContext) error
    Advanced    *Advanced      // 可选高级功能
}
```

## 核心设计

### 1. 描述符模式（Descriptor Pattern）

插件开发者不再继承框架类型，而是返回一个描述插件的"数据对象"：

```go
func New() *plugin.Descriptor {
    return &plugin.Descriptor{
        Name:    "myplugin",
        Version: "1.0.0",
        Meta: &plugin.Metadata{
            Description: "我的插件",
            Category:    "工具",
        },
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            ctx.Reg.RegisterCommand(...)
            return &MyAPI{}, nil
        },
        Teardown: func(ctx *plugin.TeardownContext) error {
            ctx.API.(*MyAPI).Close()
            return nil
        },
    }
}
```

优势：
- 插件是纯函数，易于测试（传入 mock 的 `SetupContext`）
- 无强制类型约束，API 返回类型由插件自定
- 描述符可序列化，支持插件商店的元数据发现

### 2. 自动依赖注入

依赖注入容器 `Container` 采用两阶段设计：

**阶段 1：注册阶段（可变）**

```go
type Container struct {
    services sync.Map  // 并发安全的读写
}
```

**阶段 2：冻结阶段（只读快照）**

```go
type Container struct {
    frozen    atomic.Bool
    frozenMap atomic.Pointer[map[string]any]
}

func (c *Container) Freeze() {
    c.frozen.Store(true)
    c.refreshSnapshot()
}

func (c *Container) Get(name string) (any, bool) {
    if c.frozen.Load() {
        if m := c.frozenMap.Load(); m != nil {
            v, ok := (*m)[name]
            return v, ok
        }
    }
    return c.services.Load(name)
}
```

冻结后 `Get` 仅需一次 `atomic.Load`，读性能提升 **2-3 倍**。冻结后仍可注册新服务（热重载场景），会自动刷新快照。

**Smart 注册**（v2 核心改进）：

```go
// DryRun 阶段自动推断依赖图
func (m *Manager) resolveDeps(desc *Descriptor) ([]string, error) {
    if len(desc.Deps) > 0 {
        return desc.Deps, nil  // 显式声明优先
    }
    // 模拟运行 Setup，追踪 ctx.Require / ctx.Optional 调用
    deps := dryRunResolve(desc)
    return deps, nil
}
```

插件不需要手写 `Deps`——框架在注册时通过 DryRun 自动发现依赖。

### 3. 读写分离权限模型

```go
// SetupContext 只读视图（插件开发者的主要 API）
type SetupContext struct {
    Reg     RegistryWriter  // 注册匹配器
    Config  Config          // 插件配置
    Logger  Logger
    Go      func(fn func()) // 启动生命周期绑定的 goroutine
    Require func(name string) any
    Optional func(name string) (any, bool)
    MustAs  func(name string, target any)
    Container *Container    // 注入 API 到容器
}

// ManagerWriter 写权限（需要 Privileged: true）
type ManagerWriter interface {
    DisablePlugin(name string) error
    EnablePlugin(name string) error
    ReloadPlugin(name string) error
}
```

设计要点：
- 插件 `Setup` 只能获取 `SetupContext`，其中 `Reg` 接口仅暴露注册命令/匹配器的写操作
- `ManagerWriter` 需要插件声明 `Privileged: true` 才能获取
- `PluginInfo` 是全只读的——任何人都可以查询插件状态，但不能修改

### 4. 三种热重载策略

```go
const (
    ReloadUnloadLoad ReloadStrategy = iota  // 停机重载（默认）
    ReloadInPlace                            // 原地重载
    ReloadBlueGreen                          // 蓝绿部署
)
```

#### ReloadUnloadLoad（停机重载）

1. 调用 `Teardown` 清理旧实例
2. 从 Engine 移除旧匹配器
3. 加载新配置
4. 调用 `Setup` 创建新实例
5. 注册新匹配器

**存在短暂不可用窗口**，但支持完整状态迁移（`SaveState` / `RestoreState`）。

#### ReloadInPlace（原地重载）

```go
Advanced: &plugin.Advanced{
    Strategy: ReloadInPlace,
    Reload: func(ctx *ReloadContext) error {
        // 自行处理状态更新，无需卸载
        ctx.API.(*MyPlugin).UpdateConfig(newConfig)
        return nil
    },
}
```

适用于配置更新、规则热加载等场景。

#### ReloadBlueGreen（蓝绿部署——v2 亮点）

```
旧实例运行中 → 新实例 Setup → 原子切换 Matcher → 旧实例 Teardown
```

```
时间线：
旧 Matcher ████████████████████████████████████░░░░░░░░
新 Matcher ░░░░░░░░░░░░░░░░░░░░░░░░░░██████████████████
                   ↑ 原子切换点（零停机）
```

**零停机切换**：切换过程中新旧 Matcher 共存，新实例就绪后一次性接管，旧实例清理。

实现要点：
- 新实例在并行 goroutine 中执行 `Setup`
- 切换前冻结新实例的状态，确保一致性
- 切换使用 `atomic.Store` 替换 Engine 中的匹配器引用
- 旧实例的 `Teardown` 在新实例完全就绪后才执行

### 5. 内置插件生态

框架内置了 25+ 个业务插件，覆盖机器人场景的常见需求：

| 类别 | 插件 | 功能 |
|------|------|------|
| **核心** | `core/help` | 命令自动发现 + 图片生成 |
| **核心** | `core/admin` | 管理员指令 |
| **核心** | `core/permission` | 细粒度权限（持久化 + ACL） |
| **安全** | `antispam` | 反垃圾 |
| **安全** | `keywordfilter` | 敏感词过滤 |
| **安全** | `acl` | 访问控制列表 |
| **运营** | `broadcast` | 广播消息 |
| **运营** | `scheduler` | 定时任务调度 |
| **运营** | `job` | 一次性/周期性任务 |
| **消息** | `sendqueue` | 发送队列（退避重试） |
| **消息** | `messagelog` | 消息日志持久化 |
| **消息** | `conversation` | 对话管理（含 GC） |
| **配置** | `cooldown` | 冷却系统 |
| **配置** | `i18n` | 国际化 |
| **治理** | `pluginctrl` | 插件启停管理 |
| **治理** | `pluginstore` | 插件商店 |
| **治理** | `ratelimitui` | 限流可视化 |
| **工具** | `calendar` | 日历 |
| **工具** | `idiomdict` | 成语词典 |
| **工具** | `verifycode` | 验证码生成 |
| **工具** | `vevent` | 虚拟事件扩展 |
| **其他** | `subscription` | 订阅管理 |
| **其他** | `bundle` | 资源包管理 |
| **其他** | `auditlog` | 审计日志 |
| **其他** | `stats` | 统计 |
| **存储** | `storage` | 存储抽象 |

### 6. 生命周期与 EventBus

插件管理器内部使用事件总线（EventBus）在插件之间广播生命周期事件：

```go
// EventBus 插件事件总线（泛型实现）
type EventBus struct {
    listeners sync.Map  // map[string][]Handler
}

// 插件监听其他插件的事件
func (p *Plugin) OnEvent(event any) {
    // 通过 Container 获取 EventBus
    bus := ctx.Require("eventbus").(*EventBus)
    bus.Subscribe("user.login", func(e any) {
        // 处理登录事件
    })
}
```

EventBus 支持泛型类型安全的事件处理，与 `lifecycle` 包的生命周期事件解耦。

### 7. 与生命周期管理器的集成

插件的生命周期由 `Manager` 统一管理，通过 `ManagerComponent` 适配到 `lifecycle.Manager`：

```go
func NewManagerComponent(pm *Manager) lifecycle.Component {
    return lifecycle.NewSimpleComponent(
        "plugin-manager",
        nil,                                    // OnStart
        func(ctx context.Context) error {       // OnRun
            <-ctx.Done()
            return nil
        },
        func(ctx context.Context) error {       // OnStop
            return pm.StopAll(ctx)
        },
    )
}
```

停止顺序：插件管理器 → 平台适配器 → Engine（插件 `Teardown` 在平台断开**之前**执行，确保插件仍能使用平台 API 做最后的清理）。

## 迭代过程

### V1：继承模式（BasePlugin + Plugin 接口）

最初的插件系统采用经典的 OOP 继承模式：

```go
// V1 代码 — 继承模式（根包 plugin.go）
type Plugin interface {
    Name() string
    Load(engine *Engine) error
    Unload(engine *Engine) error
    Reload(engine *Engine) error
    Dependencies() []string
}

type BasePlugin struct {
    name     string
    matchers []*Matcher
    mu       sync.RWMutex
}

func NewBasePlugin(name string) *BasePlugin {
    return &BasePlugin{name: name}
}

// 用户插件
type MyPlugin struct {
    *BasePlugin
    api *MyAPI
}

func (p *MyPlugin) Load(engine *Engine) error {
    p.BasePlugin.name = "myplugin"
    // 直接操作 engine，权限过大
    engine.OnCommand(...)
    return nil
}

func (p *MyPlugin) Dependencies() []string {
    return []string{"storage"}  // 手写依赖，容易遗漏
}
```

**问题清单**：

| 问题 | 具体表现 | 后果 |
|------|---------|------|
| 框架强耦合 | 插件导入 `*Engine`，依赖整个框架 | 无法独立编译、测试 |
| 样板代码 | 每个插件必须写 `BasePlugin` 嵌入 + 4 个方法 | 开发效率低 |
| 隐式依赖 | `Dependencies()` 手写字符串切片 | 依赖变更时忘记同步 |
| 权限过大 | `Load(engine *Engine)` 持有完整引擎引用 | 插件可以删除其他插件的匹配器 |
| 测试困难 | 必须构造完整的 `*Engine` 实例 | 单元测试成本高 |
| 热重载单一 | 只有 unload→load 一种策略 | 更新配置也需要停机 |

此外，插件都在根包 `plugin.go` 管理，随着内置插件增多（后来演变为 `builtin/` 下 25+ 个包），单个文件难以维护。

### V2 过渡：从继承到组合

在正式引入 v2 之前，框架经历了一个过渡阶段——引入了 `PluginCoordinator` 接口来缩小插件的框架视图：

```go
// 过渡阶段 — PluginCoordinator 接口
type PluginCoordinator interface {
    // 只暴露必要的操作
    RegisterCommand(eventType string, cmd string, handler Handler) *Matcher
    GetPluginInfo(name string) PluginInfo
    // ...
}
```

但 `Dependencies()` 手写的问题仍然存在，插件仍然需要继承。这个阶段的一个重要经验是：**接口隔离不能完全解决继承模式的问题**，需要根本性的设计转变。

### V3（当前）：函数式 Descriptor + DryRun 依赖注入

v2 的正式版本完全弃用继承：

```go
// V3 — 函数式 Descriptor
func New() *plugin.Descriptor {
    return &plugin.Descriptor{
        Name:    "myplugin",
        Version: "1.0.0",
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            // ctx.Reg 是受限的 RegistryWriter（不是整个 Engine）
            // ctx.Require 自动追踪依赖
            storage := ctx.Require("storage").(Storage)
            ctx.Reg.RegisterCommand("/hello", handler)
            return &MyAPI{storage: storage}, nil
        },
        Teardown: func(ctx *plugin.TeardownContext) error {
            ctx.API.(*MyAPI).Close()
            return nil
        },
    }
}
```

**关键改进**：

| 方面 | V1 | V3（当前） |
|------|----|-----------|
| 模式 | 继承 `BasePlugin` | 纯函数 `func() *Descriptor` |
| 依赖声明 | `Dependencies() []string` 手写 | DryRun 自动推断 + 可选显式覆盖 |
| 权限控制 | `Load(engine *Engine)` 全权限 | `SetupContext` 受限 + `Privileged` 声明 |
| 测试 | 需要完整 Engine | mock SetupContext + DryRun |
| 热重载 | 仅 unload-load | UnloadLoad / InPlace / BlueGreen 三种 |
| 序列化 | 不可序列化 | Descriptor 是纯数据，可 JSON/序列化 |
| 插件搜索 | 运行时遍历 | 元数据驱动，支持商店发现 |

**Smart 注册（DryRun）的原理**：

```go
// 框架在注册时模拟运行 Setup 来发现依赖
func (m *Manager) dryRunResolve(desc *Descriptor) ([]string, error) {
    var deps []string
    mockCtx := &SetupContext{
        Require: func(name string) any {
            deps = append(deps, name)
            return nil  // DryRun 时返回 nil，不真正初始化
        },
        Optional: func(name string) (any, bool) {
            deps = append(deps, name)
            return nil, false
        },
        // 其他字段为 no-op 实现
    }
    _, err := desc.Setup(mockCtx)
    return deps, err
}
```

相比手写 `Dependencies()`，DryRun 有两个核心优势：
1. **自动精确**：无论插件 `Require` 了多少依赖，框架精确知道——不会遗漏也不会冗余
2. **永不出现"写了但没用"的幽灵依赖**：依赖声明和执行逻辑之间的关联是确定性的

### V4 扩展：OptinalDeps 弱依赖

在实际使用中发现，一些插件（如 `pluginctrl`）在依赖不存在时可以降级运行，而非完全不能工作。为此引入了 `OptionalDeps`：

```go
// 弱依赖 — 影响加载顺序但不存在时不报错
type Descriptor struct {
    Deps         []string  // 强依赖：必须存在
    OptionalDeps []string  // 弱依赖：存在时调整加载顺序，不存在时不报错
}

// 使用示例 — builtin/pluginctrl
func New() *plugin.Descriptor {
    return &plugin.Descriptor{
        Name: "pluginctrl",
        OptionalDeps: []string{"storage"}, // 有 storage 就持久化，没有就内存运行
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            if storage, ok := ctx.Optional("storage"); ok {
                // 持久化模式
                return &PluginCtrl{store: storage.(Storage)}, nil
            }
            // 内存模式
            return &PluginCtrl{}, nil
        },
    }
}
```

## 迭代历程

| 版本 | 核心变化 | 动机 |
|------|---------|------|
| V1 | 继承模式（BasePlugin + Plugin 接口） | 快速实现插件能力 |
| V2 过渡 | PluginCoordinator 接口隔离 | 缩小框架接触面 |
| V3（当前） | 函数式 Descriptor + DryRun 自动依赖 | 解耦、可测试、权限隔离 |
| V4 扩展 | OptionalDeps 弱依赖 | 优雅降级能力 |

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| 组合 vs 继承 | 纯函数式描述符 | 易测试、无框架耦合 |
| 依赖声明 | Smart DryRun + 手动覆盖 | 减少样板，保留灵活性 |
| 热重载 | 三种策略可选 | 不同场景最优解 |
| 权限 | SetupContext / ManagerWriter 分离 | 最小权限原则 |
| 服务容器 | sync.Map + 原子快照 | 注册灵活，读取高效 |
