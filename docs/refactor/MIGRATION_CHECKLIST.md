# 迁移清单（V2）

> 目标：跟踪 V2 渐进迁移进度。
>
> 规则：每一步都必须保持 `go test ./...` 全绿。

## Phase 0 — 基线

- [x] 基线构建与测试通过
- [x] 增加 V2 文档骨架

## Phase 1 — V2 原语（不改变行为）

- [x] 增加 `Extensions` typed-key store（`extensions.go`）
- [x] 增加 `Context.Ext()` 访问器（`context.go`）
- [x] 基于 State 扩展实现 `ctx.Set/ctx.Get/ctx.All` 语法糖（`context.go` + `context_v2_state_test.go`）
- [x] 增加 Extensions store 的单测（Get/Set/GetOrInit、并发）（`extensions_test.go`）

## Phase 2 — 迁移内部缓存/元信息

### Context 内部缓存/元信息

- [x] Retry attempt：迁移为 typed extension（`retryAttemptExt`）
- [x] Middleware trace：迁移为 typed extension（`middlewareTraceExt`）
- [x] Parsed command cache：迁移为 typed extension（`parsedCommandExt`）
- [x] Command args cache：迁移为 typed extension（停止写 `_remilia_internal_command_args`；`command_args_v2.go` + 调用方更新）

### 清理基于字符串 key 的内部缓存

- [x] 仓库内停止写入 `_remilia_internal_*` 字符串 key（仅保留读取 fallback，直到 Phase 4）
  - 说明：目前仓库内仅剩 `_remilia_internal_*` 的“常量/测试/读 fallback”引用，不再有新的写入点。
- [x] 评估并移除 `InternalGet/InternalSet/InternalDelete`（当不再需要时）
  - 说明：仓库内未发现 `InternalGet/InternalSet/InternalDelete` 符号引用。

### Phase 2 补充核对（legacy/兼容层）

- [x] V1 State API 曾通过 build tag 隔离（`remilia_legacy`）
  - 说明：已在 Phase 4 中彻底移除，不再支持 `remilia_legacy`。
- [x] `_remilia_internal_command_args` 相关遗留引用进一步清理
  - 说明：默认构建中已不再出现该 key 的字面量引用；仅保留 legacy/tag 文件中的旧常量与 `command_args_v2.go` 中的迁移常量（用于测试与迁移期校验）。

## Phase 3 — 迁移 middleware/rules/helpers

- [ ] 将 `middleware/` 包全部迁移到 `ctx.Set/ctx.Get` 或直接使用 typed extension（按模块逐个推进）
  - 进展：已将 middleware 内常用 ctx user-state key（`request_id`/`user_id`）集中到 `middleware/context_keys.go`，并替换了 `middleware.go`/对应测试中的散落字符串。
  - `degraded` 已升级为 typed extension（`middleware.DegradedExt`），写入点使用 `middleware.SetDegraded(ctx)`，读取建议使用 `middleware.IsDegraded(ctx)`；迁移期仍会兼容写入/读取 user-state key `degraded`。
  - `user_id` 已新增 typed extension（`remilia.UserIDExt`），并在 `GetUserID(ctx)` 中实现 typed 优先（再 fallback 到 user-state key `"user_id"`，最后读 event author）。
    - 写入建议：测试/新代码优先用 `ctx.SetUserID(...)`；迁移期可同时写入 `ctx.Set("user_id", ...)`。

### Phase 3 子项核对

- [x] 权限相关：PermissionManager typed extension（`PermissionManagerExt`）已落地，测试用例不再依赖 `"permission_manager"` 字符串注入
- [x] 规则便利：`rules_convenience_test.go` 已迁移到 `ctx.SetPermissionManager(...)` + `ctx.SetUserID(...)`
- [x] middleware：`degraded` 已迁移为 typed extension，并补充 `degraded_ext_test.go`
- [x] `GetUserID(ctx)` 路径已 typed 优先（`UserIDExt`）

## Phase 4 — 移除 V1 API

- [x] 移除 `SetState/GetState/GetAllState/DeleteState`（V1）
- [x] 移除遗留的 V1-only 测试与兼容逻辑（含 `remilia_legacy` build tag 文件）
- [x] 更新文档与示例（不再提及 `-tags remilia_legacy`）

## Phase 5 — 可选后续（超出 Context 范畴）

- [ ] 评估将 root 包职责拆分为子包（core/plugin/permission 等）
- [ ] 评估 Engine 引入 `Start()` 生命周期拆分（减少 `NewEngine()` 副作用）
