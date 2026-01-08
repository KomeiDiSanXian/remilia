# V2 Context 重设计（渐进迁移）

> 状态：草案（渐进迁移）
>
> 日期：2026-01-09
>
> 范围：Context + State/Cache + Extension 机制，以及对 Engine/Middleware/Rule 的最小触点改造。

## 0. 目标（Goals）

我们将进行一次 **破坏性重设计**（仓库尚未发布，允许不兼容改动），目标是：

1. 保持 `remilia.Context` **小而稳定**。
2. 将“可选能力”（optional capabilities）迁移到 `extension/*` 相关包。
3. 避免暴露内部 KV API（例如 `InternalGet/InternalSet` 这类接口）。
4. 保留良好易用性：选择 **1B** —— 用户代码依然可以使用 `ctx.Set(...)` / `ctx.Get(...)`。
5. 使用 **typed-key 扩展存储**：选择 **2A** —— 缓存/状态按“类型”存储，而不是字符串 key。
6. 支持 **渐进迁移 + 兼容层**：迁移期间保留旧接口，通过 adapter/shim 逐步替换全仓库使用点。

## 1. 非目标（Non-goals）

- 本阶段不重设计 OpenAPI、DTO 或 Adapter。
- 第一阶段不做大规模性能重写（要尽量保持现有 Engine 的性能特征与行为一致）。

## 2. 当前问题（与 Context 相关）

- 当前 `Context` 承载了：
  - user state + internal state + 多个框架缓存
  - 一些可选能力（例如命令解析缓存）
- 这使得后续新增功能非常容易“顺手就给 Context 加字段/方法”，长期会持续膨胀。
- 导出 `InternalGet`/`InternalSet` 可以解决跨包缓存，但会把内部细节泄露成公共 API（形成隐式耦合）。

## 3. V2 模型提案（Proposed V2 model）

### 3.1 职责拆分

V2 中 `Context` 应尽量成为一个轻薄外壳，包含：

- 不可变事件输入：`event *dto.Payload`
- 标准库 context：`stdctx context.Context`
- API 客户端：`api openapi.OpenAPI`
- 执行元信息（可选）：当前 `matcher` 引用、source 等
- **扩展存储（extensions store）**：一个 typed-key 注册表，统一承载：
  - 用户态 state（map 风格）
  - 框架运行时元信息（retry attempt、middleware trace）
  - 特性缓存（parsed commands、tokenized command args 等）

### 3.2 扩展存储（typed-key）

`Extensions` 是一个并发安全容器：`reflect.Type -> any`。

必须提供的基础原语：

- `Get[T any]() (T, bool)`
- `Set[T any](v T)`
- `GetOrInit[T any](init func() T) T`
- `Clone()` 或 `Snapshot()`（用于异步/retain 语义）

设计规则：

- 默认每个 `Context` 拥有自己的 `Extensions`。
- 每个功能使用私有存储类型，避免冲突与耦合。例如：
  - `type commandArgsCacheV2 struct { ... }`
  - `type middlewareTraceV2 struct { ... }`

因此，不再需要字符串 key。

### 3.3 用户态 State 作为一等扩展

我们保留 `ctx.Set/ctx.Get` 的易用接口，但其底层存储通过扩展实现：

- `ctx.Set(key, value)` 委托给内部的 `State` 扩展
- `ctx.Get(key)` 同样委托给 `State` 扩展

收益：

- 用户体验不变（1B）
- `Context` 结构体更轻
- state 的实现可替换（例如后续优化为 COW/分片锁/只读快照等）

### 3.4 可选能力放到 `extension/*`

示例：命令解析

- `extension.Command(ctx).ParseCommand()`
- 缓存写入 `Extensions` 中的私有类型

迁移期间，root-level 的 `ctx.ParseCommand()` 可暂时保留为兼容 shim，最终在 Phase 4 移除。

## 4. API 草图（示意）

> 实际签名会在实现阶段结合仓库现状确定。

```go
// Core
type Context struct {
    // ... event/std ctx/api ...
    ext *Extensions
}

func (c *Context) Ext() *Extensions

// 1B 语法糖（Sugar）
func (c *Context) Set(key string, value any)
func (c *Context) Get(key string) (any, bool)
func (c *Context) All() map[string]any

// Extensions
type Extensions struct { /* typed-key store */ }
func (e *Extensions) Get[T any]() (T, bool)
func (e *Extensions) Set[T any](v T)
func (e *Extensions) GetOrInit[T any](init func() T) T

// 可选能力（示例：命令）
package extension
func Command(ctx *remilia.Context) CommandExt
```

## 5. 渐进迁移计划（Progressive migration plan）

### Phase 0：基线

- 基线全绿：`go test ./...`。
- 增加 V2 文档。

### Phase 1：引入 V2 原语（不改变行为）

验收标准：

- 增加 `Extensions` typed store。
- 增加 `ctx.Ext()`。
- 增加 `ctx.Set/ctx.Get/ctx.All` 语法糖（底层通过 `State` 扩展实现）。
- 旧的 V1 API 暂时保留（用于兼容层）。
- 全量测试持续全绿。

### Phase 2：迁移框架内部字段/缓存到 typed extensions

目标：

- middleware trace
- retry attempt
- parsed command
- command args

验收标准：

- 新路径不再写 `_remilia_internal_*` 字符串 key。
- 不需要对外暴露 `InternalGet`/`InternalSet`。
- 用户视角行为保持一致（语义兼容）。

### Phase 3：迁移 middleware/rules 到 V2 API

目标：

- 目前仍使用 `SetState/GetState` 的中间件实现
- permission helpers
- rules convenience helpers

验收标准：

- 仓库内不再出现 `SetState/GetState` 调用。
- 测试用例全部更新。

### Phase 4：移除 V1 API

- 移除旧方法与兼容 wrapper。

验收标准：

- `Context` 不再暴露 V1 state/internalState 相关 API。
- `go test ./...` 全绿。

## 6. 设计约束与不变量（Design constraints and invariants）

- **并发不变量** 必须明确并测试：
  - 用户态 state 当前通过锁保证并发安全；V2 必须保持安全（或明确变更契约）。
- **异步使用** 必须有迁移路径：
  - 当前有 Clone/retain 指南；V2 需要明确 extensions 的 clone/snapshot 语义。
- **性能**：typed store 的查询必须足够快，并避免热路径的额外分配。
  - 第一版优先正确与清晰；后续再针对热点做优化。

## 7. 开放问题（Open questions）

1. `Context.Ext()` 应当惰性初始化还是总是分配？
   - 惰性：减少未使用时的分配
   - 直接分配：实现更简单

2. `Extensions` 是否需要额外 namespace（除了类型之外）？
   - typed-key 一般不需要

3. clone/snapshot 语义怎么定义？
   - 全量深拷贝（简单但可能贵）
   - 选择性拷贝或 COW（复杂但更高效）

4. Handler 类型是否只保留 `HandlerE`？
   - 建议：内部执行链统一使用 `HandlerE`
   - 可保留 `Handle(func(*Context))` 作为语法糖包装到 `HandleE`
