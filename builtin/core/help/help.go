package help

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const (
	// 每页显示的命令数量
	commandsPerPage = 10
)

// helpCmdDef 是 /help 命令的结构化定义，用于在命令列表中展示参数说明。
var helpCmdDef = &command.Definition{
	Name:        "help",
	Aliases:     []string{"h"},
	Description: "查看可用命令和插件信息",
	Usage:       "/help [页码|命令名|插件名] [--text|-t]",
	Category:    "系统",
	Arguments: []*command.Argument{
		{
			Name:        "target",
			Description: "页码、命令名或插件名（留空显示第1页命令列表）",
			Type:        command.ArgTypeString,
			Required:    false,
		},
	},
	Flags: []*command.Flag{
		{
			Name:        "text",
			ShortName:   "t",
			Description: "以纯文字形式发送结果，不渲染图片",
			Type:        command.ArgTypeBool,
			Default:     false,
		},
	},
	Examples: []string{
		"/help",
		"/help 2",
		"/help weather",
		"/help plugins",
		"/help weather --text",
		"/help 2 -t",
	},
}

// PluginOption configures the help plugin at construction time.
type PluginOption func(*Plugin)

// WithImageRender controls whether help responses are rendered as images.
// Image rendering is enabled by default; pass false to always use plain text.
func WithImageRender(enabled bool) PluginOption {
	return func(p *Plugin) { p.imageRender = enabled }
}

// Plugin 帮助插件，显示所有可用命令的帮助信息
//
// 支持以下命令格式：
//   - /help - 显示所有命令（第1页）
//   - /help 2 - 显示第2页的命令
//   - /help <插件名> - 显示指定插件的所有命令
//   - /help <命令名> - 显示指定命令的详细信息
type Plugin struct {
	Engine engine.Reader // Engine 只读视图（查询命令列表，不能注册/删除 Matcher）
	Info   plugin.Info   // 插件系统只读视图

	// imageRender 控制是否以图片形式回复（默认 true）。
	// 可通过 WithImageRender(false) 关闭，退回纯文字发送。
	imageRender bool

	permSvc *plugin.ServiceProxy[*permission.Plugin]

	// 缓存 — 每个条目独立维护过期时间
	helpCache     map[string]cacheEntry[string]
	imageCache    map[string]cacheEntry[[]byte]
	cacheMu       sync.Mutex // 读写均用 Mutex，方便 lazy eviction
	cacheDuration time.Duration
}

// cacheEntry 是一个带独立过期时间的缓存条目。
type cacheEntry[T any] struct {
	data      T
	expiresAt time.Time
}

// New 创建帮助插件（v2 API）
func New(opts ...PluginOption) *plugin.Descriptor {
	p := newHelpPluginInternal()
	for _, o := range opts {
		o(p)
	}

	return &plugin.Descriptor{
		Name:         "help",
		Version:      "2.0.0",
		Deps:         []string{},
		OptionalDeps: []string{"permission"},
		Meta: &plugin.Metadata{
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
			if svc, ok := plugin.TryService[*permission.Plugin](ctx, "permission"); ok {
				p.permSvc = svc
			}
			p.Info = ctx.Info
			p.Engine = ctx.Info.Coordinator()

			ctx.Reg.RegisterCommand("", "/help").SetDefinition(helpCmdDef).Handle(p.handleHelp)

			// 订阅插件生命周期事件，Scope 追踪并在卸载时自动取消订阅
			for _, topic := range []string{"plugin.loaded", "plugin.unloaded", "plugin.reloaded"} {
				t := topic
				if _, err := ctx.Scope().Subscribe(t, func(_ any) {
					ctx.Log.Debugf("Cache invalidated due to %s event", t)
					p.invalidateCache()
				}); err != nil {
					ctx.Log.Warnf("Failed to subscribe to %s: %v", t, err)
				}
			}

			ctx.Log.Info("Help plugin loaded")
			// 返回 *Plugin 注入容器，其他插件可通过 plugin.Service[*help.Plugin](ctx, "help") 获取
			return p, nil
		},
	}
}

func (p *Plugin) checkPermission(ctx *eventctx.Context, perm string) bool {
	if p.permSvc == nil {
		return true
	}
	pp, ok := p.permSvc.Get()
	if !ok || pp == nil {
		return true
	}
	return pp.HasPermission(ctx.GetUserID(), perm)
}

// newHelpPluginInternal 创建帮助插件内部实例
func newHelpPluginInternal() *Plugin {
	return &Plugin{
		Engine:        nil,  // 将在 Setup 时设置
		Info:          nil,  // 将在 Setup 时设置
		imageRender:   true, // 默认开启图片渲染
		helpCache:     make(map[string]cacheEntry[string]),
		imageCache:    make(map[string]cacheEntry[[]byte]),
		cacheDuration: 5 * time.Minute, // 默认缓存 5 分钟
	}
}

// getCachedHelp 获取缓存的帮助信息。
// 过期条目会被惰性删除（连同同 key 的图片缓存），释放内存。
func (p *Plugin) getCachedHelp(key string) (string, bool) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	entry, ok := p.helpCache[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(p.helpCache, key)
		delete(p.imageCache, key) // 同步清理对应的图片缓存
		p.rebuildIfEmpty()
		return "", false
	}
	return entry.data, true
}

// setCachedHelp 设置缓存的帮助信息
func (p *Plugin) setCachedHelp(key string, text string) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	p.helpCache[key] = cacheEntry[string]{
		data:      text,
		expiresAt: time.Now().Add(p.cacheDuration),
	}
}

// getCachedImage 获取缓存的已渲染图片 PNG 字节。
// 过期条目会被惰性删除，释放内存。
func (p *Plugin) getCachedImage(key string) ([]byte, bool) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	entry, ok := p.imageCache[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(p.imageCache, key)
		p.rebuildIfEmpty()
		return nil, false
	}
	return entry.data, true
}

// setCachedImage 缓存已渲染的图片 PNG 字节
func (p *Plugin) setCachedImage(key string, data []byte) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	p.imageCache[key] = cacheEntry[[]byte]{
		data:      data,
		expiresAt: time.Now().Add(p.cacheDuration),
	}
}

// rebuildIfEmpty 当两个 map 都为空时，用 make 重建以释放底层桶数组内存。
// Go 的 map 桶只增不缩，delete/clear 不会归还桶内存，只有 make 新 map 才可以。
// 必须在持有 cacheMu 的情况下调用。
func (p *Plugin) rebuildIfEmpty() {
	if len(p.helpCache) == 0 && len(p.imageCache) == 0 {
		p.helpCache = make(map[string]cacheEntry[string])
		p.imageCache = make(map[string]cacheEntry[[]byte])
	}
}

// invalidateCache 清除所有缓存。
// 使用 make 重建 map 而非 clear，确保底层桶数组内存被释放。
func (p *Plugin) invalidateCache() {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	p.helpCache = make(map[string]cacheEntry[string])
	p.imageCache = make(map[string]cacheEntry[[]byte])
}

// handleHelp 处理帮助命令
func (p *Plugin) handleHelp(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()

	// 使用类型感知的结构化解析器（而非扁平的 ParseCommandLine），
	// 确保 -t / --text 作为 bool flag 不会把紧跟的数字当作自己的值。
	// 例如 `/help -t 2` 中，`2` 应当是页码而非 `-t` 的值。
	parsed, err := command.ParseFromDefinition(content, helpCmdDef, "/")
	if err != nil {
		return p.sendMessage(ctx, "命令解析失败: "+err.Error(), false)
	}

	// 获取第一个参数
	target := parsed.GetString("target")

	// --text/-t flag：本次请求强制使用纯文字发送
	forceText := parsed.GetBool("text")

	if target == "" {
		// 没有参数，显示第1页命令
		return p.showCommandsPage(ctx, 1, forceText)
	}

	// 特殊关键字：plugins
	if strings.EqualFold(target, "plugins") || strings.EqualFold(target, "plugin") {
		return p.showAllPlugins(ctx, forceText)
	}

	// 尝试解析为页码
	if page, err := command.ParseInt(target); err == nil && page > 0 {
		return p.showCommandsPage(ctx, page, forceText)
	}

	// 检查是否是插件名
	if p.Info != nil {
		plugins := p.Info.List()
		for _, pluginName := range plugins {
			if strings.EqualFold(pluginName, target) {
				return p.showPluginCommands(ctx, pluginName, forceText)
			}
		}
	}

	// 尝试作为命令名查找（支持带或不带前缀）
	if cmdInfo := p.Engine.FindCommand(target); cmdInfo != nil {
		return p.showCommandDetail(ctx, cmdInfo, forceText)
	}

	// 未找到，显示建议
	return p.showCommandNotFound(ctx, target, forceText)
}

// showCommandsPage 显示指定页的命令列表
func (p *Plugin) showCommandsPage(ctx *eventctx.Context, page int, forceText bool) error {
	// 尝试从缓存获取
	cacheKey := fmt.Sprintf("page:%d", page)
	if cached, ok := p.getCachedHelp(cacheKey); ok {
		logger.Debugf("[help] Cache hit for page: %d", page)
		return p.sendMessage(ctx, cached, forceText, cacheKey)
	}

	commands := p.Engine.GetAllCommands()

	// 如果没有命令，显示插件列表
	if len(commands) == 0 {
		if p.Info != nil {
			logger.Info("[Plugin] No commands found, showing plugin list instead")
			return p.showAllPlugins(ctx, forceText)
		}
		return p.sendMessage(ctx, "当前没有可用的命令和插件", forceText)
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
				prefix, _ := eventctx.SplitCommandPattern(cmd.Command)
				aliases := make([]string, len(cmd.Aliases))
				for i, alias := range cmd.Aliases {
					aliases[i] = prefix + alias
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

	return p.sendMessage(ctx, helpText, forceText, cacheKey)
}

// showAllPlugins 显示所有插件的列表
func (p *Plugin) showAllPlugins(ctx *eventctx.Context, forceText bool) error {
	if !p.checkPermission(ctx, "help.plugins") {
		return p.sendMessage(ctx, "权限不足：需要 help.plugins 权限", forceText)
	}
	// 尝试从缓存获取
	cacheKey := "plugins"
	if cached, ok := p.getCachedHelp(cacheKey); ok {
		logger.Debug("[help] Cache hit for plugins list")
		return p.sendMessage(ctx, cached, forceText, cacheKey)
	}

	if p.Info == nil {
		return p.sendMessage(ctx, "插件管理器不可用", forceText)
	}

	pluginsMetadata := p.Info.ListWithMetadata()
	if len(pluginsMetadata) == 0 {
		return p.sendMessage(ctx, "当前没有加载任何插件", forceText)
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

	return p.sendMessage(ctx, helpText, forceText, cacheKey)
}

// showPluginCommands 显示指定插件的所有命令
func (p *Plugin) showPluginCommands(ctx *eventctx.Context, pluginName string, forceText bool) error {
	// 尝试从缓存获取
	cacheKey := fmt.Sprintf("plugin:%s", pluginName)
	if cached, ok := p.getCachedHelp(cacheKey); ok {
		logger.Debugf("[help] Cache hit for plugin: %s", pluginName)
		return p.sendMessage(ctx, cached, forceText, cacheKey)
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
		return p.sendMessage(ctx, help.String(), forceText)
	}

	help.WriteString(fmt.Sprintf("📋 提供的命令 (%d 个):\n\n", len(pluginCommands)))

	// 按命令名排序
	sort.Slice(pluginCommands, func(i, j int) bool {
		return pluginCommands[i].Command < pluginCommands[j].Command
	})

	for _, cmd := range pluginCommands {
		help.WriteString(fmt.Sprintf("  %s", cmd.Command))

		if len(cmd.Aliases) > 0 {
			prefix, _ := eventctx.SplitCommandPattern(cmd.Command)
			aliases := make([]string, len(cmd.Aliases))
			for i, alias := range cmd.Aliases {
				aliases[i] = prefix + alias
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

	return p.sendMessage(ctx, help.String(), forceText, cacheKey)
}

// showCommandDetail 显示特定命令的详细信息
func (p *Plugin) showCommandDetail(ctx *eventctx.Context, cmdInfo *engine.CommandInfo, forceText bool) error {
	// 尝试从缓存获取
	cacheKey := fmt.Sprintf("command:%s", cmdInfo.Command)
	if cached, ok := p.getCachedHelp(cacheKey); ok {
		logger.Debugf("[help] Cache hit for command: %s", cmdInfo.Command)
		return p.sendMessage(ctx, cached, forceText, cacheKey)
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
			prefix, cmdName := eventctx.SplitCommandPattern(cmdInfo.Command)
			detail.WriteString(fmt.Sprintf("\n使用 %s%s/<子命令> 查看子命令详情\n",
				prefix, cmdName))
		}
	}

	// 缓存结果
	detailText := detail.String()
	p.setCachedHelp(cacheKey, detailText)

	return p.sendMessage(ctx, detailText, forceText, cacheKey)
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

	return p.sendMessage(ctx, help.String(), false)
}

// showCommandNotFound 命令未找到时的提示
func (p *Plugin) showCommandNotFound(ctx *eventctx.Context, target string, forceText bool) error {
	var msg strings.Builder

	msg.WriteString(fmt.Sprintf("❌ 未找到: %s\n\n", target))

	// 尝试提供相似命令建议
	allCommands := p.Engine.GetAllCommands()
	var suggestions []string

	// 去除用户输入中可能携带的命令前缀字符
	_, searchTerm := eventctx.SplitCommandPattern(target)
	if searchTerm == "" {
		searchTerm = target
	}

	for _, cmd := range allCommands {
		_, cmdName := eventctx.SplitCommandPattern(cmd.Command)
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
			_, cmdName := eventctx.SplitCommandPattern(cmd.Command)
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

	return p.sendMessage(ctx, msg.String(), forceText)
}

// sendMessage 发送消息。
// 当 imageRender 为 true 且 forceText 为 false 时，优先渲染为图片（含水印）发送，失败后自动降级为纯文字。
// 当 imageRender 为 false 或 forceText 为 true 时，直接发送纯文字。
//
// cacheKey 用于图片字节缓存的键（与文本缓存键相同）。留空则不启用图片缓存。
func (p *Plugin) sendMessage(ctx *eventctx.Context, content string, forceText bool, cacheKey ...string) error {
	if p.imageRender && !forceText {
		// 尝试从缓存获取已渲染的图片字节
		key := ""
		if len(cacheKey) > 0 {
			key = cacheKey[0]
		}
		if key != "" {
			if cachedImg, ok := p.getCachedImage(key); ok {
				logger.Debugf("[help] Image cache hit for key: %s", key)
				msg := platform.OutboundMessage{
					Attachments: []platform.Attachment{{
						Kind:     platform.AttachmentKindImage,
						Data:     cachedImg,
						Name:     "help.png",
						MimeType: "image/png",
					}},
				}
				if _, sendErr := ctx.Reply(msg); sendErr == nil {
					return nil
				}
				logger.Warnf("[help] cached image send failed, falling back to text")
			}
		}

		imgBytes, renderErr := renderHelpImage(content)
		if renderErr == nil {
			// 缓存渲染结果
			if key != "" {
				p.setCachedImage(key, imgBytes)
			}
			msg := platform.OutboundMessage{
				Attachments: []platform.Attachment{{
					Kind:     platform.AttachmentKindImage,
					Data:     imgBytes,
					Name:     "help.png",
					MimeType: "image/png",
				}},
			}
			if _, sendErr := ctx.Reply(msg); sendErr == nil {
				return nil
			}
			// 图片发送失败，降级为文字
			logger.Warnf("[help] image send failed, falling back to text")
		} else {
			logger.Debugf("[help] image render failed (%v), using text fallback", renderErr)
		}
	}
	_, err := ctx.Reply(platform.TextMessage(content))
	return err
}
