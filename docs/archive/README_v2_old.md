# Remilia 文档中心

欢迎来到 Remilia 文档中心！这里包含了所有你需要的文档和指南。

> **当前版本**: v1.2.0  
> **最后更新**: 2025-12-02

---

## 🚀 快速导航

### 新手入门
- **[快速开始](QUICKSTART.md)** - 5 分钟快速上手 ⭐
- **[完整指南](GUIDE.md)** - 详细的使用教程
- **[示例代码](../example/)** - 实用的示例程序

### 最新特性（v1.2.0）
- **[命令权限系统](PERMISSION.md)** - 完整的 RBAC 权限管理 🆕

### 核心功能
- [中间件系统](MIDDLEWARE.md) - 包括超时、限流、重试等通用控制
- [插件系统](PLUGIN.md) - 模块化开发
- [配置管理](CONFIG.md) - 灵活的配置系统
- [指标收集](METRICS.md) - Prometheus 集成
- [错误处理](ERROR_HANDLING.md) - 标准化错误管理

> 提示：早期版本中曾提供 `Context` 内置超时控制（`WithTimeout/WithDeadline/WithCancel` 等），
> 该功能已在当前版本中 **移除**，推荐使用 `middleware.Timeout` 或标准库 `context.Context` 实现超时/取消。
> 详情参考 [BREAKING_CHANGE_CONTEXT_REFACTOR.md](BREAKING_CHANGE_CONTEXT_REFACTOR.md)。

---

## 📚 完整文档列表

### 入门文档

| 文档 | 说明 |
|------|------|
| [QUICKSTART.md](QUICKSTART.md) | 5 分钟快速上手指南 |
| [GUIDE.md](GUIDE.md) | 完整使用指南 |
| [INDEX.md](INDEX.md) | 文档索引 |

### 功能文档

| 文档 | 说明 | 版本 |
|------|------|------|
| [PERMISSION.md](PERMISSION.md) | 命令权限系统（RBAC）| v1.2.0 ⭐ |
| [PLUGIN.md](PLUGIN.md) | 插件系统完整指南 | v1.2.0 |
| [METRICS.md](METRICS.md) | 指标收集与监控 | v1.2.0 |
| [ERROR_HANDLING.md](ERROR_HANDLING.md) | 错误处理机制 | v1.2.0 |
| [CONFIG.md](CONFIG.md) | 配置系统详解 | v1.2.0 |

> 历史文档：早期曾有单独的 `CONTEXT_TIMEOUT.md` 等文档，详细介绍 `Context` 内置超时控制；
> 该特性已下线，这类文档仅保留在历史分支中作为参考，不再代表当前推荐实践。

### 开发者文档

| 文档 | 说明 | 版本 |
|------|------|------|
| [COMPONENT_ANALYSIS_2025_12_02.md](COMPONENT_ANALYSIS_2025_12_02.md) | 核心组件分析与问题修复总结 | v1.2.0+ 🆕 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 架构设计说明 | v1.2.0+ |
| [MIDDLEWARE.md](MIDDLEWARE.md) | 中间件系统 | v1.0.0+ |
| [BREAKING_CHANGE_CONTEXT_REFACTOR.md](BREAKING_CHANGE_CONTEXT_REFACTOR.md) | Context 超时重构破坏性变更说明 | v0.9.0 |

> 说明：原有文档索引中提到的 `COMPONENT_ANALYSIS_SUMMARY.md` 和 `COMPONENT_ANALYSIS.md`
> 已在整理过程中合并为 `COMPONENT_ANALYSIS_2025_12_02.md`，当前仅保留这一份汇总分析文档。

### 架构与优化

| 文档 | 说明 |
|------|------|
| [ARCHITECTURE.md](ARCHITECTURE.md) | 系统架构设计 |
| [PERFORMANCE.md](PERFORMANCE.md) | 性能测试与优化指南 |
| [BATCH_PROCESSING.md](BATCH_PROCESSING.md) | 批量事件处理 |

> 说明：文档索引中原提及的 `OBJECT_POOL.md` 目前已合并到性能与组件分析类文档中，
> 关于对象池的实现和优化，请参考 [PERFORMANCE.md](PERFORMANCE.md) 与 [COMPONENT_ANALYSIS_2025_12_02.md](COMPONENT_ANALYSIS_2025_12_02.md) 中的相关章节。

### 版本历史

| 文档 | 说明 |
|------|------|
| [CHANGELOG.md](CHANGELOG.md) | 完整更新日志 ⭐ |

### 技术修复记录（精选）

下列文档主要用于记录问题排查与修复过程，属于“内部/历史分析文档”，
对理解设计演进有帮助，但不一定代表当前对外推荐实践：

| 文档 | 说明 |
|------|------|
| [CONCURRENCY_LIMIT_FIX_REPORT.md](CONCURRENCY_LIMIT_FIX_REPORT.md) | 并发限制修复报告 |
| [RELOAD_ATOMICITY_FIX_REPORT.md](RELOAD_ATOMICITY_FIX_REPORT.md) | 配置热重载原子性修复报告 |
| [INVOKE_HANDLER_ERROR_FIX_REPORT.md](INVOKE_HANDLER_ERROR_FIX_REPORT.md) | 处理器错误传播修复报告 |
| [MATCH_BLOCKING_ANALYSIS.md](MATCH_BLOCKING_ANALYSIS.md) | 匹配阻塞问题分析 |
| [MATCH_BLOCKING_SOLUTION_SUMMARY.md](MATCH_BLOCKING_SOLUTION_SUMMARY.md) | 匹配阻塞解决方案总结 |
| [FIX_CONTEXT_GOROUTINE_LEAK.md](FIX_CONTEXT_GOROUTINE_LEAK.md) | Context goroutine 泄漏分析与修复方案 |

> 说明：原有 `V080_RACE_FIXES.md`、`V080_MUTEX_FIX.md` 等 v0.8.0 阶段的细粒度修复文档
> 已在整理过程中并入综合报告（例如 `COMPONENT_ANALYSIS_2025_12_02.md`），当前不再作为独立文件保持在主分支。

---

## 🎯 常见使用场景

### 场景 1: 我想开始使用 Remilia
```
1. 阅读 QUICKSTART.md
2. 查看 example/ 目录的示例
3. 参考 GUIDE.md 了解更多细节
```

### 场景 2: 我想实现权限控制
```
1. 阅读 PERMISSION.md 了解权限系统
2. 查看 example/webhook/permission_demo/ 示例
3. 参考 GUIDE.md 中的中间件部分
```

### 场景 3: 我想优化性能
```
1. 阅读 PERFORMANCE.md 了解优化策略
2. 参考 COMPONENT_ANALYSIS_2025_12_02.md 中的性能相关章节
3. 使用 realistic_bench.bat / bench_all.bat 运行基准测试
```

### 场景 4: 我想开发插件
```
1. 阅读 PLUGIN.md 了解插件系统
2. 查看 example/plugins/example_plugins.go
3. 参考 GUIDE.md 中的插件章节
```

### 场景 5: 我想添加监控
```
1. 阅读 METRICS.md 了解指标系统
2. 查看 CONFIG.md 配置 Prometheus
3. 参考示例程序中的监控配置
```

---

## 📊 文档质量

| 指标 | 数值 |
|------|------|
| 文档总数（当前仓库） | 20+ 篇 |
| 代码示例 | 50+ 个 |
| 测试覆盖率 | 90%+ |
| 更新频率 | 持续更新 |

---

## 🤝 贡献文档

发现文档问题？欢迎提交 PR 改进文档！

1. Fork 项目
2. 修改文档
3. 提交 Pull Request

---

**Remilia - 企业级 QQ Bot 开发框架**

*让 Bot 开发更简单、更强大、更可靠*

---

**最后更新**: 2025-12-02  
**当前版本**: v1.2.0  
**文档维护**: [GitHub](https://github.com/KomeiDiSanXian/remilia)
