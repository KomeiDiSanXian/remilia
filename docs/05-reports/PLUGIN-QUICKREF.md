# Remilia 插件开发快速参考

**更新日期**: 2026-02-07

---

## 📋 待开发插件清单

### 🔴 P0 - 必需（第一优先级）

- [ ] **Admin Plugin** - 机器人管理核心插件
- [ ] **Permission Plugin** - 权限系统插件

### 🟡 P1 - 重要（高优先级）

- [ ] **Storage Plugin** - 统一数据存储抽象层
- [ ] **Cache Plugin** - 高性能缓存插件
- [ ] **Stats Plugin** - 统计和分析插件
- [ ] **Monitor Plugin** - 系统监控和告警
- [ ] **Translate Plugin** - 多语言翻译
- [ ] **AI Chat Plugin** - AI 对话集成
- [ ] **Custom Command Plugin** - 用户自定义命令
- [ ] **Debug Plugin** - 开发调试工具

### 🟢 P2 - 一般（中优先级）

**实用工具**：
- [ ] **Weather Plugin** - 天气查询
- [ ] **Search Plugin** - 多平台搜索
- [ ] **Reminder Plugin** - 定时提醒
- [ ] **Note Plugin** - 云笔记管理

**管理监控**：
- [ ] **Logger Plugin** - 增强日志记录
- [ ] **Backup Plugin** - 数据备份恢复

**扩展服务**：
- [ ] **RSS Plugin** - RSS 订阅推送
- [ ] **GitHub Plugin** - GitHub 集成
- [ ] **Image Plugin** - 图片处理

**社区功能**：
- [ ] **Welcome Plugin** - 欢迎新成员
- [ ] **AutoReply Plugin** - 自动回复

**开发工具**：
- [ ] **Test Plugin** - 插件测试工具

### 🔵 P3 - 可选（低优先级）

**娱乐互动**：
- [ ] **Random Plugin** - 随机选择工具
- [ ] **Game Plugin** - 文字游戏
- [ ] **Joke Plugin** - 笑话段子
- [ ] **Fortune Plugin** - 运势占卜

**扩展服务**：
- [ ] **Music Plugin** - 音乐点播
- [ ] **Short URL Plugin** - 短链接生成

**开发工具**：
- [ ] **Benchmark Plugin** - 性能基准测试

---

## 🚀 快速开始

### 创建新插件

```bash
# 1. 创建插件目录
mkdir -p plugins/category/pluginname

# 2. 创建主文件
cd plugins/category/pluginname
touch plugin.go plugin_test.go README.md
```

### 插件模板

```go
package pluginname

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

type Plugin struct {
    *plugin.BasePlugin
}

func New() *Plugin {
    return &Plugin{
        BasePlugin: plugin.NewBasePluginWithMetadata(&plugin.Metadata{
            Name:        "pluginname",
            Version:     "1.0.0",
            Author:      "Your Name",
            Description: "插件描述",
            Category:    "分类",
            Tags:        []string{"标签"},
            HelpText:    "使用帮助",
        }),
    }
}

func (p *Plugin) Load(eng *engine.Engine) error {
    p.OnCommand(eng, dto.C2CMessageCreate, "/command").
        Handle(p.handleCommand)
    return nil
}

func (p *Plugin) handleCommand(ctx *context.Context) error {
    // 实现命令处理逻辑
    return ctx.Reply("响应消息")
}
```

---

## 📦 推荐开发顺序

### 第 1 周：核心基础
1. Admin Plugin（管理核心）
2. Permission Plugin（权限系统）

### 第 2 周：数据层
3. Storage Plugin（存储抽象）
4. Cache Plugin（缓存系统）

### 第 3 周：监控统计
5. Stats Plugin（统计分析）
6. Monitor Plugin（系统监控）

### 第 4 周：实用工具
7. Translate Plugin（翻译）
8. AI Chat Plugin（AI 对话）

### 第 5-6 周：功能扩展
9. Custom Command Plugin
10. Debug Plugin
11. Weather Plugin
12. RSS Plugin

### 第 7-8 周：社区功能
13. Welcome Plugin
14. AutoReply Plugin
15. Logger Plugin
16. Backup Plugin

---

## 🛠️ 开发工具

### 插件生成器（待开发）

```bash
# 生成插件骨架
remilia plugin new <name> --category=<category> --author=<author>
```

### 测试命令

```bash
# 运行单元测试
go test ./plugins/category/pluginname/...

# 运行集成测试
go test -tags=integration ./plugins/category/pluginname/...

# 代码覆盖率
go test -cover ./plugins/category/pluginname/...
```

---

## 📚 相关文档

- **详细规划**: [PLUGIN-ECOSYSTEM-PLAN.md](./PLUGIN-ECOSYSTEM-PLAN.md)
- **开发指南**: `docs/02-user-guides/PLUGIN_ENHANCEMENT_QUICKREF.md`
- **架构设计**: `docs/03-architecture/BUILTIN_PLUGINS_DESIGN.md`

---

## 🤝 贡献指南

1. **Fork 项目**
2. **创建特性分支**: `git checkout -b feature/new-plugin`
3. **提交更改**: `git commit -am 'Add new plugin'`
4. **推送到分支**: `git push origin feature/new-plugin`
5. **创建 Pull Request**

### 代码审查标准

- ✅ 完整的单元测试（覆盖率 > 80%）
- ✅ GoDoc 文档注释
- ✅ README.md 使用说明
- ✅ 遵循项目代码规范
- ✅ 通过所有 CI 检查

---

## 📊 进度追踪

当前进度：**1/30** (3.3%)

- ✅ Help Plugin
- ⏳ 29 个插件待开发

预计完成时间：2026-06

---

**维护者**: Remilia Team  
**联系方式**: GitHub Issues  
**最后更新**: 2026-02-07

