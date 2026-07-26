# 24 — Bot 装配层：从 Engine 到进程的最后一公里

> `core/engine` 回答的是"一个事件如何被匹配和执行"，但一个能上线的机器人还差很多：
> 平台连接怎么建立和断开、插件何时 Setup/Teardown、收到 Ctrl+C 之后按什么顺序收尾、
> 健康检查暴露在哪里、pprof 何时启动。根包 `remilia` 的
> `Bot` / `BotBuilder` / `BotManager`（bot.go、bot_builder.go、bot_manager.go）
> 就是这最后一公里的**装配层**——它几乎不发明新机制，只负责把
> engine、platform.Registry、plugin.Manager、router、health、pprof
> 按正确的顺序拼起来，然后把生命周期整体委托给 `lifecycle.Manager`。
>
> 本文是 docs/notes 系列最后一块拼图：05 讲了 lifecycle 的通用模型，
> 本文讲这个模型在 Bot 上的具体用法和踩过的坑。

## Bot 的定位：只装配，不发明

```go
type Bot struct {
    engine           *engine.Engine
    lifecycle        *lifecycle.Manager     // 生命周期完全委托
    health           *health.Check
    config           *BotMeta               // Name/Version/Debug（区别于 config.Config 全量配置）
    pluginManager    *plugin.Manager
    platformRegistry *platform.Registry     // 唯一事件来源（D3）
    router           *router.Router         // 可选策略路由层

    adapterSnapshot  infraatomic.Value[map[string]adapterCache]  // P-1 热路径零锁
    pprofServer      *PprofServer
    started          atomic.Bool            // 防重复 Start
    syncAdapters     map[string]syncAdapterEntry  // SyncPlatforms 热替换登记簿
}
```

值得注意的是**没有**的东西：Bot 没有自己的事件循环、没有自己的 goroutine 池、
没有自己的状态机。事件处理在 engine，启停排序在 lifecycle，连接管理在各 adapter。
Bot 唯一的"业务逻辑"是 `handlePlatformEvent` 这一个函数。

## D3 决策：platformRegistry 是唯一事件来源

早期版本同时维护 `adapter` 单字段和 `platformRegistry` 两条路径，
每个用到适配器的地方都要写 `if b.adapter != nil { ... } else { registry... }`。
D3 重构统一为：**一切适配器都进 Registry**。

- `NewBot(adapter, eng)` 收到非 nil 的单适配器时，自动创建 Registry 并注册进去；
- `BotBuilder.Build()` 同样把 `WithPlatformAdapter` 的单适配器合并进 registry 再传给 `NewBot(nil, ...)`；
- `BotBuilder.WithPlatformRegistry(r)` 有**迁移合并**语义：先前通过
  `WithPlatformAdapter` 设置的适配器、以及先前的 registry 中的所有适配器，
  都会被迁移到新 registry，避免"后设置的覆盖先设置的"这种静默丢失。

单平台与多平台在 Bot 内部完全同构，双路径分支从此消失。

## P-1：adapterSnapshot——热路径零锁的又一次应用

`handlePlatformEvent` 每条事件都要根据 `event.Platform()` 找到发送器和能力声明。
如果每次都对 Registry 加 RLock，高吞吐下锁竞争会回来。解法与 COW 引擎同一哲学：

```go
// Start() 时构建一次快照
snapshot := make(map[string]adapterCache)   // {adapter, caps}
for _, pa := range reg.All() { ... }
b.adapterSnapshot.Store(snapshot)

// 热路径只需一次 atomic Load，零锁
snapshot := b.adapterSnapshot.Load()
c, ok := snapshot[event.Platform()]
```

一个容易踩的坑藏在快照结构里：缓存的是 `adapter` 引用而**不是** `adapter.Sender()`
的返回值。因为快照构建发生在 `pa.Start()` 之前，此时不少适配器的 Sender 还是
`NoopSender`（client 未初始化）。事件到达时动态调用 `c.adapter.Sender()`，
才能拿到 Start 之后的真实发送器。**快照缓存不变的部分（caps），动态解引用可变的部分（sender）**。

## handlePlatformEvent：事件入口的完整职责

```
event 到达（adapter 回调）
  ├── nil 检查
  ├── snapshot.Load() → sender / caps / botID / botName（一次 map 查找全拿到）
  │     └── 适配器实现了 platform.BotIdentity 才有 botID
  │         （没实现时 ctx.IsFromSelf() 永远 false，Debug 模式会提示）
  ├── sender 缺失 → 落到 NoopSender + Warn（Reply 静默丢弃，但不崩）
  ├── eventctx.NewContextFromEvent(event, sender)
  │     └── SetBotID / SetBotName / SetPlatformCapabilities
  └── b.router != nil ? router.Dispatch(ctx) : engine.ProcessEvent(ctx)
```

Capabilities 在这里注入（F2），Handler 里 `ctx.GetPlatformCapabilities()`
才能做"该平台支不支持图片"之类的能力探测。

## 注册顺序即关闭顺序

`Start()` 向 lifecycle 注册组件的顺序是精心设计的：

```
注册顺序：engine → platform:xxx（每个适配器一个组件）→ plugin-manager
停止顺序（逆序）：plugin-manager → platform adapters → engine
```

插件 Teardown 排在最前面执行，此时平台连接还没断、lifecycle 的 parentCtx 还有效——
插件可以在退场前发"我下线了"的告别消息。这正是 05 篇里双层 Context
（parentCtx / runCtx）设计的直接受益者：runCtx 先取消通知后台 goroutine 退出，
parentCtx 撑到所有 OnStop 完成才取消。

## 热重启的三个防叠加

`Restart()`（Stop + Start）暴露过一类"累积型"bug，现在 `Start()` 里有三道防线：

1. **`started.CompareAndSwap(false, true)`**——重复 Start 直接 no-op。
   不能依赖 `lifecycle.State()` 判断，因为下一条防线每次都会重建 Manager；
2. **每次 Start 重建 lifecycle.Manager**——否则第二次 Start 会把组件重复注册一遍；
3. **每次 Start 重建 health.Check（B-5）**——否则健康检查器叠加，同一个 checker
   注册 N 份。副作用是插件注册的探针（APIProbe 等）也被清掉了，
   所以 `Restart()` 最后要调 `discoverProbes()` 从插件容器里重新发现
   `health.CheckProvider` 并补注册。

## SyncPlatforms：平台热替换

v1.14.1 起支持不重启进程增删平台。输入是期望状态 `desired map[string]platform.Adapter`，
框架做三分支 diff：

| 分支 | 动作 |
|------|------|
| 仅在 desired | Register + 启动 |
| 两边都有 | 停旧（10s 超时）→ `reg.Replace` → 启新 |
| 仅在当前注册表 | 停止 + `reg.Remove` |

热替换启动的适配器**不进 lifecycle**（lifecycle 组件集在 Start 时已定格），
而是登记在 `syncAdapters map[string]syncAdapterEntry{cancel, wg, done}` 里，
goroutine 由 `bot.Context()`（parentCtx）派生的 context 管理。
`Bot.Stop()` 里 `stopAllSyncAdapters` 的收尾顺序是经典的三段式：
先全部 cancel → 再等全部 done → 最后逐个调 `adapter.Stop(ctx)` 清理。
diff 完成后 `rebuildAdapterSnapshot` 重建 P-1 快照，热路径下一次 Load 就看到新集合。

这也是 config.example.yaml 里 bot 配置节标 `[H⚠]` 的由来：换 Token/增删平台走
SyncPlatforms 零停机；换 webhook 监听端口属于网络层，仍需重启。

## WaitForShutdown：进程级单例监听

信号监听是**进程级**资源——两个组件同时 `signal.Notify` 会各收到一份信号、
各自发起一次 Stop。Bot 和 BotManager 的 WaitForShutdown 共用一个全局
`atomic.Bool` 守卫：

```go
if !shutdownListenerActive.CompareAndSwap(false, true) {
    logger.Warn("...already active; this call is a no-op...")
    return
}
defer releaseShutdownListener()
```

第二个调用者打 Warn 后直接返回。多 Bot 场景的正确用法是只调
`BotManager.WaitForShutdown`。

另一处对齐 CLI 惯例的细节：`sigCh` 缓冲为 2，第一个 SIGINT 开始优雅关闭，
优雅关闭期间的第二个 SIGINT 立即 `os.Exit(1)` 强制退出——
"Ctrl+C 两次强杀"和大多数命令行工具一致。

## ShutdownAsync：同步 Stop 会死锁的场景

`Stop()` 是同步的，但有三类调用点不能阻塞等它返回：

- HTTP handler 里收到 `/shutdown` 请求——同步 Stop 会先把 HTTP 服务器关了，响应发不出去；
- **插件回调内部触发关闭**——Stop 会走到 plugin-manager 的 Teardown，而 Teardown
  在等这个回调返回，lifecycle 链上直接死锁;
- 嵌入外部框架时对方不允许阻塞其事件循环。

`ShutdownAsync()` 返回 `<-chan error`（缓冲 1），后台 goroutine 执行 Stop 并写入结果，
调用方自选 fire-and-forget 还是 `<-ch` 等待。Bot 和 BotManager 都提供同名方法。

## BotBuilder：把装配次序固化成 API

Builder 的价值不在链式语法糖，而在 `Build()` 里固化的次序和校验：

1. 至少一个事件来源（adapter 或 registry），否则 `ErrAdapterRequired`；
2. 没有外部 Engine 时用 `WithEngineOptions` 收集的选项创建默认 Engine
   （注意：`WithEngine` 传入外部实例后 EngineOptions 被忽略——实例已完成初始化）；
3. `WithPlugins` 收集的描述符批量注册：`RegisterBatch(ctx, plugins, WithInferDeps())`，
   依赖自动拓扑排序——正是 19 篇三色标记 + DryRun 依赖推断的消费方；
4. 单适配器合并进 registry（D3），`NewBot(nil, ...)` + `UsePlatformRegistry`。

防御性细节：`WithEngine(nil)` 不会存下 nil，而是打 Warn 并当作没调用过——
Build 时照常创建默认 Engine，把一次潜在的空指针 panic 变成一条日志。

## BotManager：多实例编排与聚合错误

同进程跑多个 Bot（测试号 + 生产号、灰度发布）时，BotManager 提供统一编排：

- `bots map[string]*Bot` + `order []string` 双结构：map 查找，slice 保证
  StartAll/StopAll 的遍历顺序与注册顺序一致（可预期性）；
- StartAll/StopAll **并发**执行且互不阻断——某个 Bot 失败不妨碍其他 Bot 启停，
  最后聚合为 `BotManagerError{Op, Errors []BotError}`，
  支持 `errors.As` 下钻到具体哪个 Bot 失败；
- `HealthAll()` 并发收集所有 Bot 的健康检查；`Status()` 给出 Running/Stopped 摘要；
- `BotManagerBuilder` 支持 `AddBot`（现成实例）与 `AddBuilder`（延迟构建）混用，
  且收集**所有** Add 阶段的错误（`errors.Join`）而不是只报第一个。

## 健康检查树：装配层的自我报告

Bot 在 health.Check 上注册的检查器带层级路径，最终形成一棵树：

```
system/
├── bots/<name>/
│   ├── lifecycle   ← BotStatusChecker（lifecycle 非 Running → Degraded）
│   ├── engine      ← health.NewEngineHealthChecker
│   └── adapters/<platform>  ← AdapterHealthChecker
└── infrastructure/runtime   ← RuntimeChecker（goroutine 阈值，WithGoroutineThreshold）
```

`AdapterHealthChecker` 靠接口探测充实 metadata：`RecoverableAdapter`（支持重连）、
`BotIdentity`（bot_id/bot_name）、`HealthDetailer`（平台自定义细节，不覆盖已有键）。

`DLQHealthAdapter` 是一个教科书式的依赖倒置：infra/health 不应该依赖 infra/dlq，
所以 health 只定义 `DLQStats` 接口，bot 层提供适配器把 `dlq.Queue[T]` 的
Stats 转成 health 的快照类型——方向变成 bot → 两者，两个 infra 包互不相知。

## PprofServer：可观测性的收尾件

- **显式注册路由**（`netpprof.Index/Profile/...`），不 import `_ "net/http/pprof"`——
  init 副作用会把 pprof 挂到 DefaultServeMux 上，即使没启用也暴露端点；
- `net.Listen` 先行拿到实际地址，支持 `Addr: ":0"` 随机端口 + `ListenAddr()` 查询——
  测试并行跑多个实例不冲突;
- **`stopOnce` 防 double-close**：stopCh 只在 New 时创建，Restart 场景下
  Stop 会被调用两次，无保护的 `close(stopCh)` 第二次直接
  panic("close of closed channel")，在优雅停机途中崩溃并跳过插件 Teardown——
  这是修过的真实缺陷；
- `AddHandler(path, h)` 允许把 /health 等管理端点挂到同一个端口；
- autoProfile 周期采集 CPU/heap/goroutine（+ 可选 mutex/block），CPU 采集用
  `select { <-timer.C; <-p.stopCh }` 而非裸 Sleep，Stop 时可提前中止；
- `UpdateConfig` 支持热更新采样参数（AutoProfile/Interval/Duration/Mutex/Block），
  但 Enabled 和 Addr 不行——这与 CONFIGURATION_QUICKREF 里 pprof 节的
  [H]/[R] 标注一一对应；
- `WithPprof(cfg)` Option 把服务器生命周期绑到 Bot：Start 失败会回滚整个 lifecycle。

## 模式清单

| 模式 | 在装配层的体现 |
|------|----------------|
| 委托而非发明 | 启停排序给 lifecycle，事件处理给 engine，Bot 只拼装 |
| 快照 + 动态解引用 | adapterSnapshot 缓存 caps，事件时才调 Sender() |
| 进程级资源用全局 CAS 守卫 | shutdownListenerActive 单例信号监听 |
| 幂等 Stop | started.Store(false) + pprof stopOnce + StopAll 可重入 |
| 期望状态 diff | SyncPlatforms 的三分支（add/replace/remove） |
| 聚合错误 + errors.As 下钻 | BotError / BotManagerError |
| 接口探测充实元数据 | AdapterHealthChecker 对 BotIdentity/HealthDetailer 的类型断言 |
| 依赖倒置解耦 infra 包 | DLQHealthAdapter |

装配层看似"没有技术含量"，但它是所有跨组件时序 bug 的第一现场：
重复注册、双重关闭、信号竞争、Teardown 时序……这些问题没有一个出在算法上，
全部出在"谁先谁后、谁负责关谁"。把次序固化进 Builder 和 lifecycle 注册顺序，
比在每个调用点写注释提醒有效得多。
