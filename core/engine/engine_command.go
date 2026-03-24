package engine

// engine_command.go — 命令注册、命令信息查询 API
//
// 本文件包含：
//   - OnCommand / RegisterCommand / RegisterCommandDef 及其变体
//   - CommandInfo 类型定义
//   - GetAllCommands / GetCommandsByPlugin / GetCommandsByCategory / FindCommand

import (
	"strings"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// CommandInfo 命令信息（用于 Help 生成和命令发现）
type CommandInfo struct {
	Command     string              // 命令名（如 "/help"）
	Description string              // 命令描述
	Usage       string              // 使用方法
	Aliases     []string            // 别名列表
	Category    string              // 分类
	Examples    []string            // 使用示例
	Permissions []string            // 所需权限
	Plugin      string              // 所属插件名
	Source      string              // 来源标识（如 "plugin:help"）
	EventType   EventType           // 事件类型
	Definition  *command.Definition // 完整定义（直接使用 command.Definition）
}

// ---- 命令注册 ----------------------------------------------------------------

// OnCommand 注册一个命令匹配器（自动开启 O(1) 分发优化）。
//
// 此方法会自动创建一个 command.Definition 并将匹配器注册到 Hash Map 索引中，
// 消息处理时仅需 O(1) 查找，无需遍历所有规则。
func (e *Engine) OnCommand(eventType EventType, cmdPattern string, extraRules ...context.Rule) *Matcher {
	finalRules := make([]context.Rule, 0, len(extraRules)+1)
	finalRules = append(finalRules, context.OnCommand(cmdPattern))
	finalRules = append(finalRules, extraRules...)

	m := &Matcher{
		EventType:   eventType,
		Rules:       finalRules,
		coordinator: e,
		Source:      "global",
	}
	m.commandIndexed.Store(true) // OnCommand 规则（Rules[0]）通过 commandIndex 进行匹配
	m.priority.Store(50)

	cmdName := strings.TrimPrefix(strings.TrimSpace(cmdPattern), "/")
	if cmdName != "" {
		m.definition = &command.Definition{
			Name: cmdName,
		}
	}

	registered := e.registerMatcher(m)
	// 同步到可选的外部 Registry（Trie + 元数据）
	e.syncToRegistry(registered.definition, registered.Source)

	// 注入基础别名自动注册回调（引擎级，适用于 v1 API 或直接调用 Engine.OnCommand 的场景）。
	// 通过 liveRegistryWriter.RegisterCommand 注册的 Matcher 会在此之后被 plugin 层
	// 覆盖为插件感知版本（携带 Group/Source/instance 追踪）。
	// 若没有 plugin 层覆盖，此基础版本负责为简单命令（1条规则）自动注册别名路由。
	e.injectBaseAliasRegistrar(registered, eventType, extraRules)

	return registered
}

// injectBaseAliasRegistrar 为 Matcher 注入引擎级别的基础别名注册回调。
// 别名 Matcher 继承主命令的 EventType/Group/Source，但不具备插件实例追踪
// （liveRegistryWriter 会用插件感知版本覆盖此回调）。
// 仅对简单 OnCommand 匹配器（无额外规则）生效；带有额外解析规则的 Matcher
// 需要其调用方自行管理别名（避免别名缺少必要的解析规则）。
func (e *Engine) injectBaseAliasRegistrar(primary *Matcher, eventType EventType, extraRules []context.Rule) {
	primary.SetAliasRegistrar(func(def *command.Definition, h context.Handler) {
		if def == nil || len(def.Aliases) == 0 {
			return
		}
		// 仅对不携带额外规则的简单命令自动注册别名。
		// 带额外规则（如 ParseFromDefinition）的 Matcher 需要这些规则同步到别名，
		// 此基础版本不复制规则，故跳过。liveRegistryWriter 覆盖版本会正确传递 extraRules。
		if len(extraRules) > 0 {
			return
		}
		primaryCmd := "/" + def.Name
		for _, alias := range def.Aliases {
			aliasPattern := "/" + alias
			// 跳过已在 commandIndex 中存在的别名（避免覆盖已有注册）
			state := e.state.Load()
			if _, exists := state.commandIndex[aliasPattern]; exists {
				logger.Warnf("[engine] alias route %s for %s already registered, skipping auto-registration",
					aliasPattern, primaryCmd)
				continue
			}
			aliasMatcher := e.OnCommand(eventType, aliasPattern)
			aliasMatcher.SetGroup(primary.GetGroup())
			aliasMatcher.SetSource(primary.GetSource())
			aliasMatcher.SetDefinition(&command.Definition{Name: alias, Hidden: true})
			aliasMatcher.Handle(h)
			logger.Debugf("[engine] auto-registered alias route %s -> %s", aliasPattern, primaryCmd)
		}
	})
}

// RegisterCommand 注册一个高级命令定义（使用 "/" 作为默认前缀）
func (e *Engine) RegisterCommand(cmd *command.Definition, rules ...context.Rule) *Matcher {
	return e.RegisterCommandWithPrefix("/", cmd, rules...)
}

// RegisterCommandWithPrefix 带自定义前缀的 RegisterCommand
func (e *Engine) RegisterCommandWithPrefix(prefix string, cmd *command.Definition, rs ...context.Rule) *Matcher {
	trigger := prefix + cmd.Name

	parseRule := func(ctx *context.Context) bool {
		content := ctx.GetMessageContent()
		parsed, err := command.ParseFromDefinition(content, cmd, prefix)
		if err != nil {
			logger.WithError(err).WithField("trigger", trigger).Debug("[engine] Command parse match failed")
			return false
		}
		ctx.SetParsedCommand(parsed)
		return true
	}

	finalRules := append([]context.Rule{parseRule}, rs...)
	m := e.OnCommand("", trigger, finalRules...)
	m.SetDefinition(cmd)
	m.Handle(context.ExecuteCommandDefinition)
	return m
}

// RegisterCommandDef 注册 command.Definition（推荐方式，自动设置元数据）。
//
// 它会自动：
//  1. 创建命令解析规则
//  2. 设置 Definition 到 Matcher
//  3. 设置 Handler（如果 Definition 中有定义）
//
// 示例:
//
//	def := &command.Definition{
//	    Name:        "search",
//	    Description: "搜索内容",
//	    Usage:       "/search <keyword>",
//	}
//	engine.RegisterCommandDef(dto.GroupAtMessageCreate, def)
func (e *Engine) RegisterCommandDef(eventType EventType, def *command.Definition, extraRules ...context.Rule) *Matcher {
	if def == nil {
		logger.Warn("[engine] RegisterCommandDef: definition is nil")
		return newNoopMatcher(e)
	}

	trigger := "/" + def.Name

	parseRule := func(ctx *context.Context) bool {
		content := ctx.GetMessageContent()
		parsed, err := command.ParseFromDefinition(content, def, "/")
		if err != nil {
			logger.WithError(err).WithField("trigger", trigger).Debug("[engine] Command parse failed")
			return false
		}
		ctx.SetParsedCommand(parsed)
		return true
	}

	finalRules := make([]context.Rule, 0, len(extraRules)+1)
	finalRules = append(finalRules, parseRule)
	finalRules = append(finalRules, extraRules...)

	m := e.OnCommand(eventType, trigger, finalRules...)
	m.SetDefinition(def)

	if def.Handler != nil {
		m.Handle(func(ctx *context.Context) error {
			def.Handler(ctx)
			return nil
		})
	}
	// syncToRegistry 已由 OnCommand 内部调用，此处更新带完整元数据的 def
	e.syncToRegistry(def, m.Source)
	return m
}

// RegisterCommandDefWithPrefix 带自定义前缀的 RegisterCommandDef。
//
// 允许使用自定义命令前缀（如 "!" 或 "#"）：
//
//	engine.RegisterCommandDefWithPrefix(dto.GroupAtMessageCreate, "!", def)
func (e *Engine) RegisterCommandDefWithPrefix(
	eventType EventType,
	prefix string,
	def *command.Definition,
	extraRules ...context.Rule,
) *Matcher {
	if def == nil {
		logger.Warn("[engine] RegisterCommandDefWithPrefix: definition is nil")
		return newNoopMatcher(e)
	}
	if prefix == "" {
		prefix = "/"
	}

	trigger := prefix + def.Name

	parseRule := func(ctx *context.Context) bool {
		content := ctx.GetMessageContent()
		parsed, err := command.ParseFromDefinition(content, def, prefix)
		if err != nil {
			logger.WithError(err).WithField("trigger", trigger).Debug("[engine] Command parse failed")
			return false
		}
		ctx.SetParsedCommand(parsed)
		return true
	}

	finalRules := make([]context.Rule, 0, len(extraRules)+1)
	finalRules = append(finalRules, parseRule)
	finalRules = append(finalRules, extraRules...)

	m := e.OnCommand(eventType, trigger, finalRules...)
	m.SetDefinition(def)

	if def.Handler != nil {
		m.Handle(func(ctx *context.Context) error {
			def.Handler(ctx)
			return nil
		})
	}
	// 更新带完整元数据的 def 到 Registry
	e.syncToRegistry(def, m.Source)
	return m
}

// ---- 命令查询 ----------------------------------------------------------------

// GetAllCommands 获取所有已注册的命令信息快照。
//
// 直接返回预构建的命令列表缓存副本，避免每次调用遍历 map。
// 命令列表在每次 COW 写操作时自动更新，读操作 O(n) 复制一次切片。
// 返回的命令列表不包含隐藏命令（Hidden=true）。
func (e *Engine) GetAllCommands() []CommandInfo {
	state := e.state.Load()
	if len(state.commandListCache) == 0 {
		return nil
	}
	commands := make([]CommandInfo, len(state.commandListCache))
	copy(commands, state.commandListCache)
	return commands
}

// GetCommandsByPlugin 按插件分组获取命令。
// 全局命令（非插件注册）使用 "global" 作为键。
func (e *Engine) GetCommandsByPlugin() map[string][]CommandInfo {
	commands := e.GetAllCommands()
	grouped := make(map[string][]CommandInfo)
	for _, cmd := range commands {
		plugin := cmd.Plugin
		if plugin == "" {
			plugin = "global"
		}
		grouped[plugin] = append(grouped[plugin], cmd)
	}
	return grouped
}

// GetCommandsByCategory 按分类获取命令。
// 未设置分类的命令使用 "其他" 作为键。
func (e *Engine) GetCommandsByCategory() map[string][]CommandInfo {
	commands := e.GetAllCommands()
	grouped := make(map[string][]CommandInfo)
	for _, cmd := range commands {
		category := cmd.Category
		if category == "" {
			category = "其他"
		}
		grouped[category] = append(grouped[category], cmd)
	}
	return grouped
}

// FindCommand 查找特定命令（支持别名）。
// name 可以含或不含 "/" 前缀。
func (e *Engine) FindCommand(name string) *CommandInfo {
	commands := e.GetAllCommands()

	searchName := name
	if !strings.HasPrefix(searchName, "/") {
		searchName = "/" + searchName
	}

	for _, cmd := range commands {
		if cmd.Command == searchName || cmd.Command == name {
			return &cmd
		}
		for _, alias := range cmd.Aliases {
			aliasWithSlash := alias
			if !strings.HasPrefix(alias, "/") {
				aliasWithSlash = "/" + alias
			}
			if aliasWithSlash == searchName || alias == name {
				return &cmd
			}
		}
	}
	return nil
}

// ---- 命令注册表集成 ----------------------------------------------------------

// SetCommandRegistry 注入外部 command.Registry，启用统一命令系统。
//
// 注入后，OnCommand / RegisterCommandDef 等方法会在更新内部 commandIndex 的同时，
// 自动将命令的 Definition 同步注册到 Registry（Trie + 元数据），
// 使 Trie 前缀搜索、/help 发现和 commandIndex O(1) 路由始终保持一致。
//
// 未注入时（默认），Engine 只维护 commandIndex，行为与旧版本完全兼容。
//
// 使用示例：
//
//	reg := command.NewCommandRegistry()
//	eng := engine.NewEngine()
//	eng.SetCommandRegistry(reg)
//
//	// 此后 OnCommand / RegisterCommandDef 均自动同步到 reg
//	eng.OnCommand(dto.GroupAtMessageCreate, "/hello").
//	    SetDescription("打招呼").
//	    Handle(handler)
//
//	// Trie 前缀搜索和 /help 现在都能找到 "/hello"
//	matches := reg.Search("/he")
func (e *Engine) SetCommandRegistry(reg *command.Registry) {
	e.writeMu.Lock()
	e.services.commandRegistry = reg
	e.writeMu.Unlock()
}

// CommandRegistry 返回已注入的 command.Registry，未注入时返回 nil。
func (e *Engine) CommandRegistry() *command.Registry {
	e.writeMu.Lock()
	reg := e.services.commandRegistry
	e.writeMu.Unlock()
	return reg
}

// syncToRegistry 将 Matcher 的 Definition 同步注册到 commandRegistry（若已注入）。
// 在 OnCommand / RegisterCommandDef / SetDefinition 后调用，source 为插件来源标签。
// 如果 def 为 nil 或 Registry 未注入，此函数为空操作。
func (e *Engine) syncToRegistry(def *command.Definition, source string) {
	if def == nil || def.Name == "" {
		return
	}
	e.writeMu.Lock()
	reg := e.services.commandRegistry
	e.writeMu.Unlock()
	if reg == nil {
		return
	}
	opts := command.RegisterOptions{Source: source}
	// Upsert: register new or update existing entry (handles the case where
	// OnCommand already registered a bare definition and RegisterCommandDef
	// subsequently syncs the full definition with metadata).
	reg.Upsert(def, opts)
}

// WithCommandRegistry Engine 选项：注入 command.Registry。
//
// 与 SetCommandRegistry 等价，但可在 NewEngine 时一并传入：
//
//	reg := command.NewCommandRegistry()
//	eng := engine.NewEngine(engine.WithCommandRegistry(reg))
func WithCommandRegistry(reg *command.Registry) Option {
	return func(e *Engine) {
		e.services.commandRegistry = reg
	}
}
