# Debug Plugin Demo

这个示例展示了如何使用 Debug 插件进行开发调试。

## 功能演示

- ✅ 查看事件详情
- ✅ 检查上下文信息
- ✅ 查看命令匹配器
- ✅ 运行时信息监控
- ✅ 命令和插件状态查看
- ✅ 性能测试
- ✅ 系统统计

## 环境要求

- Go 1.21+
- QQ 机器人 AppID 和 Token

## 配置

### 1. 设置环境变量

```bash
# Windows PowerShell
$env:BOT_APPID="your_app_id"
$env:BOT_TOKEN="your_token"
$env:ADMIN_USER_ID="your_user_id"  # 可选，用于权限控制

# Linux/Mac
export BOT_APPID="your_app_id"
export BOT_TOKEN="your_token"
export ADMIN_USER_ID="your_user_id"
```

### 2. 运行示例

```bash
cd examples/debug-demo
go run main.go
```

## 使用方法

### 1. 查看事件详情

向机器人发送消息：

```
/debug event
```

机器人会返回当前事件的详细信息，包括：
- 事件类型
- 事件 ID
- 消息内容
- 用户信息
- 群组/频道信息
- 原始数据

### 2. 查看上下文信息

```
/debug ctx
```

查看当前上下文的：
- 标准 Context 信息
- 扩展数据
- 中间件执行链
- 解析的命令
- 重试信息

### 3. 查看命令匹配器

```
/debug matcher help
```

查看指定命令的详细定义：
- 命令名称和描述
- 使用方法
- 参数列表
- 示例
- 支持的事件类型

### 4. 查看运行时信息

```
/debug runtime
```

查看系统运行时信息：
- Goroutine 数量
- 内存使用情况
- CPU 信息
- Go 版本
- 操作系统

### 5. 查看所有命令

```
/debug commands
```

列出所有已注册的命令，按事件类型分组。

### 6. 查看所有插件

```
/debug plugins
```

查看所有插件的状态：
- 插件名称和版本
- 作者和分类
- 运行状态
- 运行时长
- 错误信息
- 依赖关系

### 7. 性能测试

```
/debug bench help
```

测试指定命令的执行性能。

### 8. 系统统计

```
/debug stats
```

查看系统统计信息：
- 命令总数和分布
- 匹配器总数
- 插件总数和状态
- 运行时资源使用

## 输出示例

### 事件详情示例

```
🔍 事件详情
========================================

📋 事件类型: C2C_MESSAGE_CREATE
🆔 事件ID: 123456789
💬 消息内容: /debug event
👤 用户ID: user_123
👤 用户名: 张三
📨 消息ID: msg_456

📦 原始数据:
```json
{
  "type": "C2C_MESSAGE_CREATE",
  "id": "123456789",
  "d": {...}
}
```
```

### 上下文信息示例

```
🔍 上下文信息
========================================

📝 标准 Context:
  - Err: <nil>
  - Deadline: 无

🔌 扩展数据:
  - user_role: admin
  - request_id: req_123

🔗 中间件链:
  1. logger
  2. recovery
  3. permission

⚙️ 解析的命令:
  - 命令: debug ctx
  - 参数数量: 0

🔄 重试信息:
  - 重试次数: 0
```

### 运行时信息示例

```
🔍 运行时信息
========================================

🔀 Goroutine 数量: 15

💾 内存使用:
  - 分配内存: 12.34 MB
  - 总分配内存: 45.67 MB
  - 系统内存: 23.45 MB
  - GC 次数: 3

🖥️ CPU 信息:
  - CPU 核心数: 8
  - GOMAXPROCS: 8

📦 Go 版本:
  - go1.21.5

🖥️ 操作系统:
  - windows/amd64
```

## 调试技巧

### 1. 快速定位问题

当命令没有响应时：
1. 使用 `/debug commands` 检查命令是否已注册
2. 使用 `/debug event` 查看事件类型是否正确
3. 使用 `/debug ctx` 检查上下文是否包含预期数据

### 2. 性能优化

1. 使用 `/debug bench <命令>` 测试命令性能
2. 使用 `/debug runtime` 监控内存和 Goroutine 数量
3. 使用 `/debug stats` 查看系统整体状态

### 3. 插件调试

1. 使用 `/debug plugins` 查看插件状态
2. 检查插件是否正确加载
3. 查看插件运行时长和错误信息

## 注意事项

1. **权限控制**
   - Debug 插件包含敏感信息
   - 建议只在开发环境使用
   - 生产环境应严格控制访问权限

2. **性能影响**
   - 频繁使用 `/debug runtime` 可能影响性能
   - 性能测试会创建额外的上下文对象

3. **数据安全**
   - 事件详情可能包含用户隐私信息
   - 建议只在私聊中使用
   - 不要将调试输出分享给他人

## 故障排除

### 问题：权限不足

**解决方案**：
```bash
# 设置管理员用户 ID
$env:ADMIN_USER_ID="your_user_id"
```

或在代码中禁用权限检查：
```go
debugPlugin.SetDevMode(true)
```

### 问题：命令无响应

**解决方案**：
1. 检查机器人是否在线
2. 使用 `/debug commands` 查看命令列表
3. 检查日志输出

### 问题：插件未加载

**解决方案**：
检查插件注册代码是否正确执行：
```go
if err := pm.Register("debug", debugPlugin); err != nil {
    logger.Fatal("注册失败:", err)
}
```

## 扩展阅读

- [Debug 插件 README](../../plugins/dev/debug/README.md)
- [插件开发指南](../../docs/04-development/plugin-development.md)
- [调试最佳实践](../../docs/04-development/debugging.md)

## 相关示例

- [基础机器人示例](../basic-bot/)
- [插件示例](../plugin-example/)
- [生产环境示例](../production-ready/)

