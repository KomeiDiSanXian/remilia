# 包合并快速参考指南

## 🎯 核心变更

### 已删除的包
- ❌ `extension/`
- ❌ `permission/`
- ❌ `internal/`

### 新增文件
- ✅ `core/context/command_extension.go`
- ✅ `core/context/permission.go`

---

## 📦 导入路径变更速查

### 命令解析
```go
// ❌ 旧代码
import "github.com/KomeiDiSanXian/remilia/extension"
args, _ := extension.ParseCommand(ctx)

// ✅ 新代码
import "github.com/KomeiDiSanXian/remilia/core/context"
args, _ := context.ParseCommand(ctx)
```

### 权限管理
```go
// ❌ 旧代码
import "github.com/KomeiDiSanXian/remilia/permission"
pm := permission.NewManager()
perm := permission.Permission{...}

// ✅ 新代码
import "github.com/KomeiDiSanXian/remilia/core/context"
pm := context.NewPermissionManager()
perm := context.Permission{...}
```

---

## 🔄 类型映射表

| 旧类型 | 新类型 |
|---|---|
| `extension.Command` | `context.CommandExtension` |
| `permission.Permission` | `context.Permission` |
| `permission.Role` | `context.Role` |
| `permission.Manager` | `context.PermissionManager` |
| `permission.Provider` | `context.PermissionProvider` |

---

## ✅ 验证检查清单

- [x] 删除旧包 (extension, permission, internal)
- [x] 创建新文件 (command_extension.go, permission.go)
- [x] 更新内部引用 (context.go, convenience.go)
- [x] 编译测试通过
- [x] 所有单元测试通过
- [x] 代码覆盖率保持

---

## 📊 项目结构

```
remilia/
├── core/
│   ├── context/               ← 合并后的统一包
│   │   ├── context.go
│   │   ├── extensions.go
│   │   ├── command_helper.go
│   │   ├── command_extension.go   ← 新增
│   │   ├── permission.go          ← 新增
│   │   ├── rules.go
│   │   ├── convenience.go
│   │   └── types.go
│   └── engine/
├── command/
├── config/
├── helper/
└── ... (其他包保持不变)
```

---

## 🚀 使用示例

### 完整示例

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia/core/context"
    "github.com/KomeiDiSanXian/remilia/core/engine"
)

func main() {
    // 创建引擎
    bot := engine.New()
    
    // 创建权限管理器
    pm := context.NewPermissionManager()
    
    // 注册处理器
    bot.On(context.OnGroupAtMessage()).Handle(func(ctx *context.Context) {
        // 解析命令
        args, err := context.ParseCommand(ctx)
        if err != nil {
            return
        }
        
        // 检查权限
        perm := context.Permission{
            Resource: "command:" + args.Command,
            Action:   "execute",
        }
        
        if !pm.HasPermission(ctx.GetUserID(), perm) {
            ctx.SendPlainMessage("权限不足")
            return
        }
        
        // 执行命令逻辑
        ctx.SendPlainMessage("命令执行成功")
    })
    
    bot.Start()
}
```

---

*最后更新: 2026-01-22*
