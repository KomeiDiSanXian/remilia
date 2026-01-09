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
- 第一阶段不做大规模性能重写（尽量保持现有 Engine 的性能特征与行为一致）。

## 2. 当前问题（与 Context 相关）

- 当前 `Context` 承载了：
  - user state + internal state + 多个框架缓存
  - 一些可选能力（例如命令解析缓存）
- 长期风险：新增功能很容易“顺手就给 Context 加字段/方法”，导致持续膨胀。
- 导出 `InternalGet`/`InternalSet` 虽能解决跨包缓存，但会把内部细节泄露为公共 API，形成隐式耦合。

## 3. V2 模型提案（Proposed V2 model）

### 3.1 职责拆分

V2 中 `Context` 应尽量成为轻薄外壳，包含：

- 不可变事件输入：`event *dto.Payload`
- 标准库 context：`stdctx context.Context`
- API 客户端：`api openapi.OpenAPI`
- 执行元信息（可选）：当前 matcher 引用、source 等
- **扩展存储（extensions store）**：一个 typed-key 注册表，统一承载：
  - 用户态 state（map 风格）
  - 框架运行时元信息（retry attempt、middleware trace）
  - 特性缓存（parsed commands、tokenized command args 等）

### 3.2 扩展存储（typed-key）

`Extensions` 是一个并发安全容器：`reflect.Type -> any`。

最小原语：

- `Get[T any]() (T, bool)`
- `Set[T any](v T)`
- `GetOrInit[T any](init func() T) T`
- `Snapshot()` / `Clone()`（用于异步/retain 语义）

规则：

- 每个 `Context` 默认拥有自己的 `Extensions`。
- 每个 feature 使用私有类型作为 key，避免冲突与耦合。

### 3.3 用户态 State（1B）

保留 `ctx.Set/ctx.Get/ctx.All` 易用 API，但底层通过 V2 state extension 存储。

删除语义（与实现保持一致）：

- `ctx.Delete(key)`：删除 key
- `ctx.Set(key, nil)`：**等价于** `ctx.Delete(key)`（删除 key，而不是保存 nil 值）

示例：

```go
ctx.Set("k", "v")
_, _ = ctx.Get("k")

ctx.Set("k", nil) // 删除
_, ok := ctx.Get("k") // ok == false
```

### 3.4 Clone 语义（A：复制 typed extensions）

- `Context.Clone()` 会复制 typed extensions 的 snapshot。
- 对 V2 user state（`stateExt`）做深拷贝，保证 clone 后 `ctx.Set/ctx.Get` 的 store 不共享。
- 对引用类型 extension value（指针/map/slice）：复制的是引用；扩展值应视为 immutable（或自行保证并发安全）。

## 4. 渐进迁移计划（Progressive migration plan）

### Phase 0：基线

- 基线全绿：`go test ./...`
- 增加 V2 文档

### Phase 1：引入 V2 原语（不改变行为）

- 引入 `Extensions` / `ctx.Ext()`
- 引入 `ctx.Set/ctx.Get/ctx.All`
- 保留 V1 API 作为兼容层

### Phase 2：迁移框架内部字段/缓存到 typed extensions

- retry attempt / middleware trace / parsed command / command args cache
- 停止写 `_remilia_internal_*` 字符串 key（仅保留读 fallback）

### Phase 3：迁移 middleware/rules/helpers

- 仓库内部调用点统一迁到 `ctx.Set/ctx.Get` 或 typed extensions

### Phase 4：移除 V1 API

- 移除 `SetState/GetState/GetAllState/DeleteState`
- 移除 `InternalGet/InternalSet/InternalDelete`（在确认不再需要后）

## 5. 附：关键决策（ADR）

- 参考：`docs/refactor/DECISIONS.md`

