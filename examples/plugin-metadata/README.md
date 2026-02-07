# 插件元数据增强示例

本示例展示如何使用 Remilia 的插件元数据功能，让每个插件都能提供详细的帮助信息。

## 📋 功能特性

### 1. 插件元数据支持

插件现在可以提供丰富的元数据信息：

```go
metadata := &plugin.PluginMetadata{
    Name:        "echo",              // 插件名称
    Version:     "1.0.0",             // 版本号
    Author:      "Example Team",      // 作者
    Description: "一个简单的消息回显插件", // 描述
    HelpText:    "...",               // 详细帮助文本
    Category:    "工具",               // 分类
    Tags:        []string{"消息", "工具"}, // 标签
    Homepage:    "https://...",       // 主页
    Repository:  "https://...",       // 仓库地址
    Hidden:      false,               // 是否隐藏
}
```

### 2. Help 插件增强

Help 插件现在支持：

- `/help` - 显示所有命令列表
- `/help plugins` - 显示所有插件及其元数据
- `/help <插件名>` - 显示指定插件的详细信息和命令
- `/help <命令名>` - 显示指定命令的详细用法

### 3. 向后兼容

- 旧的插件无需修改即可继续工作
- 可选实现 `MetadataProvider` 接口提供元数据
- 未实现元数据的插件会显示基本信息

## 🚀 使用方法

### 1. 创建带元数据的插件

```go
func NewMyPlugin() *MyPlugin {
    metadata := &plugin.PluginMetadata{
        Name:        "myplugin",
        Version:     "1.0.0",
        Author:      "Your Name",
        Description: "插件描述",
        HelpText:    "详细的使用说明...",
        Category:    "分类",
        Tags:        []string{"标签1", "标签2"},
    }

    return &MyPlugin{
        BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
    }
}
```

### 2. 注册插件

```go
bot.PluginManager().Register(myPlugin)
```

### 3. 使用 Help 命令查询

```
/help plugins        # 列出所有插件
/help myplugin      # 查看插件详情
/help <命令名>       # 查看命令详情
```

## 📦 运行示例

```bash
cd examples/plugin-metadata
go run main.go
```

然后尝试以下命令：

- `/help plugins` - 查看所有插件（echo, weather, help）
- `/help echo` - 查看 echo 插件的详细信息
- `/help weather` - 查看 weather 插件的详细信息
- `/echo Hello World` - 测试回显功能
- `/reverse Hello` - 测试反转功能
- `/weather 北京` - 测试天气查询

## 🎯 插件元数据字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| Name | string | 是 | 插件名称 |
| Version | string | 否 | 版本号（建议使用语义化版本） |
| Author | string | 否 | 作者名称 |
| Description | string | 否 | 简短描述 |
| HelpText | string | 否 | 详细的帮助文本（支持多行） |
| Category | string | 否 | 分类（如"工具"、"娱乐"、"管理"） |
| Tags | []string | 否 | 标签列表 |
| Dependencies | []string | 否 | 依赖的插件列表 |
| Hidden | bool | 否 | 是否在帮助中隐藏 |
| Homepage | string | 否 | 主页 URL |
| Repository | string | 否 | 代码仓库 URL |

## 💡 最佳实践

### 1. 提供清晰的帮助文本

```go
HelpText: `插件名称使用说明：
  /cmd1 <参数> - 功能说明
  /cmd2 - 功能说明
  
示例：
  /cmd1 hello
  /cmd2`,
```

### 2. 合理使用分类和标签

- **分类**：用于组织插件（系统、工具、娱乐、管理、生活等）
- **标签**：用于搜索和过滤（消息、文件、API、监控等）

### 3. 设置合适的版本号

使用语义化版本（Semantic Versioning）：
- `1.0.0` - 主版本.次版本.修订号
- 主版本：不兼容的 API 修改
- 次版本：向后兼容的功能性新增
- 修订号：向后兼容的问题修正

### 4. 标注依赖关系

```go
Dependencies: []string{"database", "cache"},
```

这样插件管理器会确保依赖插件先加载。

## 🔧 技术实现

### 核心接口

```go
// Plugin 插件基础接口
type Plugin interface {
    Name() string
    Load(coordinator *engine.Engine) error
    Unload(coordinator *engine.Engine) error
    Reload(coordinator *engine.Engine) error
    Dependencies() []string
}

// MetadataProvider 元数据提供者（可选实现）
type MetadataProvider interface {
    Metadata() *PluginMetadata
}
```

### BasePlugin 自动实现

`BasePlugin` 已经实现了 `MetadataProvider` 接口：

```go
// Metadata 返回插件的元数据
func (p *BasePlugin) Metadata() *PluginMetadata {
    return p.metadata
}
```

所以只需要：

```go
plugin.NewBasePluginWithMetadata(metadata)
```

就能自动获得元数据支持。

## 📚 相关文档

- [插件系统架构设计](../../docs/03-architecture/PLUGIN_ENHANCEMENT_PROPOSAL.md)
- [Help 插件设计](../../docs/03-architecture/HELP_PLUGIN_DESIGN.md)
- [插件开发指南](../../docs/02-user-guides/)

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 许可证

MIT License

