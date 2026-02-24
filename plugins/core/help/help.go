package help

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const (
	// 每页显示的命令数量
	commandsPerPage = 10
)

// Plugin 帮助插件，显示所有可用命令的帮助信息
//
// 支持以下命令格式：
//   - /help - 显示所有命令（第1页）
//   - /help 2 - 显示第2页的命令
//   - /help <插件名> - 显示指定插件的所有命令
//   - /help <命令名> - 显示指定命令的详细信息
type Plugin struct {
	Engine *engine.Engine    // 从 Engine 直接获取命令信息
	Info   plugin.PluginInfo // 插件系统只读视图（替代 *plugin.Manager）

	// 缓存
	helpCache     map[string]string // key: "page:1", "plugin:cache", "command:help", "plugins"
	cacheMu       sync.RWMutex
	cacheExpiry   time.Time
	cacheDuration time.Duration // 缓存有效期
}

// New 创建帮助插件（v2 API）
func New() *plugin.PluginDescriptor {
	// 创建 v1 Plugin 实例（闭包捕获）
	v1Plugin := newHelpPluginInternal()

	return &plugin.PluginDescriptor{
		Name:    "help",
		Version: "2.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia",
			Description: "提供命令和插件的帮助信息查询功能",
			Category:    "系统",
			Tags:        []string{"帮助", "文档", "命令"},
			HelpText: `帮助插件使用说明：
  /help - 显示所有命令列表
  /help <页码> - 指定页
  /help plugins - 插件列表
  /help <名称> - 详细信息`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Loading help plugin")
			// 使用 PluginInfo 只读视图，不再持有 *plugin.Manager 完整权限
			v1Plugin.Info = ctx.Info
			v1Plugin.Engine = ctx.Info.Coordinator()

			ctx.Reg.RegisterCommand(dto.GroupAtMessageCreate, "/help").Handle(v1Plugin.handleHelp)
			ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/help").Handle(v1Plugin.handleHelp)

			// 订阅插件生命周期事件，当插件加载/卸载/重载时立即清空缓存（Bug 2.8 修复）
			if ctx.EventBus != nil {
				for _, topic := range []string{"plugin.loaded", "plugin.unloaded", "plugin.reloaded"} {
					t := topic
					if _, err := ctx.EventBus.Subscribe(t, func(_ any) {
						ctx.Log.Debugf("Cache invalidated due to %s event", t)
						v1Plugin.invalidateCache()
					}); err != nil {
						ctx.Log.Warnf("Failed to subscribe to %s: %v", t, err)
					}
				}
			}

			ctx.Log.Info("Help plugin loaded")
			return nil, nil
		},
	}
}

// newHelpPluginInternal 创建帮助插件内部实例
func newHelpPluginInternal() *Plugin {
	return &Plugin{
		Engine:        nil, // 将在 Setup 时设置
		Info:          nil, // ���在 Setup 时设置
		helpCache:     make(map[string]string),
		cacheDuration: 5 * time.Minute, // 默认缓存 5 分钟
		cacheExpiry:   time.Now(),
	}
}

// getCachedHelp 获取缓存的帮助信息
func (p *Plugin) getCachedHelp(key string) (string, bool) {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()

	// 检查缓存是否过期
	if time.Now().After(p.cacheExpiry) {
		return "", false
	}

	text, ok := p.helpCache[key]
	return text, ok
}

// setCachedHelp 设置缓存的帮助信息
func (p *Plugin) setCachedHelp(key string, text string) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	p.helpCache[key] = text
	p.cacheExpiry = time.Now().Add(p.cacheDuration)
}

// invalidateCache 清除所有缓存
func (p *Plugin) invalidateCache() {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	p.helpCache = make(map[string]string)
	p.cacheExpiry = time.Now()
}

// Load 加载帮助插件（v1 API）
func (p *Plugin) Load(eng *engine.Engine) error {
	logger.Info("[Plugin] Loading help plugin...")

	// 保存 engine 引用以便后续获取命令信息
	p.Engine = eng

	// 注册 /help 命令
	eng.OnCommand(dto.GroupAtMessageCreate, "/help").
		Handle(p.handleHelp)

	// 同时支持私聊
	eng.OnCommand(dto.C2CMessageCreate, "/help").
		Handle(p.handleHelp)

	logger.Info("[Plugin] Help plugin loaded successfully")
	return nil
}

// SetPluginManager 已废弃，保留用于向后兼容。请改用 ctx.Info（PluginInfo 只读接口）。
// Deprecated: use ctx.Info instead.
func (p *Plugin) SetPluginManager(pm *plugin.Manager) {
	p.Info = pm.AsPluginInfo()
}

// handleHelp 处理帮助命令
func (p *Plugin) handleHelp(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()

	// 解析命令参数
	args, err := command.ParseCommandLine(content)
	if err != nil {
		return p.sendMessage(ctx, "命令解析失败: "+err.Error())
	}

	// 获取第一个参数
	target := args.Get(0)

	if target == "" {
		// 没有参数，显示第1页命令
		return p.showCommandsPage(ctx, 1)
	}

	// 特殊关键字：plugins
	if strings.EqualFold(target, "plugins") || strings.EqualFold(target, "plugin") {
		return p.showAllPlugins(ctx)
	}

	// 尝试解析为页码
	if page, err := command.ParseInt(target); err == nil && page > 0 {
		return p.showCommandsPage(ctx, page)
	}

	// 检查是否是插件名
	if p.Info != nil {
		plugins := p.Info.List()
		for _, pluginName := range plugins {
			if strings.EqualFold(pluginName, target) {
				return p.showPluginCommands(ctx, pluginName)
			}
		}
	}

	// 尝试作为命令名查找（支持带或不带 / 前缀）
	cmdName := strings.TrimPrefix(target, "/")
	if cmdInfo := p.Engine.FindCommand(cmdName); cmdInfo != nil {
		return p.showCommandDetail(ctx, cmdInfo)
	}

	// 未找到，显示建议
	return p.showCommandNotFound(ctx, target)
}

// showCommandsPage 显示指定页的命令列表
func (p *Plugin) showCommandsPage(ctx *eventctx.Context, page int) error {
	// 尝试从缓存获取
	cacheKey := fmt.Sprintf("page:%d", page)
	if cached, ok := p.getCachedHelp(cacheKey); ok {
		logger.Debugf("[help] Cache hit for page: %d", page)
		return p.sendMessage(ctx, cached)
	}

	commands := p.Engine.GetAllCommands()

	// 如果没有命令，显示插件列表
	if len(commands) == 0 {
		if p.Info != nil {
			logger.Info("[Plugin] No commands found, showing plugin list instead")
			return p.showAllPlugins(ctx)
		}
		return p.sendMessage(ctx, "当前没有可用的命令和插件")
	}

	// 计算分页
	totalPages := (len(commands) + commandsPerPage - 1) / commandsPerPage
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	startIdx := (page - 1) * commandsPerPage
	endIdx := min(startIdx+commandsPerPage, len(commands))

	var help strings.Builder
	help.WriteString(fmt.Sprintf("📖 可用命令列表 (第 %d/%d 页)\n", page, totalPages))
	help.WriteString(strings.Repeat("=", 30) + "\n\n")

	// 按分类组织命令
	categories := make(map[string][]engine.CommandInfo)
	for i := startIdx; i < endIdx; i++ {
		cmd := commands[i]
		category := cmd.Category
		if category == "" {
			category = "其他"
		}
		categories[category] = append(categories[category], cmd)
	}

	// 对分类进行排序
	sortedCategories := make([]string, 0, len(categories))
	for category := range categories {
		sortedCategories = append(sortedCategories, category)
	}
	sort.Strings(sortedCategories)

	// 输出每个分类的命令
	for _, category := range sortedCategories {
		cmds := categories[category]
		help.WriteString(fmt.Sprintf("【%s】\n", category))

		for _, cmd := range cmds {
			help.WriteString(fmt.Sprintf("  %s", cmd.Command))

			if len(cmd.Aliases) > 0 {
				aliases := make([]string, len(cmd.Aliases))
				for i, alias := range cmd.Aliases {
					aliases[i] = "/" + alias
				}
				help.WriteString(fmt.Sprintf(" (%s)", strings.Join(aliases, ", ")))
			}

			if cmd.Description != "" {
				help.WriteString(fmt.Sprintf("\n    %s", cmd.Description))
			}

			help.WriteString("\n")
		}
		help.WriteString("\n")
	}

	help.WriteString(strings.Repeat("=", 30) + "\n")
	help.WriteString("💡 使用方法:\n")
	help.WriteString("  /help <命令名> - 查看命令详情\n")
	if p.Info != nil {
		help.WriteString("  /help <插件名> - 查看插件的所有命令\n")
	}
	if totalPages > 1 {
		help.WriteString(fmt.Sprintf("  /help <页码> - 查看其他页(共 %d 页)\n", totalPages))
	}

	help.WriteString(fmt.Sprintf("\n📊 统计: 共 %d 个命令", len(commands)))

	// 缓存结果
	helpText := help.String()
	p.setCachedHelp(cacheKey, helpText)

	return p.sendMessage(ctx, helpText)
}

// showAllPlugins 显示所有插件的列表
func (p *Plugin) showAllPlugins(ctx *eventctx.Context) error {
	// 尝试从缓存获取
	cacheKey := "plugins"
	if cached, ok := p.getCachedHelp(cacheKey); ok {
		logger.Debug("[help] Cache hit for plugins list")
		return p.sendMessage(ctx, cached)
	}

	if p.Info == nil {
		return p.sendMessage(ctx, "插件管理器不可用")
	}

	pluginsMetadata := p.Info.ListWithMetadata()
	if len(pluginsMetadata) == 0 {
		return p.sendMessage(ctx, "当前没有加载任何插件")
	}

	var help strings.Builder
	help.WriteString(fmt.Sprintf("📦 已加载插件列表 (共 %d 个)\n", len(pluginsMetadata)))
	help.WriteString(strings.Repeat("=", 30) + "\n\n")

	// 按分类组织插件
	categories := make(map[string][]*plugin.Metadata)
	for _, meta := range pluginsMetadata {
		category := meta.Category
		if category == "" {
			category = "其他"
		}
		categories[category] = append(categories[category], meta)
	}

	// 对分类进行排序
	sortedCategories := make([]string, 0, len(categories))
	for category := range categories {
		sortedCategories = append(sortedCategories, category)
	}
	sort.Strings(sortedCategories)

	// 输出每个分类的插件
	for _, category := range sortedCategories {
		metas := categories[category]
		help.WriteString(fmt.Sprintf("【%s】\n", category))

		// 对插件进行排序
		sort.Slice(metas, func(i, j int) bool {
			return metas[i].Name < metas[j].Name
		})

		for _, meta := range metas {
			help.WriteString(fmt.Sprintf("  🔌 %s", meta.Name))

			if meta.Version != "" {
				help.WriteString(fmt.Sprintf(" v%s", meta.Version))
			}

			if meta.Hidden {
				help.WriteString(" [隐藏]")
			}

			help.WriteString("\n")

			if meta.Description != "" {
				help.WriteString(fmt.Sprintf("     %s\n", meta.Description))
			}

			if meta.Author != "" {
				help.WriteString(fmt.Sprintf("     👤 %s", meta.Author))
				if len(meta.Tags) > 0 {
					help.WriteString(fmt.Sprintf(" | 🏷️  %s", strings.Join(meta.Tags, ", ")))
				}
				help.WriteString("\n")
			} else if len(meta.Tags) > 0 {
				help.WriteString(fmt.Sprintf("     🏷️  %s\n", strings.Join(meta.Tags, ", ")))
			}

			help.WriteString("\n")
		}
	}

	help.WriteString(strings.Repeat("=", 30) + "\n")
	help.WriteString("💡 使用方法:\n")
	help.WriteString("  /help <插件名> - 查看插件的详细信息和命令\n")
	help.WriteString("  /help <命令名> - 查看命令详情\n")

	// 缓存结果
	helpText := help.String()
	p.setCachedHelp(cacheKey, helpText)

	return p.sendMessage(ctx, helpText)
}

// showPluginCommands 显示指定插件的所有命令
func (p *Plugin) showPluginCommands(ctx *eventctx.Context, pluginName string) error {
	// 尝试从缓存获取
	cacheKey := fmt.Sprintf("plugin:%s", pluginName)
	if cached, ok := p.getCachedHelp(cacheKey); ok {
		logger.Debugf("[help] Cache hit for plugin: %s", pluginName)
		return p.sendMessage(ctx, cached)
	}

	var help strings.Builder
	help.WriteString(fmt.Sprintf("🔌 插件【%s】信息\n", pluginName))
	help.WriteString(strings.Repeat("=", 30) + "\n\n")

	// 显示插件元数据（如果有）
	if p.Info != nil {
		if metadata, ok := p.Info.GetMetadata(pluginName); ok && metadata != nil {
			// 显示插件详细信息
			if metadata.Description != "" {
				help.WriteString(fmt.Sprintf("📝 描述: %s\n", metadata.Description))
			}
			if metadata.Version != "" {
				help.WriteString(fmt.Sprintf("📌 版本: %s\n", metadata.Version))
			}
			if metadata.Author != "" {
				help.WriteString(fmt.Sprintf("👤 作者: %s\n", metadata.Author))
			}
			if metadata.Category != "" {
				help.WriteString(fmt.Sprintf("📂 分类: %s\n", metadata.Category))
			}
			if len(metadata.Tags) > 0 {
				help.WriteString(fmt.Sprintf("🏷️  标签: %s\n", strings.Join(metadata.Tags, ", ")))
			}
			if metadata.Homepage != "" {
				help.WriteString(fmt.Sprintf("🏠 主页: %s\n", metadata.Homepage))
			}
			if len(metadata.Dependencies) > 0 {
				help.WriteString(fmt.Sprintf("📦 依赖: %s\n", strings.Join(metadata.Dependencies, ", ")))
			}

			// 显示帮助文本
			if metadata.HelpText != "" {
				help.WriteString(fmt.Sprintf("\n💡 帮助:\n%s\n", metadata.HelpText))
			}

			help.WriteString("\n")
		}
	}

	// 查找属于该插件的命令
	allCommands := p.Engine.GetAllCommands()
	pluginCommands := make([]engine.CommandInfo, 0)

	for _, cmd := range allCommands {
		// 通过 Plugin 字段判断命令是否属于该插件
		if strings.EqualFold(cmd.Plugin, pluginName) {
			pluginCommands = append(pluginCommands, cmd)
		}
	}

	if len(pluginCommands) == 0 {
		help.WriteString("该插件没有注册任何命令")
		return p.sendMessage(ctx, help.String())
	}

	help.WriteString(fmt.Sprintf("📋 提供的命令 (%d 个):\n\n", len(pluginCommands)))

	// 按命令名排序
	sort.Slice(pluginCommands, func(i, j int) bool {
		return pluginCommands[i].Command < pluginCommands[j].Command
	})

	for _, cmd := range pluginCommands {
		help.WriteString(fmt.Sprintf("  %s", cmd.Command))

		if len(cmd.Aliases) > 0 {
			aliases := make([]string, len(cmd.Aliases))
			for i, alias := range cmd.Aliases {
				aliases[i] = "/" + alias
			}
			help.WriteString(fmt.Sprintf(" (%s)", strings.Join(aliases, ", ")))
		}

		if cmd.Description != "" {
			help.WriteString(fmt.Sprintf("\n    %s", cmd.Description))
		}

		if cmd.Usage != "" {
			help.WriteString(fmt.Sprintf("\n    用法: %s", cmd.Usage))
		}

		help.WriteString("\n\n")
	}

	help.WriteString(strings.Repeat("=", 30) + "\n")
	help.WriteString("💡 使用 /help <命令名> 查看命令的详细用法")

	return p.sendMessage(ctx, help.String())
}

// showCommandDetail 显示特定命令的详细信息
func (p *Plugin) showCommandDetail(ctx *eventctx.Context, cmdInfo *engine.CommandInfo) error {
	// 尝试从缓存获取
	cmdName := strings.TrimPrefix(cmdInfo.Command, "/")
	cacheKey := fmt.Sprintf("command:%s", cmdName)
	if cached, ok := p.getCachedHelp(cacheKey); ok {
		logger.Debugf("[help] Cache hit for command: %s", cmdName)
		return p.sendMessage(ctx, cached)
	}

	var detail strings.Builder

	detail.WriteString("📝 命令详情\n")
	detail.WriteString(strings.Repeat("=", 30) + "\n\n")

	// 命令名称
	detail.WriteString(fmt.Sprintf("命令: %s\n", cmdInfo.Command))

	// 别名
	if len(cmdInfo.Aliases) > 0 {
		detail.WriteString(fmt.Sprintf("别名: %s\n", strings.Join(cmdInfo.Aliases, ", ")))
	}

	// 所属插件
	if cmdInfo.Plugin != "" && cmdInfo.Plugin != "global" {
		detail.WriteString(fmt.Sprintf("插件: %s\n", cmdInfo.Plugin))
	}

	// 分类
	if cmdInfo.Category != "" {
		detail.WriteString(fmt.Sprintf("分类: %s\n", cmdInfo.Category))
	}

	// 描述
	if cmdInfo.Description != "" {
		detail.WriteString(fmt.Sprintf("\n描述:\n  %s\n", cmdInfo.Description))
	}

	// 用法
	if cmdInfo.Usage != "" {
		detail.WriteString(fmt.Sprintf("\n用法:\n  %s\n", cmdInfo.Usage))
	}

	// 参数信息（如果有增强命令定义）
	if cmdInfo.Definition != nil {
		def := cmdInfo.Definition

		// 位置参数
		if len(def.Arguments) > 0 {
			detail.WriteString("\n参数:\n")
			for _, arg := range def.Arguments {
				required := ""
				if arg.Required {
					required = " [必需]"
				}
				detail.WriteString(fmt.Sprintf("  <%s>%s - %s\n",
					arg.Name, required, arg.Description))
				if arg.Default != nil {
					detail.WriteString(fmt.Sprintf("    默认值: %v\n", arg.Default))
				}
			}
		}

		// 标志参数
		if len(def.Flags) > 0 {
			detail.WriteString("\n选项:\n")
			for _, flag := range def.Flags {
				flagName := fmt.Sprintf("--%s", flag.Name)
				if flag.ShortName != "" {
					flagName += fmt.Sprintf(", -%s", flag.ShortName)
				}
				required := ""
				if flag.Required {
					required = " [必需]"
				}
				detail.WriteString(fmt.Sprintf("  %s%s\n    %s\n",
					flagName, required, flag.Description))
				if flag.Default != nil {
					detail.WriteString(fmt.Sprintf("    默认值: %v\n", flag.Default))
				}
			}
		}

		// 使用示例
		if len(def.Examples) > 0 {
			detail.WriteString("\n示例:\n")
			for _, example := range def.Examples {
				detail.WriteString(fmt.Sprintf("  %s\n", example))
			}
		}

		// 所需权限
		if len(def.Permissions) > 0 {
			detail.WriteString(fmt.Sprintf("\n所需权限: %s\n",
				strings.Join(def.Permissions, ", ")))
		}

		// 子命令
		if len(def.SubCommands) > 0 {
			detail.WriteString("\n子命令:\n")
			for _, subCmd := range def.SubCommands {
				detail.WriteString(fmt.Sprintf("  %s - %s\n",
					subCmd.Name, subCmd.Description))
			}
			cmdName := strings.TrimPrefix(cmdInfo.Command, "/")
			detail.WriteString(fmt.Sprintf("\n使用 /help %s/<子命令> 查看子命令详情\n",
				cmdName))
		}
	}

	// 缓存结果
	detailText := detail.String()
	p.setCachedHelp(cacheKey, detailText)

	return p.sendMessage(ctx, detailText)
}

// showCategoryCommands 显示特定分类下的所有命令
func (p *Plugin) showCategoryCommands(ctx *eventctx.Context, category string, commands []*command.Meta) error {
	var help strings.Builder

	help.WriteString(fmt.Sprintf("📂 分类【%s】的命令\n", category))
	help.WriteString(strings.Repeat("=", 30) + "\n\n")

	// 对命令排序
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	for _, cmd := range commands {
		help.WriteString(fmt.Sprintf("  %s", cmd.Name))

		if len(cmd.Aliases) > 0 {
			help.WriteString(fmt.Sprintf(" (%s)", strings.Join(cmd.Aliases, ", ")))
		}

		if cmd.Description != "" {
			help.WriteString(fmt.Sprintf("\n    %s", cmd.Description))
		}

		if cmd.Usage != "" {
			help.WriteString(fmt.Sprintf("\n    用法: %s", cmd.Usage))
		}

		help.WriteString("\n\n")
	}

	help.WriteString(strings.Repeat("=", 30) + "\n")
	help.WriteString("💡 使用 /help <命令名> 查看命令的详细用法")

	return p.sendMessage(ctx, help.String())
}

// showCommandNotFound 命令未找到时的提示
func (p *Plugin) showCommandNotFound(ctx *eventctx.Context, target string) error {
	var msg strings.Builder

	msg.WriteString(fmt.Sprintf("❌ 未找到: %s\n\n", target))

	// 尝试提供相似命令建议
	allCommands := p.Engine.GetAllCommands()
	var suggestions []string
	searchTerm := strings.TrimPrefix(target, "/")

	for _, cmd := range allCommands {
		cmdName := strings.TrimPrefix(cmd.Command, "/")
		// 前缀匹配
		if strings.HasPrefix(cmdName, searchTerm) {
			suggestions = append(suggestions, cmd.Command)
			if len(suggestions) >= 5 {
				break
			}
		}
	}

	// 如果前缀匹配没有结果，尝试包含匹配
	if len(suggestions) == 0 {
		for _, cmd := range allCommands {
			cmdName := strings.TrimPrefix(cmd.Command, "/")
			if strings.Contains(cmdName, searchTerm) {
				suggestions = append(suggestions, cmd.Command)
				if len(suggestions) >= 5 {
					break
				}
			}
		}
	}

	if len(suggestions) > 0 {
		msg.WriteString("💡 你可能想找:\n")
		for _, suggestion := range suggestions {
			msg.WriteString(fmt.Sprintf("  %s\n", suggestion))
		}
	} else {
		msg.WriteString("💡 使用 /help 查看所有可用命令")

		// 如果有插件信息，显示可用插件
		if p.Info != nil {
			plugins := p.Info.List()
			if len(plugins) > 0 {
				msg.WriteString("\n\n📦 可用插件:\n")
				for _, pluginName := range plugins {
					msg.WriteString(fmt.Sprintf("  %s\n", pluginName))
				}
				msg.WriteString("\n使用 /help <插件名> 查看插件命令")
			}
		}
	}

	return p.sendMessage(ctx, msg.String())
}

// sendMessage 根据事件类型自动选择发送消息的方式
func (p *Plugin) sendMessage(ctx *eventctx.Context, content string) error {
	eventType := ctx.GetEventType()
	msg := &dto.Message{
		Type:    dto.TextMessage,
		Content: content,
	}

	switch eventType {
	case dto.GroupAtMessageCreate:
		_, err := ctx.ReplyGroup(msg)
		return err
	case dto.C2CMessageCreate:
		_, err := ctx.ReplyPrivate(msg)
		return err
	default:
		logger.WithField("event_type", eventType).Warn("[Plugin] Unsupported event type for reply")
		return fmt.Errorf("unsupported event type: %s", eventType)
	}
}

// Dependencies 返回插件依赖列表（帮助插件无依赖）
func (p *Plugin) Dependencies() []string {
	return []string{}
}
