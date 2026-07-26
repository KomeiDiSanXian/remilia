# 23 — Context 设计：双键扩展、克隆语义与延迟副作用

> `*context.Context` 是框架里被触碰最频繁的类型——每个事件一个实例，穿过规则、
> 中间件、handler、FSM 和插件。它的设计目标是三件事：**热路径零开销**、
> **异步安全可克隆**、**框架状态与用户状态互不踩踏**。

## 每事件新鲜分配

Context 曾经用 `sync.Pool` 复用，后来去池化——池化要求"归还时机"可证明，
而 handler 可以把 ctx 交给任意 goroutine（定时器、Future 回调），
use-after-free 的排查成本远高于每事件 ~272B/3 allocs 的分配成本。
如今的原则：**新鲜分配 + 惰性初始化**（扩展容器、消息内容都在首次访问时才建）。

## 双键状态系统

Context 提供两套完全隔离的键值存储：

| | 字符串键 | 类型键（typed extensions） |
|---|---------|--------------------------|
| API | `ctx.Set("k", v)` / `ctx.Get("k")` | `ExtSet(ctx.Ext(), v)` / `ExtGet[T](ctx.Ext())` |
| 键 | string | `reflect.Type`（由泛型参数隐式给出） |
| 使用者 | 插件 / handler | 框架组件（parsedCommand、retryMetadata、middlewareTrace…） |
| 冲突面 | 保留键黑名单拦截（`mw_trace` 等） | 类型即键，编译期唯一 |

隔离是硬保证：两套系统底层是不同的 map，`ctx.Set("parsed_command", v)` 永远
碰不到框架经 `ExtSet` 存的 `parsedCommand`。框架侧选类型键的动机：零字符串
分配、编译期检查、且**插件作者无法拼写出框架的键**。

热路径字段不进任何 map：`GetMessageContent()` 用 `sync.Once` 缓存解析结果——
六路匹配时几十个规则读同一条消息内容，只解析一次。

## Reply 是异步的

```go
f := ctx.Reply(platform.TextMessage("pong"))  // 入队即返回 Future
res, err := f.Wait(ctx.Context())             // 可选：同步等待发送结果
```

Reply 把发送任务交给 [`OutboundDispatcher`](21-outbound-dispatcher.md)，
handler 返回后发送仍会继续。忽略返回值是合法用法；需要 MessageID 或
错误处理时才 Wait。

## Clone：为异步执行而生

matcher 被判定入池时（见 [`22-adaptive-execution.md`](22-adaptive-execution.md)），
池 goroutine 不能与派发循环共用一个 ctx——`SetMatcher` 是两个字长的接口写入
（可撕裂），中间件的 `SetStdContext` 会互相覆盖 deadline/span。`Clone()` 的语义
是为"接下来独立执行"精确定制的：

- **保留 deadline**：父 ctx 有超时则克隆同 deadline 的独立 context；
- **不继承取消**：父 ctx 被 cancel 不影响克隆（异步任务不该被派发循环的
  生命周期株连）；克隆自带 `Cancel()`，不调用也不泄漏；
- **传递 trace span**：克隆保持在同一条追踪链上；
- **扩展拷贝规则**：字符串键 map 深拷贝（用户状态彼此隔离），
  类型键条目浅拷贝（框架数据如 parsedCommand 按不可变约定共享指针）。

## 延迟副作用：DeferRuleEffect（2026-07 新增）

规则应当是纯函数，但有些规则天然想写状态——`OnCooldown` 通过检查后需要
记录时间戳。直接写会产生两类误扣：同 matcher 后续规则失败（冷却被白白消耗）、
真正处理事件的是另一个 matcher。机制化的解法是三段协议：

```go
// 规则内：登记而不执行
ctx.DeferRuleEffect(func() { cooldownStore.Add(key, now) })

// 引擎在 processEventMatchers 中：
if !m.Match(ctx) { ctx.DiscardPendingRuleEffects(); continue }
ctx.CommitPendingRuleEffects()   // 全部规则通过，才提交副作用
```

副作用在"matcher 确认命中"这一刻原子生效，规则顺序不再影响正确性。
约束：只有引擎匹配路径会提交——在 FSM 的 `Event.Match` 里直接调用带副作用的
规则，登记的副作用不会执行。字段是普通切片（单 goroutine 的 Match 周期内
使用），提交发生在克隆/入池之前，Clone 不拷贝它。

## 能力探测：Try* 方法族

平台能力差异用"可选接口 + 类型断言"表达而非大一统接口：

```go
func (ctx *Context) TryDeleteMessage(id string) error {
    deleter, ok := ctx.platformSender.(platform.MessageDeleter)
    if !ok { return nil }   // 平台不支持 → 静默忽略
    return deleter.Delete(ctx.Context(), chatID, id)
}
```

`TryGetGroupMembers`、`TryMuteMessageAuthor`、`TrySendTyping`……全部遵循
"不支持则空操作"的渐进增强约定，插件无需 per-platform 分支。
配合 `GetPlatformCapabilities()`（Engine 注入的能力并集）可做功能降级。

## 设计权衡

- **为什么不用 `context.Context` 携带一切？** 标准 context 的 Value 链是
  O(深度) 查找且无类型安全；框架状态读写在热路径上，需要 O(1) 与零断言。
  标准 context 仍保留（`ctx.Context()`），只承担 deadline/trace 传播。
- **为什么 SetMatcher 等注入方法不加锁？** 单事件的派发循环是单 goroutine，
  跨 goroutine 的场景被 Clone 隔离——用所有权约定替代锁，是热路径零开销的前提。
