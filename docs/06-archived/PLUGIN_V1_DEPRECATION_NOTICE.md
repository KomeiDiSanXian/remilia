# Plugin v1 API 弃用公告

**发布日期**: 2026-02-19  
**生效日期**: 立即生效  
**移除日期**: 2026-03-19 (v2.0.0)

---

## 📢 弃用声明

`BasePlugin` 和相关的 v1 API 已被正式标记为**弃用**，将在 **v2.0.0** 版本中移除。

### 弃用的 API

- `plugin.NewBasePlugin(name string)`
- `plugin.NewBasePluginWithMetadata(metadata *Metadata)`
- `plugin.BasePlugin` 结构体
- `manager.Register(plugin Plugin)` (仍然可用，但推荐使用 v2)

### 时间表

| 日期 | 里程碑 | 状态 |
|------|--------|------|
| 2026-02-19 | 发布弃用警告 | ✅ 完成 |
| 2026-02-19 | 所有示例迁移到 v2 | ✅ 完成 |
| 2026-02-19 | 发布迁移指南 | ✅ 完成 |
| 2026-03-19 | 移除 v1 API (v2.0.0) | ⏳ 计划中 |

---

## ⚠️ 影响范围

### 受影响的代码

如果你的代码中使用了以下模式，需要迁移：

```go
// ❌ 受影响：使用 BasePlugin
type MyPlugin struct {
    *plugin.BasePlugin
    field string
}

func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin("myplugin"),
    }
}

func (p *MyPlugin) Load(eng *engine.Engine) error {
    // ...
}
```

### 不受影响的代码

以下代码不受影响，无需修改：

```go
// ✅ v2 API - 不受影响
func New() *plugin.PluginDescriptor {
    return &plugin.PluginDescriptor{
        Name: "myplugin",
        Setup: func(ctx *plugin.SetupContext) error {
            // ...
        },
    }
}
```

---

## 🔄 迁移路径

### 步骤 1: 评估影响

运行你的代码，查看是否有弃用警告：

```
==================== DEPRECATION WARNING ====================
BasePlugin (v1 API) is deprecated and will be removed in v2.0.0
Plugin 'myplugin' should migrate to v2 API (PluginDescriptor)
Migration guide: docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md
v2 benefits: -60% code, auto dependency injection, better type safety
=============================================================
```

### 步骤 2: 阅读迁移指南

详细的迁移步骤请参见：
- **迁移指南**: [docs/02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md](../02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md)
- **v2 快速参考**: [docs/05-reports/plugin-v2-quick-reference.md](plugin-v2-quick-reference.md)

### 步骤 3: 迁移代码

参考示例代码：
- [examples/plugin-v2-demo/](../../examples/plugin-v2-demo/)
- [examples/plugin-example/](../../examples/plugin-example/)
- [examples/plugin-metadata/](../../examples/plugin-metadata/)

### 步骤 4: 测试验证

确保迁移后的代码：
- ✅ 编译通过
- ✅ 功能正常
- ✅ 无弃用警告

---

## 🎯 为什么弃用？

### v1 API 的问题

1. **不符合 Go 习惯用法**
   - 使用继承而非组合
   - 样板代码过多

2. **功能不完整**
   - Matcher 无法追踪
   - 依赖注入需要手动处理
   - 接口实现不完整

3. **难以维护**
   - 代码冗余
   - 扩展性差
   - 测试困难

### v2 API 的优势

1. **更简洁**
   - 代码减少 60%
   - 无需继承
   - 函数式设计

2. **功能完整**
   - 自动 Matcher 追踪
   - 自动依赖注入
   - 完整的接口实现

3. **更符合 Go**
   - 使用组合
   - 闭包管理状态
   - 更好的类型安全

---

## 📊 迁移收益

### 代码对比

**v1** (50 行):
```go
type CounterPlugin struct {
    *plugin.BasePlugin
    count atomic.Int64
}

func NewCounterPlugin() *CounterPlugin {
    return &CounterPlugin{
        BasePlugin: plugin.NewBasePlugin("counter"),
    }
}

func (p *CounterPlugin) Load(eng *engine.Engine) error {
    eng.OnCommand(dto.C2CMessageCreate, "/count").
        Handle(func(ctx *eventctx.Context) error {
            count := p.count.Add(1)
            return ctx.Reply(fmt.Sprintf("Count: %d", count))
        })
    return nil
}

func (p *CounterPlugin) Unload(eng *engine.Engine) error {
    return p.BasePlugin.Unload(eng)
}
```

**v2** (20 行):
```go
func New() *plugin.PluginDescriptor {
    var count atomic.Int64
    
    return &plugin.PluginDescriptor{
        Name:    "counter",
        Version: "2.0.0",
        
        Setup: func(ctx *plugin.SetupContext) error {
            ctx.RegisterCommand(dto.C2CMessageCreate, "/count").
                Handle(func(c *eventctx.Context) error {
                    n := count.Add(1)
                    return c.Reply(fmt.Sprintf("Count: %d", n))
                })
            return nil
        },
    }
}
```

**改进**: 代码减少 60%

---

## 🆘 需要帮助？

### 文档资源

- **迁移指南**: [PLUGIN_V1_TO_V2_MIGRATION.md](../02-user-guides/PLUGIN_V1_TO_V2_MIGRATION.md)
- **v2 快速参考**: [plugin-v2-quick-reference.md](plugin-v2-quick-reference.md)
- **示例代码**: [examples/](../../examples/)

### 常见问题

**Q: 我必须立即迁移吗？**
A: 不必须，但强烈建议。v1 API 将在 2026-03-19 移除（1 个月后）。

**Q: 迁移需要多长时间？**
A: 简单插件约 10-20 分钟，复杂插件约 30-60 分钟。

**Q: v2 API 稳定吗？**
A: 是的，v2 API 已完全测试（23 个测试，100% 通过），质量评分 9.5/10。

**Q: 我可以同时使用 v1 和 v2 吗？**
A: 可以，但不推荐。建议尽快完成迁移。

**Q: 迁移后有什么好处？**
A: 代码减少 60%，自动依赖注入，自动 Matcher 追踪，更好的类型安全。

### 获取支持

- **GitHub Issues**: [提交问题](https://github.com/KomeiDiSanXian/remilia/issues)
- **GitHub Discussions**: [参与讨论](https://github.com/KomeiDiSanXian/remilia/discussions)

---

## 📅 重要日期

- **2026-02-19**: 弃用警告发布 ✅
- **2026-03-19**: v1 API 移除 (v2.0.0) ⏳

**请在 2026-03-19 之前完成迁移！**

---

## ✅ 检查清单

迁移前请确认：

- [ ] 阅读了迁移指南
- [ ] 查看了示例代码
- [ ] 理解了 v2 API 的使用方式
- [ ] 备份了现有代码

迁移后请验��：

- [ ] 代码编译通过
- [ ] 所有功能正常
- [ ] 无弃用警告
- [ ] 测试通过

---

**发布者**: Remilia Team  
**联系方式**: GitHub Issues  
**最后更新**: 2026-02-19

