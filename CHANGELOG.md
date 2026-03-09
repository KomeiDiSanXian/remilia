# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

<!-- 下一个版本的变更记录在此处 -->

## [1.0.0] - 2026-03-09

### Added
- 初始版本发布
- 事件驱动引擎（COW 并发模型）
- **Plugin v2 API** - 全新的插件系统 API
  - 函数式设计，无需继承
  - 自动依赖注入（`Require` / `Optional` / `MustAs`）
  - 生命周期绑定的后台 goroutine（`ctx.Go`）
  - 读写分离权限模型（`PluginInfo` 只读 / `ManagerWriter` 写）
  - Smart 注册自动推断依赖图（无需手写 `Deps`）
  - 完整的接口实现（StatefulPlugin, MetadataProvider, MatcherProvider, ConfigurablePlugin）
- 命令解析器（Trie 树加速，前缀/精确/别名匹配）
- 中间件系统（限流、重试、熔断、降级、死信队列）
- 配置管理（YAML、热更新、fsnotify）
- 生命周期管理（`lifecycle.Manager`）
- Webhook 适配器
- pprof 性能剖析支持
- 健康检查 HTTP 端点
- 分布式追踪（OpenTelemetry）
- 完整的文档体系与使用示例

---

## 版本号说明

- **主版本号**: 不兼容的 API 修改
- **次版本号**: 向下兼容的功能性新增
- **修订号**: 向下兼容的问题修正

<!-- 版本比较链接 -->
[Unreleased]: https://github.com/KomeiDiSanXian/remilia/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/KomeiDiSanXian/remilia/releases/tag/v1.0.0

