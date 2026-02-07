# 插件帮助系统使用指南

## 📚 目录

- [概述](#概述)
- [快速开始](#快速开始)
- [创建带元数据的插件](#创建带元数据的插件)
- [Help 插件使用](#help-插件使用)
- [最佳实践](#最佳实践)
- [常见问题](#常见问题)

---

## 概述

Remilia 框架现已支持完整的插件元数据系统，每个插件都可以提供：

- 📝 **名称和版本**：标识插件的基本信息
- 👤 **作者信息**：显示插件的维护者
- 📖 **描述和帮助文本**：提供详细的使用说明
- 🏷️ **分类和标签**：便于组织和搜索
- 🔗 **链接信息**：主页、仓库地址等
- 📦 **依赖声明**：明确插件间的依赖关系

所有这些信息都可以通过统一的 Help 插件进行查询。

---

## 快速开始

### 1. 创建简单插件（无元数据）

```go
type MyPlugin struct {
    *plugin.BasePlugin
}

func NewMyPlugin() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin("myplugin"),
    }
}

func (p *MyPlugin) Load(eng *remilia.Engine) error {
    matcher := eng.OnCommand(dto.C2CMessageCreate, "/mycommand").
        Handle(p.handleCommand)
    p.AddMatcher(matcher)
    return nil
}
```

### 2. 创建带元数据的插件（推荐）

```go
func NewMyPlugin() *MyPlugin {
    metadata := &plugin.PluginMetadata{
        Name:        "myplugin",
        Version:     "1.0.0",
        Author:      "Your Name",
        Description: "这是一个示例插件",
        HelpText: `插件使用说明：
  /mycommand <参数> - 命令说明
  
示例：
  /mycommand hello`,
        Category:    "工具",
        Tags:        []string{"示例", "工具"},
    }

    return &MyPlugin{
        BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
    }
}
```

---

## 创建带元数据的插件

### 完整示例

```go
package myplugin

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/command"
    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/KomeiDiSanXian/remilia/plugin"
)

type MyPlugin struct {
    *plugin.BasePlugin
}

func NewMyPlugin() *MyPlugin {
    metadata := &plugin.PluginMetadata{
        // 必填字段
        Name:        "myplugin",
        
        // 推荐字段
        Version:     "1.0.0",
        Author:      "Your Name",
        Description: "插件的简短描述（一句话）",
        
        // 详细帮助文本
        HelpText: `这是详细的帮助文本，可以包含多行。

使用方法：
  /cmd1 <arg1> <arg2> - 功能描述
  /cmd2 [可选参数] - 功能描述
  
示例：
  /cmd1 hello world
  /cmd2
  
注意事项：
  - 注意事项1
  - 注意事项2`,
        
        // 分类和标签
        Category:    "工具",  // 推荐分类：系统、工具、娱乐、管理、生活
        Tags:        []string{"消息", "工具", "API"},
        
        // 依赖声明
        Dependencies: []string{"database", "cache"},
        
        // 可见性
        Hidden:      false,  // true 表示在 help 中隐藏
        
        // 链接信息（可选）
        Homepage:    "https://example.com/myplugin",
        Repository:  "https://github.com/yourname/myplugin",
    }

    return &MyPlugin{
        BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
    }
}

func (p *MyPlugin) Load(eng *remilia.Engine) error {
    // 注册命令
    m1 := eng.OnCommand(dto.C2CMessageCreate, "/cmd1").
        Handle(p.handleCmd1)
    p.AddMatcher(m1)
    
    m2 := eng.OnCommand(dto.C2CMessageCreate, "/cmd2").
        Handle(p.handleCmd2)
    p.AddMatcher(m2)
    
    return nil
}

func (p *MyPlugin) handleCmd1(ctx *eventctx.Context) error {
    // 命令处理逻辑
    return nil
}

func (p *MyPlugin) handleCmd2(ctx *eventctx.Context) error {
    // 命令处理逻辑
    return nil
}

// Dependencies 返回依赖列表
func (p *MyPlugin) Dependencies() []string {
    // 如果在元数据中已声明，这里会返回空
    // 也可以在这里动态返回
    return []string{}
}
```

### 后续设置元数据

如果需要在创建后修改元数据：

```go
plugin := plugin.NewBasePlugin("myplugin")

metadata := &plugin.PluginMetadata{
    Name:        "myplugin",
    Version:     "1.0.0",
    Description: "更新的描述",
}

plugin.SetMetadata(metadata)
```

---

## Help 插件使用

### 注册 Help 插件

```go
// 创建命令注册表
registry := command.NewCommandRegistry()

// 创建 Help 插件
helpPlugin := help.NewHelpPlugin(registry)

// 设置插件管理器（可选，用于查询插件信息）
helpPlugin.SetPluginManager(bot.PluginManager())

// 注册插件
bot.PluginManager().Register(helpPlugin)
```

### Help 命令格式

| 命令 | 说明 |
|------|------|
| `/help` | 显示所有命令列表（第1页） |
| `/help 2` | 显示第2页的命令 |
| `/help plugins` | 显示所有插件及其元数据 |
| `/help <插件名>` | 显示指定插件的详细信息 |
| `/help <命令名>` | 显示指定命令的详细用法 |

### 示例输出

#### `/help plugins`

```
📦 已加载插件列表 (共 3 个)
==============================

【系统】
  🔌 help v1.0.0
     提供命令和插件的帮助信息查询功能
     👤 Remilia | 🏷️  帮助, 文档, 命令

【工具】
  🔌 echo v1.0.0
     一个简单的消息回显插件
     👤 Example Team | 🏷️  消息, 工具, 示例

【生活】
  🔌 weather v2.1.0
     查询城市天气信息
     👤 Weather Team | 🏷️  天气, 生活, 信息

==============================
💡 使用方法:
  /help <插件名> - 查看插件的详细信息和命令
  /help <命令名> - 查看命令详情
```

#### `/help echo`

```
🔌 插件【echo】信息
==============================

📝 描述: 一个简单的消息回显插件
📌 版本: 1.0.0
👤 作者: Example Team
📂 分类: 工具
🏷️  标签: 消息, 工具, 示例
🏠 主页: https://example.com/echo-plugin

💡 帮助:
回显插件使用说明：
  /echo <消息> - 回显你发送的消息
  /reverse <消息> - 反转消息内容

📋 提供的命令 (2 个):

  /echo
    回显消息
    用法: /echo <消息内容>

  /reverse
    反转消息
    用法: /reverse <消息内容>

==============================
💡 使用 /help <命令名> 查看命令的详细用法
```

---

## 最佳实践

### 1. 元数据字段建议

#### 必填字段
- **Name**：简短、有意义的名称

#### 强烈推荐字段
- **Version**：使用语义化版本（如 1.0.0）
- **Description**：一句话描述插件功能
- **HelpText**：详细的使用说明

#### 推荐字段
- **Author**：便于用户联系
- **Category**：便于分类查找
- **Tags**：便于搜索过滤

### 2. HelpText 编写规范

```go
HelpText: `插件名称使用说明：

命令列表：
  /cmd1 <必需参数> [可选参数] - 功能描述
  /cmd2 - 功能描述

使用示例：
  /cmd1 hello world
  /cmd2

注意事项：
  - 注意事项1
  - 注意事项2

更多信息：
  访问 https://example.com/docs 查看完整文档`,
```

### 3. 分类建议

推荐使用以下分类：

- **系统**：框架核心功能（help、config 等）
- **工具**：实用工具类（echo、format 等）
- **娱乐**：娱乐功能（游戏、抽奖等）
- **管理**：管理功能（权限、统计等）
- **生活**：生活服务（天气、新闻等）
- **开发**：开发调试工具

### 4. 标签建议

使用更细粒度的标签：

```go
Tags: []string{"消息处理", "文本工具", "API"}
```

### 5. 版本号规范

使用语义化版本（Semantic Versioning）：

- **主版本号**：不兼容的 API 修改
- **次版本号**：向后兼容的功能性新增
- **修订号**：向后兼容的问题修正

示例：`1.2.3`

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
- [实施报告](../05-reports/PLUGIN_METADATA_ENHANCEMENT.md)

---

## 总结

插件元数据系统让 Remilia 的插件生态更加规范和易用：

✅ **统一的帮助系统**：用户可以轻松查询所有插件和命令  
✅ **向后兼容**：旧插件无需修改即可工作  
✅ **灵活扩展**：支持丰富的元数据字段  
✅ **开发友好**：简单的 API，清晰的文档

开始为你的插件添加元数据吧！🚀

