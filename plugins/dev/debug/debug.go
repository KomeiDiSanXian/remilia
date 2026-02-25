package debug

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/core/permission"
)

// Plugin Debug调试插件
type Plugin struct {
	Engine     engine.EngineReader // Engine 只读视图（仅查询命令列表、Matcher 统计等）
	PermPlugin *permission.Plugin
	DevMode    bool
	Info       plugin.PluginInfo // 插件系统只读视图
	setupCtx   *plugin.SetupContext
}

// New 创建调试插件（v2 API）
func New() *plugin.PluginDescriptor {
	// 创建 v1 Plugin 实例
	v1Plugin := newDebugPluginInternal()

	return &plugin.PluginDescriptor{
		Name:    "debug",
		Version: "2.0.0",
		Deps:    []string{"permission"},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia Team",
			Description: "开发调试工具集合，提供事件查看、上下文检查、性能分析等功能",
			Category:    "开发",
			Tags:        []string{"调试", "开发", "性能"},
			HelpText: `调试插件使用说明：
  /debug event|ctx|matcher|runtime|commands|plugins|bench`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Loading debug plugin")
			_ = ctx.MustGet("permission")
			if ctx.Config != nil {
				v1Plugin.DevMode = ctx.Config.GetBool("dev_mode", false)
			}
			v1Plugin.setupCtx = ctx
			if err := v1Plugin.Load(ctx); err != nil {
				return nil, err
			}
			return v1Plugin, nil // 导出到容器，可被其他插件通过 Must[debug.Plugin] 发现
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Debug plugin unloaded")
			return nil
		},
	}
}

// newDebugPluginInternal 创建调试插件内部实例
func newDebugPluginInternal() *Plugin {
	return &Plugin{
		DevMode: false, // 默认关闭，由 Setup 从配置读取
	}
}

// Load 加载插件
func (p *Plugin) Load(ctx *plugin.SetupContext) error {
	// 使用 ctx.Info 只读视图（无需私有接口断言）
	p.Info = ctx.Info
	p.Engine = ctx.Info.Coordinator()
	p.setupCtx = ctx
	p.registerDebugCommands(ctx)
	return nil
}

// SetPermissionPlugin 设置权限插件
func (p *Plugin) SetPermissionPlugin(pp *permission.Plugin) {
	p.PermPlugin = pp
}

// SetDevMode 设置开发模式
func (p *Plugin) SetDevMode(enabled bool) {
	p.DevMode = enabled
}

// registerDebugCommands 注册调试命令
func (p *Plugin) registerDebugCommands(ctx *plugin.SetupContext) {
	debugCmd := &command.Definition{
		Name:        "debug",
		Description: "开发调试工具集合",
		Usage:       "/debug <子命令> [参数]",
		Category:    "开发",
		Aliases:     []string{"dbg"},
		SubCommands: []*command.Definition{
			{Name: "event", Description: "显示当前事件的详细信息", Usage: "/debug event", Examples: []string{"/debug event"}},
			{Name: "ctx", Description: "显示当前上下文的所有信息", Usage: "/debug ctx", Examples: []string{"/debug ctx"}},
			{
				Name: "matcher", Description: "查看命令匹配器的详细信息", Usage: "/debug matcher <命令名>",
				Arguments: []*command.Argument{{Name: "command", Type: command.ArgTypeString, Description: "命令名称", Required: true}},
				Examples:  []string{"/debug matcher help"},
			},
			{Name: "runtime", Description: "显示运行时信息", Usage: "/debug runtime", Examples: []string{"/debug runtime"}},
			{Name: "commands", Description: "显示所有注册的命令", Usage: "/debug commands", Examples: []string{"/debug commands"}},
			{Name: "plugins", Description: "显示所有插件的状态", Usage: "/debug plugins", Examples: []string{"/debug plugins"}},
			{
				Name: "bench", Description: "测试命令的执行性能", Usage: "/debug bench <命令名>",
				Arguments: []*command.Argument{{Name: "command", Type: command.ArgTypeString, Description: "命令名称", Required: true}},
				Examples:  []string{"/debug bench help"},
			},
			{Name: "stats", Description: "显示系统统计信息", Usage: "/debug stats", Examples: []string{"/debug stats"}},
		},
	}

	ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/debug").SetDefinition(debugCmd).Handle(p.handleDebugCommand)
	ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/debug").SetDefinition(debugCmd).Handle(p.handleDebugCommand)
}

// handleDebugCommand 统一处理 debug 命令
func (p *Plugin) handleDebugCommand(ctx *eventctx.Context) error {
	// 解析子命令
	content := ctx.GetMessageContent()
	args, err := command.ParseCommandLine(content)
	if err != nil {
		return p.reply(ctx, "❌ 命令解析失败: "+err.Error())
	}

	subCommand := args.Get(0)
	if subCommand == "" {
		return p.showDebugHelp(ctx)
	}

	// 分发到对应的处理函数
	switch subCommand {
	case "event":
		return p.handleDebugEvent(ctx)
	case "ctx":
		return p.handleDebugContext(ctx)
	case "matcher":
		return p.handleDebugMatcher(ctx)
	case "runtime":
		return p.handleDebugRuntime(ctx)
	case "commands":
		return p.handleDebugCommands(ctx)
	case "plugins":
		return p.handleDebugPlugins(ctx)
	case "bench":
		return p.handleDebugBench(ctx)
	case "stats":
		return p.handleDebugStats(ctx)
	default:
		return p.reply(ctx, fmt.Sprintf("❌ 未知的子命令: %s\n使用 /debug 查看帮助", subCommand))
	}
}

// showDebugHelp 显示 debug 命令的帮助信息
func (p *Plugin) showDebugHelp(ctx *eventctx.Context) error {
	var msg strings.Builder
	msg.WriteString("🔍 调试工具集合\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	msg.WriteString("📋 可用命令:\n\n")

	msg.WriteString("事件调试:\n")
	msg.WriteString("  /debug event - 显示当前事件的详细信息\n")
	msg.WriteString("  /debug ctx - 显示当前上下文的所有信息\n")
	msg.WriteString("  /debug matcher <命令> - 查看命令匹配器\n\n")

	msg.WriteString("系统调试:\n")
	msg.WriteString("  /debug runtime - 显示运行时信息\n")
	msg.WriteString("  /debug commands - 显示所有注册的命令\n")
	msg.WriteString("  /debug plugins - 显示所有插件的状态\n\n")

	msg.WriteString("性能分析:\n")
	msg.WriteString("  /debug bench <命令> - 测试命令性能\n")
	msg.WriteString("  /debug stats - 显示系统统计信息\n\n")

	msg.WriteString("⚠️ 提示: 使用 /debug <子命令> 执行对应功能")

	return p.reply(ctx, msg.String())
}

// checkPermission 检查权限
func (p *Plugin) checkPermission(ctx *eventctx.Context, permission string) bool {
	// 如果未设置权限插件，默认允许（开发模式）
	if p.PermPlugin == nil {
		return p.DevMode
	}

	userID := ctx.GetUserID()
	return p.DevMode || p.PermPlugin.HasPermission(userID, permission) // 开发模式下允许所有权限，生产环境需要检查
}

// reply 发送消息
func (p *Plugin) reply(ctx *eventctx.Context, message string) error {
	eventType := ctx.GetEventType()
	msg := &dto.Message{
		Type:    dto.TextMessage,
		Content: message,
	}

	switch eventType {
	case dto.GroupAtMessageCreate:
		_, err := ctx.ReplyGroup(msg)
		return err
	case dto.C2CMessageCreate:
		_, err := ctx.ReplyPrivate(msg)
		return err
	default:
		logger.WithField("event_type", eventType).Warn("[DebugPlugin] Unsupported event type for reply")
		return fmt.Errorf("unsupported event type: %s", eventType)
	}
}

// handleDebugEvent 处理 /debug event 命令
func (p *Plugin) handleDebugEvent(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "debug.view") {
		return p.reply(ctx, "❌ 权限不足：需要 debug.view 权限")
	}

	event := ctx.GetEvent()
	if event == nil {
		return p.reply(ctx, "❌ 事件对象为空")
	}

	var msg strings.Builder
	msg.WriteString("🔍 事件详情\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	// 基本信息
	msg.WriteString(fmt.Sprintf("📋 事件类型: %s\n", ctx.GetEventType()))
	msg.WriteString(fmt.Sprintf("🆔 事件ID: %s\n", event.ID))

	// 消息内容
	if content := ctx.GetMessageContent(); content != "" {
		msg.WriteString(fmt.Sprintf("💬 消息内容: %s\n", content))
	}

	// 用户信息
	msg.WriteString(fmt.Sprintf("👤 用户ID: %s\n", ctx.GetUserID()))
	if author := ctx.GetAuthor(); author != nil {
		if author.ID != "" {
			msg.WriteString(fmt.Sprintf("👤 作者ID: %s\n", author.ID))
		}
	}

	// 原始数据（简化显示）
	msg.WriteString("\n📦 原始事件:\n")
	msg.WriteString(fmt.Sprintf("  - Type: %s\n", event.Type))
	msg.WriteString(fmt.Sprintf("  - ID: %s\n", event.ID))

	return p.reply(ctx, msg.String())
}

// handleDebugContext 处理 /debug ctx 命令
func (p *Plugin) handleDebugContext(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "debug.view") {
		return p.reply(ctx, "❌ 权限不足：需要 debug.view 权限")
	}

	var msg strings.Builder
	msg.WriteString("🔍 上下文信息\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	// 标准 Context
	msg.WriteString("📝 标准 Context:\n")
	stdCtx := ctx.Context()
	if stdCtx != nil {
		msg.WriteString(fmt.Sprintf("  - Err: %v\n", stdCtx.Err()))
		if deadline, ok := stdCtx.Deadline(); ok {
			msg.WriteString(fmt.Sprintf("  - Deadline: %v\n", deadline))
		} else {
			msg.WriteString("  - Deadline: 无\n")
		}
	}

	// 扩展数据
	msg.WriteString("\n🔌 扩展数据:\n")
	extensions := ctx.All()
	if len(extensions) == 0 {
		msg.WriteString("  (无扩展数据)\n")
	} else {
		for key, value := range extensions {
			msg.WriteString(fmt.Sprintf("  - %s: %v\n", key, value))
		}
	}

	// 中间件追踪
	msg.WriteString("\n🔗 中间件链:\n")
	trace, hasTrace := ctx.GetMiddlewareTrace()
	if !hasTrace || len(trace) == 0 {
		msg.WriteString("  (无中间件追踪)\n")
	} else {
		for i, middleware := range trace {
			msg.WriteString(fmt.Sprintf("  %d. %s\n", i+1, middleware))
		}
	}

	// 解析的命令
	msg.WriteString("\n⚙️ 解析的命令:\n")
	if parsedCmd := ctx.GetParsedCommand(); parsedCmd != nil {
		cmdPath := strings.Join(parsedCmd.CommandPath, " ")
		msg.WriteString(fmt.Sprintf("  - 命令: %s\n", cmdPath))
		msg.WriteString(fmt.Sprintf("  - 参数数量: %d\n", len(parsedCmd.Arguments)))
		if len(parsedCmd.Arguments) > 0 {
			msg.WriteString("  - 参数:\n")
			for key, val := range parsedCmd.Arguments {
				msg.WriteString(fmt.Sprintf("    · %s: %v\n", key, val))
			}
		}
		if len(parsedCmd.Flags) > 0 {
			msg.WriteString("  - 标志:\n")
			for key, val := range parsedCmd.Flags {
				msg.WriteString(fmt.Sprintf("    · %s: %v\n", key, val))
			}
		}
	} else {
		msg.WriteString("  (未解析命令)\n")
	}

	// 重试次数
	msg.WriteString("\n🔄 重试信息:\n")
	if attempt, hasRetry := ctx.GetRetryAttempt(); hasRetry {
		msg.WriteString(fmt.Sprintf("  - 重试次数: %d\n", attempt))
	} else {
		msg.WriteString("  - 重试次数: 0\n")
	}

	return p.reply(ctx, msg.String())
}

// handleDebugMatcher 处理 /debug matcher 命令
func (p *Plugin) handleDebugMatcher(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "debug.view") {
		return p.reply(ctx, "❌ 权限不足：需要 debug.view 权限")
	}

	// 解析命令参数
	content := ctx.GetMessageContent()
	args, err := command.ParseCommandLine(content)
	if err != nil {
		return p.reply(ctx, "❌ 命令解析失败: "+err.Error())
	}

	cmdName := args.Get(1)
	if cmdName == "" {
		return p.reply(ctx, "❌ 请指定要查看的命令名称\n用法: /debug matcher <命令>")
	}

	// 去除命令前缀
	cmdName = strings.TrimPrefix(cmdName, "/")

	// 查找命令
	cmdInfo := p.Engine.FindCommand(cmdName)
	if cmdInfo == nil {
		return p.reply(ctx, fmt.Sprintf("❌ 未找到命令: %s", cmdName))
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🔍 命令匹配器: %s\n", cmdName))
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	// 命令定义
	if cmdInfo.Definition != nil {
		def := cmdInfo.Definition
		msg.WriteString("📋 命令定义:\n")
		msg.WriteString(fmt.Sprintf("  - 名称: %s\n", def.Name))
		msg.WriteString(fmt.Sprintf("  - 描述: %s\n", def.Description))
		msg.WriteString(fmt.Sprintf("  - 用法: %s\n", def.Usage))
		msg.WriteString(fmt.Sprintf("  - 分类: %s\n", def.Category))

		if len(def.Aliases) > 0 {
			msg.WriteString(fmt.Sprintf("  - 别名: %s\n", strings.Join(def.Aliases, ", ")))
		}

		if len(def.Arguments) > 0 {
			msg.WriteString("  - 参数:\n")
			for _, arg := range def.Arguments {
				required := "可选"
				if arg.Required {
					required = "必需"
				}
				msg.WriteString(fmt.Sprintf("    · %s (%s): %s\n", arg.Name, required, arg.Description))
			}
		}

		if len(def.Examples) > 0 {
			msg.WriteString("  - 示例:\n")
			for _, example := range def.Examples {
				msg.WriteString(fmt.Sprintf("    · %s\n", example))
			}
		}
	}

	// 事件类型
	msg.WriteString("\n📡 支持的事件类型:\n")
	msg.WriteString(fmt.Sprintf("  - %s\n", cmdInfo.EventType))

	// 插件信息
	if cmdInfo.Plugin != "" {
		msg.WriteString(fmt.Sprintf("\n🔌 所属插件: %s\n", cmdInfo.Plugin))
	}

	// 来源信息
	if cmdInfo.Source != "" {
		msg.WriteString(fmt.Sprintf("📍 来源: %s\n", cmdInfo.Source))
	}

	return p.reply(ctx, msg.String())
}

// handleDebugRuntime 处理 /debug runtime 命令
func (p *Plugin) handleDebugRuntime(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "debug.view") {
		return p.reply(ctx, "❌ 权限不足：需要 debug.view 权限")
	}

	var msg strings.Builder
	msg.WriteString("🔍 运行时信息\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	// Goroutine 信息
	numGoroutine := runtime.NumGoroutine()
	msg.WriteString(fmt.Sprintf("🔀 Goroutine 数量: %d\n", numGoroutine))

	// 内存信息
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	msg.WriteString("\n💾 内存使用:\n")
	msg.WriteString(fmt.Sprintf("  - 分配内存: %.2f MB\n", float64(m.Alloc)/1024/1024))
	msg.WriteString(fmt.Sprintf("  - 总分配内存: %.2f MB\n", float64(m.TotalAlloc)/1024/1024))
	msg.WriteString(fmt.Sprintf("  - 系统内存: %.2f MB\n", float64(m.Sys)/1024/1024))
	msg.WriteString(fmt.Sprintf("  - GC 次数: %d\n", m.NumGC))

	// CPU 信息
	msg.WriteString("\n🖥️ CPU 信息:\n")
	msg.WriteString(fmt.Sprintf("  - CPU 核心数: %d\n", runtime.NumCPU()))
	msg.WriteString(fmt.Sprintf("  - GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0)))

	// Go 版本
	msg.WriteString("\n📦 Go 版本:\n")
	msg.WriteString(fmt.Sprintf("  - %s\n", runtime.Version()))

	// 操作系统
	msg.WriteString("\n🖥️ 操作系统:\n")
	msg.WriteString(fmt.Sprintf("  - %s/%s\n", runtime.GOOS, runtime.GOARCH))

	return p.reply(ctx, msg.String())
}

// handleDebugCommands 处理 /debug commands 命令
func (p *Plugin) handleDebugCommands(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "debug.view") {
		return p.reply(ctx, "❌ 权限不足：需要 debug.view 权限")
	}

	commands := p.Engine.GetAllCommands()

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🔍 注册的命令列表 (共 %d 个)\n", len(commands)))
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	// 按事件类型分组
	cmdsByEvent := make(map[dto.EventType][]engine.CommandInfo)
	for _, cmdInfo := range commands {
		cmdsByEvent[cmdInfo.EventType] = append(cmdsByEvent[cmdInfo.EventType], cmdInfo)
	}

	// 显示每个事件类型的命令
	for eventType, cmds := range cmdsByEvent {
		msg.WriteString(fmt.Sprintf("📡 %s (%d):\n", eventType, len(cmds)))
		for _, cmdInfo := range cmds {
			msg.WriteString(fmt.Sprintf("  - /%s", cmdInfo.Command))
			if cmdInfo.Description != "" {
				msg.WriteString(fmt.Sprintf(" - %s", cmdInfo.Description))
			}
			if cmdInfo.Plugin != "" {
				msg.WriteString(fmt.Sprintf(" [%s]", cmdInfo.Plugin))
			}
			msg.WriteString("\n")
		}
		msg.WriteString("\n")
	}

	return p.reply(ctx, msg.String())
}

// handleDebugPlugins 处理 /debug plugins 命令
func (p *Plugin) handleDebugPlugins(ctx *eventctx.Context) error {
	if !p.checkPermission(ctx, "debug.view") {
		return p.reply(ctx, "❌ 权限不足：需要 debug.view 权限")
	}

	if p.Info == nil {
		return p.reply(ctx, "❌ 插件管理器未初始化")
	}

	plugins := p.Info.ListWithMetadata()

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🔍 插件状态 (共 %d 个)\n", len(plugins)))
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	for name, meta := range plugins {
		msg.WriteString(fmt.Sprintf("📦 %s v%s\n", meta.Name, meta.Version))
		msg.WriteString(fmt.Sprintf("  - 作者: %s\n", meta.Author))
		msg.WriteString(fmt.Sprintf("  - 分类: %s\n", meta.Category))

		if inst, ok := p.Info.Get(name); ok {
			state := inst.GetState()
			msg.WriteString(fmt.Sprintf("  - 状态: %v\n", state))

			uptime := inst.GetUptime()
			msg.WriteString(fmt.Sprintf("  - 运行时长: %s\n", uptime.Round(time.Second)))

			if lastErr := inst.GetLastError(); lastErr != nil {
				msg.WriteString(fmt.Sprintf("  - 最后错误: %v\n", lastErr))
			}
		}

		if len(meta.Dependencies) > 0 {
			msg.WriteString(fmt.Sprintf("  - 依赖: %s\n", strings.Join(meta.Dependencies, ", ")))
		}

		msg.WriteString("\n")
	}

	return p.reply(ctx, msg.String())
}

// handleDebugBench 处理 /debug bench 命令
func (p *Plugin) handleDebugBench(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "debug.bench") {
		return p.reply(ctx, "❌ 权限不足：需要 debug.bench 权限")
	}

	// 解析命令参数
	content := ctx.GetMessageContent()
	args, err := command.ParseCommandLine(content)
	if err != nil {
		return p.reply(ctx, "❌ 命令解析失败: "+err.Error())
	}

	// Get the command name (should be at index 1, since index 0 is "bench")
	cmdName := args.Get(1)
	if cmdName == "" {
		return p.reply(ctx, "❌ 请指定要测试的命令\n用法: /debug bench <命令>")
	}

	// 去除命令前缀
	cmdName = strings.TrimPrefix(cmdName, "/")

	// 查找命令
	cmdInfo := p.Engine.FindCommand(cmdName)
	if cmdInfo == nil {
		return p.reply(ctx, fmt.Sprintf("❌ 未找到命令: %s", cmdName))
	}

	// 执行性能测试
	const iterations = 10
	var totalDuration time.Duration

	for range iterations {
		start := time.Now()

		// 创建测试上下文（复用当前事件）
		testCtx := ctx.Clone()

		// 这里需要实际执行命令，但为了安全起见，我们只测量上下文创建和基本操作
		// 实际的命令执行需要更复杂的机制来避免副作用
		_ = testCtx

		duration := time.Since(start)
		totalDuration += duration
	}

	avgDuration := totalDuration / iterations

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🔍 性能测试: %s\n", cmdName))
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	msg.WriteString(fmt.Sprintf("📊 测试次数: %d\n", iterations))
	msg.WriteString(fmt.Sprintf("⏱️ 平均耗时: %v\n", avgDuration))
	msg.WriteString(fmt.Sprintf("⏱️ 总耗时: %v\n", totalDuration))
	msg.WriteString("\n⚠️ 注意: 此测试仅测量上下文创建开销，不包括实际命令执行")

	return p.reply(ctx, msg.String())
}

// handleDebugStats 处理 /debug stats 命令
func (p *Plugin) handleDebugStats(ctx *eventctx.Context) error {
	// 检查权限
	if !p.checkPermission(ctx, "debug.view") {
		return p.reply(ctx, "❌ 权限不足：需要 debug.view 权限")
	}

	var msg strings.Builder
	msg.WriteString("🔍 系统统计信息\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	// 命令统计
	commands := p.Engine.GetAllCommands()
	msg.WriteString(fmt.Sprintf("📝 命令总数: %d\n", len(commands)))

	// 按事件类型统计
	cmdsByEvent := make(map[dto.EventType]int)
	for _, cmdInfo := range commands {
		cmdsByEvent[cmdInfo.EventType]++
	}

	msg.WriteString("\n📡 按事件类型分布:\n")
	for eventType, count := range cmdsByEvent {
		msg.WriteString(fmt.Sprintf("  - %s: %d\n", eventType, count))
	}

	// 插件统计
	if p.Info != nil {
		plugins := p.Info.ListWithMetadata()
		msg.WriteString(fmt.Sprintf("\n📦 插件总数: %d\n", len(plugins)))

		stateCount := make(map[string]int)
		for name := range plugins {
			if inst, ok := p.Info.Get(name); ok {
				state := inst.GetState()
				stateCount[fmt.Sprintf("%v", state)]++
			}
		}

		if len(stateCount) > 0 {
			msg.WriteString("\n📊 按状态分布:\n")
			for state, count := range stateCount {
				msg.WriteString(fmt.Sprintf("  - %s: %d\n", state, count))
			}
		}
	}

	// 运行时统计
	msg.WriteString(fmt.Sprintf("\n🔀 Goroutine: %d\n", runtime.NumGoroutine()))

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	msg.WriteString(fmt.Sprintf("💾 内存使用: %.2f MB\n", float64(m.Alloc)/1024/1024))

	return p.reply(ctx, msg.String())
}
