# 自动依赖注入功能实现报告

**日期：** 2026-02-12  
**实施状态：** ✅ 核心功能已完成，插件迁移待进行

---

## 概述

实现了插件系统的**自动依赖注入**功能，通过结构体标签（tags）自动识别和注入依赖，无需手动声明。

---

## 已完成功能

### 1. ✅ 核心依赖注入框架

**文件：** `plugin/dependency.go`

实现了以下核心功能：

#### ExtractDependencies(plugin interface{}) []string
- 通过反射扫描结构体字段
- 识别 `inject:"plugin:xxx"` 标签
- 自动提取依赖列表

#### InjectDependencies(plugin interface{}, deps map[string]interface{}) error
- 自动注入依赖到插件字段
- 支持类型检查和验证
- 支持可选和必需依赖

#### GetDependencyFields(plugin interface{}) []DependencyField
- 获取所有依赖字段的详细信息
- 包括字段名、插件名、是否必需等

#### ValidateDependencies(plugin interface{}, availablePlugins map[string]Plugin) error
- 验证必需依赖是否满足
- 在插件加载前检查依赖完整性

### 2. ✅ 插件管理器集成

**文件：** `plugin/manager.go`

- 在 `Register()` 方法中自动调用依赖注入
- 收集已注册插件作为可注入依赖
- 支持注入 `manager`, `coordinator`, `engine` 等特殊依赖

### 3. ✅ 标签语法支持

支持以下标签格式：

```go
type Plugin struct {
    *plugin.BasePlugin
    
    // 插件依赖 - 自动注入
    PermPlugin *permission.Plugin `inject:"plugin:permission"`
    
    // 必需依赖
    StoragePlugin *storage.Plugin `inject:"plugin:storage,required"`
    
    // 非插件依赖
    Manager *plugin.Manager `inject:"manager"`
    Engine  *engine.Engine  `inject:"engine"`
}
```

### 4. ✅ 完整测试覆盖

**文件：** `plugin/dependency_test.go`

- TestExtractDependencies - 测试依赖提取
- TestInjectDependencies - 测试依赖注入
- TestInjectDependencies_MissingRequired - 测试必需依赖缺失
- TestInjectDependencies_MissingOptional - 测试可选依赖缺失
- TestGetDependencyFields - 测试字段信息获取
- TestInjectDependencies_TypeMismatch - 测试类型不匹配
- TestExtractDependencies_NoTags - 测试无标签情况
- TestExtractDependencies_OnlyMetadata - 测试元数据依赖

**测试结果：** ✅ 全部通过

---

## 使用方式

### 方式 1：使用标签自动注入（推荐）⭐

```go
type Plugin struct {
    *plugin.BasePlugin
    
    // 使用标签声明依赖
    PermPlugin *permission.Plugin `inject:"plugin:permission"`
    Manager    *plugin.Manager    `inject:"manager"`
}

// Dependencies() 自动从标签提取
func (p *Plugin) Dependencies() []string {
    deps := plugin.ExtractDependencies(p)
    if len(deps) == 0 {
        return p.BasePlugin.Dependencies() // 回退到元数据
    }
    return deps
}

// 使用依赖（自动注入后）
func (p *Plugin) someMethod() {
    if p.PermPlugin != nil {
        hasPermission := p.PermPlugin.HasPermission(userID, "admin")
    }
}
```

### 方式 2：使用元数据声明（兼容模式）

```go
func New() *Plugin {
    metadata := &plugin.Metadata{
        Name:         "myPlugin",
        Dependencies: []string{"permission"}, // 声明式
    }
    return &Plugin{
        BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
    }
}

// 不需要实现 Dependencies() 方法
// BasePlugin 提供默认实现
```

---

## 标签语法说明

### 基本语法

```go
`inject:"plugin:名称"`         // 可选插件依赖
`inject:"plugin:名称,required"` // 必需插件依赖
`inject:"manager"`             // 管理器引用
`inject:"engine"`              // Engine引用
```

### 示例

```go
type Plugin struct {
    *plugin.BasePlugin
    
    // 可选依赖 - 如果不存在，字段为 nil
    CachePlugin *cache.Plugin `inject:"plugin:cache"`
    
    // 必需依赖 - 如果不存在，注入失败
    StoragePlugin *storage.Plugin `inject:"plugin:storage,required"`
    
    // 特殊依赖
    Manager *plugin.Manager `inject:"manager"`
    Engine  *engine.Engine  `inject:"engine"`
}
```

---

## 注意事项

### ⚠️ 重要：字段必须是导出的（大写）

```go
// ❌ 错误 - 小写字段无法通过反射访问
type Plugin struct {
    *plugin.BasePlugin
    permPlugin *permission.Plugin `inject:"plugin:permission"` // ❌
}

// ✅ 正确 - 大写字段可以被反射访问
type Plugin struct {
    *plugin.BasePlugin
    PermPlugin *permission.Plugin `inject:"plugin:permission"` // ✅
}
```

### 依赖注入时机

依赖注入发生在 `Manager.Register()` 方法中，**在 `Load()` 方法之前**：

```
1. Manager.Register(plugin)
2.   ├─ 初始化配置
3.   ├─ 自动注入依赖 ← 这里注入
4.   ├─ 设置状态为 Loading
5.   └─ plugin.Load(engine) ← Load 时依赖已经注入
```

因此在 `Load()` 方法中可以直接使用注入的依赖：

```go
func (p *Plugin) Load(eng *engine.Engine) error {
    // 依赖已经被注入，可以直接使用
    if p.PermPlugin != nil {
        logger.Info("Permission plugin is available")
    }
    return nil
}
```

---

## 待完成工作

### 📝 插件迁移

由于字段访问权限问题（Go 中小写字段是私有的），现有插件需要进行迁移：

#### 需要迁移的插件

1. **debug 插件** (`plugins/dev/debug`)
   - 字段：`engine`, `permPlugin`, `pluginManager`, `devMode`
   - 需要改为：`Engine`, `PermPlugin`, `PluginManager`, `DevMode`

2. **admin 插件** (`plugins/core/admin`)
   - 字段：`pluginManager`, `permPlugin`
   - 需要改为：`PluginManager`, `PermPlugin`

3. **help 插件** (`plugins/core/help`)
   - 字段：`engine`, `pluginManager`
   - 需要改为：`Engine`, `PluginManager`

#### 迁移步骤

1. **修改结构体定义**
   ```go
   // 之前
   type Plugin struct {
       permPlugin *permission.Plugin
   }
   
   // 之后
   type Plugin struct {
       PermPlugin *permission.Plugin `inject:"plugin:permission"`
   }
   ```

2. **更新所有字段引用**
   ```go
   // 之前
   p.permPlugin.HasPermission(...)
   
   // 之后
   p.PermPlugin.HasPermission(...)
   ```

3. **添加 Dependencies() 方法**
   ```go
   func (p *Plugin) Dependencies() []string {
       deps := plugin.ExtractDependencies(p)
       if len(deps) == 0 {
           return p.BasePlugin.Dependencies()
       }
       return deps
   }
   ```

4. **移除元数据中的手动依赖声明**
   ```go
   // 之前
   metadata := &plugin.Metadata{
       Dependencies: []string{"permission"}, // ❌ 不再需要
   }
   
   // 之后
   metadata := &plugin.Metadata{
       // Dependencies 会从标签自动提取 ✅
   }
   ```

---

## 优势对比

### 之前（手动声明）

```go
type Plugin struct {
    *plugin.BasePlugin
    permPlugin *permission.Plugin // 容易忘记声明
}

// 需要手动维护
func (p *Plugin) Dependencies() []string {
    return []string{"permission"}
}

// 还需要在元数据中声明
metadata := &plugin.Metadata{
    Dependencies: []string{"permission"},
}

// 需要手动注入（通过 Setter）
func (p *Plugin) SetPermissionPlugin(pp *permission.Plugin) {
    p.permPlugin = pp
}
```

### 现在（自动注入）

```go
type Plugin struct {
    *plugin.BasePlugin
    PermPlugin *permission.Plugin `inject:"plugin:permission"` // ✅ 一处声明
}

// 自动从标签提取
func (p *Plugin) Dependencies() []string {
    return plugin.ExtractDependencies(p)
}

// 自动注入，无需手动 Setter
```

---

## 性能考虑

### 反射开销

- 依赖注入只在插件注册时执行一次
- 运行时无额外开销
- 对性能影响可忽略不计

### 内存占用

- 标签信息存储在类型元数据中
- 不占用额外运行时内存

---

## 兼容性

### ✅ 向后兼容

- 现有使用元数据声明的插件继续工作
- 可以逐步迁移到标签注入
- 两种方式可以共存

### 迁移路径

1. **阶段 1（当前）**：核心功能完成，测试通过
2. **阶段 2**：迁移现有插件使用标签注入
3. **阶段 3**：更新文档和示例
4. **阶段 4**：废弃手动依赖声明

---

## 文档更新

需要更新以下文档：

- [ ] 插件开发指南
- [ ] API 参考文档
- [ ] 迁移指南
- [ ] 示例代码

---

## 示例代码

创建了完整的测试用例展示用法：
- `plugin/dependency_test.go` - 包含多个测试场景

---

## 总结

### ✅ 已完成

1. 核心依赖注入框架（dependency.go）
2. 插件管理器集成（manager.go）
3. 完整测试覆盖（dependency_test.go）
4. 标签语法支持
5. 文档编写

### 🔄 进行中

1. 现有插件迁移（需要手动修改字段为大写）
2. 示例代码更新

### 📋 待进行

1. 完整文档更新
2. 迁移指南编写
3. 最佳实践总结

---

## 建议

### 短期

1. ✅ 完成核心功能开发（已完成）
2. 📝 迁移现有插件使用标签注入
3. 📄 更新文档和示例

### 中期

1. 添加代码生成工具自动生成 Dependencies() 方法
2. 添加静态分析工具检查依赖声明一致性
3. 完善错误提示和调试信息

### 长期

1. 考虑支持更复杂的依赖关系（如版本要求）
2. 支持依赖配置（如超时、重试等）
3. 实现依赖图可视化工具

---

**状态：** ✅ 核心功能完成，待插件迁移  
**测试：** ✅ 全部通过  
**文档：** ✅ 已创建  
**兼容性：** ✅ 向后兼容

