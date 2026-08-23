# Cookbook — 插件开发菜谱

> 面向插件开发者的「问题 → 最小可运行示例」速查集。每条菜谱自包含，
> 可直接复制到你的插件中改造。完整 API 说明见各主题的参考文档。

## 目录

- [如何实现一个定时任务插件？](#如何实现一个定时任务插件)
- [如何实现一个简单的 AI Tool？](#如何实现一个简单的-ai-tool)
- [如何实现消息去重？](#如何实现消息去重)

---

## 如何实现一个定时任务插件？

框架内置 `builtin/scheduler` 插件（cron / 固定间隔任务）与 `builtin/job`
插件（Once / Retry / Chain 一次性后台作业）。周期任务优先用 scheduler。

### 步骤 1：声明依赖并获取服务

在 Descriptor 上声明 `Deps: []string{"scheduler"}`，框架会保证 scheduler
先初始化；随后在 `Setup` 中用 `ctx.Service` 获取其插件实例：

```go
package myplugin

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/scheduler"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

func New() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name: "myplugin",
		Deps: []string{"scheduler"}, // 保证 scheduler 先于本插件初始化
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			sched := ctx.Service[*scheduler.Plugin]("scheduler")

			// 固定间隔任务（每 10 分钟执行一次）
			jobID := sched.EveryNamed("myplugin:cleanup", 10*time.Minute, func() {
				logger.Info("[myplugin] cleanup job fired")
			})
			_ = jobID // 可在 Teardown 中 Remove

			// 或 cron 表达式（每天 3:00 执行）
			sched.CronNamed("myplugin:daily", "0 3 * * *", func() {
				logger.Info("[myplugin] daily job fired")
			})
			return nil, nil
		},
	}
}
```

### 关键点

- `Every` / `EveryNamed`：固定间隔（ticker 实现，可防重入）。
- `Cron` / `CronNamed`：标准 cron 表达式（robfig/cron，支持 `秒 分 时 日 月 周` 六段）。
- 任务函数签名是 `func()`，不要在任务里直接持有 `*plugin.SetupContext`——
  它只在 Setup 期间有效。
- 需要手动停止任务时保存 `scheduler.JobID`，在 `Teardown` 中调用
  `sched.Remove(jobID)`。
- 更完整的参考实现见 `builtin/subscription`（`Deps` + `ctx.Service` +
  `EveryNamed` 的轮询订阅模式）。

参考文档：[插件开发指南](../06-plugins/PLUGIN_DEVELOPMENT_GUIDE.md)、
[内置插件指南](../06-plugins/BUILTIN_PLUGINS.md)。

---

## 如何实现一个简单的 AI Tool？

AI 插件会自动发现实现了 `ai.ToolProvider` 接口的插件：插件只需在 `Setup`
返回自己的实例（放入容器），再实现 `ListTools() []ai.Tool`，无需手动注册。

```go
package mytool

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

func New() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name: "mytool",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			return &Plugin{}, nil // 返回实例，AI 插件在 FreezeContainer 后自动发现
		},
	}
}

type Plugin struct{}

// ListTools 实现 ai.ToolProvider，注册给 AI 插件的工具列表。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "roll_dice",
			Categories:  []string{"general"},
			Description: "掷骰子，返回 1 到 N 的随机点数（N 为骰子面数）",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"sides": {Type: "integer", Description: "骰子面数，如 6"},
				},
				Required: []string{"sides"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				sides, _ := args["sides"].(float64) // JSON 数字解码为 float64
				if sides < 1 {
					return "", fmt.Errorf("sides 必须为正整数")
				}
				n := 1 + rand.Intn(int(sides))
				return fmt.Sprintf("🎲 你掷出了 %d 点", n), nil
			},
		},
	}
}
```

最后把 `mytool.New()` 加入 `cmd/bot/plugins.go` 的插件列表即可。

### 关键点

- 工具名建议 `snake_case` 且全局唯一；`Description` 写清楚**何时使用**，
  LLM 据此决定是否调用。
- `Parameters` 是 JSON Schema：数字参数在 `Execute` 里是 `float64`，布尔是
  `bool`，对象是 `map[string]any`。
- 需要权限的工具设置 `Permissions`（如 `["myplugin.manage"]`），AI 插件在
  调用前强制校验；执行回调内可用 `ai.CallerInfoFromContext(ctx)` 获取调用者。
- 敏感工具可设 `RequiresApproval: true`（配合 `tool_approval=restricted`）
  或 `AlwaysRequireApproval: true`（不可关闭的强制审批）。
- 需要显式注册（覆盖自动发现）时，在 Setup 里
  `aiSvc := ctx.TryService[*ai.Plugin]("ai")` 后调用
  `aiSvc.RegisterToolProvider(p)`。

参考文档：[AI 对话插件](../06-plugins/AI_PLUGIN.md)、
[插件接口速查](../06-plugins/PLUGIN_OPTIONAL_INTERFACES.md)。

---

## 如何实现消息去重？

框架提供 `middleware/dedup`：内存 LRU + TTL 的 `DedupFilter`，可挂为引擎
级中间件（按平台事件 ID 去重，防止重放/重复投递），也可在插件内按业务
key 手动去重。

### 方式一：引擎级去重中间件

```go
import (
	"time"

	"github.com/KomeiDiSanXian/remilia/middleware/dedup"
)

// 在启动组装 engine 的地方：
filter := dedup.NewDedupFilter(dedup.DedupConfig{
	MaxSize:    10000,          // 缓存上限
	DefaultTTL: 5 * time.Minute, // 去重窗口
})
eng.Use(dedup.Dedup(filter))

// 进程退出前停止后台清理 goroutine：
defer filter.Stop()
```

重复事件会被中间件直接阻断，不进入 handler。

### 方式二：插件内按业务 key 去重

```go
// 在插件 Setup 中创建过滤器：
filter := dedup.NewDedupFilter(dedup.DefaultDedupConfig())

// 在 handler 中：
dup, err := filter.CheckDuplicate("task:" + taskID)
if err != nil {
	// 缓存已满（best-effort）：按需继续处理
}
if dup {
	return nil // 该 key 在 TTL 内已处理过，跳过
}
```

### 热更新

配合 `middleware/hotreload` 的 Bridge，配置变更可即时生效：

```go
bridge.WatchDedup(filter) // 监听 middleware.dedup.max_size / default_ttl
```

参考文档：[配置热更新](../02-user-guides/CONFIG_HOTRELOAD_QUICKREF.md)、
[快速开始指南](../01-getting-started/GETTING_STARTED.md)。
