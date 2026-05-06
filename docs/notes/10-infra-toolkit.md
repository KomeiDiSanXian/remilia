# 基础设施工具包——小而美的通用组件

Remilia 的 `infra/` 目录包含了 20+ 个独立的基础设施包，每个包解决一个特定问题。它们既可以作为框架的一部分使用，也完全可以单独提取出来复用到其他项目。

## 1. 并发原语

### infra/atomic

```go
// 泛型 atomic.Value 包装
type Value[T any] struct {
    inner atomic.Value
}

func NewValue[T any](v T) *Value[T] {
    val := &Value[T]{}
    val.Store(v)
    return val
}

func (v *Value[T]) Load() T {
    return v.inner.Load().(T)
}

func (v *Value[T]) Store(val T) {
    v.inner.Store(val)
}
```

Go 1.26 引入泛型后，`atomic.Value` 的类型断言可以封装在泛型方法中，消除调用方的强制类型转换。

### infra/syncx

并发安全的 Map、Lazy 初始化、GroupState：

```go
// Lazy 线程安全的懒加载
type Lazy[T any] struct {
    once   sync.Once
    value  T
    err    error
}

func (l *Lazy[T]) Get(fn func() (T, error)) (T, error) {
    l.once.Do(func() {
        l.value, l.err = fn()
    })
    return l.value, l.err
}

// Map 封装 sync.Map 提供泛型接口
type Map[K comparable, V any] struct {
    inner sync.Map
}
```

### infra/pool

```go
// TypedPool 基于 sync.Pool 的泛型对象池
type TypedPool[T any] struct {
    pool  sync.Pool
    reset func(T) T  // 可选重置函数
}

func NewTypedPool[T any](newFn func() T) *TypedPool[T] {
    return &TypedPool[T]{
        pool: sync.Pool{New: func() any { return newFn() }},
    }
}

func (p *TypedPool[T]) Get() T {
    return p.pool.Get().(T)
}

func (p *TypedPool[T]) Put(v T) {
    p.pool.Put(v)
}
```

用于 Engine 的 Matcher 切片复用，减少热路径上的 GC 压力：

```go
e.internals.matcherPool = infrapool.New(func() []*Matcher {
    return make([]*Matcher, 0, DefaultMatcherPoolCapacity)
})
```

## 2. 可观测性组件

### infra/health

```go
type Check struct {
    checkers []Checker
    mu       sync.RWMutex
}

type Checker interface {
    Name() string
    Check(ctx context.Context) CheckResult
}

type CheckResult struct {
    Status  Status  // Healthy / Degraded / Unhealthy
    Message string
    Details map[string]any
}
```

内置检查器：`EngineHealthChecker`、`AdapterHealthChecker`、`BotStatusChecker`。

### infra/logger

基于 `rs/zerolog` 的结构化日志封装，提供 `WithField`、`WithFields`、`WithError` 等便捷方法。

### infra/metrics

Prometheus 指标封装（独立的 Custom Registry），支持事件、中间件、插件、平台四层指标。

### infra/tracing

```go
type Config struct {
    ServiceName    string
    Endpoint       string
    SamplerType    SamplerType
    TargetTPS      float64   // 自适应采样目标
}
```

OpenTelemetry 封装，支持自适应采样。

### infra/coredump

跨平台 coredump 生成：

```go
func SetupCoredump() error
```

支持 Linux（`prctl`）、macOS（`setrlimit`）、Windows（`MiniDumpWriteDump`）。

## 3. 存储与缓存

### infra/storage

```go
type Storage interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte) error
    Delete(key string) error
    List(prefix string) ([]string, error)
}
```

内存实现 + GORM（SQLite）实现，通过 `builtin/storage` 插件暴露给业务代码。

### infra/cache

```go
type TTLCache struct {
    mu    sync.RWMutex
    items map[string]*cacheItem
    ttl   time.Duration
    maxSize int
}
```

TTL 缓存，用于 DedupFilter 的事件去重。

### infra/dlq

死信队列：

```go
type Queue[T any] struct {
    items     []T
    mu        sync.Mutex
    notEmpty  chan struct{}
}

type Consumer[T any] struct {
    Handler func(T) error
    // 支持重试
}
```

## 4. 网络通信

### infra/httpclient

```go
type Client struct {
    baseURL    string
    timeout    time.Duration
    retryCount int
    retryDelay time.Duration
    middleware []Middleware  // 请求/响应中间件
}

type Middleware func(next RoundTripper) RoundTripper
```

支持请求级中间件链（日志、重试、鉴权熔断等）。

### infra/server

```go
type Server struct {
    addr    string
    handler http.Handler
    srv     *http.Server
}
```

HTTP Server 封装，内置优雅关闭。

## 5. 图像处理

### infra/textimage

纯文本渲染引擎（不依赖外部字体服务）：

```
┌─────────────────────┐
│   Remilia Bot       │
│   ───────────       │
│   Status: Running   │
│   Uptime: 12:34:56  │
│   ───────────       │
│   Version: v1.0.0   │
└─────────────────────┘
```

- `textimage.go`：核心文本渲染
- `badge.go`：徽章生成（类似 Shields.io）
- `chart.go`：图表生成
- `vector.go`：矢量图支持
- `compose.go`：多元素合成
- `gradient.go`：渐变背景
- `blur.go`：模糊效果
- `sysfont.go`：跨平台系统字体加载（Linux/macOS/Windows）

### infra/webimage

```go
// 从 URL 抓取网页截图或图片
func Capture(url string) (image.Image, error)
```

### infra/gif

```go
// GIF 动图处理
type GIF struct {
    images []*image.Paletted
    delays []int
}
```

## 6. 中文文本处理

### infra/zhtext

```go
// 繁简转换
func SimplifiedToTraditional(s string) string
func TraditionalToSimplified(s string) string

// CJK 字符检测
func IsCJK(r rune) bool

// 模糊搜索（编辑距离）
func FuzzyFind(pattern string, texts []string) []string

// 文本规范化（全角→半角、大小写等）
func Normalize(s string) string
```

利用 `lithammer/fuzzysearch` 实现成语词典中的模糊搜索。

## 7. 其他工具

### infra/expr

```go
// 表达式求值（用于动态配置）
func Eval(expr string, vars map[string]any) (any, error)
```

### infra/option

```go
// Option 模式通用封装
type Option[T any] func(*T)
```

### infra/fs

```go
// 懒加载文件系统
type LazyFile struct {
    path   string
    data   []byte
    once   sync.Once
    err    error
}

func (f *LazyFile) Read() ([]byte, error) {
    f.once.Do(func() {
        f.data, f.err = os.ReadFile(f.path)
    })
    return f.data, f.err
}
```

### infra/audit

操作审计：

```go
type Auditor struct {
    // 记录配置变更、插件启停、权限修改等操作
}

type AuditEntry struct {
    Time    time.Time
    Actor   string
    Action  string
    Target  string
    Details map[string]any
}
```

## 迭代过程

### 背景：从"散落各处的通用代码"到"集中管理的 infra 包"

`infra/` 目录不是一开始就存在的。早期版本中，通用工具代码散落在根包和各模块中：

```
V0 分布（散落各处）：
├── engine.go          # 包含 atomic.Value 的直接使用
├── pool.go            # Context 对象池（根包）
├── errors.go          # 错误处理（根包）
├── health.go          # 健康检查（Bot 的方法）
├── metrics.go         # Prometheus 收集器（根包）
├── config/watcher.go  # 文件监听（但 watcher 和 engine 耦合）
└── middleware/        # 一些通用组件在中间件目录里
```

这种散落分布的问题：
- **重复实现**：多个模块各自实现类似的 "懒加载"、"原子值"、"对象池"
- **依赖混乱**：健康检查在 bot 包里，但 engine 也想用——导致 engine 导入 bot 包
- **难以复用**：外部项目想用 Remilia 的 textimage 渲染引擎？需要导入整个框架

### V1：按功能提取

逐个模块提取到 `infra/` 下独立目录，每个包只解决一个特定问题：

```bash
# 提取过程（按时间顺序）
infra/atomic/     # 从 engine 和 context 的 atomic.Value 使用中提取
infra/pool/       # 从 context pool 中提取泛型版本
infra/syncx/      # 从多个模块提取并发工具
infra/health/     # 从 bot.Health() 提取框架化健康检查
infra/metrics/    # 从 engine 和 middleware 提取统一指标收集
infra/tracing/    # 从 middleware/tracing.go 提取 OpenTelemetry 封装
infra/server/     # 从 webhook server 提取通用 HTTP Server
infra/httpclient/ # 从 openapi 客户端提取通用 HTTP 客户端
infra/textimage/  # 从 help 插件提取文本渲染引擎
infra/zhtext/     # 从 idiomdict 插件提取中文文本处理
infra/dlq/        # 从 engine 死信队列提取泛型 DLQ
infra/cache/      # 从 dedup 提取 TTL 缓存
infra/coredump/   # 新增的跨平台 coredump
infra/audit/      # 新增的操作审计
infra/option/     # 从各模块 Option 模式提取
infra/fs/         # 从配置文件加载提取懒加载
infra/expr/       # 从规则引擎提取表达式求值
infra/gif/        # 新增的 GIF 动图处理
infra/webimage/   # 新增的网页截图抓取
```

### V2：泛型化（Go 1.26）

Go 1.26 引入泛型后，对已有包进行泛型改造：

**`infra/atomic`** 从 `*state` 特定类型变为泛型：

```go
// V1 — 特定类型
type engineState struct{ *state }
var engineStateValue atomic.Value // 存储 *state

// V2 — 泛型
type Value[T any] struct { inner atomic.Value }
var engineStateValue = NewValue[*state](nil)
```

**`infra/pool`** 从 `*Context` 特定池变为泛型：

```go
// V1 — 专用池
type ContextPool struct {
    pool sync.Pool  // 只能放 *Context
}
func (p *ContextPool) Get() *Context { ... }

// V2 — 泛型池
type TypedPool[T any] struct {
    pool  sync.Pool
    reset func(T) T  // 可选重置函数
}
// 可用于任何类型
matcherPool := NewTypedPool(func() []*Matcher { return make([]*Matcher, 0, 32) })
contextPool := NewTypedPool(func() *Context { return &Context{state: make(State)} })
```

**`infra/syncx.Map`** 从 `string→any` 变为泛型：

```go
// V2 — 泛型 Map
type Map[K comparable, V any] struct {
    inner sync.Map
}

func (m *Map[K, V]) Load(key K) (V, bool) { ... }
func (m *Map[K, V]) Store(key K, value V) { ... }
```

减少模块内对 `interface{}` 的依赖，消除类型断言。

### V3：跨平台字体处理

`infra/textimage` 的 `sysfont.go` 需要跨平台处理系统字体路径：

```go
// 平台特定实现
// sysfont_unix.go — Linux
func systemFontPath() string { return "/usr/share/fonts/" }

// sysfont_darwin.go — macOS
func systemFontPath() string { return "/System/Library/Fonts/" }

// sysfont_windows.go — Windows
func systemFontPath() string {
    return filepath.Join(os.Getenv("WINDIR"), "Fonts")
}
```

这也是第一个需要平台特定编译标签（`_unix`、`_darwin`、`_windows`）的包。coredump 也借鉴了同样的模式，按平台实现 `SetupCoredump()`。

### V4：死信队列泛型化

死信队列最初硬编码为 `*dto.Payload` 类型（跟 Engine 紧耦合）：

```go
// V1 — QQ 专用死信
type DeadLetterItem struct {
    Event   *dto.Payload  // 只有 QQ 能用
    Err     error
    Attempt int
}
```

V4 改为泛型接口，任何类型都可以入队：

```go
// V4（当前）— 泛型死信队列
type Queue[T any] struct {
    items    []T
    mu       sync.Mutex
    notEmpty chan struct{}
}

type Consumer[T any] struct {
    Handler func(T) error
}
```

向后兼容通过类型别名实现：

```go
// 旧类型仍可用
type DeadLetterItem = dlq.Item[*dto.Payload]
```

## 迭代历程

| 版本 | 核心变化 | 解决的问题 |
|------|---------|-----------|
| V0 | 通用代码散落各处 | 快速实现 |
| V1 | 按功能提取到 infra/ | 减少重复，解耦 |
| V2 | 泛型化（Go 1.26） | 消除类型断言，类型安全 |
| V3 | 跨平台支持（编译标签） | Linux/macOS/Windows 兼容 |
| V4（当前） | 泛型接口 + 向后兼容 | 复用性 + 平滑迁移 |

## 设计原则

这些基础设施包遵循几个共同的设计原则：

1. **无外部依赖原则**：除非必要（如 Prometheus、OpenTelemetry），尽量只依赖 Go 标准库
2. **泛型优先**：Go 1.26 的泛型广泛用于容器包装（atomic.Value、sync.Map、Pool）
3. **零值可用**：所有类型都设计为 `var x T` 即可安全使用，不需要显示的构造函数
4. **接口小**：每个包暴露的接口尽量小（1-3 个方法），方便 mock
