# WASM Plugin——沙箱化第三方插件运行时

> 插件系统的终极愿景是让第三方开发者能安全地贡献功能。Go 插件（`plugin.Open`）绑定宿主 Go 版本且无安全隔离，gRPC 插件有网络延迟和部署复杂度。WASM 插件通过 [wazero](https://github.com/tetratelabs/wazero) v1.11.0 提供纯 Go 实现的沙箱运行时——无 CGo、无系统调用、资源可控。

## 问题背景

现有插件系统的局限性：

| 方案 | 隔离性 | 部署复杂度 | 语言限制 | 性能 |
|------|--------|-----------|---------|------|
| Go plugin (`plugin.Open`) | 无（同一进程） | 低（.so 文件） | 仅 Go | 高（直接调用） |
| gRPC 微服务 | 强（独立进程） | 高（部署 + 注册） | 不限 | 中（网络开销） |
| 内置 Descriptor | 框架级别 | 低（代码内） | 仅 Go | 最高 |

WASM 插件的定位：**在隔离性和便捷性之间取得平衡**——无需独立部署，但提供内存安全、资源限制的沙箱执行环境。

## 核心设计

### 架构概览

```
┌──────────────────────────────────────────────┐
│                  Host Process                  │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐  │
│  │ Runtime   │   │ Manager  │   │ Bridge   │  │
│  │ (wazero)  │   │          │   │          │  │
│  │  ├─ModuleA│   │  Load    │   │  WASM    │  │
│  │  ├─ModuleB│   │  Call    │   │  reg.    │  │
│  │  └─ModuleC│   │  Close   │   │  →Engine │  │
│  └──────────┘   └──────────┘   └──────────┘  │
│                      │                        │
│               ┌──────┴──────┐                 │
│               │  Sandbox    │                 │
│               │  TokenBucket│                 │
│               │  MemLimit   │                 │
│               └─────────────┘                 │
└──────────────────────────────────────────────┘
```

### WASM ABI

```
数据通道：WASM 线性内存
序列化：JSON
方向：Host → Guest（调用）／ Guest → Host（宿主函数）
```

### 导出函数

```go
// 每个 WASM 模块必须导出
// plugin_init: 初始化函数，返回 JSON 格式的初始化结果
// plugin_handle: 事件处理函数，接收 JSON 格式的事件，返回 JSON 格式的响应
// malloc: 内存分配函数，供 Host 在 Guest 线性内存中分配空间
```

### 宿主函数（remilia_host 模块）

```go
// 通过 remilia_host 模块暴露给 WASM
"remilia_host": map[string]interface{}{
    "log":           func(level, msg uint32) { /* level=0debug,1info,2warn,3error */ },
    "get_config":    func(key, defaultVal uint32) uint32,
    "register_command": func(pattern uint32) uint32,
    "get_sender":    func() uint32,
    "reply_message": func(content uint32) uint32,
    "get_event":     func() uint32,
    "now":           func() uint64,
}
```

所有字符串参数通过线性内存传递：Guest 分配内存 → Host 写入 → 函数签名使用内存偏移量。

### Runtime 核心流程

```go
type Runtime struct {
    wasmCtx     context.Context
    moduleCache map[string]wazero.CompiledModule
    l          wazero.Runtime
    config     RuntimeConfig
}

func NewRuntime(ctx context.Context, config RuntimeConfig) (*Runtime, error) {
    runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
        WithCloseOnContextDone(true))
    return &Runtime{
        wasmCtx:     ctx,
        moduleCache: make(map[string]wazero.CompiledModule),
        l:           runtime,
        config:      config,
    }, nil
}

func (r *Runtime) LoadModule(name string, wasmBytes []byte) (*Module, error) {
    compiled, err := r.l.CompileModule(r.wasmCtx, wasmBytes)
    if err != nil {
        return nil, err
    }
    r.moduleCache[name] = compiled
    return newModule(r.wasmCtx, r.l, compiled, r.config), nil
}

func (m *Module) CallInit(configJSON string) ([]byte, error) {
    // 将 configJSON 写入线性内存
    inputPtr := m.malloc(len(configJSON))
    m.writeMemory(inputPtr, []byte(configJSON))
    // 调用 plugin_init
    results, err := m.instance.ExportedFunction("plugin_init").Call(m.ctx, inputPtr, uint64(len(configJSON)))
    if err != nil {
        return nil, err
    }
    return m.readMemory(uint32(results[0])), nil
}

func (m *Module) CallHandle(eventJSON []byte) ([]byte, error) {
    inputPtr := m.malloc(len(eventJSON))
    m.writeMemory(inputPtr, eventJSON)
    results, err := m.instance.ExportedFunction("plugin_handle").Call(m.ctx, inputPtr, uint64(len(eventJSON)))
    if err != nil {
        return nil, err
    }
    return m.readMemory(uint32(results[0])), nil
}

func (m *Module) Close() error {
    return m.instance.Close(m.ctx)
}
```

### Bridge：WASM → Engine 适配

```go
type Bridge struct {
    engine  *engine.Engine
    modules map[string]*Module
}

func (b *Bridge) handleRegistration(moduleName string, reg WASMRegistration) {
    matcher := &Matcher{
        Type:    reg.EventType,
        Handler: func(ctx *Context) error {
            module := b.modules[moduleName]
            eventJSON := marshalEvent(ctx)
            result, err := module.CallHandle(eventJSON)
            if err != nil {
                return err
            }
            return unmarshalResult(ctx, result)
        },
    }
    b.engine.RegisterMatcher(matcher)
}
```

WASM 插件在 `CallInit` 返回注册请求（命令、事件类型等），Bridge 将这些转换为 Engine Matcher，之后事件到来时由 Bridge 转发到对应 WASM Module。

### 沙箱（Sandbox）

```go
type ResourceLimit struct {
    MaxMemory    uint64        // WASM 线性内存上限（默认 10MB）
    MaxCPU       time.Duration // 单次调用 CPU 时间上限
    MaxRate      float64       // 每秒最大调用次数
    MaxBurst     int           // 令牌桶容量
}

type Sandbox struct {
    module    *Module
    limiter   *rate.Limiter  // 令牌桶限流
    memLimit  uint64
}
```

TokenBucket 限流：

```go
func (s *Sandbox) Allow() bool {
    return s.limiter.Allow()
}
```

不通过则拒绝调用，返回限流错误，避免 WASM 插件过度消耗宿主资源。

### WASMDescriptor

```go
type WASMDescriptor struct {
    Name          string
    Version       string
    Path          string          // .wasm 文件路径或 embed
    Config        map[string]any
    ResourceLimit ResourceLimit
}
```

Manager 独立管理，不修改 `plugin.Manager`：

```go
type Manager struct {
    runtime   *Runtime
    bridge    *Bridge
    modules   map[string]*Module
    sandboxes map[string]*Sandbox
    mu        sync.RWMutex
}
```

## 用法示例

```go
wasmManager := wasm.NewManager(wasm.RuntimeConfig{
    CacheDir: "./wasm-cache",
})

desc := &wasm.WASMDescriptor{
    Name:    "hello-plugin",
    Version: "1.0.0",
    Path:    "./plugins/hello/hello.wasm",
    Config: map[string]any{
        "greeting": "你好",
    },
    ResourceLimit: wasm.ResourceLimit{
        MaxMemory: 10 * 1024 * 1024, // 10MB
        MaxRate:   50,               // 每秒 50 次调用
        MaxBurst:  10,
    },
}

if err := wasmManager.Load(desc); err != nil {
    log.Fatal(err)
}
defer wasmManager.Close()
```

## 文件清单

```
plugin/wasm/
├── runtime.go       # Runtime: wazero 封装，LoadModule/CallInit/CallHandle/Close
├── module.go        # Module: 单个 WASM 实例，线性内存读写
├── bridge.go        # Bridge: WASM 注册 → Engine Matcher 转换
├── sandbox.go       # Sandbox: 令牌桶限流 + 内存限制
├── manager.go       # Manager: 独立管理 Load/Unload/List
├── descriptor.go    # WASMDescriptor 定义
├── host.go          # remilia_host 模块注册
├── abi.go           # ABI 常量和序列化工具
└── errors.go        # 错误类型
```

## 依赖

- `core/engine`：Bridge 将 WASM 注册请求转为 Engine Matcher
- `router`（可选）：Router 可将 WASM 路由加入规则链
- 外部依赖：`github.com/tetratelabs/wazero v1.11.0`

## 设计权衡

| 方面 | 选择 | 理由 |
|------|------|------|
| 运行时 | wazero（纯 Go，无 CGo） | 无平台依赖，无系统调用 => 天然沙箱 |
| ABI | 线性内存 + JSON | 简单成熟，所有语言都能编译到 WASM 的目标都支持 |
| 模块注册入口 | `plugin_init` 返回注册描述 | 无需在宿主硬编码注册逻辑，WASM 自描述 |
| 限流 | 令牌桶 | 平滑突发，实现简单 |
| 与 plugin.Manager 关系 | 独立 Manager，不修改 | 保持已有系统稳定，避免耦合爆炸 |
