# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] - 2026-02-19

### Removed - BREAKING CHANGES ⚠️
- **Plugin v1 API 完全移除**
  - `plugin.BasePlugin` 结构体已删除
  - `plugin.NewBasePlugin(name)` 函数已删除
  - `plugin.NewBasePluginWithMetadata(metadata)` 函数已删除
  - 所有基于继承的插件实现已移除
  - 迁移指南: `docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md`

### Migration Notes
- 所有插件必须使用 v2 API (PluginDescriptor)
- v1 测试代码已移除（备份为 `*_v1_removed.go.bak`）
- 核心插件已全部迁移到 v2
- 所有示例已更新为 v2 API

### What to Do
如果您的代码仍在使用 v1 API，请参考：
1. 迁移指南: `docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md`
2. v2 示例: `examples/plugin-example/`, `examples/plugin-metadata/`
3. v2 快速参考: `docs/05-reports/plugin-v2-quick-reference.md`

## [Unreleased]

### Added
- **Plugin v2 API** - 全新的插件系统 API
  - 函数式设计，无需继承
  - 自动依赖注入
  - 自动 Matcher 追踪
  - 代码减少 60%
  - 完整的接口实现（StatefulPlugin, MetadataProvider, MatcherProvider, ConfigurablePlugin）
- **Plugin v2 文档**
  - 完整的迁移指南 (`docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md`)
  - v2 快速参考 (`docs/05-reports/plugin-v2-quick-reference.md`)
  - 详细的问题分析和修复报告
- **Plugin v2 示例**
  - 所有示例已迁移到 v2 API
  - 新增 `examples/plugin-v2-demo/`
- **核心插件 v2 迁移**
  - ✅ cache - 缓存插件
  - ✅ storage - 存��插件
  - ✅ permission - 权限插件
  - ✅ help - 帮助插件
  - ✅ admin - 管理插件

### Changed
- **Plugin Manager** - 新增 `RegisterV2()` 方法支持 v2 插件
- **Plugin Manager** - 新增 `GetContainer()` 方法访问依赖注入容器

### Deprecated
- **Plugin v1 API** - 已弃用，将在 v2.0.0 移除
  - `plugin.NewBasePlugin(name)` - 使用 `PluginDescriptor` 替代
  - `plugin.NewBasePluginWithMetadata(metadata)` - 使用 `PluginDescriptor` 替代
  - `plugin.BasePlugin` 结构体 - 使用函数式设计替代
  - 运行时会显示弃用警告
  - 迁移指南: `docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md`
  - 弃用公告: `docs/05-reports/PLUGIN_V1_DEPRECATION_NOTICE.md`

### Fixed
- **Plugin v2 P0 问题修复** (质量评分: 7.25 → 9.5)
  - Matcher 注册追踪机制
  - StatefulPlugin 接口完整实现
  - 热重载时 SetupContext 更新
  - RegisterV2 并发安全优化

### Performance
- **Plugin 系统** - v2 API 性能与 v1 相同，但代码更简洁

## [1.0.0] - 2026-02-xx

### Added
- 初始版本发布
- 事件驱动引擎（COW 并发模型）
- 插件系统 v1
- 命令解析器
- 中间件系统（限流、重试、熔断、死信队列）
- 配置管理（YAML、热更新）
- 生命周期管理
- Webhook 适配器
- 完整的文档体系

---

## 版本号说明

- **主版本号**: 不兼容的 API 修改
- **次版本号**: 向下兼容的功能性新增
- **修订号**: 向下兼容的问题修正

## 迁移指南

### v1 to v2 Plugin API

如果你正在使用 v1 Plugin API，请参考迁移指南完成升级：
- **迁移指南**: `docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md`
- **弃用时间表**: 2026-03-19 (v2.0.0) 将移除 v1 API

---

**注意**: 本 CHANGELOG 从 v2 Plugin API 发布开始维护。

