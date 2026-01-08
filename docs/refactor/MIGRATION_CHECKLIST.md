# 迁移清单（V2）

> 目标：跟踪 V2 渐进迁移进度。
>
> 规则：每一步都必须保持 `go test ./...` 全绿。

## Phase 0 — 基线

- [x] 基线构建与测试通过
- [x] 增加 V2 文档骨架

## Phase 1 — V2 原语（不改变行为）

- [ ] 增加 `Extensions` typed-key store
- [ ] 增加 `Context.Ext()` 访问器
- [ ] 基于 State 扩展实现 `ctx.Set/ctx.Get/ctx.All` 语法糖
- [ ] 增加 Extensions store 的单测（Get/Set/GetOrInit、并发）

## Phase 2 — 迁移内部缓存/元信息

### Context 内部缓存/元信息

- [ ] Retry attempt：迁移为 typed extension
- [ ] Middleware trace：迁移为 typed extension
- [ ] Parsed command cache：迁移为 typed extension
- [ ] Command args cache：迁移为 typed extension（并更新 `extension.Command` 路径）

### 删除基于字符串 key 的内部缓存

- [ ] 新代码路径停止写入 `_remilia_internal_*` 字符串 key
- [ ] 从 `Context` 移除 `InternalGet/InternalSet/InternalDelete`（当不再需要时）

## Phase 3 — 迁移 middleware/rules/helpers

- [ ] 将 `middleware/` 包迁移到 `ctx.Set/ctx.Get`（若仍存在 V1 依赖）或直接使用 extension
- [ ] 迁移权限相关 helper（`permission.go`、`permission_middleware.go`）
- [ ] 迁移规则便利函数（`rules_convenience.go`）

## Phase 4 — 移除 V1 API

- [ ] 移除 `SetState/GetState/GetAllState/DeleteState`（V1）
- [ ] 移除遗留的 V1-only 测试
- [ ] 更新文档与示例

## Phase 5 — 可选后续

- [ ] 评估将 root 包职责拆分为子包（core/plugin/permission）
- [ ] 评估 Engine 引入 `Start()` 生命周期拆分（减少 `NewEngine()` 副作用）
