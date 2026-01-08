# 关键决策（轻量 ADR）

本文档用于记录 V2 渐进迁移过程中的关键决策。

> 形式：短、可执行、可追溯。重点记录“为什么”，以降低未来二次重构成本。

---

## D001 — Context V2 使用 typed-key 扩展存储（2A）

**状态**：已接受（Accepted）

**决策**

使用 typed-key 存储（`reflect.Type -> any`）作为底层机制，用于：

- 框架缓存（framework caches）
- 框架运行时元信息（trace / attempt 等）
- 用户态 state 存储（作为一种 extension）

**原因（Rationale）**

- 避免字符串 key 冲突。
- 允许每个功能使用私有缓存类型（feature-scoped cache types）。
- 避免导出 `InternalGet/InternalSet`，同时仍可实现跨包缓存。

**代价（Trade-offs）**

- 需要 `reflect` 或等价的 type-key 策略。
- 若缺少辅助工具，调试/观测略困难（后续可补 debug helper）。

---

## D002 — 保留 `ctx.Set/ctx.Get` 的易用性（1B），但底层通过 State 扩展实现

**状态**：已接受（Accepted）

**决策**

保留用户易用接口：

- `ctx.Set(key, value)`
- `ctx.Get(key)`
- `ctx.All()`

但其底层存储不直接挂在 `Context` 的字段上，而是通过 `State` 扩展实现。

**原因（Rationale）**

- 保持常见 middleware/handler 使用习惯。
- 让 `Context` 结构体保持小而稳定。

**代价（Trade-offs）**

- `Context` 仍然暴露“state 风格”的方法；我们认为这是可接受的稳定核心易用 API。

---

## D003 — 渐进迁移（临时保留 V1 适配层）

**状态**：已接受（Accepted）

**决策**

不使用 big-bang 一次性迁移，而是：

- 先引入 V2 基础原语
- 再逐步迁移内部缓存/元信息
- 再逐步迁移 middleware/rules
- 最后在全仓库完成迁移后移除 V1

**原因（Rationale）**

- 迁移期间持续保持 `go test ./...` 全绿。
- 将风险拆分为小 PR，降低 review 与回滚成本。

---

## D004 — Handler 类型统一

**状态**：提议中（Proposed）

**决策**

内部执行链统一使用 `HandlerE`（`func(*Context) error`）。

对“无 error handler”的使用体验，可选提供语法糖：

- `Handle(func(*Context))` 内部包装为 `HandleE`。

**原因（Rationale）**

- middleware、retry、DLQ、错误传播都是核心能力。
- 统一 handler 形态降低心智负担与适配成本。

**待确认点（Open points）**

- 是否保留 `Handler` type alias 作为 public sugar。
