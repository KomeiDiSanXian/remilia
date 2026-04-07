// Package pluginctrl 提供逐群/逐用户的插件开关管理，并支持持久化。
//
// 功能：
//   - 管理员通过聊天指令开启/关闭某个插件（仅对当前群生效）
//   - 超级管理员可全局开启/关闭插件
//   - 状态持久化（依赖 storage 插件；未注入时降级为内存模式，重启后丢失）
//   - 提供 Rule(pluginName) 方法，可插入 engine.On() 的 Rule 列表实现自动过滤
//
// 指令（默认，可通过 WithCommandPrefix 修改）：
//
//	开启 <插件名>    — 在当前群开启指定插件（群管理员）
//	关闭 <插件名>    — 在当前群关闭指定插件（群管理员）
//	服务列表         — 查看本群所有插件及开关状态（所有人）
//	全局开启 <插件名> — 全局开启插件（超级管理员）
//	全局关闭 <插件名> — 全局关闭插件（超级管理员）
//
// 使用示例：
//
//	// 注册（storage 为可选依赖）
//	pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("admin1", "admin2")))
//
//	// 在业务插件中注册带开关的命令
//	ctrl := plugin.Must[pluginctrl.Plugin](ctx, "pluginctrl")
//	ctx.Reg.On(string(platform.EventKindGroupMessage), ctrl.Rule("myplugin")).
//	    Handle(myHandler)
package pluginctrl

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// GroupPluginState 每个群/插件对的开关状态记录（GORM 模型）。
type GroupPluginState struct {
	ID         uint      `gorm:"primaryKey;autoIncrement"`
	GroupID    string    `gorm:"uniqueIndex:idx_group_plugin;not null"`
	PluginName string    `gorm:"uniqueIndex:idx_group_plugin;not null"`
	Enabled    bool      `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime"`
}

// Plugin 逐群插件开关管理器。
//
// 通过 plugin.Must[pluginctrl.Plugin](ctx, "pluginctrl") 获取。
type Plugin struct {
	mu sync.RWMutex
	// groupStates[groupID][pluginName] = enabled
	// 未设置的插件默认为开启（defaultEnabled）
	groupStates map[string]map[string]bool
	// globalStates[pluginName] = enabled（超级管理员控制，优先级高于群开关）
	globalStates map[string]bool

	storage    storage.Client // 可选：持久化存储
	superUsers map[string]bool
	opts       *options
}

func newPlugin(o *options) *Plugin {
	p := &Plugin{
		groupStates:  make(map[string]map[string]bool),
		globalStates: make(map[string]bool),
		superUsers:   make(map[string]bool),
		opts:         o,
	}
	for _, su := range o.superUsers {
		p.superUsers[su] = true
	}
	return p
}

// NewPlugin 创建 Plugin 实例（不注册指令，供直接使用或测试）。
func NewPlugin(opts ...Option) *Plugin {
	o := defaultOptions()
	for _, fn := range opts {
		fn(o)
	}
	return newPlugin(o)
}

// NewPluginWithStorage 创建带存储后端的 Plugin 实例（不注册指令，供直接使用或测试）。
func NewPluginWithStorage(client storage.Client, opts ...Option) *Plugin {
	p := NewPlugin(opts...)
	p.storage = client
	return p
}

// LoadFromDB 从数据库加载持久化状态（公开接口，供测试或手动调用）。
func (p *Plugin) LoadFromDB() {
	p.loadFromDB()
}

// ----- 查询 API -----

// IsEnabled 查询指定群内某插件是否处于开启状态。
//
// 判断逻辑（优先级从高到低）：
//  1. 全局状态（超级管理员通过"全局关闭"设置）
//  2. 群级状态（群管理员通过"关闭/开启"设置）
//  3. 默认值（defaultEnabled，通常为 true）
func (p *Plugin) IsEnabled(groupID, pluginName string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// 1. 检查全局覆盖
	if enabled, ok := p.globalStates[pluginName]; ok {
		return enabled
	}
	// 2. 检查群级状态
	if gs, ok := p.groupStates[groupID]; ok {
		if enabled, ok := gs[pluginName]; ok {
			return enabled
		}
	}
	// 3. 默认开启
	return p.opts.defaultEnabled
}

// Rule 返回一个 engine Rule，用于过滤当前群内已关闭的插件。
//
// 用法：
//
//	ctrl := plugin.Must[pluginctrl.Plugin](ctx, "pluginctrl")
//	ctx.Reg.On(string(platform.EventKindGroupMessage), ctrl.Rule("weather")).Handle(handler)
func (p *Plugin) Rule(pluginName string) eventctx.Rule {
	return func(ctx *eventctx.Context) bool {
		chatInfo := ctx.GetChatInfo()
		if !chatInfo.IsGroup {
			return true // 非群消息不受群级开关控制
		}
		return p.IsEnabled(chatInfo.ID, pluginName)
	}
}

// ----- 写入 API -----

// SetGroupEnabled 设置指定群内某插件的开关状态，并持久化。
func (p *Plugin) SetGroupEnabled(groupID, pluginName string, enabled bool) error {
	p.mu.Lock()
	if _, ok := p.groupStates[groupID]; !ok {
		p.groupStates[groupID] = make(map[string]bool)
	}
	p.groupStates[groupID][pluginName] = enabled
	p.mu.Unlock()
	return p.persist(groupID, pluginName, enabled)
}

// SetGlobalEnabled 全局设置某插件的开关状态（超级管理员专用），并持久化。
func (p *Plugin) SetGlobalEnabled(pluginName string, enabled bool) error {
	const globalGroupID = "__global__"
	p.mu.Lock()
	p.globalStates[pluginName] = enabled
	p.mu.Unlock()
	return p.persist(globalGroupID, pluginName, enabled)
}

// IsSuperUser 检查用户是否是超级管理员
func (p *Plugin) IsSuperUser(userID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.superUsers[userID]
}

// GroupList 返回指定群的所有已设置的插件状态列表（{PluginName, Enabled} 对）。
func (p *Plugin) GroupList(groupID string) []GroupPluginState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	gs, ok := p.groupStates[groupID]
	if !ok {
		return nil
	}
	result := make([]GroupPluginState, 0, len(gs))
	for name, enabled := range gs {
		result = append(result, GroupPluginState{
			GroupID:    groupID,
			PluginName: name,
			Enabled:    enabled,
		})
	}
	return result
}

// ----- 持久化 -----

const globalGroupID = "__global__"

func (p *Plugin) persist(groupID, pluginName string, enabled bool) error {
	if p.storage == nil {
		return nil // 纯内存模式，无需持久化
	}
	state := &GroupPluginState{
		GroupID:    groupID,
		PluginName: pluginName,
		Enabled:    enabled,
	}
	// 先查是否存在
	var existing GroupPluginState
	err := p.storage.Where("group_id = ? AND plugin_name = ?", groupID, pluginName).First(&existing)
	if err == nil {
		// 更新
		existing.Enabled = enabled
		return p.storage.Save(&existing)
	}
	// 新建
	return p.storage.Create(state)
}

func (p *Plugin) loadFromDB() {
	if p.storage == nil {
		return
	}
	var states []GroupPluginState
	if err := p.storage.Find(&states); err != nil {
		logger.WithError(err).Warn("[pluginctrl] Failed to load states from DB")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range states {
		if s.GroupID == globalGroupID {
			p.globalStates[s.PluginName] = s.Enabled
			continue
		}
		if _, ok := p.groupStates[s.GroupID]; !ok {
			p.groupStates[s.GroupID] = make(map[string]bool)
		}
		p.groupStates[s.GroupID][s.PluginName] = s.Enabled
	}
	logger.Infof("[pluginctrl] Loaded %d group-plugin states from DB", len(states))
}

// ----- 权限检查辅助函数 -----

// isGroupAdmin 简单判断发送者是否具有群管理权限。
// 当前通过检查超级用户列表实现；未来可扩展为检查平台群角色。
func (p *Plugin) isGroupAdmin(ctx *eventctx.Context) bool {
	sender := ctx.GetSenderInfo()
	return p.IsSuperUser(sender.ID)
}

// ----- 指令处理 -----

func (p *Plugin) handleEnable(ctx *eventctx.Context) error {
	return p.handleToggle(ctx, true)
}

func (p *Plugin) handleDisable(ctx *eventctx.Context) error {
	return p.handleToggle(ctx, false)
}

func (p *Plugin) handleToggle(ctx *eventctx.Context, enable bool) error {
	if !p.isGroupAdmin(ctx) {
		_, _ = ctx.Reply(platform.TextMessage("❌ 权限不足，需要管理员权限"))
		return nil
	}
	chat := ctx.GetChatInfo()
	if !chat.IsGroup {
		_, _ = ctx.Reply(platform.TextMessage("❌ 该指令仅在群内使用"))
		return nil
	}
	verb := p.opts.enableCmd
	if !enable {
		verb = p.opts.disableCmd
	}
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil || len(args.Positional) == 0 {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 用法：/%s <插件名>", verb)))
		return nil
	}
	pluginName := args.Positional[0]
	if err := p.SetGroupEnabled(chat.ID, pluginName, enable); err != nil {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
		return nil
	}
	action := "开启"
	if !enable {
		action = "关闭"
	}
	_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("✅ 已在本群 %s 插件「%s」", action, pluginName)))
	return nil
}

func (p *Plugin) handleGlobalEnable(ctx *eventctx.Context) error {
	return p.handleGlobalToggle(ctx, true)
}

func (p *Plugin) handleGlobalDisable(ctx *eventctx.Context) error {
	return p.handleGlobalToggle(ctx, false)
}

func (p *Plugin) handleGlobalToggle(ctx *eventctx.Context, enable bool) error {
	if !p.IsSuperUser(ctx.GetSenderInfo().ID) {
		_, _ = ctx.Reply(platform.TextMessage("❌ 权限不足，需要超级管理员权限"))
		return nil
	}
	verb := p.opts.globalEnableCmd
	if !enable {
		verb = p.opts.globalDisableCmd
	}
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil || len(args.Positional) == 0 {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 用法：/%s <插件名>", verb)))
		return nil
	}
	pluginName := args.Positional[0]
	if err := p.SetGlobalEnabled(pluginName, enable); err != nil {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
		return nil
	}
	action := "开启"
	if !enable {
		action = "关闭"
	}
	_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("✅ 已全局 %s 插件「%s」", action, pluginName)))
	return nil
}

func (p *Plugin) handleServiceList(ctx *eventctx.Context) error {
	chat := ctx.GetChatInfo()
	if !chat.IsGroup {
		_, _ = ctx.Reply(platform.TextMessage("❌ 该指令仅在群内使用"))
		return nil
	}
	states := p.GroupList(chat.ID)
	if len(states) == 0 {
		_, _ = ctx.Reply(platform.TextMessage("📋 本群所有插件均处于默认状态（已开启）"))
		return nil
	}
	var sb strings.Builder
	sb.WriteString("📋 本群插件状态：\n")
	for _, s := range states {
		status := "✅ 开启"
		if !s.Enabled {
			status = "❌ 关闭"
		}
		_, _ = fmt.Fprintf(&sb, "  %s — %s\n", s.PluginName, status)
	}
	_, _ = ctx.Reply(platform.TextMessage(sb.String()))
	return nil
}

// ----- New -----

// New 创建逐群插件开关管理器的描述符。
//
// 示例：
//
//	pm.Register(pluginctrl.New(
//	    pluginctrl.WithSuperUsers("openid-admin1"),
//	))
func New(opts ...Option) *plugin.Descriptor {
	o := defaultOptions()
	for _, fn := range opts {
		fn(o)
	}
	p := newPlugin(o)
	return &plugin.Descriptor{
		Name:         "pluginctrl",
		Version:      "1.0.0",
		OptionalDeps: []string{"storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "逐群插件开关管理，支持持久化",
			Category:    "管理",
			Tags:        []string{"管理", "插件", "开关", "权限"},
			HelpText: fmt.Sprintf(`逐群插件开关管理指令：
  %s <插件名>  — 在本群开启插件（管理员）
  %s <插件名>  — 在本群关闭插件（管理员）
  %s           — 查看本群插件状态
  %s <插件名>  — 全局开启插件（超级管理员）
  %s <插件名>  — 全局关闭插件（超级管理员）`,
				o.enableCmd, o.disableCmd, o.listCmd,
				o.globalEnableCmd, o.globalDisableCmd),
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			// 尝试获取存储（可选依赖）
			if client, ok := plugin.TryAs[storage.Client](ctx, "storage"); ok {
				p.storage = client
				ctx.Log.Info("Storage backend connected, plugin states will be persisted")
				if !ctx.DryRun {
					// 迁移表结构
					if err := client.AutoMigrate(&GroupPluginState{}); err != nil {
						ctx.Log.Warnf("Failed to migrate GroupPluginState table: %v", err)
					} else {
						p.loadFromDB()
					}
				}
			} else {
				ctx.Log.Warn("No storage backend found, plugin states are in-memory only (lost on restart)")
			}
			if !ctx.DryRun {
				p.registerCommands(ctx)
			}
			return p, nil
		},
	}
}

// registerCommands 在 Setup 阶段注册所有管理指令
func (p *Plugin) registerCommands(ctx *plugin.SetupContext) {
	o := p.opts
	groupEvent := string(platform.EventKindGroupMessage)
	// /<enableCmd> <插件名>  ——  开启 weather
	ctx.Reg.RegisterCommand(groupEvent, "/"+o.enableCmd).Handle(p.handleEnable)
	// /<disableCmd> <插件名>  ——  关闭 weather
	ctx.Reg.RegisterCommand(groupEvent, "/"+o.disableCmd).Handle(p.handleDisable)
	// /<listCmd>  ——  服务列表
	ctx.Reg.RegisterCommand(groupEvent, "/"+o.listCmd).Handle(p.handleServiceList)
	// /<globalEnableCmd> <插件名>  ——  全局开启 weather（私聊或群均可）
	ctx.Reg.RegisterCommand("", "/"+o.globalEnableCmd).Handle(p.handleGlobalEnable)
	// /<globalDisableCmd> <插件名>  ——  全局关闭 weather
	ctx.Reg.RegisterCommand("", "/"+o.globalDisableCmd).Handle(p.handleGlobalDisable)
}
