# Remilia 文档索引

> 最后更新: 2025-12-07 | 版本: v1.2.1+ | 状态: ✅ 核心功能生产就绪  
> 📦 文档已优化精简，内部报告已归档到 [archive/](archive/)

---

## 📚 文档导航

### 🚀 快速开始

| 文档 | 描述 | 适合人群 |
|------|------|---------|
| [README](README.md) | 项目介绍和特性概览 | 所有人 |
| [快速开始](QUICKSTART.md) | 5分钟快速上手指南 | 新用户 |
| [使用指南](GUIDE.md) | 完整的使用说明和示例 | 所有用户 |

---

### 🏗️ 核心概念

| 文档 | 描述 | 内容 |
|------|------|------|
| [架构说明](ARCHITECTURE.md) | 系统架构和设计原则 | 三层架构、核心组件、设计理念 |
| [中间件系统](MIDDLEWARE.md) | 中间件使用和开发 | 12+ 内置中间件、自定义中间件 |
| [插件系统](PLUGIN.md) | 插件开发和管理 | 插件接口、依赖管理、热重载 |
| [Context 使用指南](CONTEXT_USAGE_GUIDE.md) | Context 详细使用说明 | 引用计数、状态管理、最佳实践 |

---

### ⚙️ 功能特性

| 文档 | 描述 | 内容 |
|------|------|------|
| [配置说明](CONFIG.md) | 配置文件详解 | YAML 配置、热重载、最佳实践 |
| [错误处理](ERROR_HANDLING.md) | 错误处理和重试 | 错误恢复、重试策略、死信队列 |
| [权限系统](PERMISSION.md) | 权限验证和鉴权 | 权限中间件、规则定义 |
| [指标收集](METRICS.md) | 监控和指标 | Prometheus 集成、自定义指标 |
| [批量处理](BATCH_PROCESSING.md) | 批量事件处理 | 批量 API、性能优化 |

---

### ⚡ 性能与优化

| 文档 | 描述 | 内容 |
|------|------|------|
| [性能优化](PERFORMANCE.md) | 性能测试和优化指南 | 对象池、索引优化、批量处理 |
| [性能分析指南](PERFORMANCE_ANALYSIS.md) | pprof 使用和性能调优 | CPU/内存分析、优化案例 |
| [规则最佳实践](RULE_BEST_PRACTICES.md) | 规则编写最佳实践 | 纯函数、性能优化 |

---

### 🔬 高级功能

| 文档 | 描述 | 内容 |
|------|------|------|
| [分布式追踪集成](TRACING_INTEGRATION.md) | OpenTelemetry 集成指南 | 追踪中间件、最佳实践 |

---

### 📝 项目信息

| 文档 | 描述 | 内容 |
|------|------|------|
| [变更日志](CHANGELOG.md) | 版本历史和变更记录 | 各版本的新特性和修复 |
| [归档文档](archive/) | 内部开发文档归档 | 修复报告、分析文档等 |

---

## 🎯 按角色导航

### 👶 新手用户

推荐阅读顺序：
1. [README](README.md) - 了解项目
2. [快速开始](QUICKSTART.md) - 5分钟上手
3. [使用指南](GUIDE.md) - 深入学习

### 🎓 进阶用户

推荐阅读：
1. [架构说明](ARCHITECTURE.md) - 理解设计
2. [中间件系统](MIDDLEWARE.md) - 掌握中间件
3. [插件系统](PLUGIN.md) - 开发插件
4. [性能优化](PERFORMANCE.md) - 优化性能

### 👨‍💻 开发者/贡献者

推荐阅读：
1. [架构说明](ARCHITECTURE.md) - 理解核心设计
2. [Context 使用指南](CONTEXT_USAGE_GUIDE.md) - 掌握 Context API
3. [规则最佳实践](RULE_BEST_PRACTICES.md) - 规则编写规范
4. [变更日志](CHANGELOG.md) - 版本历史
5. [归档文档](archive/) - 历史开发文档（可选）

### 🔧 运维人员

推荐阅读：
1. [配置说明](CONFIG.md) - 配置管理
2. [指标收集](METRICS.md) - 监控指标
3. [错误处理](ERROR_HANDLING.md) - 故障恢复
4. [性能分析指南](PERFORMANCE_ANALYSIS.md) - 性能调优

---

## 📖 按主题导航

### 🎯 基础使用

- [快速开始](QUICKSTART.md) - 第一个 Bot
- [使用指南](GUIDE.md) - 详细教程
  - 事件匹配
  - 规则组合
  - 链式调用
  - 多重处理器
- [Context 使用指南](CONTEXT_USAGE_GUIDE.md) - Context API 详解

### 🚀 高级功能

- [中间件系统](MIDDLEWARE.md)
  - 内置中间件（12+）
  - 自定义中间件
  - 中间件组合
- [插件系统](PLUGIN.md)
  - 插件开发
  - 依赖管理
  - 热重载
- [权限系统](PERMISSION.md)
  - RBAC 权限控制
  - 角色管理

### 🔧 运维部署

- [配置说明](CONFIG.md)
  - YAML 配置
  - 环境变量
  - 热重载
- [错误处理](ERROR_HANDLING.md)
  - 重试策略
  - 死信队列
  - 错误恢复
- [指标收集](METRICS.md)
  - Prometheus 集成
  - 自定义指标
  - 监控面板

### ⚡ 性能优化

- [性能优化](PERFORMANCE.md)
  - 性能测试结果
  - 对象池优化
  - 批量处理
  - 并发控制
- [性能分析指南](PERFORMANCE_ANALYSIS.md)
  - pprof 使用
  - CPU/内存分析
  - 性能调优案例
- [规则最佳实践](RULE_BEST_PRACTICES.md)
  - 纯函数编写
  - 性能优化技巧

---

## 🔍 常见问题快速导航

| 问题 | 文档链接 |
|------|---------|
| 如何开始？ | → [快速开始](QUICKSTART.md) |
| 如何匹配消息？ | → [使用指南 - 规则匹配](GUIDE.md#内置规则) |
| 如何添加中间件？ | → [中间件系统](MIDDLEWARE.md) |
| 如何开发插件？ | → [插件系统](PLUGIN.md) |
| 如何处理错误？ | → [错误处理](ERROR_HANDLING.md) |
| 如何监控性能？ | → [指标收集](METRICS.md) |
| 如何配置权限？ | → [权限系统](PERMISSION.md) |
| 性能如何优化？ | → [性能优化](PERFORMANCE.md) |
| 如何配置 Bot？ | → [配置说明](CONFIG.md) |
| Context 如何使用？ | → [Context 使用指南](CONTEXT_USAGE_GUIDE.md) |

---

## 📊 文档统计

| 类别 | 文档数 | 说明 |
|------|--------|------|
| **快速开始** | 3 | README, QUICKSTART, GUIDE |
| **核心概念** | 4 | ARCHITECTURE, MIDDLEWARE, PLUGIN, CONTEXT_USAGE_GUIDE |
| **功能特性** | 5 | CONFIG, ERROR_HANDLING, PERMISSION, METRICS, BATCH_PROCESSING |
| **性能优化** | 3 | PERFORMANCE, PERFORMANCE_ANALYSIS, RULE_BEST_PRACTICES |
| **高级功能** | 1 | TRACING_INTEGRATION |
| **项目信息** | 2 | CHANGELOG, archive/ |
| **总计** | **18** | 精简后的核心文档 |
| **归档** | **28** | 内部开发文档（archive/） |

---

## 🗂️ 文档维护

### 最近更新 (2025-12-07)

- ✅ **文档精简**: 将 28 个内部报告归档到 `archive/` 目录
- ✅ **导航优化**: 重组文档索引，提升查找效率
- ✅ **内容更新**: 更新文档版本号和覆盖率数据
- ✅ **链接修复**: 移除失效链接，确保所有链接有效

### 文档版本

| 文档 | 版本标注 | 最后更新 |
|------|---------|---------|
| README.md | v1.2.1 | 2025-12-02 |
| INDEX.md | v1.2.1+ | 2025-12-07 |
| CHANGELOG.md | v1.2.1 | 2025-12-02 |
| ARCHITECTURE.md | v1.2.0+ | 2025-11-30 |
| MIDDLEWARE.md | v1.2.0+ | 2025-11-30 |
| PERFORMANCE_ANALYSIS.md | v1.2.1+ | 2025-12-07 |

### 贡献指南

文档贡献请遵循：
1. 保持简洁明了，避免冗余
2. 提供可运行的示例代码
3. 标注文档版本和更新日期
4. 确保链接有效性

---

## 📮 反馈与建议

如果你发现文档问题或有改进建议，请：
1. 提交 [Issue](https://github.com/KomeiDiSanXian/remilia/issues)
2. 提交 Pull Request
3. 在文档中添加注释说明

---

**提示**: 如需查看历史开发文档和修复报告，请访问 [archive/](archive/) 目录。
| **核心概念** | 3 | ARCHITECTURE, MIDDLEWARE, PLUGIN |
| **功能特性** | 4 | CONFIG, ERROR_HANDLING, PERMISSION, METRICS |
| **性能优化** | 1 | PERFORMANCE |
| **项目信息** | 3 | CHANGELOG, COMPONENT_ANALYSIS, COMPONENT_ANALYSIS_SUMMARY |
| **总计** | 14 | 核心文档 |

---

## 🔗 外部资源

- [GitHub 仓库](https://github.com/KomeiDiSanXian/remilia)
- [QQ 官方文档](https://bot.q.qq.com/wiki/)
- [Go 语言文档](https://golang.org/doc/)

---

## 💡 文档编写原则

1. **简洁明了** - 直击重点，避免冗余
2. **示例丰富** - 代码示例胜过长篇大论
3. **结构清晰** - 层次分明，易于导航
4. **持续更新** - 跟随代码变更及时更新

---

## 📝 贡献文档

发现文档问题或想要改进？欢迎贡献！

1. Fork 项目
2. 修改文档
3. 提交 Pull Request

---

**最后更新**: 2025-11-30  
**文档版本**: v1.2.0+  
**维护者**: Remilia Team

