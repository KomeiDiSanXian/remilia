# 插件依赖声明问题与解决方案

## 问题描述

在当前的插件系统中，如果一个插件依赖了其他插件，需要在 `Dependencies()` 方法中手动声明。这种方式存在以下问题：

### 当前实现方式

```go
type Plugin struct {
    *plugin.BasePlugin
    permPlugin    *permission.Plugin  // ❌ 容易忘记在 Dependencies() 中声明
    pluginManager *plugin.Manager      // ❌ 容易忘记在 Dependencies() 中声明
}

func (p *Plugin) Dependencies() []string {
    return []string{"permission"}  // 手动维护，容易遗漏或过时
}
```

### 存在的问题

1. ❌ **容易遗忘**: 添加了依赖字段但忘记在 `Dependencies()` 中声明
2. ❌ **不一致**: 代码中使用了依赖但 `Dependencies()` 没有返回
3. ❌ **维护困难**: 重构时容易遗漏更新
4. ❌ **没有编译时检查**: 运行时才能发现依赖问题
5. ❌ **容易过时**: 移除依赖时忘记更新 `Dependencies()`

## 解决方案

### 方案 1: 自动依赖注入 + 声明式依赖（推荐）⭐

通过结构体标签自动识别依赖，无需手动声明。

#### 实现方式

```go
// 1. 在插件结构体中使用标签声明依赖
type Plugin struct {
    *plugin.BasePlugin
    
    // 使用 `inject:"plugin:permission"` 标签声明依赖
    permPlugin    *permission.Plugin `inject:"plugin:permission"`
    pluginManager *plugin.Manager     `inject:"manager"`
    engine        *engine.Engine      `inject:"engine"`
}

// 2. Dependencies() 方法自动从标签中提取
func (p *Plugin) Dependencies() []string {
    // 由 BasePlugin 自动实现，通过反射提取 inject 标签
    return plugin.ExtractDependencies(p)
}

// 3. 或者使用更简单的方式，直接在元数据中声明
func New() *Plugin {
    metadata := &plugin.Metadata{
        Name:        "admin",
        Dependencies: []string{"permission"},  // 在元数据中声明
    }
    return &Plugin{
        BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
    }
}

// 4. BasePlugin 提供默认实现
func (p *BasePlugin) Dependencies() []string {
    if p.metadata != nil {
        return p.metadata.Dependencies
    }
    return []string{}
}
```

#### 优点

✅ **自动化**: 自动从标签或元数据提取依赖  
✅ **声明式**: 依赖在一个地方声明  
✅ **类型安全**: 结构体字段提供类型检查  
✅ **易于维护**: 添加/删除字段自动更新依赖  
✅ **向后兼容**: 不影响现有代码

#### 实现示例

```go
// plugin/dependency.go
package plugin

import (
    "reflect"
    "strings"
)

// ExtractDependencies 从插件结构体中提取依赖
func ExtractDependencies(p Plugin) []string {
    deps := make(map[string]bool)
    
    v := reflect.ValueOf(p)
    if v.Kind() == reflect.Ptr {
        v = v.Elem()
    }
    
    t := v.Type()
    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        tag := field.Tag.Get("inject")
        
        if tag != "" {
            // 解析标签: plugin:permission -> permission
            if strings.HasPrefix(tag, "plugin:") {
                depName := strings.TrimPrefix(tag, "plugin:")
                deps[depName] = true
            }
        }
    }
    
    result := make([]string, 0, len(deps))
    for dep := range deps {
        result = append(result, dep)
    }
    return result
}

// BasePlugin 默认实现
func (p *BasePlugin) Dependencies() []string {
    // 优先从元数据获取
    if p.metadata != nil && len(p.metadata.Dependencies) > 0 {
        return p.metadata.Dependencies
    }
    
    // 如果元数据中没有，尝试从结构体标签提取
    // 注意：这需要插件实现 StructProvider 接口
    return []string{}
}
```

---

### 方案 2: 依赖检测工具（编译时检查）

创建一个静态分析工具，在编译时检查依赖声明是否正确。

#### 实现方式

```go
// tools/depcheck/main.go
package main

import (
    "go/ast"
    "go/parser"
    "go/token"
    "fmt"
)

// 检查插件依赖声明
func checkPluginDependencies(filename string) []Issue {
    fset := token.NewFileSet()
    file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
    if err != nil {
        return nil
    }
    
    var issues []Issue
    
    // 1. 查找插件结构体中的依赖字段
    structDeps := findDependencyFields(file)
    
    // 2. 查找 Dependencies() 方法返回的依赖
    declaredDeps := findDeclaredDependencies(file)
    
    // 3. 对比差异
    for dep := range structDeps {
        if !declaredDeps[dep] {
            issues = append(issues, Issue{
                Message: fmt.Sprintf("字段 %s 未在 Dependencies() 中声明", dep),
                Severity: "error",
            })
        }
    }
    
    return issues
}
```

#### 使用方式

```bash
# 在 CI/CD 中运行
go run tools/depcheck/main.go ./plugins/...

# 输出示例
plugins/core/admin/admin.go:20: error: 字段 permPlugin 未在 Dependencies() 中声明
```

#### 优点

✅ **编译时检查**: 在编译阶段发现问题  
✅ **CI 集成**: 可集成到 CI/CD 流程  
✅ **强制规范**: 确保所有插件遵循规范  
✅ **无运行时开销**: 不影响运行时性能

---

### 方案 3: 依赖注册器模式

使用注册器模式集中管理依赖。

#### 实现方式

```go
// Plugin 结构
type Plugin struct {
    *plugin.BasePlugin
    deps *plugin.DependencyRegistry
}

// 在 New() 中注册依赖
func New() *Plugin {
    p := &Plugin{
        BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
        deps:       plugin.NewDependencyRegistry(),
    }
    
    // 注册依赖
    p.deps.Register("permission", &p.permPlugin)
    p.deps.Register("manager", &p.pluginManager)
    
    return p
}

// Dependencies 自动从注册器获取
func (p *Plugin) Dependencies() []string {
    return p.deps.List()
}

// SetDependency 由插件管理器调用
func (p *Plugin) SetDependency(name string, dep interface{}) error {
    return p.deps.Inject(name, dep)
}
```

#### DependencyRegistry 实现

```go
type DependencyRegistry struct {
    mu   sync.RWMutex
    deps map[string]interface{}
}

func NewDependencyRegistry() *DependencyRegistry {
    return &DependencyRegistry{
        deps: make(map[string]interface{}),
    }
}

func (r *DependencyRegistry) Register(name string, target interface{}) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.deps[name] = target
}

func (r *DependencyRegistry) List() []string {
    r.mu.RLock()
    defer r.mu.RUnlock()
    
    result := make([]string, 0, len(r.deps))
    for name := range r.deps {
        result = append(result, name)
    }
    return result
}

func (r *DependencyRegistry) Inject(name string, value interface{}) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    
    target, exists := r.deps[name]
    if !exists {
        return fmt.Errorf("dependency %s not registered", name)
    }
    
    // 使用反射设置值
    v := reflect.ValueOf(target)
    if v.Kind() == reflect.Ptr && v.Elem().CanSet() {
        v.Elem().Set(reflect.ValueOf(value))
    }
    
    return nil
}
```

#### 优点

✅ **集中管理**: 依赖在一个地方注册  
✅ **自动同步**: 注册和声明自动一致  
✅ **类型安全**: 编译时类型检查  
✅ **灵活注入**: 支持动态注入

---

### 方案 4: 接口依赖声明

通过接口明确依赖关系。

#### 实现方式

```go
// 1. 定义依赖接口
type PermissionDependent interface {
    SetPermissionPlugin(*permission.Plugin)
}

type PluginManagerDependent interface {
    SetPluginManager(*plugin.Manager)
}

// 2. 插件实现接口
type Plugin struct {
    *plugin.BasePlugin
    permPlugin    *permission.Plugin
    pluginManager *plugin.Manager
}

func (p *Plugin) SetPermissionPlugin(pp *permission.Plugin) {
    p.permPlugin = pp
}

func (p *Plugin) SetPluginManager(pm *plugin.Manager) {
    p.pluginManager = pm
}

// 3. 插件管理器自动检测接口并注入
func (m *Manager) LoadPlugin(p Plugin) error {
    // 检测并注入 permission 依赖
    if dep, ok := p.(PermissionDependent); ok {
        if perm := m.GetPlugin("permission"); perm != nil {
            dep.SetPermissionPlugin(perm.(*permission.Plugin))
        }
    }
    
    // 检测并注入 manager 依赖
    if dep, ok := p.(PluginManagerDependent); ok {
        dep.SetPluginManager(m)
    }
    
    return p.Load(m.engine)
}

// 4. Dependencies() 基于接口自动生成
func (p *Plugin) Dependencies() []string {
    var deps []string
    
    // 通过接口判断依赖
    if _, ok := interface{}(p).(PermissionDependent); ok {
        deps = append(deps, "permission")
    }
    if _, ok := interface{}(p).(PluginManagerDependent); ok {
        // manager 不算插件依赖
    }
    
    return deps
}
```

#### 优点

✅ **明确契约**: 接口清晰定义依赖关系  
✅ **自动注入**: 管理器自动检测并注入  
✅ **类型安全**: 编译时检查  
✅ **易于发现**: 通过接口可以发现所有依赖

---

### 方案 5: Build 标签 + 代码生成

使用 Go 的 generate 功能自动生成依赖代码。

#### 实现方式

```go
// admin.go
//go:generate go run ../../tools/genep/main.go -pkg admin -out admin_deps.go

type Plugin struct {
    *plugin.BasePlugin
    permPlugin    *permission.Plugin // @inject:plugin:permission
    pluginManager *plugin.Manager     // @inject:manager
}

// admin_deps.go (自动生成)
// Code generated by genep. DO NOT EDIT.

func (p *Plugin) Dependencies() []string {
    return []string{"permission"}
}

func (p *Plugin) InjectDependencies(deps map[string]interface{}) error {
    if v, ok := deps["permission"]; ok {
        p.permPlugin = v.(*permission.Plugin)
    }
    if v, ok := deps["manager"]; ok {
        p.pluginManager = v.(*plugin.Manager)
    }
    return nil
}
```

#### 优点

✅ **自动生成**: 代码自动生成，无需手写  
✅ **编译时检查**: 生成的代码在编译时检查  
✅ **性能优异**: 无运行时反射开销  
✅ **IDE 友好**: 生成的代码可以被 IDE 识别

---

## 方案对比

| 方案 | 自动化程度 | 类型安全 | 运行时开销 | 实现难度 | 推荐度 |
|------|-----------|---------|-----------|---------|--------|
| 方案1: 标签注入 | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| 方案2: 静态检查 | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| 方案3: 注册器 | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| 方案4: 接口 | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐⭐ |
| 方案5: 代码生成 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |

## 推荐实施方案

### 短期方案（立即实施）

**方案 1: 元数据声明 + BasePlugin 默认实现**

这是最简单、影响最小的方案：

```go
// 1. 在 BasePlugin 中提供默认实现
func (p *BasePlugin) Dependencies() []string {
    if p.metadata != nil {
        return p.metadata.Dependencies
    }
    return []string{}
}

// 2. 插件只需在元数据中声明依赖
func New() *Plugin {
    metadata := &plugin.Metadata{
        Name:         "admin",
        Dependencies: []string{"permission"},  // ✅ 在这里声明
    }
    return &Plugin{
        BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
    }
}

// 3. 不再需要手动实现 Dependencies() 方法
// func (p *Plugin) Dependencies() []string {
//     return []string{"permission"}  // ❌ 不再需要
// }
```

**优点:**
- ✅ 实现简单，改动最小
- ✅ 向后兼容
- ✅ 依赖在一个地方声明
- ✅ 减少重复代码

### 中期方案（逐步推进）

**方案 2: 静态检查工具**

开发一个 linter 工具：

```bash
# 检查所有插件的依赖声明
go run tools/depcheck/main.go ./plugins/...

# 在 CI 中强制检查
make lint-deps
```

### 长期方案（终极目标）

**方案 5: 代码生成 + 接口注入**

结合代码生成和接口注入：

```go
//go:generate go run ../../tools/gendeps/main.go

type Plugin struct {
    *plugin.BasePlugin
    permPlugin *permission.Plugin // @dep:permission
}

// 自动生成 Dependencies() 和注入方法
```

## 实施建议

### 第一步：立即改进

1. **在 BasePlugin 中添加默认实现**
   ```go
   func (p *BasePlugin) Dependencies() []string {
       if p.metadata != nil {
           return p.metadata.Dependencies
       }
       return []string{}
   }
   ```

2. **更新现有插件，移除手动 Dependencies() 实现**
   ```go
   // 删除这个方法
   // func (p *Plugin) Dependencies() []string {
   //     return []string{"permission"}
   // }
   
   // 在元数据中声明即可
   metadata := &plugin.Metadata{
       Dependencies: []string{"permission"},
   }
   ```

### 第二步：添加检查

1. **创建简单的依赖检查脚本**
2. **集成到 CI/CD**
3. **添加文档说明**

### 第三步：长期优化

1. **开发代码生成工具**
2. **迁移到自动依赖注入**
3. **完善工具链**

## 最佳实践

### ✅ DO（推荐做法）

1. **在元数据中声明依赖**
   ```go
   metadata := &plugin.Metadata{
       Dependencies: []string{"permission", "storage"},
   }
   ```

2. **使用 BasePlugin 的默认实现**
   ```go
   // 不需要手动实现 Dependencies()
   ```

3. **命名规范一致**
   ```go
   // 依赖名称应与插件名称一致
   Dependencies: []string{"permission"}  // ✅
   permPlugin: *permission.Plugin
   ```

4. **添加注释说明依赖关系**
   ```go
   type Plugin struct {
       // permPlugin 权限插件依赖，用于权限检查
       permPlugin *permission.Plugin  // depends on: permission
   }
   ```

### ❌ DON'T（避免做法）

1. **不要重复实现 Dependencies()**
   ```go
   // ❌ 避免
   func (p *Plugin) Dependencies() []string {
       return []string{"permission"}  // 元数据中已声明
   }
   ```

2. **不要依赖名称不一致**
   ```go
   // ❌ 避免
   Dependencies: []string{"perm"}  // 插件名是 permission
   ```

3. **不要忘记更新依赖声明**
   ```go
   // ❌ 避免
   type Plugin struct {
       newDep *other.Plugin  // 添加了依赖但未在元数据中声明
   }
   ```

## 总结

**推荐方案组合:**

1. **短期（立即）**: 方案 1 - 元数据声明 + BasePlugin 默认实现
2. **中期（3个月内）**: 方案 2 - 添加静态检查工具
3. **长期（6个月内）**: 方案 5 - 代码生成 + 自动注入

这样可以：
- ✅ 立即解决当前问题
- ✅ 保持向后兼容
- ✅ 逐步提升自动化程度
- ✅ 最终实现零手动维护

---

**关键要点:**

1. 🎯 **核心原则**: 依赖应该只在一个地方声明
2. 🔧 **实施策略**: 先简单改进，再逐步优化
3. 📝 **最佳实践**: 使用元数据声明，避免手动实现
4. 🛠️ **工具支持**: 添加静态检查，防止遗漏
5. 🚀 **长期目标**: 自动化依赖管理

