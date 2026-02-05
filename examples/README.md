# Remilia Examples

本目录包含 Remilia 框架的各种示例，帮助你快速上手。

**更新状态**: ✅ 全部13个示例已更新并测试通过 (2026-02-05)

---

## 📚 示例列表

### 🚀 入门示例

#### [basic-bot](./basic-bot/) ⭐ 推荐新手
最简单的 Bot 示例，展示基础功能。

**功能**:
- Echo 命令
- Ping/Pong
- 使用 BotBuilder
- 使用简化中间件

**适合**: 完全的新手，想要快速创建第一个Bot

**状态**: ✅ 已更新，编译通过

---

#### [middleware-example](./middleware-example/) ⭐ 推荐
展示4种使用中间件的方式。

**功能**:
- 预定义中间件集
- 简化工厂
- 中间件构建器
- 自定义配置

**适合**: 想要了解中间件系统的开发者

**状态**: ✅ 已更新，编译通过

---

### 🎯 进阶示例

#### [command-bot](./command-bot/)
完整的命令系统示例。

**功能**:
- 命令注册
- 多个命令演示
- 帮助系统

**适合**: 需要构建命令系统的开发者

**状态**: ✅ 已更新，编译通过

---

#### [plugin-example](./plugin-example/)
插件开发示例。

**功能**:
- 插件创建
- 插件生命周期
- 多个插件协作

**适合**: 想要开发插件的开发者

**状态**: ✅ 已更新，编译通过

---

#### [config_hotreload](./config_hotreload/)
配置热更新示例。

**功能**:
- 配置文件监听
- 动态更新配置
- 配置变更回调

**适合**: 需要配置热更新的开发者

**状态**: ✅ 已更新，编译通过

---

### 🏭 生产实践示例

#### [production-ready](./production-ready/) ⭐⭐⭐ 重要
生产环境最佳实践。

**功能**:
- 生产级中间件配置
- 完善的错误处理
- 慢请求监控
- 健康检查
- 优雅关闭

**适合**: 准备部署到生产环境的开发者

**状态**: ✅ 新增，编译通过

---

#### [error-handling](./error-handling/) ⭐⭐ 推荐
完善的错误处理示例。

**功能**:
- 多种错误类型
- 自定义错误
- 重试机制
- Panic恢复

**适合**: 需要健壮错误处理的开发者

**状态**: ✅ 新增，编译通过

---

#### [metrics-monitoring](./metrics-monitoring/) ⭐⭐ 推荐
性能监控示例。

**功能**:
- 请求统计
- 延迟跟踪
- 系统资源监控
- 慢请求检测

**适合**: 需要监控Bot性能的开发者

**状态**: ✅ 新增，编译通过

---

#### [async-tasks](./async-tasks/)
异步任务处理示例。

**功能**:
- 异步任务创建
- 状态跟踪
- 进度更新
- 任务管理

**适合**: 需要处理长时间任务的开发者

**状态**: ✅ 新增，编译通过

---

### 🔧 专题示例

#### [handler-chain](./handler-chain/)
处理器链模式示例。

**功能**:
- 责任链模式
- Handler 组合

**状态**: ✅ 已更新，编译通过

---

#### [help-discovery](./help-discovery/)
自动帮助系统示例。

**功能**:
- 命令发现
- 自动生成帮助

**状态**: ✅ 已更新，编译通过

---

#### [command-integration](./command-integration/)
命令集成测试示例。

**功能**:
- 命令集成
- 测试验证

**状态**: ✅ 已更新，编译通过

---

#### [command-conflict-test](./command-conflict-test/)
命令冲突测试示例。

**功能**:
- 冲突检测
- 行为验证

**状态**: ✅ 已更新，编译通过

---

## 🎯 快速开始

### 1. 选择示例

根据你的需求选择合适的示例：

- **第一次使用?** → [basic-bot](./basic-bot/)
- **了解中间件?** → [middleware-example](./middleware-example/)
- **构建命令系统?** → [command-bot](./command-bot/)
- **开发插件?** → [plugin-example](./plugin-example/)

### 2. 配置示例

每个示例都有 `config.example.yaml`：

```bash
cd basic-bot
cp config.example.yaml config.yaml
# 编辑 config.yaml 填入你的机器人信息
```

### 3. 运行示例

```bash
go run main.go
```

---

## 📖 学习路径

### 新手路径

1. [basic-bot](./basic-bot/) - 创建第一个Bot
2. [middleware-example](./middleware-example/) - 了解中间件
3. [command-bot](./command-bot/) - 学习命令系统

### 进阶路径

1. [plugin-example](./plugin-example/) - 开发插件
2. [config-integration](./config-integration/) - 配置管理
3. [handler-chain](./handler-chain/) - 高级模式

---

## 🔧 通用配置

所有示例都使用相同的配置格式：

```yaml
bot:
  app_id: 123456          # 你的 AppID
  bot_id: 654321          # 你的机器人 QQ 号
  token: "YOUR_TOKEN"     # 你的 Token
  secret: "YOUR_SECRET"   # 你的 Secret

server:
  host: "0.0.0.0"
  port: 8080

log:
  level: "info"           # trace, debug, info, warn, error
  format: "text"          # json, text
```

---

## 📊 示例对比

| 示例 | 难度 | 代码行数 | 学习时间 | 适用场景 | 状态 |
|------|------|---------|---------|---------|------|
| basic-bot | ⭐ 简单 | ~107 | 10分钟 | 快速上手 | ✅ |
| middleware-example | ⭐⭐ 简单 | ~146 | 15分钟 | 了解中间件 | ✅ |
| command-bot | ⭐⭐⭐ 中等 | ~165 | 20分钟 | 命令系统 | ✅ |
| plugin-example | ⭐⭐⭐⭐ 中等 | ~236 | 30分钟 | 插件开发 | ✅ |
| config_hotreload | ⭐⭐ 简单 | ~77 | 15分钟 | 配置管理 | ✅ |
| **production-ready** | ⭐⭐⭐⭐⭐ 高级 | ~228 | 45分钟 | **生产部署** | ✅ |
| **error-handling** | ⭐⭐⭐ 中等 | ~351 | 30分钟 | **错误处理** | ✅ |
| **metrics-monitoring** | ⭐⭐⭐⭐ 高级 | ~321 | 40分钟 | **性能监控** | ✅ |
| **async-tasks** | ⭐⭐⭐⭐ 高级 | ~285 | 35分钟 | **异步任务** | ✅ |
| handler-chain | ⭐⭐⭐ 中等 | ~94 | 20分钟 | 高级模式 | ✅ |
| help-discovery | ⭐⭐ 简单 | ~75 | 15分钟 | Help系统 | ✅ |
| command-integration | ⭐⭐ 简单 | ~59 | 10分钟 | 集成测试 | ✅ |
| command-conflict-test | ⭐⭐ 简单 | ~69 | 10分钟 | 冲突测试 | ✅ |

**总计**: 13个示例，全部可用 ✅

**新增**: 4个生产实践示例 🎉

---

## 💡 使用技巧

### 1. 从简单开始

不要直接跳到复杂示例，按顺序学习：
```
basic-bot → middleware-example → command-bot → plugin-example
```

### 2. 修改代码实验

每个示例都鼓励你修改代码：
- 添加新命令
- 调整中间件配置
- 尝试不同的组合

### 3. 查看日志

运行示例时注意观察日志输出，了解执行流程：
```bash
go run main.go 2>&1 | tee bot.log
```

### 4. 使用调试模式

在 `config.yaml` 中设置：
```yaml
log:
  level: "debug"  # 查看详细日志
```

---

## 🐛 常见问题

### Q: 示例无法启动

**A**: 检查以下几点：
1. 是否创建了 `config.yaml`
2. 配置文件中的信息是否正确
3. 端口是否被占用
4. Go 版本是否 ≥ 1.19

### Q: 修改代码后不生效

**A**: 确保重新编译：
```bash
go clean -cache
go run main.go
```

### Q: 找不到某个包

**A**: 更新依赖：
```bash
go mod tidy
go mod download
```

### Q: 机器人没有响应

**A**: 检查：
1. 机器人是否启动成功
2. Webhook 地址是否正确配置
3. 命令格式是否正确（如 `/ping`）
4. 日志中是否有错误信息

---

## 📚 相关文档

### 入门文档
- [快速开始](../docs/01-getting-started/GETTING_STARTED.md)
- [故障排除](../docs/01-getting-started/TROUBLESHOOTING.md)

### 用户指南
- [最佳实践](../docs/02-user-guides/BEST_PRACTICES.md)
- [工厂函数指南](../docs/02-user-guides/FACTORY_FUNCTIONS_GUIDE.md)
- [配置快速参考](../docs/02-user-guides/CONFIGURATION_QUICKREF.md)

### 架构设计
- [并发事件处理](../docs/03-architecture/CONCURRENT_EVENT_PROCESSING.md)
- [插件系统设计](../docs/03-architecture/BUILTIN_PLUGINS_DESIGN.md)

---

## 🤝 贡献示例

如果你创建了新的示例，欢迎贡献：

1. 在 `examples/` 下创建新目录
2. 添加 `main.go`、`config.example.yaml`、`README.md`
3. 确保代码可运行
4. 更新本 README

---

## 📝 最佳实践

所有示例遵循以下最佳实践：

1. ✅ **使用 BotBuilder** - 流畅的创建接口
2. ✅ **使用简化中间件** - ProductionSet/DevelopmentSet
3. ✅ **配置文件驱动** - 所有配置来自 config.yaml
4. ✅ **完善的日志** - 结构化日志记录
5. ✅ **优雅关闭** - 使用 WaitForShutdown()
6. ✅ **错误处理** - 完善的错误处理和恢复

---

**最后更新**: 2026年2月5日  
**示例总数**: 13个  
**推荐起点**: [basic-bot](./basic-bot/) ⭐
