# ZeroBot + FloatTech 系列库 vs Remilia 框架层对比报告

> 生成时间：2026-04-07  
> 比较基准：wdvxdr1123/ZeroBot（主干）+ FloatTech 系列库 vs Remilia 当前工作区  
> **报告定位**：本报告聚焦**框架层**（核心引擎 + 基础设施库），不讨论 ZeroBot-Plugin 中 90+ 业务插件的应用层差距，应用层差距请参阅 [`comparison-zerobotplugin.md`](comparison-zerobotplugin.md)。

---

## 一、两套技术栈概览

### ZeroBot 侧（上游框架 + 基础库）

```
wdvxdr1123/ZeroBot          核心框架（单平台，QQ/OneBot）
└── FloatTech/zbpctrl       插件开关/权限/群级管理
└── FloatTech/sqlite        SQLite ORM 封装
└── FloatTech/gg            2D 矢量图形（fogleman/gg 定制版）
└── FloatTech/rendercard    预制卡片模板
└── FloatTech/ttl           TTL 泛型缓存 Map
└── FloatTech/floatfile     资源懒加载管理器
└── FloatTech/zbputils      通用工具集（HTML→图、消息段等）
└── FloatTech/AnimeAPI      动漫 API 客户端（应用层，不讨论）
```

### Remilia 侧（框架本体）

```
remilia
├── core/engine             多组件事件引擎（Matcher/Rule/Handler/Middleware 链）
├── core/permission         完整 RBAC 权限系统
├── platform/               多平台适配器（QQ/Discord/Telegram/WeChat）
├── command/                Trie 前缀树命令路由
├── builtin/pluginctrl      群级插件开关（持久化）
├── builtin/cooldown        用户/命令级冷却
├── builtin/subscription    推送订阅框架
├── infra/storage           GORM 存储抽象（SQLite/Postgres/MySQL）
├── infra/textimage         文字→图片渲染（光栅，含 Canvas/Badge/Chart/Gradient）
├── infra/metrics           Prometheus 指标
├── infra/tracing           OpenTelemetry 追踪
├── middleware/             企业级中间件（熔断/限流/重试/DLQ/追踪/自适应 CPU）
└── config/                 热重载配置（viper + fsnotify）
```

---

## 二、核心引擎架构对比

### 2.1 事件路由机制

| 能力 | ZeroBot | Remilia |
|------|---------|---------|
| 路由算法 | 线性扫描（`[]*Matcher` 遍历） | **Trie 前缀树** O(log n) |
| 命令匹配 | `CommandRule("cmd")` 字符串比较 | `command.Registry`（Trie，支持别名/子命令/模糊匹配） |
| 正则匹配 | `RegexRule(re)`，预编译 | `engine.RegexMatcher`，支持命名捕获组 |
| 事件过滤 | Rule 函数列表 | Rule 函数列表（语义完全相同） |
| 临时 Matcher | `ctx.Send()` 触发二次 handler | `engine.TempManager`（超时自动清理） |
| 并发 | goroutine-per-event，无背压 | 事件引擎内置并发控制 + 工作池 |

**ZeroBot 的 Handler 注册模式：**
```go
engine.OnCommand("weather", zero.GroupOnly).Handle(func(ctx *zero.Ctx) {
    ctx.Send("...")
})
```

**Remilia 的 Handler 注册模式：**
```go
ctx.Reg.RegisterCommand(string(platform.EventKindGroupMessage), "/weather").
    Use(rateLimitMiddleware, tracingMiddleware).
    Handle(weatherHandler)
```

**差距**：路由层 Remilia 明显更强（Trie 树 + 统计 + 别名系统）；ZeroBot 的线性扫描对 100+ 命令的大型 Bot 存在性能瓶颈。

---

### 2.2 中间件系统

| 中间件 | ZeroBot | Remilia |
|--------|---------|---------|
| 限速 | 无内置（需手写） | `middleware.RateLimit`（令牌桶，`x/time/rate`）|
| 熔断 | 无 | `middleware.CircuitBreaker`（状态机，支持半开/探测）|
| 重试 | 无 | `middleware.Retry`（指数退避 + Jitter）|
| 去重 | 无 | `middleware.Dedup`（基于消息 fingerprint）|
| 降级 | 无 | `middleware.Degradation` |
| DLQ | 无 | `middleware.DeadLetter`（死信队列）|
| 权限检查 | `zero.OnlyAdmin`/`CheckSuperAdmin`（简单 bool） | `middleware.Permission`（RBAC `Resource:Action`）|
| 链路追踪 | 无 | `middleware.Tracing`（OTel Span）|
| Prometheus | 无 | `middleware.Prometheus`（命令维度指标）|
| 自适应 CPU | 无 | `middleware.AdaptiveCPU`（高负载自动丢弃）|

**结论**：中间件层 Remilia 全面领先，ZeroBot 没有企业级基础设施。

---

### 2.3 平台抽象

| 能力 | ZeroBot | Remilia |
|------|---------|---------|
| 支持平台 | 仅 QQ（OneBot/WebSocket） | QQ、Discord、Telegram、WeChat（均有 Adapter 实现）|
| 平台接口 | 硬编码 OneBot API | `platform.Adapter`（标准接口）|
| 消息发送 | `ctx.Send(CQ 码字符串)` | `platform.Sender.Send(SendRequest)` |
| 消息撤回 | `ctx.DeleteMessage(id)` | `platform.MessageDeleter`（可选接口）|
| 表情回应 | 无通用接口 | `platform.ReactionSender`（可选接口）|
| 群管理 | `ctx.SetGroupBan()` 等（QQ 专属） | `platform.GroupManager`（可选接口，跨平台）|
| 邀请处理 | 无 | `platform.InvitationHandler`（可选接口）|
| 能力声明 | 无 | `platform.Capabilities`（运行时特性探测）|
| 断连回调 | 无 | `platform.RecoverableAdapter.OnDisconnect` |

---

## 三、FloatTech 系列库逐一对比（核心）

### 3.1 FloatTech/zbpctrl → builtin/pluginctrl

这是最核心的框架层对比。zbpctrl 提供"逐群插件控制"的完整解决方案。

**zbpctrl 的核心模式（声明式注册）：**
```go
// 插件在包 init() 时声明自己的控制策略
var ctrl = control.Register("weather", &ctrl.Options{
    DisableInDefault: false,
    Brief:            "天气查询",
    Help:             "发送 /weather <城市> 查询天气",
    GroupLimit:       2 * time.Second,   // 群级冷却
    UserLimit:        10 * time.Second,  // 用户级冷却
    GlobalLimit:      0,                 // 全局冷却
})

engine.OnCommand("weather").Handle(func(ctx *zero.Ctx) {
    // ctrl.Handler(ctx) 自动完成：
    //   1. 检查本群是否开启了 "weather" 插件
    //   2. 检查群冷却 / 用户冷却
    if ctrl.Handler(ctx); !ctrl.Enabled(ctx.Event.GroupID) { return }
    // ...业务逻辑
})
```

**Remilia builtin/pluginctrl 的模式：**
```go
// 运行时注册，分两步
ctrl := plugin.Must[pluginctrl.Plugin](ctx, "pluginctrl")

ctx.Reg.RegisterCommand(groupEvent, "/weather").
    With(ctrl.Rule("weather")).  // 附加群级开关规则
    Handle(weatherHandler)
```

**功能对比：**

| 能力 | FloatTech/zbpctrl | Remilia/pluginctrl |
|------|-------------------|-------------------|
| 群级开启/关闭 | ✅ | ✅ |
| 全局开启/关闭 | ✅ | ✅ |
| 服务列表指令 | ✅ `/服务列表` | ✅ `/服务列表` |
| 持久化（SQLite）| ✅ | ✅（GORM）|
| **用户级开启/关闭** | ✅ 可禁用特定用户 | ❌ **缺失** |
| **注册时声明冷却策略** | ✅ `GroupLimit`/`UserLimit`/`GlobalLimit` | ❌ **缺失** |
| **统一控制点（群+冷却一体）** | ✅ `ctrl.Handler(ctx)` | ❌ 需手动组合 pluginctrl + cooldown |
| 声明式注册（init 时） | ✅ | ❌（需在 Setup 中手动调用）|

**差距小结**：Remilia 的 pluginctrl 已经覆盖了最核心的群级开关功能，但缺少两点：
1. **用户级禁用**（针对特定用户屏蔽某插件）
2. **注册时声明冷却策略**（现在需要分别使用 `pluginctrl` + `cooldown` 两个插件，且冷却在 pluginctrl 层不感知群级语义）

---

### 3.2 FloatTech/sqlite → infra/storage

| 能力 | FloatTech/sqlite | Remilia/infra/storage |
|------|------------------|-----------------------|
| 底层驱动 | mattn/go-sqlite3（CGO） | gorm.io/driver/sqlite（CGO，同款）|
| ORM 层 | 自制轻量封装 | 完整 GORM v2（更强大）|
| 接口抽象 | 无（直接暴露底层） | `storage.Client` 接口（可 mock，利于测试）|
| 其他数据库 | 仅 SQLite | SQLite + Postgres + MySQL（任意 GORM 驱动）|
| 内存模式 | 无 | ✅ `memory.go`（测试友好）|
| AutoMigrate | 无 | ✅ |
| 事务/关联 | 无 | ✅ `client.DB()` 逃生舱访问 `*gorm.DB` |

**结论**：Remilia 的存储层**明显优于** FloatTech/sqlite，接口设计更干净，功能更完整。

---

### 3.3 FloatTech/gg + rendercard → infra/textimage

这是差距最大的一个领域。

**FloatTech/gg（2D 矢量图形上下文）的核心能力：**
```go
dc := gg.NewContext(800, 600)
dc.DrawRoundedRectangle(10, 10, 200, 100, 15)   // 圆角矩形路径
dc.DrawCircle(100, 100, 50)                      // 圆形路径
dc.MoveTo(0, 0); dc.LineTo(100, 100)             // 任意路径
dc.CubicTo(cx1, cy1, cx2, cy2, ex, ey)          // 贝塞尔曲线
dc.SetLineDash(10, 5)                            // 虚线
dc.DrawRegularPolygon(6, 100, 100, 50, 0)        // 多边形
dc.Translate(100, 100); dc.Rotate(0.5); dc.Scale(2, 2) // 矩阵变换
dc.SetFillRuleEvenOdd()                          // 复合路径裁剪
dc.SavePNG("output.png")
// GIF 支持
```

**Remilia infra/textimage 已有能力：**
- ✅ 文字光栅渲染（TTF/OTF/TTC，字体缓存）
- ✅ 单词换行 + CJK 字符折行
- ✅ 背景图片（Stretch/Fill/Fit/Center/Tile）
- ✅ 文字阴影（硬边 + 高斯软边）
- ✅ 文字背景块（矩形/圆角/椭圆）
- ✅ Canvas 多块布局（文字块 + 图片块垂直拼合）
- ✅ 横向条形图（chart.go）
- ✅ Badge 徽章渲染（badge.go）
- ✅ 渐变填充（gradient.go）
- ✅ 高斯模糊效果（blur.go）
- ✅ 圆形/圆角裁剪图片（compose.go）

**Remilia infra/textimage 缺失能力（相比 gg）：**

| 缺失能力 | FloatTech/gg 实现方式 | 影响范围 |
|---------|----------------------|---------|
| **任意路径 / 矢量绘制** | `MoveTo` / `LineTo` / `CubicTo` / `ClosePath` | 无法画折线图、雷达图、自定义图形 |
| **变换矩阵** | `Translate` / `Rotate` / `Scale` / `Push/PopMatrix` | 无法做旋转文字、斜线图表 |
| **GIF 动图生成** | `gif.EncodeAll` + 帧序列 | 无法生成动态表情包、动态卡片 |
| **ClipPath / 复合裁剪** | `Clip` + 任意路径 | 无法实现复杂形状遮罩 |
| **折线图 / 雷达图** | gg 路径 + 循环 | 无法渲染时序数据可视化 |
| **自定义笔刷（虚线/点线）** | `SetLineDash` | 部分图表需要 |

**FloatTech/rendercard 的能力（基于 gg）：**
- 预制的"个人资料卡"、"排行榜卡"等卡片模板
- Remilia 无对应的预制模板包

**结论**：Remilia 的 textimage 对**文字和图片合成**场景覆盖较好，但对**矢量绘图**场景完全空白。制作折线图、自定义卡片边框、旋转元素、GIF 等需求无法满足。

---

### 3.4 FloatTech/ttl → 无对应（完全缺失 ⚠️）

**FloatTech/ttl 的核心功能：**
```go
// 泛型 TTL Map，每个键独立过期
pool := ttl.New[string, *UserSession](30 * time.Minute)
pool.Set("user-123", &UserSession{...})
val, ok := pool.Get("user-123")   // 超时后 ok=false
pool.Delete("user-123")
pool.GC()  // 手动触发垃圾回收
```

**FloatTech 生态中的典型用途：**
- 缓存第三方 API 响应（天气、翻译等，避免重复请求）
- 推送去重（同一内容 N 分钟内不重复推送）
- 短期会话状态（验证码有效期、多轮对话临时数据）
- 防刷票（某用户 X 分钟内的请求记录）

**Remilia 当前状态：**
- 项目已引入 `hashicorp/golang-lru/v2`（含 `Expirable` LRU），但：
  - Expirable LRU 是**容量触发淘汰**，并非纯 TTL 语义
  - 没有封装为 `infra/cache` 包暴露给插件开发者使用
  - `middleware/dedup.go` 有内部去重逻辑，但不对外开放
  - 没有统一的短期键值缓存工具

**影响**：插件开发者需要自己管理缓存，不同插件各自实现不同的 map+timer 模式，容易出现 goroutine 泄漏和内存增长问题。

---

### 3.5 FloatTech/floatfile → 无对应（完全缺失 ⚠️）

**FloatTech/floatfile 的核心功能：**
```go
// 懒加载：首次调用时从 URL 下载到本地，后续直接读本地
err := floatfile.DownloadTo(
    "https://cdn.example.com/wordlist.txt",
    "data/wordlist.txt",
    floatfile.SkipOriginal,   // 已存在则跳过下载
)
data, err := floatfile.ReadFile("data/wordlist.txt")
```

**典型用途：**
- 词库/模型文件（首次运行时按需下载）
- 图片素材资源（头像、背景图等，延迟拉取）
- 支持镜像加速 URL 降级
- 版本校验（可选）

**Remilia 当前状态：**
- `infra/` 目录下无任何文件资源管理工具
- 插件开发者若需要外部资源（如字体文件、词库），需自行管理下载逻辑
- **完全缺失**

---

### 3.6 FloatTech/zbputils → 部分有对应

zbputils 是一个杂项工具包，包含多个子模块：

| zbputils 子模块 | 功能 | Remilia 对应 |
|----------------|------|-------------|
| `control/web` | Web 管理 UI | ❌ 无 |
| `img/url` | 图片 URL 转 base64/binary | ❌ 无（需手写）|
| `html` | HTML 字符串 → 图片（无头浏览器） | ❌ **缺失** |
| `ctxext` | ctx 扩展（消息段解析、CQ 码处理） | ✅ `core/context` 有类似设计 |
| `math` | 概率计算、随机工具 | ❌ 无（通用工具，影响不大）|
| `process` | 消息中提取关键字段 | ✅ `platform.Event` 有完整字段 |

**最值得关注的缺失**：**HTML → 图片渲染**。zbputils 使用无头浏览器（chromedp）将 HTML 渲染为图片，这使 ZeroBot-Plugin 可以把复杂卡片设计为 HTML 模板，而不是手写像素坐标。Remilia 无此能力。

---

## 四、Remilia 的实际缺失清单（优先级排序）

### P0：需要立即补齐（框架基础设施层核心空白）

#### ① `infra/cache`：TTL Map 泛型工具

**现状**：完全缺失。  
**方案**：基于已引入的 `hashicorp/golang-lru/v2` 封装，或单独实现：

```go
// infra/cache/ttl.go（建议接口草案）
package cache

// TTLMap 是一个带 per-key 过期时间的泛型并发安全 Map。
// 与 LRU 的区别：不基于容量淘汰，仅依赖时间淘汰。
type TTLMap[K comparable, V any] interface {
    Set(key K, val V, ttl time.Duration)   // 设置键值及其 TTL
    Get(key K) (V, bool)                   // 获取；超时则返回 (zero, false)
    Delete(key K)
    Len() int                              // 当前有效条目数
    GC() int                               // 手动触发 GC，返回清理数量
}

// New 创建一个带后台 GC 的 TTL Map。
// gcInterval：后台 GC goroutine 的运行间隔（0 = 禁用后台 GC，需手动调用 GC()）。
func New[K comparable, V any](gcInterval time.Duration) TTLMap[K, V]
```

**注意**：`builtin/cooldown` 已有类似的 map+timer 内部实现，可以统一提取到 `infra/cache`。

---

#### ② `infra/fs`（或 `infra/resource`）：文件资源懒加载管理

**现状**：完全缺失。  
**方案**：

```go
// infra/fs/lazy.go（建议接口草案）
package fs

// LazyResource 按需下载并缓存本地文件资源。
type LazyResource struct {
    LocalPath string
    RemoteURL string
    // 可选：镜像 URL 降级列表（主 URL 失败时尝试）
    MirrorURLs []string
}

// Ensure 确保文件存在于 LocalPath。若已存在则直接返回，否则从 RemoteURL 下载。
func (r *LazyResource) Ensure(ctx context.Context) error

// Read 等价于 Ensure() + os.ReadFile(LocalPath)。
func (r *LazyResource) Read(ctx context.Context) ([]byte, error)
```

---

### P1：建议近期补齐（影响插件开发体验）

#### ③ `builtin/pluginctrl`：补充用户级禁用 + 注册时冷却声明

**用户级禁用扩展：**
```go
// 现有 API 扩展（向后兼容）
func (p *Plugin) SetUserEnabled(userID, pluginName string, enabled bool) error
func (p *Plugin) IsUserEnabled(userID, pluginName string) bool

// Rule 扩展（同时检查群级和用户级）
func (p *Plugin) RuleFull(pluginName string) eventctx.Rule  // group + user 双检
```

**注册时冷却声明（对标 zbpctrl `GroupLimit`/`UserLimit`）：**
```go
// pluginctrl.Option 扩展
func WithGroupCooldown(pluginName string, d time.Duration) Option
func WithUserCooldown(pluginName string, d time.Duration) Option

// ctrl.Rule 内部自动应用冷却检查
// 这样开发者只需：ctx.Reg.On(..., ctrl.Rule("weather")).Handle(h)
// 无需再单独引入 cooldown 插件
```

---

#### ④ `builtin/cooldown`：补充群级冷却维度

当前 cooldown 的 key 是 `userID:command`，只有用户维度。  
需要新增：
```go
// 群级冷却：同一群内该命令的共享冷却（如防止多用户轮流刷）
func (p *Plugin) AllowGroup(groupID, command string, d time.Duration) bool
func (p *Plugin) RemainingGroup(groupID, command string, d time.Duration) time.Duration
```

---

### P2：中期可考虑（提升图像处理能力）

#### ⑤ `infra/canvas`（可选）：引入 2D 矢量绘图能力

**方案 A（推荐）**：直接引入 `github.com/fogleman/gg` 作为可选依赖，并在 `infra/canvas/` 下封装一个适合 Bot 场景的高级 API：

```go
// infra/canvas/canvas.go
package canvas

// Canvas 是对 gg.Context 的 Bot 友好封装。
type Canvas struct { *gg.Context }

// NewCardCanvas 创建一个标准"通知卡片"尺寸的画布。
func NewCardCanvas(width, height int) *Canvas

// DrawAvatar 在指定位置绘制圆形头像（常用功能）。
func (c *Canvas) DrawAvatar(img image.Image, cx, cy, radius float64)

// DrawProgressBar 绘制进度条（常见于积分/等级卡）。
func (c *Canvas) DrawProgressBar(x, y, w, h, percent float64, fg, bg color.Color)

// SavePNG / ToPNG / ToBytesJPEG 输出方法
```

**方案 B**：在现有 `infra/textimage` 中新增 `VectorCanvas` 类型，内部持有 `*gg.Context`。

**GIF 支持**：`gg` 不直接支持 GIF，可单独封装 `infra/gif` 包（基于 `image/gif`）。

---

#### ⑥ `infra/webimage`（可选）：HTML → 图片渲染

如果目标用户有"用 HTML 设计卡片"的需求：
```go
// 方案：基于 chromedp 或 rod
package webimage

// Render 将 HTML 字符串渲染为 PNG 图片字节。
func Render(ctx context.Context, html string, opts ...Option) ([]byte, error)

// RenderURL 对指定 URL 截图。
func RenderURL(ctx context.Context, url string, opts ...Option) ([]byte, error)
```

**注意**：此方案依赖 Chromium 二进制，增加了部署重量，建议作为独立可选包，不纳入框架核心依赖。

---

## 五、Remilia 相比 ZeroBot+FloatTech 的核心优势

这些是 Remilia **明显领先**的地方，不应削弱：

| 优势点 | Remilia | ZeroBot+FloatTech |
|--------|---------|-------------------|
| **多平台支持** | QQ/Discord/Telegram/WeChat | 仅 QQ（OneBot）|
| **RBAC 权限系统** | 完整（Permission/Role/Manager，通配符，外部 Provider）| 简单 bool（IsAdmin/IsSuperAdmin）|
| **企业级中间件** | 熔断/限流/重试/DLQ/降级/自适应 CPU | 无内置（需插件自行实现）|
| **命令路由性能** | Trie O(log n)，支持别名/统计 | 线性扫描 O(n)|
| **链路追踪** | OpenTelemetry（OTLP HTTP 导出）| 无 |
| **Prometheus 指标** | `infra/metrics`，命令维度 | 无 |
| **配置热重载** | viper + fsnotify（`config/watcher.go`）| 无 |
| **生命周期管理** | 完整 Start/Stop/GC，插件依赖注入 | `init()` 模式，无生命周期 |
| **存储抽象** | GORM Client 接口，支持多数据库，可 mock | 仅 SQLite 直接访问 |
| **测试基础设施** | 完善测试套件（>90% 关键路径覆盖）| 测试覆盖率低 |
| **平台能力声明** | `platform.Capabilities`（运行时特性探测）| 无（硬编码 QQ 特性）|

---

## 六、综合评分对比

> 评分为 5 分制，针对**框架层能力**评估（不含应用层业务插件）。

| 维度 | ZeroBot+FloatTech | Remilia | 备注 |
|------|:-----------------:|:-------:|------|
| 多平台抽象 | ★☆☆☆☆ | ★★★★★ | Remilia 全面领先 |
| 命令路由性能 | ★★☆☆☆ | ★★★★☆ | Trie vs 线性扫描 |
| 中间件生态 | ★★☆☆☆ | ★★★★★ | 企业级 vs 无内置 |
| 插件开关管理 | ★★★★★ | ★★★★☆ | zbpctrl 声明式更简洁 |
| 持久化存储 | ★★★☆☆ | ★★★★☆ | Remilia GORM 更完整 |
| 权限系统 | ★★☆☆☆ | ★★★★★ | RBAC vs 简单 bool |
| 2D 图形渲染 | ★★★★☆ | ★★★☆☆ | gg 矢量能力 Remilia 缺失 |
| TTL 缓存工具 | ★★★★☆ | ★☆☆☆☆ | Remilia **完全缺失** |
| 文件懒加载 | ★★★☆☆ | ★☆☆☆☆ | Remilia **完全缺失** |
| HTML→图片 | ★★★☆☆ | ★☆☆☆☆ | Remilia 无此能力 |
| 可观测性 | ★☆☆☆☆ | ★★★★★ | Remilia 全面领先 |
| 配置热重载 | ★☆☆☆☆ | ★★★★★ | Remilia 全面领先 |
| 生命周期管理 | ★★☆☆☆ | ★★★★★ | Remilia 全面领先 |
| 测试覆盖率 | ★★☆☆☆ | ★★★★☆ | Remilia 明显更好 |

---

## 七、行动建议

```
P0（立即，不超过一周）：
  - infra/cache：实现 TTLMap[K,V] 泛型 TTL 缓存
    （可基于已有 hashicorp/lru Expirable 封装，或独立实现）
  - infra/fs：LazyResource 文件懒加载（仅需 ~100 行）

P1（近期，一个月内）：
  - builtin/pluginctrl：新增用户级禁用 API（UserEnabled）
  - builtin/cooldown：新增群级冷却维度（AllowGroup / RemainingGroup）
  - builtin/pluginctrl：WithGroupCooldown / WithUserCooldown 注册时声明冷却

P2（中期，可选）：
  - infra/canvas/：引入 fogleman/gg，封装 Bot 友好的矢量画布 API
  - infra/gif/：GIF 动图生成工具
  - infra/webimage/：HTML→PNG（可选重型依赖，独立包）
```

---

*本报告基于代码静态分析。ZeroBot/FloatTech 侧若有更新，请以实际仓库为准。*

