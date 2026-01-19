# Remilia 包结构对比图

## 当前结构（v0.7.x）

```
remilia/ (130 个 Go 文件, 638 KB)
│
├─ 核心引擎 (35 文件) ─────────────────┐
│  ├─ bot.go, bot_*.go                │
│  ├─ engine_core.go, engine_*.go     │  职责混杂
│  ├─ adapter.go, adapter_*.go        │  认知负担高
│  ├─ context.go, context_*.go        │  难以维护
│  └─ matcher.go, matcher_*.go        │
│                                      │
├─ 插件系统 (8 文件) ───────────────────┤
│  └─ plugin.go, plugin_*.go          │
│                                      │
├─ 权限系统 (4 文件) ───────────────────┤
│  └─ permission.go, permission_*.go  │
│                                      │
├─ 命令解析 (8 文件) ───────────────────┤
│  ├─ command_parser.go    ← 简单版   │
│  └─ command_enhanced.go  ← 增强版   │  ⚠️ 双轨制！
│                                      │
├─ 规则系统 (10 文件) ──────────────────┤
│  └─ rules.go, rules_*.go            │
│                                      │
├─ 错误处理 (7 文件) ───────────────────┤
│  ├─ errors.go                        │
│  ├─ handler_error.go                 │
│  └─ deadletter_queue.go              │
│                                      │
├─ 健康检查 (4 文件) ───────────────────┤
│  └─ health.go, health_*.go          │
│                                      │
├─ 指标监控 (4 文件) ───────────────────┤
│  └─ metrics.go, metrics_*.go        │
│                                      │
├─ 对象池 (5 文件) ─────────────────────┤
│  └─ pool.go, pool_*.go              │  ⚠️ 与 infra/ 重复
│                                      │
├─ 扩展系统 (4 文件) ───────────────────┤
│  └─ extensions.go, user_id_ext.go   │
│                                      │
└─ 其他 (37 文件) ──────────────────────┘
   └─ 测试、基准、工具等

问题:
❌ 130 文件在一个包中
❌ 200+ 公开 API 混杂
❌ 职责边界不清
❌ 新手难以上手
❌ 维护成本高
```

---

## 目标结构（v0.9.0+）

```
remilia/ (根包: 40 文件, 仅保留核心入口)
│
├─ bot.go              ← 主入口
├─ compat.go           ← 兼容别名
└─ doc.go              ← 包文档

└─ 子包结构:

    ┌─────────────────────────────────────────────────────┐
    │  L0: 用户入口层                                     │
    │  remilia/ - Bot 主入口 + 兼容层                     │
    └─────────────────────────────────────────────────────┘
                            │
                            ↓
    ┌─────────────────────────────────────────────────────┐
    │  L1: 功能模块层 (独立子包)                          │
    │                                                       │
    │  ┌──────────────┐  ┌──────────────┐                │
    │  │ plugin/      │  │ permission/  │  [P0-高优先级] │
    │  │ - plugin.go  │  │ - permission.go              │
    │  │ - manager.go │  │ - role.go    │                │
    │  └──────────────┘  └──────────────┘                │
    │                                                       │
    │  ┌──────────────┐  ┌──────────────┐  ┌───────────┐│
    │  │ command/     │  │ rules/       │  │ errors/   ││
    │  │ - parser.go  │  │ - rules.go   │  │ - errors.go││  [P1-中优先级]
    │  │ - enhanced.go│  │ - regex.go   │  │ - handler.go│
    │  └──────────────┘  └──────────────┘  └───────────┘│
    │                                                       │
    │  ┌──────────────┐  ┌──────────────┐                │
    │  │ deadletter/  │  │ middleware/  │                │
    │  │ - queue.go   │  │ (已存在)     │                │
    │  └──────────────┘  └──────────────┘                │
    └─────────────────────────────────────────────────────┘
                            │
                            ↓
    ┌─────────────────────────────────────────────────────┐
    │  L2: 核心引擎层                                     │
    │                                                       │
    │  ┌────────────────────────────────────────┐        │
    │  │ core/ (长期目标: P2)                   │        │
    │  │ - engine.go   - 事件路由               │        │
    │  │ - matcher.go  - 匹配器                 │        │
    │  │ - context.go  - 上下文                 │        │
    │  │ - adapter.go  - 适配器接口             │        │
    │  └────────────────────────────────────────┘        │
    └─────────────────────────────────────────────────────┘
                            │
                            ↓
    ┌─────────────────────────────────────────────────────┐
    │  L3: 基础设施层                                     │
    │                                                       │
    │  ┌──────────┐  ┌──────────┐  ┌──────────┐         │
    │  │ infra/   │  │ infra/   │  │ infra/   │         │
    │  │ pool/    │  │ metrics/ │  │ health/  │         │
    │  └──────────┘  └──────────┘  └──────────┘         │
    │                                                       │
    │  ┌──────────┐                                       │
    │  │ infra/   │                                       │
    │  │ dlq/     │                                       │
    │  └──────────┘                                       │
    └─────────────────────────────────────────────────────┘
                            │
                            ↓
    ┌─────────────────────────────────────────────────────┐
    │  L4: 外部适配层                                     │
    │                                                       │
    │  ┌──────────────┐  ┌──────────────┐                │
    │  │ openapi/     │  │ config/      │                │
    │  │ - QQ Bot API │  │ - 配置管理   │                │
    │  └──────────────┘  └──────────────┘                │
    └─────────────────────────────────────────────────────┘

优势:
✅ 职责单一，易于理解
✅ 依赖关系清晰
✅ 便于单元测试
✅ 降低认知负担
✅ 支持独立演进
```

---

## 迁移路径对比

### 阶段 1: P0 模块（1-2 个月）

```diff
  remilia/
  ├─ bot.go
  ├─ engine_core.go
  ├─ context.go
  ├─ matcher.go
+ ├─ compat.go          ← 新增: 兼容别名
  │
- ├─ permission.go      ← 移除: 迁移到子包
- ├─ permission_ext.go
- ├─ permission_test.go
  │
- ├─ plugin.go          ← 移除: 迁移到子包
- ├─ plugin_test.go
  │
- ├─ pool.go            ← 移除: 使用 infra/pool
- ├─ health.go          ← 移除: 使用 infra/health
- ├─ metrics.go         ← 移除: 使用 infra/metrics
  │
+ └─ permission/        ← 新增
+    ├─ permission.go
+    ├─ role.go
+    ├─ manager.go
+    └─ middleware.go
  │
+ └─ plugin/            ← 新增
     ├─ plugin.go
     ├─ base.go
     └─ manager.go

影响: 低 (用户代码无需修改)
风险: 低
收益: 减少 15 个文件
```

### 阶段 2: P1 模块（3-6 个月）

```diff
  remilia/
  ├─ bot.go
  ├─ engine_core.go
  ├─ compat.go
  │
- ├─ command_parser.go      ← 移除: 统一到 command/
- ├─ command_enhanced.go
  │
- ├─ rules.go               ← 移除: 迁移到 rules/
- ├─ rules_convenience.go
- ├─ rules_regex.go
  │
- ├─ errors.go              ← 部分移除: 拆分职责
- ├─ handler_error.go
  │
- ├─ deadletter_queue.go    ← 移除: 迁移到 deadletter/
- ├─ deadletter_consumers.go
  │
+ └─ command/               ← 新增: 统一命令系统
+    ├─ parser.go
+    ├─ enhanced.go
+    └─ definition.go
  │
+ └─ rules/                 ← 新增
+    ├─ rules.go
+    ├─ regex.go
+    └─ convenience.go
  │
+ └─ errors/                ← 新增
+    ├─ errors.go
+    └─ wrapper.go
  │
+ └─ deadletter/            ← 新增
     ├─ queue.go
     └─ consumer.go

影响: 中 (需要更新导入路径)
风险: 中
收益: 再减少 40 个文件
```

### 阶段 3: P2 核心（6-12 个月，可选）

```diff
  remilia/
  ├─ bot.go               ← 保留: 用户主入口
  ├─ compat.go            ← 保留: v1.0.0 兼容层
  │
- ├─ engine_core.go       ← 移除: 迁移到 core/
- ├─ engine_*.go
- ├─ matcher.go
- ├─ context.go
- ├─ adapter.go
  │
+ └─ core/                ← 新增: 核心引擎
     ├─ engine.go
     ├─ matcher.go
     ├─ context.go
     └─ adapter.go

影响: 极高 (所有用户代码)
风险: 高
收益: 架构清晰，适合 v1.0.0
时机: 需要社区反馈决策
```

---

## 依赖关系图

### 当前依赖（扁平化，易产生循环依赖）

```
     ┌─────────────────────────────┐
     │                             │
     │     remilia (单包)          │
     │                             │
     │  ┌─────┐   ┌─────┐         │
     │  │Engine├──►│Plugin│        │
     │  └─────┘   └──┬──┘         │
     │      ▲         │            │
     │      └─────────┘            │   ⚠️ 循环依赖风险
     │                             │
     │  ┌───────┐  ┌──────────┐   │
     │  │Context├─►│Permission│   │
     │  └───────┘  └──────────┘   │
     │                             │
     └─────────────────────────────┘
              │
              ↓
     ┌─────────────┐
     │  infra/*    │
     │  openapi/*  │
     └─────────────┘
```

### 目标依赖（分层清晰，避免循环）

```
┌────────────────────────┐
│   remilia (入口层)      │  用户只需导入这一个包
│   - Bot                │  其他包通过 re-export
└───────────┬────────────┘
            │
            ↓
┌────────────────────────────────────────┐
│         功能模块层 (L1)                 │
│                                         │
│  plugin/  permission/  command/  rules/ │
│                                         │
│        (互不依赖，独立演进)             │
└──────────────────┬──────────────────────┘
                   │
                   ↓
        ┌──────────────────┐
        │  核心层 (L2)      │  定义核心类型与接口
        │  core/           │  Engine, Matcher, Context
        └──────┬───────────┘
               │
               ↓
    ┌──────────────────────┐
    │  基础设施层 (L3)      │  提供底层能力
    │  infra/*             │  pool, metrics, health
    └──────┬───────────────┘
           │
           ↓
    ┌──────────────────────┐
    │  外部适配层 (L4)      │  与外部系统对接
    │  openapi/, config/   │  QQ API, 配置文件
    └──────────────────────┘

规则:
✅ 上层可以依赖下层
❌ 下层不能依赖上层
❌ 同层之间避免依赖
```

---

## 用户体验对比

### 当前体验（v0.7.x）

```go
// 导入根包
import "github.com/KomeiDiSanXian/remilia"

// 问题 1: IDE 自动补全列表过长 (200+ 项)
remilia. // ← 输入点号后看到海量函数
// Bot, Engine, Context, Matcher,
// Plugin, Permission, OnCommand, OnKeyword,
// pool, metrics, health, ...
// ❌ 难以找到需要的函数

// 问题 2: 命令解析双轨制
args1, _ := ctx.ParseCommand()           // 简单版
parser := remilia.NewCommandParser()      // 增强版
cmd2, _ := parser.Parse(ctx)
// ❌ 不知道该用哪个

// 问题 3: 职责混杂
perm := remilia.Permission{}             // 权限
plugin := remilia.NewBasePlugin("test")  // 插件
rule := remilia.OnCommand("/ping")       // 规则
// ❌ 感觉什么都能做，但边界不清
```

### 目标体验（v0.9.0+）

```go
// 方式 1: 导入根包（简单场景）
import "github.com/KomeiDiSanXian/remilia"

bot := remilia.New(info)
engine := bot.GetEngine()
// ✅ 核心功能简洁明了

// 方式 2: 按需导入子包（复杂场景）
import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/permission"
    "github.com/KomeiDiSanXian/remilia/plugin"
    "github.com/KomeiDiSanXian/remilia/command"
    "github.com/KomeiDiSanXian/remilia/rules"
)

// 清晰的模块边界
perm := permission.NewPermission("admin", "manage")  // 权限相关
mgr := permission.NewManager()

myPlugin := plugin.NewBase("weather")                // 插件相关

engine.On(rules.OnCommand("/ping"))                  // 规则相关

cmd, _ := command.Parse(ctx)                         // 命令相关
// ✅ 职责清晰，易于理解

// IDE 自动补全示例
permission. // ← 只看到权限相关的函数
// Permission, Role, Manager, RequirePermission

plugin. // ← 只看到插件相关的函数
// Plugin, BasePlugin, Manager

// ✅ 减少认知负担
```

### 兼容性保证

```go
// 旧代码无需修改（v0.8.0 - v0.9.0）
import "github.com/KomeiDiSanXian/remilia"

// 这些仍然可以工作（通过类型别名）
perm := remilia.Permission{}             // ✅ OK
plugin := remilia.NewBasePlugin("test")  // ✅ OK

// IDE 会提示 Deprecated 警告
// ⚠️  Warning: remilia.Permission is deprecated
//     Use permission.Permission instead

// v0.10.0 后会编译错误
// ❌ Error: undefined: remilia.Permission
//    Hint: Import "github.com/KomeiDiSanXian/remilia/permission"
```

---

## 文件数量对比

### 当前文件分布

```
remilia/ (130 文件)
├─ 核心引擎: 35 (27%)
├─ 插件系统: 8 (6%)
├─ 权限系统: 4 (3%)
├─ 命令解析: 8 (6%)
├─ 规则系统: 10 (8%)
├─ 错误处理: 7 (5%)
├─ 死信队列: 4 (3%)
├─ 健康/指标/池: 13 (10%)
├─ 扩展系统: 4 (3%)
└─ 其他测试: 37 (29%)

问题: 🔴 单包 130 文件，认知负担极高
```

### P0 完成后（预计）

```
remilia/ (40 文件) ← 减少 69%
├─ 核心引擎: 35
├─ compat.go: 1
├─ doc.go: 1
└─ 其他: 3

permission/ (4 文件)
plugin/ (8 文件)
infra/pool/ (已有)
infra/health/ (已有)
infra/metrics/ (已有)

改善: 🟢 Root 包减少到 40 文件
```

### P1 完成后（预计）

```
remilia/ (30 文件) ← 减少 77%
├─ 核心引擎: 25
├─ compat.go: 1
└─ 其他: 4

command/ (8 文件)
rules/ (10 文件)
errors/ (7 文件)
deadletter/ (4 文件)
permission/ (4 文件)
plugin/ (8 文件)

改善: 🟢 Root 包减少到 30 文件，职责清晰
```

---

## 总结

| 指标 | 当前 | P0 | P1 | P2 |
|------|------|----|----|-----|
| Root 包文件数 | 130 | 40 (-69%) | 30 (-77%) | 10 (-92%) |
| 子包数量 | 8 | 11 | 14 | 15 |
| 公开 API 数量 | 200+ | 80 | 60 | 40 |
| 认知负担 | 高 | 中 | 低 | 极低 |
| 时间成本 | - | 1-2月 | 3-6月 | 6-12月 |
| 破坏性变更 | - | 无 | 低 | 高 |

**推荐路线**: 
1. ✅ 立即执行 P0（低风险，快速见效）
2. ✅ 稳步推进 P1（解决历史债务）
3. ⏸️ P2 视情况决定（需社区反馈）

**关键成功因素**:
- 渐进式迁移（避免"大爆炸"）
- 类型别名兼容（用户代码无需修改）
- 充分测试（防止回归）
- 提供工具（自动迁移脚本）
- 清晰文档（迁移指南）

---

**相关文档**:
- [完整评估文档](./PACKAGE_REFACTORING_EVALUATION.md) - 详细技术方案
- [执行摘要](./PACKAGE_REFACTORING_SUMMARY_CN.md) - 5 分钟速览
