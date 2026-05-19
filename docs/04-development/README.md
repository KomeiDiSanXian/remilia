# 开发指南

> **最后更新**: 2026-05-20

本目录包含 Remilia 框架的开发规范和高级指南。

---

## 📚 文档列表

### [plugin-best-practices.md](./plugin-best-practices.md) ⭐
**插件开发最佳实践**

涵盖：
- 插件文件结构规范（v2 Descriptor）
- 依赖声明与 Smart 注册
- 错误处理模式
- 后台 goroutine 生命周期管理（ctx.Go）
- DryRun 保护
- Privileged 管理类插件
- 测试（plugintest 包）
- ctx.Set vs ctx.Delete 行为

**适合**: 所有插件开发者

---

### [wasm-plugin-development.md](./wasm-plugin-development.md) 🆕
**WASM 跨语言插件开发指南**

涵盖：
- ABI 合约（导出/导入/TLV 序列化）
- TinyGo 插件开发（含最小示例）
- Rust / C 等其他语言 ABI 模板
- 安全模型与资源限制
- 宿主集成代码示例

**适合**: 需要跨语言插件的开发者

---

## 🎯 学习路径

1. **入门**: [快速上手](../02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md)
2. **进阶**: [插件接口速查](../02-user-guides/PLUGIN_OPTIONAL_INTERFACES.md)
3. **规范**: [插件开发最佳实践](./plugin-best-practices.md)
4. **跨语言**: [WASM 插件开发](./wasm-plugin-development.md)

---

## 🔗 相关资源

- [用户指南](../02-user-guides/)
- [架构设计](../03-architecture/)
- [示例代码](../../examples/)

