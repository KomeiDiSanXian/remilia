# refactor/context-v2 分支：渐进迁移与兼容层指南

> 日期：2026-01-09
>
> 目标读者：仓库贡献者（重构期的开发者与 Reviewer）。
>
> 背景：我们正在进行一次“破坏性重构”，但选择 **渐进迁移 + 兼容层** 的方式推进，确保任意时刻都能 `go test ./...` 通过，降低回归风险。

## 1. 分支约定

- 重构分支名：`refactor/context-v2`
- 原则：
  - 主分支（或默认分支）保持可用、可回滚。
  - `refactor/context-v2` 允许引入“新旧并存”的过渡代码，但必须有明确的移除计划。

## 2. 迁移策略总览（Progressive + Shim）

我们把迁移分为两条线并行推进：

1) **引入 V2 原语（不改变行为）**
- 目标：让新能力先“有地方放”，但不要求立刻搬完所有旧逻辑。
- 例：`Extensions` typed store、`ctx.Ext()`、`ctx.Set/Get/All`。

2) **逐步把内部能力从 V1 迁移到 V2**
- 目标：把“字符串 key + internalState/userState”逐步替换为“typed extensions”。
- 做法：
  - 写入：只写 V2
  - 读取：优先读 V2，读不到再 fallback 到 V1（保证兼容与回归安全）

当仓库内所有调用点都迁完后，再进入 Phase 4 清理 V1。

## 3. 兼容层（Shim）的设计规则

为了保证迁移可控，兼容层需要遵守以下规则：

### 3.1 读取优先级：V2 优先，V1 兜底

- 新逻辑一律使用 typed extensions
- 为了兼容历史数据路径，读取时允许：
  - 先查 V2（typed extension）
  - 若不存在，再查 V1（`internalState` / legacy key）

### 3.2 写入规则：只写 V2（避免“双写发散”）

- 迁移期严禁“V1/V2 双写”作为常态做法
- 原因：双写会让未来的回收难以验证一致性，并引入竞态与覆盖问题

### 3.3 V1 key 的处置原则

- `_remilia_internal_*`：框架保留 key，只允许旧代码读取兜底，不再新增写入点。
- `mw_trace` / `retry_attempt` 等历史 user-state key：
  - 统一归为“保留 key”，禁止用户态写入（避免用户与框架 key 冲突）。

## 4. 里程碑与验收标准

请以 `docs/refactor/MIGRATION_CHECKLIST.md` 为唯一进度来源。

每个阶段都必须满足：

- `go test ./...` 通过
- 新增 public API 或新行为必须补测试
- 引入 fallback 时必须补“含 fallback 的覆盖测试”，以保证回收时不会误删关键路径

## 5. 关于“删掉所有测试再重构”的决策

我们不采用“一刀切删除所有测试”，原因见 `docs/refactor/TEST_STRATEGY.md`。

允许的做法：

- 个别测试因设计变更短期不适配时：
  - 使用 `t.Skip("v2 migration")` 并写清 TODO
  - 在迁移清单中登记

不允许的做法：

- 整包跳过
- 默认关闭测试或把 CI 绿当作非目标

## 6. PR/Review 指南（重构期）

- 单次 PR 尽量只完成“一件事”（例如：迁移 retry attempt / 迁移 parsed command cache）
- 每一处迁移点至少包含：
  - 1 个 happy-path 测试
  - 1 个边界/回归测试（例如 fallback 读取、reserved key 禁止）

## 7. 常见问题（FAQ）

### Q1：为什么不直接把 `InternalGet/InternalSet` 当成长期 API？

因为它会把“内部缓存结构”暴露为公共契约，未来等同于背上兼容包袱。
V2 的 typed extensions 能让跨包扩展继续存在，但不会把 key 设计泄露给用户。

### Q2：什么时候可以删掉 V1 state/internalState？

当满足以下条件：

- 仓库内代码不再写入任何 `_remilia_internal_*` key
- 仓库内所有读路径都已迁移到 typed extensions（无 fallback 依赖或 fallback 可删）
- 测试证明回收不会破坏行为

然后进入 Phase 4 清理。

