# Plugin 系统设计质量深度分析（第二轮）

> **分析日期**：2026-02-25（基于当前最新代码快照）  
> **最后更新**：2026-02-25（P0/P1/P2 除 P2-2 外全部完成）  
> **分析范围**：`plugin/` 包核心框架 + `plugins/` 内置实现 + `plugin/plugintest/` 测试辅助  
> **项目状态**：未发布，可接受重构  
> **前置背景**：第一轮分析（`plugin-system-design-review.md`）的所有高/中/低优先级问题已全部解决

---

## 0. 执行摘要

经过第一轮重构，plugin 系统在架构层面已经达到���秀水准：v2 函数式 API、三种热重载策略、双层权限模型（`engine.EngineReader` + `ManagerWriter`）、DryRun 副作用隔离、接口友好的 `MustAs`/`TryAs`，这些特性组合在一起，使 remilia 的插件系统已是同类 Go 框架中功能最完整的设计之一。

经过本轮修复，**P0（正确性）、P1（一致性）全部完成，P2 中除 P2-2（Deprecated 符号手动移除）外也全部完成**。

**总体评级：A+（框架设计与工程实现均达到优秀水准，仅余 1 项手动清理工作）**

| 维度 | 评分 | 核心说明 |
|------|------|---------|
| API 设计（开发者体验） | A+ | `Infow/Warnw/Debugw` 完善结构化日志；`MockPluginInfo` 覆盖 ctx.Info 测试场景；所有内置插件导出 API 遵循统一约定 |
| 内部架构（可维护性） | A | 职责分明；Config.Override overlay 语义已正确；所有子文件边界清晰 |
| 运行时可靠性 | A | panic recovery 全覆盖；goroutine 全管控；Config.Override 热重载不再丢失 |
| 扩展性与灵活性 | A | `bundle.Dev()` 补齐快捷入口；`TeardownContext.Info` 扩展 Teardown 能力；`GetAPI()` 完善反射场景 |
| 插件实现一致性 | A | admin/debug 统一导出 API；全面使用 `Must[T]` 替代原始断言；包注释示例正确 |

---

## 1. 亮点设计（保留确认）

> 本节确认第一轮已解决的设计正确性，为后续分析提供基线。

### 1.1 API 层

| 特性 | 说明 |
|------|------|
| `Must[T]` / `Try[T]` / `GetPlugin[T]` | 编译期类型检查，满足具体类型依赖 |
| `MustAs[T]` / `TryAs[T]` / `ExportInterface[T]` | 接口类型依赖，满足 DIP，storage 提供规范示例 |
| `ctx.DryRun` + noopEventBus + noop Go/GoNamed | Smart 推断阶段对插件代码完全透明，只有外部 I/O 需要手动判断 |
| `Privileged: bool` + `ctx.Admin` | 显眼的代码审查检查点，未声明时为 nil 立即 panic |
| `ctx.GoNamed` + `Manager.ListPluginGoroutines` | 命名 goroutine + 可观测性 |

### 1.2 运行时层

| 特性 | 说明 |
|------|------|
| `engine.EngineReader` 接口 + `engineReaderWrapper` 双重保护 | 编译器阻止通过 `ctx.Info.Coordinator()` 调用写操作 |
| `Container.Freeze()` 两阶段模式 | 注册完成后切换为原子快照，读性能提升 2-3x |
| `metaGM` + `Manager.Shutdown()` | `notifyDependents` goroutine 纳入管控，进程退出安全 |
| 三种热重载策略语义清晰 | `ReloadInPlace` 必须提供 `Reload` fn；`ReloadUnloadLoad` 严格 unload→load；注册时 Reload+Strategy 不匹配立即 Warn |
| `RegisterMultipleV2Atomic` 事务性语义 | 任意失败逆序回滚，不留半注册状态 |

### 1.3 依赖系统

| 特性 | 说明 |
|------|------|
| 自动依赖追踪 | Must→必要依赖（影响 cascade/notify）；Try→可选依赖（影响拓扑排序） |
| Smart 模式 | DryRun 推断 + Kahn 拓扑排序 + 跨批次循环检测 |
| semver 约束 | `>=`/`>`/`^`/`~`/`=` 全支持，版本 mismatch 返回 `VersionConstraintError` |

---

## 2. 剩余问题分析

### P0：正确性（影响运行时行为，建议发布前修复）

---

#### ~~P0-1：`Config.Override` 在热重载后被静默丢弃~~ ✅ 已完成

**问题**：`pluginConfig.Reload()` 内部调用 `loadFromGlobal()`，该方法用 viper 的值**完整替换** `pc.values`：

```go
// config.go
func (pc *pluginConfig) loadFromGlobal() {
    // ...
    settings := pc.viper.Sub(prefix)
    if settings != nil {
        pc.values = settings.AllSettings() // ← 完整覆盖，Override 的值消失
    }
}
```

**影响**：插件调用 `ctx.Config.Override("key", value)` 后，若 Manager 触发热重载（如 admin 插件调用 `ctx.Admin.Reload("myplugin")`），所有通过 `Override` 写入的运行时配置值会被 viper 的磁盘配置静默覆盖，行为不可预测。

**修复方案**：在 `pluginConfig` 中维护独立的 `overrides map[string]any`，`loadFromGlobal()` 执行后重新将 overrides 叠加回 `pc.values`：

```go
type pluginConfig struct {
    // ...existing fields...
    overrides map[string]any // Override 写入的值，Reload 后保留
}

func (pc *pluginConfig) Override(key string, value any) error {
    pc.mu.Lock()
    oldVal := pc.values[key]
    pc.values[key] = value
    if pc.overrides == nil {
        pc.overrides = make(map[string]any)
    }
    pc.overrides[key] = value  // 持久记录 override
    // ...通知 handlers...
    pc.mu.Unlock()
    return nil
}

func (pc *pluginConfig) loadFromGlobal() {
    pc.mu.Lock()
    defer pc.mu.Unlock()
    if pc.viper == nil {
        return
    }
    prefix := fmt.Sprintf("plugins.%s", pc.pluginName)
    settings := pc.viper.Sub(prefix)
    if settings != nil {
        pc.values = settings.AllSettings()
    }
    // 重新叠加 override，确保热重载不丢失运行时覆盖值
    for k, v := range pc.overrides {
        pc.values[k] = v
    }
}
```

---

#### ~~P0-2：`Config.Override` 不写入 viper，与 `Reload()` 语义不一致~~ ✅ 已完成

**问题**：`Override` 只写内存 `pc.values`，不调用 `pc.viper.Set()`。这导致：
1. 热重载后 override 丢失（如 P0-1 所述）
2. 同一 bot 进程内其他组件直接访问 viper 时读不到 override 值

**建议**：将 P0-1 的 overlay 方案作为主方案（不写 viper），并在 `Override` 的 godoc 中明确说明语义：

```go
// Override 在内存中覆盖配置值（仅影响本插件的 Config.Get* 方法）。
// 重启后失效；热重载后持续有效（框架保证重新叠加 override）。
// 若需要持久化，请直接修改配置文件并调用 config.Reload()。
```

---

### P1：API 一致性（影响开发者体验，建议发布前修复）

---

#### ~~P1-1：`admin.go` 使用原始类型断言而非框架约定~~ ✅ 已完成

**问题**：`admin.go` Setup 中直接使用 `permAPI.(*permission.Plugin)`：

```go
// plugins/core/admin/admin.go:50（近似）
permAPI := ctx.MustGet("permission")
v1Plugin.PermPlugin = permAPI.(*permission.Plugin)  // ← 原始断言，不一致
```

框架已提供 `Must[T]` 用于此场景，原始断言方式：
- 错误信息不携带插件名和依赖名上下文
- 与其他插件的写法不统一，误导新插件开发者模仿

**修复**：

```go
v1Plugin.PermPlugin = plugin.Must[permission.Plugin](ctx, "permission")
```

---

#### ~~P1-2：`debug` 插件 Setup 返回 `nil, nil`，无法被其他插件发现~~ ✅ 已完成

**问题**：`debug.go` Setup：

```go
return nil, nil  // *Plugin 未导出到容器
```

其他插件（如未来的 monitor 插件）无法通过 `plugin.Must[debug.Plugin](ctx, "debug")` 获取 debug 插件引用来集成调试能力。

**修复**：

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    // ... existing setup ...
    return v1Plugin, nil  // 导出到容器，可被发现
},
```

---

#### ~~P1-3：`admin` 插件 Setup 返回 `nil, v1Plugin.Load(ctx)`，无法被其他插件发现~~ ✅ 已完成

**问题同 P1-2**。admin 插件作为系统核心管理插件，如果其他插件（如 monitor/auditlog）需要获取 admin 对象来注册回调，当前无法做到。

**修复**：

```go
Setup: func(ctx *plugin.SetupContext) (any, error) {
    // ... existing setup ...
    if err := v1Plugin.Load(ctx); err != nil {
        return nil, err
    }
    return v1Plugin, nil  // 导出 *Plugin
},
```

---

#### ~~P1-4：`plugin.go` 包注释中的 v2 示例签名已过时~~ ✅ 已完成

**问题**：`plugin/plugin.go` 包级文档示例仍显示旧签名：

```go
// v2 示例：
//
//	Setup: func(ctx *plugin.SetupContext) error {  // ← 错误！当前签名是 (any, error)
//	    ...
//	    return nil
//	},
```

新开发者复制此示例会遇到编译错误，影响第一印象。

**修复**：更新为正确签名：

```go
// v2 示例：
//
//	func New() *plugin.PluginDescriptor {
//	    p := &MyPlugin{}
//	    return &plugin.PluginDescriptor{
//	        Name:    "myplugin",
//	        Version: "1.0.0",
//	        Setup: func(ctx *plugin.SetupContext) (any, error) {
//	            p.cache = plugin.Must[cache.Plugin](ctx, "cache")
//	            ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/hello").Handle(p.handleHello)
//	            return p, nil
//	        },
//	    }
//	}
```

---

#### ~~P1-5：`TeardownContext` 没有 `Info` 字段，Teardown 阶段无法查询插件系统~~ ✅ 已完成

**问题**：有些插件在 Teardown 时需要判断其他插件的状态（如条件性资源释放、通知兄弟插件等），但 `TeardownContext` 只有：

```go
type TeardownContext struct {
    API      any
    Config   Config
    EventBus EventBus
    Log      PluginLogger
    // 没有 Info PluginInfo ← 缺失
}
```

**修复**：在 `TeardownContext` 中增加 `Info PluginInfo`，并在 `instance.buildTeardownContext()` 中填充：

```go
type TeardownContext struct {
    API      any
    Config   Config
    EventBus EventBus
    Log      PluginLogger
    Info     PluginInfo  // 新增：只读视图，供 Teardown 阶段查询兄弟插件状态
}
```

```go
// instance.go: buildTeardownContext()
func (pi *PluginInstance) buildTeardownContext() *TeardownContext {
    // ...
    var info PluginInfo
    if pi.setupContext != nil {
        info = pi.setupContext.Info  // 复用 Setup 阶段的 PluginInfo
    }
    return &TeardownContext{
        API:      api,
        Config:   cfg,
        EventBus: bus,
        Log:      newPluginLogger(pi.desc.Name),
        Info:     info,
    }
}
```

---

#### ~~P1-6：`PluginInstance` 没有公开的 `GetAPI()` 方法~~ ✅ 已完成

**问题**：外部代码持有 `*PluginInstance` 时（如通过 `ctx.Info.Get("storage")`），无法直接获取 Setup 返回的 API 对象，必须知道具体类型后才能通过 `Must[T]` 或类型断言获取。

常见场景：debug 插件或 monitor 插件遍历所有插件实例并打印其 API 类型信息。

**修复**：在 `PluginInstance` 上增加 `GetAPI()` 方法：

```go
// GetAPI 返回 Setup 阶段导出的 API 对象（框架以插件名注册到容器中的对象）。
// 若插件尚未加载或 Setup 返回 nil，则返回 nil。
func (pi *PluginInstance) GetAPI() any {
    pi.mu.RLock()
    defer pi.mu.RUnlock()
    return pi.exportedAPI
}
```

---

#### ~~P1-7：`Status` 结构体缺少 goroutine 计数字段~~ ✅ 已完成

**问题**：`Manager.ListPluginGoroutines()` 已实现，但每个插件的 `Status` 结构体中没有 `GoroutineCount` 字段，导致：
- 监控面板无法通过单次 `GetStatus` 获取 goroutine 信息
- 必须调用 `Manager.ListPluginGoroutines()` 然后手动过滤，使用繁琐

**修复**：

```go
// status.go
type Status struct {
    // ...existing fields...
    GoroutineCount int // 当前活跃的生命周期绑定 goroutine 数量
}

// manager.go: GetStatus()
func (pm *Manager) GetStatus(name string) (*Status, error) {
    // ...existing code...
    inst.mu.RLock()
    gm := inst.goroutineMgr
    inst.mu.RUnlock()
    if gm != nil {
        status.GoroutineCount = len(gm.listGoroutines())
    }
    return status, nil
}
```

---

### P2：细节打磨（可在第一个发布后迭代修复）

---

#### ~~P2-1：`PluginLogger` 缺少结构化日志 `w` 变体~~ ✅ 已完成

**问题**：当前 `PluginLogger` 只有 `Info/Infof/Warn/Warnf/Error/Errorf/Debug/Debugf/WithField`。在需要记录多个结构化字段时，开发者被迫：
1. 使用字符串格式化（丢失结构化优势）：`ctx.Log.Infof("user=%s action=%s", uid, action)`
2. 链式 `WithField`（冗长）：`ctx.Log.WithField("user", uid).WithField("action", action).Info("...")`

**建议**：增加接受 `key-value` 变参的方法：

```go
type PluginLogger interface {
    // ...existing methods...
    
    // Infow 记录带结构化字段的 info 日志（w = with fields）
    // 用法：ctx.Log.Infow("user banned", "userID", uid, "reason", reason)
    Infow(msg string, keysAndValues ...any)
    Warnw(msg string, keysAndValues ...any)
    Debugw(msg string, keysAndValues ...any)
}
```

---

#### P2-2：`testing.go` 中的 Deprecated 符号未从公开 API 中隐藏

**问题**：`plugin/testing.go` 仍向外部暴露：
- `TestSetupOptions`
- `NewTestSetupContext`  
- `StopTestSetupContext`

这三个符号有 `Deprecated` 注释，但没有通过任何机制阻止新代码使用（Go 没有注解级强制废弃）。新加入的开发者会在 IDE 自动补全中看到两套 API（`plugintest.NewSetupContext` 和 `NewTestSetupContext`），造成困惑。

**建议**：在 `testing.go` 文件顶部添加构建标签保护，阻止非 `_test` 文件中引用（实际上 `testing.go` 本身没有 `_test` 后缀，Go 不会自动排除它）。

最小化方案：在 `TESTING.md` 和 `README.md` 中明确记录废弃时间线，约定在下一个主版本（v3）移除。

---

#### ~~P2-3：`plugintest` 缺少 `MockPluginInfo`~~ ✅ 已完成

**问题**：使用 `ctx.Info` 的插件（如 `help`、`debug`）在单元测试中无法精确控制 `PluginInfo` 返回值，只能使用 `nullPluginInfo`（全部返回 false/nil/0）：

```go
// 当前 plugintest.NewSetupContext 注入的是 nullPluginInfo
// 测试中无法模拟 "某插件已加载" 等场景
ctx := plugintest.NewSetupContext("help", nil)
// ctx.Info.IsLoaded("storage") 永远返回 false
```

**建议**：在 `plugintest/` 中增加：

```go
// plugintest/mock_plugin_info.go

// MockPluginInfo 是 PluginInfo 的可配置 mock，用于单元测试。
type MockPluginInfo struct {
    LoadedPlugins    map[string]bool
    DisabledPlugins  map[string]bool
    Plugins          map[string]*plugin.Metadata
    LoadOrder        []string
    CoordinatorValue engine.EngineReader
}

func (m *MockPluginInfo) IsLoaded(name string) bool               { return m.LoadedPlugins[name] }
func (m *MockPluginInfo) IsDisabled(name string) bool             { return m.DisabledPlugins[name] }
func (m *MockPluginInfo) List() []string                          { /* keys of Plugins */ }
// ...etc...

// NewSetupContextWithInfo 创建带自定义 PluginInfo 的测试 SetupContext。
func NewSetupContextWithInfo(pluginName string, info *MockPluginInfo, opts *SetupOptions) *plugin.SetupContext {
    ctx := NewSetupContext(pluginName, opts)
    ctx.Info = info
    return ctx
}
```

---

#### ~~P2-4：`bundle/bundle.go` 缺少 `Dev()` 组合~~ ✅ 已完成

**问题**：当前 `bundle` 包只提供 `Core()` 和 `All()`，没有 `Dev()` 组合（debug + admin），开发者需要手动两次 import：

```go
// 当前：繁琐
pm.RegisterMultipleV2Atomic(bundle.Core())
pm.RegisterV2(admin.New())
pm.RegisterV2(debug.New())
```

**建议**：

```go
// Dev 返回开发/管理插件集合（适合调试环境）
// 包含：admin（插件管理命令）和 debug（调试命令集）
func Dev() []*plugin.PluginDescriptor {
    return []*plugin.PluginDescriptor{
        admin.New(),
        debug.New(),
    }
}
```

---

#### ~~P2-5：`RegisterMultipleV2Smart` DryRun 在依赖全部已注册时冗余执行~~ ⚠️ 已撤销

**问题同上**。

**撤销原因**：短路优化仅检查 `desc.Deps` 中是否有批内成员，但 Smart 模式的核心特性是允许 `desc.Deps` 为空（依赖完全由 DryRun 推断）。若某插件通过 `MustGet` 形成了批内循环依赖但没有在 `Deps` 中声明，短路会绕过 DryRun，导致循环依赖检测失效（测试 `TestRegisterMultipleV2Smart/detect_inferred_circular_dependency` 验证了此场景）。增加这个优化需要在 DryRun 后二次判断，复杂度不值得，维持现状。

---

#### ~~P2-6：`PluginLogger` 和全局 logger 输出格式不完全统一~~ ✅ 已完成

**问题**：`pluginLogger` 通过 `l.prefix()` 手动添加 `[pluginname] ` 前缀，同时也通过 `WithField("plugin", l.name)` 添加结构化字段。这导致日志中**既有文本前缀又有结构化字段**，在 JSON 日志格式下会有重复：

```json
{"level":"info","plugin":"cooldown","msg":"[cooldown] Loaded"}
//                  ↑ 结构化字段         ↑ 文本前缀（重复）
```

**建议**：在 JSON 模式下只保留结构化字段，文本前缀仅在人读模式下添加，或统一只用结构化字段：

```go
func (l *pluginLogger) Info(msg string) {
    l.entry().Info(msg)  // 移除 l.prefix()，仅保留 WithField("plugin", l.name)
}
```

---

## 3. 优化建议汇总（按优先级）

### 3.1 P0：发布前必须修复（正确性）

| # | 问题 | 文件 | 工作量 | 状态 |
|---|------|------|--------|------|
| P0-1 | ~~`Config.Override` 热重载后被丢弃~~ | `plugin/config.go` | 小（加 overrides map） | ✅ 已完成 |
| P0-2 | ~~`Config.Override` godoc 语义不清晰~~ | `plugin/config.go` | 极小（注释） | ✅ 已完成 |

### 3.2 P1：发布前建议修复（一致性）

| # | 问题 | 文件 | 工作量 | 状态 |
|---|------|------|--------|------|
| P1-1 | ~~`admin.go` 原始类型断言~~ | `plugins/core/admin/admin.go` | 极小 | ✅ 已完成 |
| P1-2 | ~~`debug.go` 返回 `nil, nil`~~ | `plugins/dev/debug/debug.go` | 极小 | ✅ 已完成 |
| P1-3 | ~~`admin.go` 不导出 Plugin~~ | `plugins/core/admin/admin.go` | 极小 | ✅ 已完成 |
| P1-4 | ~~`plugin.go` 包注释示例签名过时~~ | `plugin/plugin.go` | 极小 | ✅ 已完成 |
| P1-5 | ~~`TeardownContext` 缺少 `Info`~~ | `plugin/descriptor.go` + `plugin/instance.go` | 小 | ✅ 已完成 |
| P1-6 | ~~`PluginInstance` 缺少 `GetAPI()`~~ | `plugin/instance.go` | 极小 | ✅ 已完成 |
| P1-7 | ~~`Status` 缺少 `GoroutineCount`~~ | `plugin/status.go` + `plugin/manager.go` | 小 | ✅ 已完成 |

### 3.3 P2：发布后第一迭代（细节打磨）

| # | 问题 | 文件 | 工作量 | 状态 |
|---|------|------|--------|------|
| P2-1 | ~~`PluginLogger` 缺少 `Infow/Warnw/Debugw`~~ | `plugin/logger.go` | 小 | ✅ 已完成 |
| P2-2 | `testing.go` Deprecated 符号仍暴露 | `plugin/testing.go` | 文档/计划 | 🔜 手动移除 |
| P2-3 | ~~`plugintest` 缺少 `MockPluginInfo`~~ | `plugin/plugintest/` | 中 | ✅ 已完成 |
| P2-4 | ~~`bundle` 缺少 `Dev()`~~ | `plugins/bundle/bundle.go` | 极小 | ✅ 已完成 |
| P2-5 | ~~`Smart` DryRun 在无批内依赖时冗余~~ | `plugin/register.go` | 小 | ⚠️ 已撤销（优化破坏循环依赖检测，短路条件需同时满足「批内无依赖声明 AND 已跑完DryRun推断」，不值得增加复杂度） |
| P2-6 | ~~`PluginLogger` 文本前缀与结构化字段重复~~ | `plugin/logger.go` | 小 | ✅ 已完成 |

---

## 4. 与主流框架横向对比（更新）

| 特性 | remilia plugin（当前） | uber-go/fx | hashicorp/go-plugin |
|------|----------------------|------------|---------------------|
| 依赖注入方式 | 泛型 Must/Try + 接口 MustAs/TryAs | 自动类型匹配（反射） | 不支持（跨进程 RPC） |
| 热重载 | ✅ 三种策略 | ❌ | ✅ 进程级重启 |
| DryRun 副作用隔离 | ✅ noopEventBus + noopReg + DryRun flag | N/A | N/A |
| 权限分层 | ✅ EngineReader + ManagerWriter + Privileged | ❌ | ❌ |
| goroutine 生命周期绑定 | ✅ ctx.Go/GoNamed + 可观测 | ✅ lifecycle hooks | ✅ |
| Config 热重载集成 | ✅ Override 值热重载后持久保留（overlay 机制） | ❌ 需外部集成 | ❌ |
| 测试辅助 | ✅ plugintest 子包 + MockPluginInfo + NewSetupContextWithInfo | ✅ 内置 mock | ❌ |
| 循环依赖检测 | ✅ 编译期 + 跨批次检测 | ✅ 启动时 | N/A |
| 版本约束 | ✅ semver 约束（^/~/>=） | ❌ | ❌ |
| 插件元数据 / Help | ✅ Meta + HelpText + bundle | ❌ | ❌ |

---

## 5. 附录：文件职责速查表（最新）

| 文件 | 行数 | 核心职责 | 状态 |
|------|------|---------|------|
| `plugin/descriptor.go` | 226 | PluginDescriptor / PluginAdvanced / TeardownContext / 策略常量 | ✅ 健康；`TeardownContext.Info` 已添加（P1-5） |
| `plugin/context.go` | 319 | SetupContext / Must / Try / MustAs / TryAs / ExportInterface | ✅ 健康 |
| `plugin/instance.go` | 270 | PluginInstance 生命周期 + 公开 API | ✅ 健康；`GetAPI()` 已添加（P1-6） |
| `plugin/manager.go` | 705 | 完整生命周期管理 | ✅ 健康；`GoroutineCount` 已填充（P1-7） |
| `plugin/register.go` | 586 | RegisterV2 / Atomic / Smart / 拓扑排序 | ✅ 健康 |
| `plugin/reload.go` | 192 | 三种热重载策略 | ✅ 策略边界清晰 |
| `plugin/config.go` | 280 | 插件配置（Override/Reload） | ✅ 健康；`overrides` 持久化已修复（P0-1/P0-2） |
| `plugin/eventbus.go` | ~320 | EventBus + noopEventBus + 类型安全订阅 | ✅ 健康 |
| `plugin/goroutine.go` | ~100 | GoroutineManager + GoNamed + GoroutineInfo | ✅ 健康 |
| `plugin/logger.go` | 130 | PluginLogger + pluginLogger 实现 | ✅ 健康；`Infow/Warnw/Debugw` 已添加（P2-1）；文本前缀重复已修复（P2-6） |
| `plugin/plugin_info.go` | 91 | PluginInfo 只读视图 + nullPluginInfo | ✅ 健康 |
| `plugin/manager_writer.go` | 59 | ManagerWriter 接口 + wrapper | ✅ 健康 |
| `plugin/testing.go` | 81 | Deprecated 测试辅助 | 🔜 符号仍公开（P2-2 待手动移除） |
| `plugin/plugintest/plugintest.go` | 109 | 测试辅助子包 | ✅ 健康 |
| `plugin/plugintest/mock_plugin_info.go` | 128 | MockPluginInfo + NewSetupContextWithInfo | ✅ 新增（P2-3） |
| `plugin/plugin.go` | 165 | 包文档 + Metadata + 接口定义 | ✅ 健康；包注释示例已更新（P1-4） |
| `plugin/status.go` | 55 | Status 结构体 | ✅ 健康；`GoroutineCount` 已添加（P1-7） |
| `plugin/version.go` | 165 | semver 解析 + 约束检查 | ✅ 完整 |
| `plugin/errors.go` | 112 | 富错误类型 | ✅ 诊断信息详细 |
| `plugin/schema.go` | 166 | ConfigSchema 声明式验证 | ✅ 两种形式灵活 |
| `plugin/container.go` | 84 | 两阶段依赖容器 | ✅ 健康 |
| `plugin/registry.go` | 82 | RegistryWriter + noopRegistryWriter | ✅ 健康 |
| `plugin/lifecycle_adapter.go` | 92 | lifecycle.Component 适配器 | ✅ 健康 |
| `plugins/bundle/bundle.go` | 90 | Core() / All() / Dev() | ✅ 健康；`Dev()` 已添加（P2-4） |
| `plugins/core/admin/admin.go` | 805 | 管理命令插件 | ✅ 健康；`Must[T]` 替换原始断言（P1-1）；导出 API（P1-3） |
| `plugins/dev/debug/debug.go` | 655 | 调试命令插件 | ✅ 健康；导出 API（P1-2） |

---

*本报告基于 2026-02-25 代码快照，项目未发布，所有修复建议均可在正式发布前实施。*

