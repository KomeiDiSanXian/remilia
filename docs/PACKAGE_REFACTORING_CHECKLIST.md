# Remilia 包拆分行动清单

> **目标**: 渐进式拆分 root 包，降低代码复杂度  
> **时间**: 1-6 个月（分阶段执行）  
> **负责人**: [待分配]

---

## 📋 Phase 0: 准备阶段（第 1 周）

### 基础设施准备

- [ ] **评审文档**
  - [ ] 团队会议讨论 `PACKAGE_REFACTORING_EVALUATION.md`
  - [ ] 确认优先级（P0 → P1 → P2）
  - [ ] 分配模块负责人

- [ ] **创建 GitHub Project**
  - [ ] 里程碑: v0.8.0 (P0 拆分)
  - [ ] 里程碑: v0.9.0 (P1 拆分)
  - [ ] 创建 Issue 追踪每个模块

- [ ] **设置 CI 检查**
  - [ ] 添加 golangci-lint 规则：禁止新增 root 包公开类型
  - [ ] 添加包依赖检查（防止循环依赖）
  - [ ] 添加兼容性测试套件

- [ ] **准备工具**
  - [ ] 编写自动迁移脚本 (`tools/migrate/`)
  - [ ] 编写废弃 API 检测脚本 (`tools/check_deprecated.sh`)
  - [ ] 准备基准测试对比工具

- [ ] **通知社区**
  - [ ] 发布 GitHub Discussion: "RFC: 包结构重构计划"
  - [ ] 收集社区反馈（2 周）
  - [ ] 更新 ROADMAP.md

---

## 🎯 Phase 1: P0 模块拆分（第 2-4 周）

### 模块 1: permission/ 包

**负责人**: [待分配]  
**预计工作量**: 5 人日

- [ ] **第 1 天: 创建新包**
  - [ ] 创建目录 `remilia/permission/`
  - [ ] 复制文件:
    - `permission.go` → `permission/permission.go`
    - `permission_ext.go` → `permission/ext.go`
    - `permission_middleware.go` → `permission/middleware.go`
    - `permission_test.go` → `permission/permission_test.go`
  - [ ] 修改 package 声明为 `package permission`
  - [ ] 修复内部导入路径

- [ ] **第 2 天: 添加兼容层**
  - [ ] 创建 `remilia/compat_permission.go`
  - [ ] 添加类型别名:
    ```go
    // Deprecated: 使用 permission.Permission
    type Permission = permission.Permission
    
    // Deprecated: 使用 permission.Role
    type Role = permission.Role
    ```
  - [ ] 添加函数包装器:
    ```go
    // Deprecated: 使用 permission.NewRole
    func NewRole(name string, perms ...Permission) *Role {
        return permission.NewRole(name, perms...)
    }
    ```

- [ ] **第 3 天: 测试与验证**
  - [ ] 运行所有测试: `go test ./...`
  - [ ] 运行基准测试: `go test -bench=. ./permission/...`
  - [ ] 兼容性测试（旧 API 仍可用）
  - [ ] 检查 godoc 生成

- [ ] **第 4 天: 文档更新**
  - [ ] 更新 README.md 示例
  - [ ] 创建 `permission/README.md`
  - [ ] 更新 API 文档
  - [ ] 添加迁移说明到 `MIGRATION_GUIDE.md`

- [ ] **第 5 天: Review & Merge**
  - [ ] 提交 PR: "refactor: extract permission package"
  - [ ] Code Review
  - [ ] Merge to main

---

### 模块 2: plugin/ 包

**负责人**: [待分配]  
**预计工作量**: 10 人日

- [ ] **第 1-2 天: 接口设计**
  - [ ] 定义 `MatcherCoordinator` 接口
    ```go
    type MatcherCoordinator interface {
        AddMatcher(m *Matcher)
        DeleteMatcher(m *Matcher)
        UpdateMatcherIndex()
        SetMatcherGroup(m *Matcher, group, source string)
    }
    ```
  - [ ] Engine 实现该接口
  - [ ] 编写接口测试

- [ ] **第 3-4 天: 创建新包**
  - [ ] 创建 `remilia/plugin/` 目录
  - [ ] 移动文件:
    - `plugin.go` → `plugin/plugin.go`
    - 8 个测试文件
  - [ ] 修改依赖:
    - `*Engine` → `MatcherCoordinator`
  - [ ] 修复编译错误

- [ ] **第 5-6 天: 添加兼容层**
  - [ ] 创建 `remilia/compat_plugin.go`
  - [ ] 添加类型别名
  - [ ] 添加适配器（如需要）

- [ ] **第 7-8 天: 测试**
  - [ ] 单元测试
  - [ ] 插件生命周期测试
  - [ ] 依赖解析测试
  - [ ] 性能测试（AddMatcher 写放大问题）

- [ ] **第 9 天: 文档**
  - [ ] 插件开发指南
  - [ ] API 文档
  - [ ] 迁移说明

- [ ] **第 10 天: Review**
  - [ ] PR + Review
  - [ ] Merge

---

### 模块 3: 清理 infra compat

**负责人**: [待分配]  
**预计工作量**: 3 人日

- [ ] **第 1 天: 移除重复文件**
  - [ ] 删除 `remilia/pool.go`
  - [ ] 删除 `remilia/health.go`
  - [ ] 删除 `remilia/metrics.go`
  - [ ] 删除 `remilia/infra_compat.go`

- [ ] **第 2 天: 更新调用方**
  - [ ] 全局搜索替换:
    - `remilia.NewPool()` → `pool.New()`
    - `remilia.HealthCheck` → `health.Check`
    - `remilia.MetricsCollector` → `metrics.Collector`
  - [ ] 更新导入语句

- [ ] **第 3 天: 测试 & PR**
  - [ ] 运行所有测试
  - [ ] 提交 PR: "cleanup: remove infra compat layer"

---

### Phase 1 总结

- [ ] **发布 v0.8.0-beta**
  - [ ] Tag: `v0.8.0-beta.1`
  - [ ] Release Notes
  - [ ] 社区公告

- [ ] **收集反馈**
  - [ ] 监控 GitHub Issues
  - [ ] 社区讨论区
  - [ ] 记录迁移问题

- [ ] **正式发布 v0.8.0**
  - [ ] 修复 beta 期间发现的问题
  - [ ] Tag: `v0.8.0`
  - [ ] 更新文档网站

---

## 🚀 Phase 2: P1 模块拆分（第 5-12 周）

### 模块 4: command/ 包（统一双轨）

**负责人**: [待分配]  
**预计工作量**: 15 人日

- [ ] **决策: 选择主线**
  - [ ] 团队投票（基础解析 vs 增强系统）
  - [ ] 推荐: 增强系统（功能更强大）

- [ ] **统一实现**
  - [ ] 移动 `command/` 子包到顶层
  - [ ] 废弃 `command_parser.go`
  - [ ] 统一入口函数

- [ ] **提供迁移工具**
  - [ ] 自动重写工具:
    ```bash
    go run tools/migrate/command.go --path ./...
    ```
  - [ ] 测试迁移工具

- [ ] **文档**
  - [ ] 命令系统使用指南
  - [ ] 迁移指南（详细步骤）

- [ ] **测试 & Review**

---

### 模块 5: rules/ 包

**负责人**: [待分配]  
**预计工作量**: 8 人日

- [ ] **创建新包**
  - [ ] 移动 10 个 rules 文件到 `remilia/rules/`

- [ ] **Re-export 到 root 包**
  - [ ] 创建 `remilia/rules_compat.go`
  - [ ] 导出所有规则函数:
    ```go
    var (
        OnCommand = rules.OnCommand
        OnKeyword = rules.OnKeyword
        // ...
    )
    ```

- [ ] **性能测试**
  - [ ] 规则匹配基准测试
  - [ ] 确保无性能回归

- [ ] **文档 & PR**

---

### 模块 6: errors/ 包

**负责人**: [待分配]  
**预计工作量**: 6 人日

- [ ] **拆分职责**
  - [ ] `errors/errors.go` - 通用错误工具
  - [ ] `errors/handler.go` - HandlerError/BlockError（保留在 root？）
  - [ ] `errors/wrapper.go` - 包装器

- [ ] **决策: HandlerError 是否迁移**
  - [ ] 评估影响范围
  - [ ] 如果影响大，保留在 root 包

- [ ] **测试 & PR**

---

### 模块 7: deadletter/ 包

**负责人**: [待分配]  
**预计工作量**: 5 人日

- [ ] **创建新包**
  - [ ] 移动到 `remilia/deadletter/`

- [ ] **统一 retry attempt key**
  - [ ] 修复 `retry_attempt` vs `retry_attempts` 不一致
  - [ ] 统一使用 `_remilia_internal_retry_attempt`

- [ ] **测试 & PR**

---

### Phase 2 总结

- [ ] **发布 v0.9.0-beta**
  - [ ] Tag: `v0.9.0-beta.1`
  - [ ] 标记旧 API 为 Deprecated
  - [ ] 所有示例使用新 API

- [ ] **正式发布 v0.9.0**
  - [ ] Tag: `v0.9.0`
  - [ ] 更新所有文档

---

## 🔄 Phase 3: P2 核心拆分（第 6-12 个月，可选）

### 评估阶段

- [ ] **社区调研**
  - [ ] 发起讨论: "是否拆分 core 包？"
  - [ ] 收集使用数据（哪些 API 最常用）

- [ ] **内部试验**
  - [ ] 在 `internal/core` 试验新设计
  - [ ] 性能测试
  - [ ] API 易用性测试

- [ ] **决策**
  - [ ] Go/No-Go 决策会议
  - [ ] 如果 Go: 规划 v1.0.0

### 实施阶段（如果决定执行）

- [ ] **设计 v1.0.0 API**
  - [ ] 重新设计 Engine 公开方法
  - [ ] 重新设计 Context 接口
  - [ ] 减少公开类型数量

- [ ] **迁移实现**
  - [ ] 创建 `remilia/core/` 包
  - [ ] 移动核心类型
  - [ ] Root 包保留兼容层

- [ ] **发布 v1.0.0**
  - [ ] 破坏性变更公告
  - [ ] 详细迁移指南
  - [ ] 提供自动迁移工具

---

## 📊 进度追踪

### 里程碑

| 里程碑 | 目标日期 | 状态 | 负责人 |
|--------|----------|------|--------|
| Phase 0 完成 | Week 1 | ⏳ 待开始 | - |
| permission/ 完成 | Week 2 | ⏳ 待开始 | - |
| plugin/ 完成 | Week 3 | ⏳ 待开始 | - |
| infra cleanup 完成 | Week 4 | ⏳ 待开始 | - |
| v0.8.0-beta 发布 | Week 4 | ⏳ 待开始 | - |
| v0.8.0 正式发布 | Week 5 | ⏳ 待开始 | - |
| command/ 完成 | Week 8 | ⏳ 待开始 | - |
| rules/ 完成 | Week 10 | ⏳ 待开始 | - |
| errors/ 完成 | Week 11 | ⏳ 待开始 | - |
| v0.9.0 发布 | Week 12 | ⏳ 待开始 | - |

### 关键指标

| 指标 | 基线 | 目标 | 当前 |
|------|------|------|------|
| Root 包文件数 | 130 | 40 | - |
| Root 包代码量 | 638 KB | 200 KB | - |
| 公开 API 数量 | 200+ | 80 | - |
| 单元测试通过率 | 100% | 100% | - |
| 基准测试性能 | 100% | ≥95% | - |

---

## 🚨 风险与应对

### 技术风险

| 风险 | 应对措施 | 负责人 |
|------|----------|--------|
| 循环依赖 | 提前绘制依赖图；使用接口解耦 | - |
| 性能回退 | 每次迁移运行基准测试；设置阈值 | - |
| 编译失败 | 分阶段提交；CI 自动检查 | - |

### 业务风险

| 风险 | 应对措施 | 负责人 |
|------|----------|--------|
| 用户代码破坏 | 类型别名兼容；2-3 版本过渡期 | - |
| 插件生态中断 | 提前 3 个月通知；提供迁移工具 | - |
| 社区负面反馈 | 开放讨论；收集反馈；及时调整 | - |

---

## 📝 每周例会议程

### 会议信息

- **频率**: 每周一次（建议周五下午）
- **时长**: 30 分钟
- **参会人**: 核心团队 + 模块负责人

### 议程模板

1. **进度回顾** (10 分钟)
   - 上周完成的任务
   - 遇到的问题
   - 解决方案

2. **本周计划** (10 分钟)
   - 本周目标
   - 任务分配
   - 里程碑确认

3. **风险讨论** (5 分钟)
   - 新发现的风险
   - 应对措施

4. **社区反馈** (5 分钟)
   - GitHub Issues
   - Discussion 讨论
   - 用户反馈

---

## 🎉 成功标准

### Phase 1 (P0) 成功标准

- ✅ Root 包文件数减少到 40 个以下
- ✅ permission/ 和 plugin/ 包独立运行
- ✅ 所有单元测试通过
- ✅ 基准测试性能无回退 (≥95%)
- ✅ 旧代码无需修改即可运行
- ✅ 文档完整（迁移指南 + API 文档）
- ✅ 社区反馈积极

### Phase 2 (P1) 成功标准

- ✅ Root 包文件数减少到 30 个以下
- ✅ 命令系统双轨制问题解决
- ✅ 职责边界清晰（6 个独立子包）
- ✅ 迁移工具可用
- ✅ 90% 用户顺利迁移

### Phase 3 (P2) 成功标准（如果执行）

- ✅ v1.0.0 API 稳定
- ✅ 架构清晰，易于理解
- ✅ 社区满意度 > 80%

---

## 📚 参考资源

### 内部文档

- [完整评估文档](./PACKAGE_REFACTORING_EVALUATION.md)
- [执行摘要](./PACKAGE_REFACTORING_SUMMARY_CN.md)
- [结构对比图](./PACKAGE_STRUCTURE_COMPARISON.md)
- [架构评审](./ARCH_REVIEW.md)

### 工具

- `tools/migrate/` - 自动迁移工具
- `tools/check_deprecated.sh` - 废弃 API 检测
- `tools/draw_deps.sh` - 依赖关系可视化

### 外部参考

- [Go Package Layout](https://github.com/golang-standards/project-layout)
- [Kubernetes 包拆分历史](https://github.com/kubernetes/kubernetes/tree/master/staging)
- [Prometheus Client Go](https://github.com/prometheus/client_golang)

---

## 📞 联系方式

- **项目负责人**: [姓名]
- **技术负责人**: [姓名]
- **社区联络人**: [姓名]

---

**下一步行动**: 
1. ✅ 分配模块负责人
2. ✅ 确认第一次会议时间
3. ✅ 开始 Phase 0 准备工作

**预计完成时间**: 
- Phase 1 (P0): 1-2 个月
- Phase 2 (P1): 3-6 个月
- Phase 3 (P2): 可选，视反馈而定

---

*最后更新: 2026-01-19*
