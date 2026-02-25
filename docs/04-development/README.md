# 开发指南

> **最后更新**: 2026-02-25

本目录包含 Remilia 框架的开发规范和高级指南。

---

## 📚 文档列表

### [plugin-best-practices.md](./plugin-best-practices.md) ⭐
**插件开发最佳实践**

涵盖：
- 插件文件结构规范（v2 PluginDescriptor）
- 依赖声明与 Smart 注册
- 错误处理模式
- 后台 goroutine 生命周期管理（ctx.Go）
- DryRun 保护
- Privileged 管理类插件
- 测试（plugintest 包）
- ctx.Set vs ctx.Delete 行为

**适合**: 所有插件开发者

---

## 🎯 学习路径

1. **入门**: [快速上手](../02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md)
2. **进阶**: [插件接口速查](../02-user-guides/PLUGIN_OPTIONAL_INTERFACES.md)
3. **规范**: [插件开发最佳实践](./plugin-best-practices.md)
4. **架构**: [插件系统设计](../03-architecture/BUILTIN_PLUGINS_DESIGN.md)

---

## 🔗 相关资源

- [用户指南](../02-user-guides/)
- [架构设计](../03-architecture/)
- [示例代码](../../examples/)

