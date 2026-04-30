# 插件帮助系统使用指南

> **最后更新**: 2026-02-25  
> **适用版本**: v2.0.0+

---

## 概述

Remilia 框架提供完整的插件元数据与帮助系统，每个插件可以通过 `Metadata` 提供：

- 名称 / 版本 / 作者
- 描述与帮助文本
- 分类和标签
- 主页 / 仓库地址

这些信息由内置 `help` 插件自动聚合，响应 `/help` 命令。

---

## 快速开始

### 创建带元数据的插件

```go
package myplugin

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/platform"
)

func New() *plugin.Descriptor {
    p := &MyPlugin{}
    return &plugin.Descriptor{
        Name:    "myplugin",
        Version: "1.0.0",

        Meta: &plugin.Metadata{
            Author:      "Your Name",
            Description: "这是一个示例插件，提供 /echo 和 /ping 命令",
            HelpText: `可用命令：
  /echo <文本>  — 回显输入的文本
  /ping         — 测试 Bot 是否在线`,
            Category: "工具",
            Tags:     []string{"示例", "工具"},
            // Hidden: true  // 设为 true 则不在 /help 中显示
        },

        Setup: func(ctx *plugin.SetupContext) (any, error) {
ctx.Reg.RegisterCommand(platform.EventKindGroupMessage, "/echo").Handle(p.handleEcho)
ctx.Reg.RegisterCommand(platform.EventKindGroupMessage, "/ping").Handle(p.handlePing)
            return p, nil
        },
    }
}

func (p *MyPlugin) handleEcho(ctx *eventctx.Context) error {
    cmd := ctx.GetParsedCommand()
    text, _ := cmd.Args["text"]
    return ctx.Reply(platform.TextMessage(text))
}

func (p *MyPlugin) handlePing(ctx *eventctx.Context) error {
	return ctx.Reply(platform.TextMessage("Pong!"))
}
```

---

## Metadata 字段完整说明

```go
Meta: &plugin.Metadata{
    Author:      "作者名",         // 显示在 /help 详情
    Description: "简短功能描述",   // /help 列表视图
    HelpText:    `详细帮助文本`,   // /help <name> 详情视图
    Category:    "工具",           // 用于 /help 按分类列出
    Tags:        []string{"tag"},  // 搜索标签
    Hidden:      false,            // true = 不在 /help 中显示
    Homepage:    "https://...",    // 可选
    Repository:  "https://...",    // 可选
},
```

`Name` / `Version` / `Dependencies` 字段同样存在于 `plugin.Metadata` 结构中，
由 Manager 自动填充，无需手动同步。

---

## Help 插件 — 命令发现

内置 `help` 插件通过 `ctx.Info.Coordinator()` 的只读视图获取所有命令信息：

```go
reader := ctx.Info.Coordinator()  // engine.Reader

// 获取所有已注册命令（不含 Hidden=true 的命令）
commands := reader.GetAllCommands()  // []engine.CommandInfo

// 按插件分组
byPlugin := reader.GetCommandsByPlugin()  // map[string][]engine.CommandInfo

// 按分类分组
byCategory := reader.GetCommandsByCategory()  // map[string][]engine.CommandInfo

// 查找单个命令（支持别名）
info := reader.FindCommand("/echo")  // *engine.CommandInfo 或 nil
```

`engine.CommandInfo` 结构：

```go
type CommandInfo struct {
    Command     string              // "/echo"
    Description string
    Usage       string
    Aliases     []string
    Category    string
    Examples    []string
    Permissions []string
    Plugin      string              // 所属插件名
    Source      string              // "plugin:myplugin"
    EventType   dto.EventType
    Definition  *command.Definition // 完整命令定义
}
```

---

## 自定义 Help 插件

如果需要自定义帮助格式，实现一个 Privileged 或普通插件，
通过 `ctx.Info.Coordinator()` 读取命令列表即可：

```go
func New() *plugin.Descriptor {
    return &plugin.Descriptor{
        Name: "myhelp",
        Setup: func(ctx *plugin.SetupContext) (any, error) {
            reader := ctx.Info.Coordinator()

            ctx.Reg.RegisterCommand(platform.EventKindGroupMessage, "/help").
                Handle(func(c *eventctx.Context) error {
                    cmds := reader.GetAllCommands()
                    var sb strings.Builder
                    sb.WriteString("📖 可用命令：\n")
                    for _, cmd := range cmds {
                        sb.WriteString(fmt.Sprintf("  %s — %s\n",
                            cmd.Command, cmd.Description))
                    }
                    return c.Reply(sb.String())
                })

            return nil, nil
        },
    }
}
```

---

## 最佳实践

1. **始终填写 `Description`**：这是 `/help` 列表视图的唯一文本
2. **`HelpText` 换行对齐**：使用 `` ` `` 原始字符串保持缩进
3. **合理设置 `Category`**：建议使用「工具」「管理」「娱乐」「系统」等
4. **仅对系统内部命令设置 `Hidden: true`**
5. **不要在 `HelpText` 中硬编码命令前缀**（前缀可配置）

---

## 常见问题

### Q1: 旧插件需要修改吗？

**A**: 不需要。旧插件会自动显示基本信息（只有名称）。但强烈建议添加元数据以提供更好的用户体验。

### Q2: 如何判断命令属于哪个插件？

**A**: 有两种方式：

1. **Source 字段**（推荐）：在注册命令时设置 Source
2. **Category 字段**：通过分类名匹配（兼容模式）

### Q3: Help 插件如何获取插件信息？

**A**: Help 插件通过 `PluginManager.GetMetadata()` 方法查询。如果插件实现了 `MetadataProvider` 接口，会返回详细元数据。

### Q4: 元数据可以动态修改吗？

**A**: 可以通过 `SetMetadata()` 方法修改，但不推荐在运行时频繁修改。

### Q5: Hidden 字段的作用？

**A**: 设置为 `true` 时，插件不会出现在 `/help plugins` 列表中，但仍可以通过插件名查询。

### Q6: Dependencies 字段如何工作？

**A**: 声明依赖后，PluginManager 会确保依赖的插件先于当前插件加载。如果依赖不满足，会返回错误。

### Q7: 如何测试插件的元数据？

**A**: 可以通过单元测试验证：

```go
func TestPluginMetadata(t *testing.T) {
    p := NewMyPlugin()
    metadata := p.Metadata()
    
    assert.Equal(t, "myplugin", metadata.Name)
    assert.Equal(t, "1.0.0", metadata.Version)
    assert.NotEmpty(t, metadata.Description)
}
```

---

## 相关文档

- [完整示例代码](../../examples/plugin-metadata/)
- [插件系统增强方案](../03-architecture/PLUGIN_ENHANCEMENT_PROPOSAL.md)
- [Help 插件设计](../03-architecture/HELP_PLUGIN_DESIGN.md)
- [实施报告](../06-archived/PLUGIN_METADATA_ENHANCEMENT.md)

---

## 总结

插件元数据系统让 Remilia 的插件生态更加规范和易用：

✅ **统一的帮助系统**：用户可以轻松查询所有插件和命令  
✅ **向后兼容**：旧插件无需修改即可工作  
✅ **灵活扩展**：支持丰富的元数据字段  
✅ **开发友好**：简单的 API，清晰的文档

开始为你的插件添加元数据吧！🚀
