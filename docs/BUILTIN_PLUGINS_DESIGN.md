# Remilia 内置插件设计方案

**版本**: v1.0  
**日期**: 2026-01-24  
**状态**: 设计阶段

---

## 📋 目录

1. [概述](#概述)
2. [插件分类](#插件分类)
3. [核心功能插件](#核心功能插件)
4. [管理与监控插件](#管理与监控插件)
5. [实用工具插件](#实用工具插件)
6. [扩展功能插件](#扩展功能插件)
7. [AI集成插件](#ai集成插件)
8. [实施路线图](#实施路线图)

---

## 概述

### 设计目标

1. **开箱即用**: 提供常用功能，降低使用门槛
2. **模块化**: 每个插件独立，可按需启用/禁用
3. **可配置**: 支持通过配置文件灵活定制
4. **高性能**: 不影响框架核心性能
5. **易扩展**: 为第三方插件提供参考实现

### 插件架构

```
remilia/
├── plugin/
│   ├── builtin/              # 内置插件目录
│   │   ├── core/            # 核心功能插件
│   │   ├── admin/           # 管理与监控插件
│   │   ├── util/            # 实用工具插件
│   │   ├── extension/       # 扩展功能插件
│   │   └── ai/              # AI集成插件
│   ├── registry.go          # 插件注册表
│   └── loader.go            # 插件加载器
```

---

## 插件分类

### 1️⃣ 核心功能插件 (Core)
基础功能，大多数Bot需要

### 2️⃣ 管理与监控插件 (Admin)
Bot管理、监控、调试

### 3️⃣ 实用工具插件 (Util)
常用工具功能

### 4️⃣ 扩展功能插件 (Extension)
高级功能，特定场景

### 5️⃣ AI集成插件 (AI)
AI能力集成

---

## 核心功能插件

### 🔹 1. Echo Plugin (回声插件)

**功能**: 回复用户发送的消息内容

**使用场景**:
- 测试Bot连接性
- 调试消息格式
- 演示基础功能

**配置示例**:
```yaml
plugins:
  echo:
    enabled: true
    trigger: "/echo"         # 触发命令
    prefix: "你说: "          # 回复前缀
    max_length: 500          # 最大回复长度
    strip_mentions: true     # 去除@提及
```

**实现要点**:
```go
type EchoPlugin struct {
    *plugin.BasePlugin
    config EchoConfig
}

func (p *EchoPlugin) Load(eng *engine.Engine) error {
    m := eng.OnCommand(dto.GroupAtMessageCreate, p.config.Trigger)
    m.Handle(func(ctx *context.Context) error {
        content := ctx.GetMessageContent()
        // 去除命令前缀
        content = strings.TrimPrefix(content, p.config.Trigger)
        content = strings.TrimSpace(content)
        
        // 长度限制
        if len(content) > p.config.MaxLength {
            content = content[:p.config.MaxLength] + "..."
        }
        
        reply := p.config.Prefix + content
        return ctx.Reply(reply)
    })
    p.AddMatcher(m)
    return nil
}
```

**依赖**: 无

---

### 🔹 2. Help Plugin (帮助插件)

**功能**: 自动生成并显示所有可用命令的帮助信息

**状态**: ✅ **命令发现机制已实现**（详见 [HELP_PLUGIN_IMPLEMENTATION_REPORT.md](./HELP_PLUGIN_IMPLEMENTATION_REPORT.md)）

**使用场景**:
- 用户查询Bot功能
- 新用户引导
- 命令文档展示

**配置示例**:
```yaml
plugins:
  help:
    enabled: true
    trigger: "/help"
    show_aliases: true       # 显示命令别名
    group_by_plugin: true    # 按插件分组
    show_usage: true         # 显示用法示例
    format: "markdown"       # 格式: text/markdown
```

**核心机制**:

```go
// 1. 插件注册命令时设置元数据
m := eng.OnCommand(dto.GroupAtMessageCreate, "/echo")
m.SetDescription("回显用户发送的消息").
  SetUsage("/echo <消息内容>").
  SetCategory("实用工具").
  SetAliases("/repeat", "/mirror")

// 2. Help Plugin 通过 Engine API 发现命令
func (p *HelpPlugin) handleHelp(ctx *context.Context) error {
    // 获取所有命令
    commands := p.engine.GetAllCommands()
    
    // 按插件分组
    grouped := p.engine.GetCommandsByPlugin()
    
    // 按分类分组
    byCategory := p.engine.GetCommandsByCategory()
    
    // 查找特定命令
    cmd := p.engine.FindCommand(name)
    
    // 生成帮助文本
    return p.renderHelp(ctx, commands)
}
```

**功能特性**:
- ✅ 自动发现所有注册的命令（无需手动维护）
- ✅ 支持按插件/分类分组显示
- ✅ 支持别名匹配和查询
- ✅ 支持隐藏命令（不在帮助中显示）
- ✅ 支持分页（命令过多时）
- ✅ 支持查询单个命令详情: `/help <command>`
- ✅ 显示参数、选项、示例、权限等完整信息

**实现要点**:

**Matcher 元数据结构**:
```go
type MatcherMetadata struct {
    Description string   // 命令描述
    Usage       string   // 使用方法
    Aliases     []string // 别名
    Category    string   // 分类
    Examples    []string // 使用示例
    Permissions []string // 所需权限
    Hidden      bool     // 是否隐藏
    Arguments   []*ArgumentMeta
    Flags       []*FlagMeta
}
```

**Engine 发现 API**:
```go
// 获取所有命令
func (e *Engine) GetAllCommands() []CommandInfo

// 按插件分组
func (e *Engine) GetCommandsByPlugin() map[string][]CommandInfo

// 按分类分组
func (e *Engine) GetCommandsByCategory() map[string][]CommandInfo

// 查找命令（支持别名）
func (e *Engine) FindCommand(name string) *CommandInfo
```

**完整实现示例**: 参见 [examples/help-discovery](../examples/help-discovery/main.go)

**技术文档**: 
- [Help Plugin 设计方案](./HELP_PLUGIN_DESIGN.md)
- [Help Plugin 实施报告](./HELP_PLUGIN_IMPLEMENTATION_REPORT.md)

**依赖**: 无

---

### 🔹 3. Permission Plugin (权限插件)

**功能**: 基于角色的访问控制 (RBAC)

**使用场景**:
- 管理员命令保护
- 分级权限管理
- VIP功能限制

**配置示例**:
```yaml
plugins:
  permission:
    enabled: true
    storage: "file"          # file/redis/memory
    storage_path: "./data/permissions.json"
    
    roles:
      - name: "admin"
        permissions: ["*"]   # 所有权限
        
      - name: "moderator"
        permissions:
          - "kick_user"
          - "mute_user"
          - "manage_messages"
      
      - name: "vip"
        permissions:
          - "use_advanced_commands"
          - "bypass_rate_limit"
      
      - name: "user"
        permissions:
          - "use_basic_commands"
    
    # 用户角色映射
    user_roles:
      "user_id_12345": ["admin"]
      "user_id_67890": ["moderator", "vip"]
    
    # 默认角色
    default_role: "user"
    
    # 超级管理员（绕过所有检查）
    super_admins:
      - "user_id_00001"
```

**功能特性**:
- 角色继承（TODO）
- 动态权限更新
- 权限缓存（提升性能）
- 权限检查中间件

**实现要点**:
```go
type PermissionPlugin struct {
    *plugin.BasePlugin
    config  PermissionConfig
    storage PermissionStorage
    cache   *permissionCache
}

type PermissionStorage interface {
    GetUserRoles(userID string) ([]string, error)
    SetUserRoles(userID string, roles []string) error
    HasPermission(userID, permission string) (bool, error)
}

// 权限检查中间件
func (p *PermissionPlugin) RequirePermission(perm string) context.Middleware {
    return func(next context.Handler) context.Handler {
        return func(ctx *context.Context) error {
            userID := ctx.GetAuthor().ID
            
            // 检查超级管理员
            if p.isSuperAdmin(userID) {
                return next(ctx)
            }
            
            // 检查权限
            hasPermission, err := p.storage.HasPermission(userID, perm)
            if err != nil {
                return fmt.Errorf("permission check failed: %w", err)
            }
            
            if !hasPermission {
                return ctx.Reply("❌ 权限不足，需要权限: " + perm)
            }
            
            return next(ctx)
        }
    }
}

// 注册管理命令
func (p *PermissionPlugin) Load(eng *engine.Engine) error {
    // /permission grant <user> <role>
    m1 := eng.OnCommand(dto.GroupAtMessageCreate, "/permission grant")
    m1.Use(p.RequirePermission("manage_permissions"))
    m1.Handle(p.handleGrant)
    p.AddMatcher(m1)
    
    // /permission revoke <user> <role>
    m2 := eng.OnCommand(dto.GroupAtMessageCreate, "/permission revoke")
    m2.Use(p.RequirePermission("manage_permissions"))
    m2.Handle(p.handleRevoke)
    p.AddMatcher(m2)
    
    // /permission list [user]
    m3 := eng.OnCommand(dto.GroupAtMessageCreate, "/permission list")
    m3.Handle(p.handleList)
    p.AddMatcher(m3)
    
    return nil
}
```

**依赖**: 无

---

### 🔹 4. RateLimit Plugin (频率限制插件)

**功能**: 限制用户/群组命令调用频率

**使用场景**:
- 防止命令滥用
- 保护API配额
- 防止刷屏

**配置示例**:
```yaml
plugins:
  rate_limit:
    enabled: true
    storage: "memory"        # memory/redis
    
    # 全局限制
    global:
      max_requests: 100
      window: "1m"
    
    # 用户级别限制
    per_user:
      max_requests: 10
      window: "1m"
    
    # 群组级别限制
    per_group:
      max_requests: 50
      window: "1m"
    
    # 命令级别限制
    per_command:
      "/search":
        max_requests: 5
        window: "1m"
      "/image":
        max_requests: 3
        window: "5m"
    
    # 白名单（不受限制）
    whitelist_users:
      - "user_id_admin"
    
    # 限流响应
    response:
      enabled: true
      message: "⏰ 操作过于频繁，请稍后再试"
```

**功能特性**:
- 多级限流（全局/用户/群组/命令）
- 滑动窗口算法
- 令牌桶算法（可选）
- Redis支持（分布式场景）

**实现要点**:
```go
type RateLimitPlugin struct {
    *plugin.BasePlugin
    config  RateLimitConfig
    limiter RateLimiter
}

type RateLimiter interface {
    Allow(key string, limit int, window time.Duration) (bool, error)
    Remaining(key string) (int, error)
}

// 频率限制中间件
func (p *RateLimitPlugin) RateLimitMiddleware() context.Middleware {
    return func(next context.Handler) context.Handler {
        return func(ctx *context.Context) error {
            userID := ctx.GetAuthor().ID
            
            // 检查白名单
            if p.isWhitelisted(userID) {
                return next(ctx)
            }
            
            // 检查用户级别限制
            userKey := "user:" + userID
            allowed, err := p.limiter.Allow(
                userKey, 
                p.config.PerUser.MaxRequests,
                p.config.PerUser.Window,
            )
            
            if err != nil {
                logrus.WithError(err).Warn("Rate limit check failed")
                return next(ctx) // 限流失败时放行
            }
            
            if !allowed {
                if p.config.Response.Enabled {
                    return ctx.Reply(p.config.Response.Message)
                }
                return engine.NewBlockError("rate limit exceeded")
            }
            
            return next(ctx)
        }
    }
}

func (p *RateLimitPlugin) Load(eng *engine.Engine) error {
    // 应用全局限流中间件
    eng.Use(p.RateLimitMiddleware())
    
    // 注册查询命令
    m := eng.OnCommand(dto.GroupAtMessageCreate, "/ratelimit status")
    m.Handle(func(ctx *context.Context) error {
        userID := ctx.GetAuthor().ID
        remaining, _ := p.limiter.Remaining("user:" + userID)
        return ctx.Reply(fmt.Sprintf("⏱️ 剩余请求次数: %d", remaining))
    })
    p.AddMatcher(m)
    
    return nil
}
```

**依赖**: 无（可选 Redis）

---

### 🔹 5. Scheduler Plugin (定时任务插件)

**功能**: 定时执行任务和提醒

**使用场景**:
- 定时消息发送
- 周期性数据统计
- 定时任务执行

**配置示例**:
```yaml
plugins:
  scheduler:
    enabled: true
    timezone: "Asia/Shanghai"
    
    # 预定义任务
    tasks:
      - name: "daily_summary"
        schedule: "0 0 22 * * *"  # 每天22:00
        type: "send_message"
        target:
          group_id: "group_123"
        content: "📊 今日数据统计已生成"
      
      - name: "weekly_report"
        schedule: "0 0 18 * * 5"  # 每周五18:00
        type: "callback"
        handler: "generate_weekly_report"
```

**功能特性**:
- Cron表达式支持
- 持久化任务（重启后恢复）
- 动态添加/删除任务
- 任务执行历史记录

**实现要点**:
```go
type SchedulerPlugin struct {
    *plugin.BasePlugin
    config    SchedulerConfig
    scheduler *cron.Cron
    tasks     map[string]*ScheduledTask
    bot       *remilia.Bot
}

type ScheduledTask struct {
    Name     string
    Schedule string
    Type     string // send_message/callback
    Target   TaskTarget
    Content  string
    Handler  TaskHandler
    EntryID  cron.EntryID
}

type TaskHandler func(task *ScheduledTask) error

func (p *SchedulerPlugin) Load(eng *engine.Engine) error {
    // 初始化定时器
    p.scheduler = cron.New(cron.WithSeconds())
    
    // 加载预定义任务
    for _, taskConfig := range p.config.Tasks {
        if err := p.AddTask(taskConfig); err != nil {
            logrus.WithError(err).Warnf("Failed to add task: %s", taskConfig.Name)
        }
    }
    
    // 启动定时器
    p.scheduler.Start()
    
    // 注册管理命令
    p.registerCommands(eng)
    
    return nil
}

// /scheduler add <name> <cron> <type> <target> <content>
func (p *SchedulerPlugin) handleAddTask(ctx *context.Context) error {
    parsed := ctx.GetParsedCommand()
    name := parsed.GetString("name")
    schedule := parsed.GetString("cron")
    // ...
    
    task := &ScheduledTask{
        Name:     name,
        Schedule: schedule,
        // ...
    }
    
    if err := p.AddTask(task); err != nil {
        return ctx.Reply(fmt.Sprintf("❌ 添加任务失败: %v", err))
    }
    
    return ctx.Reply(fmt.Sprintf("✅ 任务 %s 已添加", name))
}
```

**依赖**: github.com/robfig/cron/v3

---

## 管理与监控插件

### 🔹 6. Admin Plugin (管理员插件)

**功能**: Bot管理和控制面板

**使用场景**:
- 在线管理Bot
- 查看运行状态
- 执行管理操作

**配置示例**:
```yaml
plugins:
  admin:
    enabled: true
    admins:                  # 管理员列表
      - "user_id_12345"
    
    commands:
      status: true           # 查看状态
      reload: true           # 重载插件
      shutdown: true         # 关闭Bot
      stats: true            # 统计信息
      logs: true             # 查看日志
```

**功能特性**:
- `/admin status` - 查看Bot状态
- `/admin reload <plugin>` - 热重载插件
- `/admin shutdown` - 优雅关闭Bot
- `/admin stats` - 查看统计信息
- `/admin logs [level]` - 查看最近日志
- `/admin plugin list` - 列出所有插件
- `/admin plugin enable <name>` - 启用插件
- `/admin plugin disable <name>` - 禁用插件

**实现要点**:
```go
type AdminPlugin struct {
    *plugin.BasePlugin
    config        AdminConfig
    pluginManager *plugin.Manager
    bot           *remilia.Bot
}

func (p *AdminPlugin) Load(eng *engine.Engine) error {
    // 所有命令都需要管理员权限
    adminCheck := p.requireAdmin()
    
    // /admin status
    m1 := eng.OnCommand(dto.C2CMessageCreate, "/admin status")
    m1.Use(adminCheck)
    m1.Handle(func(ctx *context.Context) error {
        health := p.bot.Health()
        uptime := p.bot.Uptime()
        
        status := fmt.Sprintf(
            "🤖 Bot状态\n"+
            "状态: %s\n"+
            "运行时间: %v\n"+
            "启动时间: %s\n"+
            "匹配器数量: %d\n"+
            "插件数量: %d",
            health.Status,
            uptime,
            health.StartTime.Format("2006-01-02 15:04:05"),
            eng.GetMatcherCount(),
            p.pluginManager.Count(),
        )
        
        return ctx.Reply(status)
    })
    p.AddMatcher(m1)
    
    // /admin reload <plugin>
    m2 := eng.OnCommand(dto.C2CMessageCreate, "/admin reload")
    m2.Use(adminCheck)
    m2.Handle(p.handleReload)
    p.AddMatcher(m2)
    
    // ... 其他命令
    
    return nil
}

func (p *AdminPlugin) requireAdmin() context.Middleware {
    return func(next context.Handler) context.Handler {
        return func(ctx *context.Context) error {
            userID := ctx.GetAuthor().ID
            if !p.isAdmin(userID) {
                return ctx.Reply("❌ 权限不足，仅管理员可用")
            }
            return next(ctx)
        }
    }
}
```

**依赖**: Permission Plugin (可选)

---

### 🔹 7. Metrics Plugin (指标插件)

**功能**: 暴露Prometheus指标和性能监控

**使用场景**:
- 性能监控
- 问题排查
- 容量规划

**配置示例**:
```yaml
plugins:
  metrics:
    enabled: true
    port: 9090
    path: "/metrics"
    
    collect:
      events: true           # 事件统计
      commands: true         # 命令统计
      latency: true          # 延迟统计
      errors: true           # 错误统计
      plugins: true          # 插件统计
```

**功能特性**:
- HTTP端点暴露指标
- 自定义指标注册
- 与 middleware.Prometheus 集成

**依赖**: infra/metrics

---

### 🔹 8. Logger Plugin (日志插件)

**功能**: 高级日志记录和管理

**使用场景**:
- 审计日志
- 用户行为分析
- 问题排查

**配置示例**:
```yaml
plugins:
  logger:
    enabled: true
    storage: "file"          # file/database/elasticsearch
    log_path: "./logs/bot.log"
    
    # 记录内容
    log_commands: true       # 记录所有命令
    log_messages: false      # 记录所有消息（隐私敏感）
    log_errors: true         # 记录所有错误
    
    # 日志轮转
    rotation:
      max_size: 100          # MB
      max_age: 30            # days
      max_backups: 10
      compress: true
```

**功能特性**:
- 结构化日志
- 日志查询API
- 敏感信息脱敏

**依赖**: infra/logger

---

## 实用工具插件

### 🔹 9. Translator Plugin (翻译插件)

**功能**: 多语言翻译

**使用场景**:
- 国际化支持
- 语言学习
- 跨语言交流

**配置示例**:
```yaml
plugins:
  translator:
    enabled: true
    provider: "google"       # google/baidu/youdao
    api_key: "your-api-key"
    
    # 默认语言对
    default_source: "auto"
    default_target: "zh"
    
    # 快捷命令
    shortcuts:
      en: "en"               # /trans en <text>
      ja: "ja"
      ko: "ko"
```

**功能特性**:
- `/trans <text>` - 自动检测语言并翻译
- `/trans <target> <text>` - 翻译到指定语言
- `/trans <source>-<target> <text>` - 指定源和目标语言

**依赖**: HTTP Client

---

### 🔹 10. Search Plugin (搜索插件)

**功能**: 网络搜索集成

**使用场景**:
- 快速查询
- 信息检索
- 知识问答

**配置示例**:
```yaml
plugins:
  search:
    enabled: true
    engines:
      google:
        enabled: true
        api_key: "your-key"
        search_engine_id: "your-id"
      
      bing:
        enabled: false
        api_key: "your-key"
    
    default_engine: "google"
    max_results: 5
    safe_search: true
```

**功能特性**:
- `/search <query>` - 搜索并返回结果
- `/google <query>` - 使用Google搜索
- `/bing <query>` - 使用Bing搜索

**依赖**: HTTP Client

---

### 🔹 11. Weather Plugin (天气插件)

**功能**: 天气查询

**使用场景**:
- 天气预报
- 生活服务

**配置示例**:
```yaml
plugins:
  weather:
    enabled: true
    provider: "openweather"  # openweather/qweather
    api_key: "your-api-key"
    default_city: "Beijing"
    unit: "metric"           # metric/imperial
```

**功能特性**:
- `/weather` - 查询默认城市天气
- `/weather <city>` - 查询指定城市天气
- 支持多日预报

**依赖**: HTTP Client

---

### 🔹 12. Reminder Plugin (提醒插件)

**功能**: 个人提醒和闹钟

**使用场景**:
- 待办事项
- 会议提醒
- 定时通知

**配置示例**:
```yaml
plugins:
  reminder:
    enabled: true
    storage: "file"
    storage_path: "./data/reminders.json"
    max_per_user: 20
```

**功能特性**:
- `/remind <time> <message>` - 设置提醒
  - 例: `/remind 10m 开会`
  - 例: `/remind 2024-01-24 15:00 提交报告`
- `/reminders` - 查看所有提醒
- `/remind cancel <id>` - 取消提醒

**依赖**: Scheduler Plugin

---

### 🔹 13. Poll Plugin (投票插件)

**功能**: 创建和管理投票

**使用场景**:
- 群组决策
- 意见收集
- 活动安排

**配置示例**:
```yaml
plugins:
  poll:
    enabled: true
    storage: "memory"
    max_options: 10
    allow_multiple: true     # 允许多选
```

**功能特性**:
- `/poll create <question> | <opt1> | <opt2> | ...` - 创建投票
- `/poll vote <id> <option>` - 投票
- `/poll result <id>` - 查看结果
- `/poll close <id>` - 关闭投票

**实现要点**:
```go
type PollPlugin struct {
    *plugin.BasePlugin
    config PollConfig
    polls  map[string]*Poll
    mu     sync.RWMutex
}

type Poll struct {
    ID          string
    Question    string
    Options     []string
    Votes       map[string][]int  // userID -> option indices
    CreatedBy   string
    CreatedAt   time.Time
    ClosedAt    *time.Time
    AllowMulti  bool
}

func (p *PollPlugin) handleCreate(ctx *context.Context) error {
    content := ctx.GetMessageContent()
    parts := strings.Split(content, "|")
    
    if len(parts) < 3 {
        return ctx.Reply("❌ 格式错误\n用法: /poll create 问题 | 选项1 | 选项2 | ...")
    }
    
    question := strings.TrimSpace(parts[0])
    options := make([]string, 0, len(parts)-1)
    for i := 1; i < len(parts); i++ {
        opt := strings.TrimSpace(parts[i])
        if opt != "" {
            options = append(options, opt)
        }
    }
    
    poll := &Poll{
        ID:         generateID(),
        Question:   question,
        Options:    options,
        Votes:      make(map[string][]int),
        CreatedBy:  ctx.GetAuthor().ID,
        CreatedAt:  time.Now(),
        AllowMulti: p.config.AllowMultiple,
    }
    
    p.mu.Lock()
    p.polls[poll.ID] = poll
    p.mu.Unlock()
    
    // 生成投票消息
    msg := p.formatPoll(poll)
    return ctx.Reply(msg)
}
```

**依赖**: 无

---

## 扩展功能插件

### 🔹 14. Statistics Plugin (统计插件)

**功能**: 群组和用户数据统计

**使用场景**:
- 活跃度分析
- 数据报表
- 运营分析

**配置示例**:
```yaml
plugins:
  statistics:
    enabled: true
    storage: "database"      # memory/file/database
    
    collect:
      messages: true         # 消息统计
      commands: true         # 命令统计
      active_users: true     # 活跃用户
      peak_hours: true       # 高峰时段
    
    reports:
      daily: true
      weekly: true
      monthly: true
```

**功能特性**:
- `/stats` - 查看今日统计
- `/stats user [@user]` - 查看用户统计
- `/stats group` - 查看群组统计
- `/stats command` - 查看命令使用统计
- 自动生成周报/月报

**依赖**: 无

---

### 🔹 15. Backup Plugin (备份插件)

**功能**: 数据备份和恢复

**使用场景**:
- 数据保护
- 灾难恢复
- 迁移支持

**配置示例**:
```yaml
plugins:
  backup:
    enabled: true
    storage: "local"         # local/s3/oss
    backup_path: "./backups"
    
    schedule:
      enabled: true
      cron: "0 0 2 * * *"    # 每天凌晨2点
    
    retention:
      daily: 7               # 保留7天
      weekly: 4              # 保留4周
      monthly: 12            # 保留12月
    
    include:
      - "data/"
      - "config.yaml"
      - "permissions.json"
```

**功能特性**:
- `/backup create` - 手动创建备份
- `/backup list` - 列出所有备份
- `/backup restore <id>` - 恢复备份
- 自动定时备份

**依赖**: Scheduler Plugin

---

### 🔹 16. Webhook Plugin (Webhook插件)

**功能**: 接收和发送Webhook

**使用场景**:
- 与外部系统集成
- 事件通知
- 自动化流程

**配置示例**:
```yaml
plugins:
  webhook:
    enabled: true
    
    # 接收Webhook
    receivers:
      - name: "github"
        path: "/webhook/github"
        secret: "your-secret"
        handler: "handle_github_event"
      
      - name: "gitlab"
        path: "/webhook/gitlab"
        secret: "your-secret"
        handler: "handle_gitlab_event"
    
    # 发送Webhook
    senders:
      - name: "notify_channel"
        url: "https://api.example.com/notify"
        method: "POST"
        headers:
          Authorization: "Bearer token"
```

**功能特性**:
- 接收外部Webhook并转换为Bot消息
- 发送Bot事件到外部系统
- 自定义处理器

**依赖**: HTTP Server

---

## AI集成插件

### 🔹 17. ChatGPT Plugin (ChatGPT插件)

**功能**: 集成OpenAI ChatGPT

**使用场景**:
- 智能对话
- 内容生成
- 问题解答

**配置示例**:
```yaml
plugins:
  chatgpt:
    enabled: true
    api_key: "your-openai-key"
    model: "gpt-4"
    
    # 对话设置
    system_prompt: "你是一个友好的助手"
    max_tokens: 1000
    temperature: 0.7
    
    # 上下文记忆
    context:
      enabled: true
      max_messages: 10       # 保留最近10条消息
      ttl: "1h"              # 1小时后清除
    
    # 触发方式
    trigger:
      mention: true          # @Bot时触发
      keyword: "/chat"       # 关键词触发
      always: false          # 始终响应（慎用）
```

**功能特性**:
- 上下文记忆（会话管理）
- 流式响应支持
- Token计数和限制
- 多用户隔离

**实现要点**:
```go
type ChatGPTPlugin struct {
    *plugin.BasePlugin
    config   ChatGPTConfig
    client   *openai.Client
    sessions map[string]*ChatSession
    mu       sync.RWMutex
}

type ChatSession struct {
    UserID   string
    Messages []openai.ChatCompletionMessage
    UpdateAt time.Time
}

func (p *ChatGPTPlugin) handleChat(ctx *context.Context) error {
    userID := ctx.GetAuthor().ID
    content := ctx.GetMessageContent()
    
    // 获取或创建会话
    session := p.getOrCreateSession(userID)
    
    // 添加用户消息
    session.Messages = append(session.Messages, openai.ChatCompletionMessage{
        Role:    openai.ChatMessageRoleUser,
        Content: content,
    })
    
    // 调用ChatGPT
    resp, err := p.client.CreateChatCompletion(
        context.Background(),
        openai.ChatCompletionRequest{
            Model:    p.config.Model,
            Messages: session.Messages,
            MaxTokens: p.config.MaxTokens,
            Temperature: p.config.Temperature,
        },
    )
    
    if err != nil {
        return ctx.Reply(fmt.Sprintf("❌ ChatGPT错误: %v", err))
    }
    
    reply := resp.Choices[0].Message.Content
    
    // 保存助手回复到会话
    session.Messages = append(session.Messages, openai.ChatCompletionMessage{
        Role:    openai.ChatMessageRoleAssistant,
        Content: reply,
    })
    
    // 限制上下文长度
    if len(session.Messages) > p.config.Context.MaxMessages*2 {
        session.Messages = session.Messages[2:] // 保留后面的消息
    }
    
    return ctx.Reply(reply)
}
```

**依赖**: github.com/sashabaranov/go-openai

---

### 🔹 18. ImageGen Plugin (图片生成插件)

**功能**: AI图片生成

**使用场景**:
- 创意图片生成
- 头像/表情包制作
- 设计辅助

**配置示例**:
```yaml
plugins:
  imagegen:
    enabled: true
    provider: "dall-e"       # dall-e/stable-diffusion/midjourney
    api_key: "your-key"
    
    models:
      dall-e:
        model: "dall-e-3"
        size: "1024x1024"
        quality: "standard"  # standard/hd
      
      stable-diffusion:
        model: "stable-diffusion-xl"
        steps: 30
    
    default_model: "dall-e"
    max_per_user_daily: 5    # 每用户每日限额
```

**功能特性**:
- `/image <prompt>` - 生成图片
- `/image <model> <prompt>` - 指定模型生成
- 支持风格参数

**依赖**: OpenAI API / Stability AI API

---

### 🔹 19. VoiceChat Plugin (语音对话插件)

**功能**: 语音识别和合成

**使用场景**:
- 语音交互
- 无障碍访问
- 语音助手

**配置示例**:
```yaml
plugins:
  voicechat:
    enabled: true
    
    # 语音识别
    stt:
      provider: "azure"      # azure/google/baidu
      api_key: "your-key"
      language: "zh-CN"
    
    # 语音合成
    tts:
      provider: "azure"
      api_key: "your-key"
      voice: "zh-CN-XiaoxiaoNeural"
      rate: 1.0
```

**功能特性**:
- 自动识别语音消息并转文字
- 文字消息转语音
- `/tts <text>` - 文字转语音

**依赖**: Azure Speech SDK / Google Cloud Speech

---

## 实施路线图

### Phase 1: 核心功能 (2周)
**优先级**: P0

- [x] Echo Plugin
- [x] Help Plugin
- [ ] Permission Plugin
- [ ] RateLimit Plugin

### Phase 2: 管理工具 (2周)
**优先级**: P1

- [ ] Admin Plugin
- [ ] Metrics Plugin
- [ ] Logger Plugin
- [ ] Scheduler Plugin

### Phase 3: 实用工具 (3周)
**优先级**: P2

- [ ] Translator Plugin
- [ ] Search Plugin
- [ ] Weather Plugin
- [ ] Reminder Plugin
- [ ] Poll Plugin

### Phase 4: 扩展功能 (2周)
**优先级**: P2

- [ ] Statistics Plugin
- [ ] Backup Plugin
- [ ] Webhook Plugin

### Phase 5: AI集成 (3周)
**优先级**: P3

- [ ] ChatGPT Plugin
- [ ] ImageGen Plugin
- [ ] VoiceChat Plugin

---

## 插件开发规范

### 目录结构

```
plugin/builtin/
├── core/
│   ├── echo/
│   │   ├── echo.go
│   │   ├── echo_test.go
│   │   ├── config.go
│   │   └── README.md
│   ├── help/
│   └── ...
├── admin/
├── util/
├── extension/
└── ai/
```

### 代码模板

```go
package pluginname

import (
    "github.com/KomeiDiSanXian/remilia/plugin"
    "github.com/KomeiDiSanXian/remilia/core/engine"
    "github.com/KomeiDiSanXian/remilia/core/context"
)

// Config 插件配置
type Config struct {
    Enabled bool   `yaml:"enabled"`
    // ... 其他配置
}

// Plugin 插件实现
type Plugin struct {
    *plugin.BasePlugin
    config Config
}

// New 创建插件实例
func New(cfg Config) *Plugin {
    return &Plugin{
        BasePlugin: plugin.NewBasePlugin("plugin-name"),
        config:     cfg,
    }
}

// Load 加载插件
func (p *Plugin) Load(eng *engine.Engine) error {
    // 注册命令和处理器
    m := eng.OnCommand(dto.GroupAtMessageCreate, "/command")
    m.Handle(p.handleCommand)
    p.AddMatcher(m)
    
    return nil
}

// Unload 卸载插件
func (p *Plugin) Unload(eng *engine.Engine) error {
    // 清理资源
    return p.BasePlugin.Unload(eng)
}

// Dependencies 声明依赖
func (p *Plugin) Dependencies() []string {
    return []string{} // 或 []string{"other-plugin"}
}

// handleCommand 命令处理器
func (p *Plugin) handleCommand(ctx *context.Context) error {
    // 实现逻辑
    return ctx.Reply("Hello!")
}
```

### 测试要求

每个插件必须包含:
- 单元测试 (覆盖率 > 80%)
- 集成测试
- 示例代码
- README文档

---

## 配置集成

### config.yaml 示例

```yaml
# Remilia 配置文件

bot:
  name: "MyBot"
  version: "1.0.0"

# 插件配置
plugins:
  # 核心功能
  echo:
    enabled: true
    trigger: "/echo"
  
  help:
    enabled: true
    trigger: "/help"
  
  permission:
    enabled: true
    storage: "file"
    default_role: "user"
  
  rate_limit:
    enabled: true
    per_user:
      max_requests: 10
      window: "1m"
  
  # 管理工具
  admin:
    enabled: true
    admins:
      - "user_id_12345"
  
  metrics:
    enabled: true
    port: 9090
  
  # 实用工具
  translator:
    enabled: false
    provider: "google"
  
  weather:
    enabled: false
    provider: "openweather"
  
  # AI集成
  chatgpt:
    enabled: false
    api_key: "${OPENAI_API_KEY}"
    model: "gpt-4"
```

---

## 最佳实践

### 1. 性能考虑
- 使用对象池减少内存分配
- 缓存频繁查询的数据
- 异步处理耗时操作
- 合理设置超时

### 2. 安全考虑
- 敏感配置使用环境变量
- 输入验证和过滤
- API密钥加密存储
- 权限检查

### 3. 可维护性
- 清晰的错误消息
- 详细的日志记录
- 完善的文档
- 合理的配置项

### 4. 可扩展性
- 提供回调接口
- 支持自定义处理器
- 插件间通信机制
- 事件发布订阅

---

## 参考资源

### 相关文档
- [插件开发指南](./PLUGIN_DEVELOPMENT_GUIDE.md)
- [命令系统文档](../command/README.md)
- [中间件系统](../middleware/README.md)

### 依赖库
- cron: github.com/robfig/cron/v3
- openai: github.com/sashabaranov/go-openai
- redis: github.com/go-redis/redis/v8

### 示例代码
- [examples/plugin-example](../examples/plugin-example)
- [examples/command-bot](../examples/command-bot)

---

**更新日志**:
- 2026-01-24: 初始版本，定义19个内置插件

**待办事项**:
- [ ] 完成Phase 1插件开发
- [ ] 编写插件开发指南
- [ ] 创建插件市场/注册表
- [ ] 添加插件热更新支持
