// Package pluginctrl 提供逐群/逐用户的插件开关管理，并支持持久化。
//
// 功能：
//   - 管理员通过聊天指令开启/关闭某个插件（仅对当前群生效）
//   - 超级管理员可全局开启/关闭插件
//   - 超级管理员可针对特定用户禁用/启用某插件（用户级黑名单）
//   - 超级管理员可将整个群设为静默状态（bot 不响应任何消息）
//   - 超级管理员可全局封禁用户（不能使用任何插件）
//   - 超级管理员可在运行时翻转插件的默认启用状态（FlipDefault）
//   - 注册时声明冷却策略（对标 zbpctrl GroupLimit/UserLimit），由 Middleware 自动执行
//   - 状态持久化（依赖 storage 插件；未注入时降级为内存模式，重启后丢失）
//   - 提供 Rule(pluginName) / RuleFull(pluginName) 方法，插入 engine.On() 的 Rule 列表
//   - 提供 Middleware(pluginName) 方法，一步完成群开关 + 用户禁用 + 冷却检查
//
// # 自动注入模式（推荐）
//
// pluginctrl 作为 Privileged 插件，会在 Setup 时自动将 combinedGuard 注册为
// 每个业务插件分组的引擎级中间件，无需业务插件手动挂载 Middleware。
// 业务插件只需正常注册，pluginctrl 自动为其提供完整的访问管控保护。
//
// 指令（默认，可通过 With* 选项修改）：
//
//	开启 <插件名>      — 在当前群开启指定插件（群管理员）
//	关闭 <插件名>      — 在当前群关闭指定插件（群管理员）
//	服务列表           — 查看本群所有插件及开关状态（所有人）
//	全局开启 <插件名>  — 全局开启插件（超级管理员）
//	全局关闭 <插件名>  — 全局关闭插件（超级管理员）
//	禁用用户 <userID> <插件名> — 禁止指定用户使用某插件（超级管理员）
//	启用用户 <userID> <插件名> — 解除对指定用户的禁用（超级管理员）
//	沉默 [群ID]        — 将指定群（或当前群）设为静默状态（超级管理员）
//	响应 [群ID]        — 解除指定群（或当前群）的静默状态（超级管理员）
//	封禁 <userID>      — 全局封禁指定用户（超级管理员）
//	解封 <userID>      — 解除全局封禁（超级管理员）
//	反转默认 <插件名>  — 翻转插件的默认启用状态（超级管理员）
//
// 使用示例：
//
//	// 注册（storage 为可选依赖）
//	pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("admin1", "admin2")))
//
//	// 业务插件无需任何 pluginctrl 相关代码，自动受保护
//	pm.Register(&plugin.Descriptor{
//	    Name: "weather",
//	    Setup: func(ctx *plugin.SetupContext) (any, error) {
//	        ctx.Reg.RegisterCommand(groupEvent, "/天气").Handle(handler) // 自动受 pluginctrl 管控
//	        return p, nil
//	    },
//	})
//
//	// 若需要冷却控制，仍可手动使用 Middleware（与自动注入兼容）：
//	ctrl := plugin.Must[pluginctrl.Plugin](ctx, "pluginctrl")
//	ctrl.RegisterPolicy("weather", pluginctrl.PluginPolicy{UserLimit: 10 * time.Second})
//	ctx.Reg.RegisterCommand(groupEvent, "/天气").
//	    Use(ctrl.CooldownOnly("weather")).  // 只挂冷却，其他由 combinedGuard 处理
//	    Handle(handler)
package pluginctrl

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/cooldown"
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

// UserPluginState 每个用户/插件对的禁用状态记录（GORM 模型）。
//
// Enabled=false 表示该用户被超级管理员禁止使用此插件。
// 未设置（不存在记录）时默认允许使用（defaultUserEnabled=true）。
// PluginName 为特殊值 "__global_ban__" 时，Enabled=true 表示全局封禁该用户。
type UserPluginState struct {
	ID         uint      `gorm:"primaryKey;autoIncrement"`
	UserID     string    `gorm:"uniqueIndex:idx_user_plugin;not null"`
	PluginName string    `gorm:"uniqueIndex:idx_user_plugin;not null"`
	Enabled    bool      `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime"`
}

// PluginPolicy 注册时声明的插件冷却策略（对标 zbpctrl GroupLimit/UserLimit/GlobalLimit）。
//
// 通过 [Plugin.RegisterPolicy] 为插件声明策略后，
// [Plugin.Middleware] 会自动在群开关 + 用户禁用检查通过后执行冷却检查。
//
// 零值字段（0）表示该维度不限制。
type PluginPolicy struct {
	// UserLimit 每用户冷却时间（0 = 不限制）
	UserLimit time.Duration
	// GroupLimit 每群共享冷却时间（0 = 不限制）
	// 群内任意用户触发后，整个群进入冷却状态
	GroupLimit time.Duration
	// GlobalLimit 全局冷却时间（0 = 不限制）
	GlobalLimit time.Duration
}

// Plugin 逐群/逐用户插件开关管理器。
//
// 通过 plugin.Must[pluginctrl.Plugin](ctx, "pluginctrl") 获取。
type Plugin struct {
	mu sync.RWMutex
	// groupStates[groupID][pluginName] = enabled
	groupStates map[string]map[string]bool
	// globalStates[pluginName] = enabled（超级管理员控制，优先级高于群开关）
	globalStates map[string]bool
	// userStates[userID][pluginName] = enabled（超级管理员控制，false = 禁用该用户）
	userStates map[string]map[string]bool
	// policies[pluginName] = *PluginPolicy（注册时声明的冷却策略）
	policies map[string]*PluginPolicy
	// silencedGroups[groupID] = true 表示该群已被静默（bot 不响应任何消息）
	silencedGroups map[string]bool
	// bannedUsers[userID] = true 表示该用户已被全局封禁（不能使用任何插件）
	bannedUsers map[string]bool
	// pluginDefaults[pluginName] = bool 运行时翻转的每插件默认状态（FlipDefault 设置）
	pluginDefaults map[string]bool

	// groupWireFn 由 Setup 阶段从 ctx.NewGroupMiddlewareApplier() 获取，
	// 用于在 LifecycleListener 中为新加载的插件自动注入 combinedGuard。
	// 若为 nil（纯内存/测试模式），自动注入功能不可用。
	groupWireFn func(group string, mw ...eventctx.Middleware)

	// groupResetFn 由 Setup 阶段从 ctx.NewGroupMiddlewareResetter() 获取，
	// 用于在插件卸载/重新注册前清除分组守卫，防止重复追加。
	// 若为 nil（纯内存/测试模式），清除操作为 no-op。
	groupResetFn func(group string)

	storage    storage.Client // 可选：持久化存储
	superUsers map[string]bool
	opts       *options
	cd         *cooldown.Plugin // 内置冷却插件，供 Middleware 使用
}

func newPlugin(o *options) *Plugin {
	p := &Plugin{
		groupStates:    make(map[string]map[string]bool),
		globalStates:   make(map[string]bool),
		userStates:     make(map[string]map[string]bool),
		policies:       make(map[string]*PluginPolicy),
		silencedGroups: make(map[string]bool),
		bannedUsers:    make(map[string]bool),
		pluginDefaults: make(map[string]bool),
		superUsers:     make(map[string]bool),
		opts:           o,
		cd:             cooldown.NewPlugin(),
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
//  3. 运行时每插件默认状态（FlipDefault 翻转后的值）
//  4. 全局默认值（defaultEnabled，通常为 true）
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
	// 3. 检查运行时每插件默认状态（FlipDefault 设置）
	if def, ok := p.pluginDefaults[pluginName]; ok {
		return def
	}
	// 4. 全局默认值
	return p.opts.defaultEnabled
}

// IsGroupSilenced 检查指定群是否处于静默状态。
//
// 处于静默状态的群，机器人不响应任何消息。
// Rule、RuleFull 和 Middleware 在静默群中均直接返回 false/nil。
func (p *Plugin) IsGroupSilenced(groupID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.silencedGroups[groupID]
}

// Rule 返回一个 engine Rule，用于过滤当前群内已关闭的插件。
//
// 静默群中该 Rule 始终返回 false。
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
		// 群静默检查（优先于插件状态）
		if p.IsGroupSilenced(chatInfo.ID) {
			return false
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
	p.mu.Lock()
	p.globalStates[pluginName] = enabled
	p.mu.Unlock()
	return p.persist(globalGroupID, pluginName, enabled)
}

// SilenceGroup 将指定群设置为静默状态，并持久化。
//
// 静默后，该群的所有 Rule/Middleware 检查均直接返回 false，
// 机器人不再响应该群的任何消息，直到调用 [Plugin.ResumeGroup]。
func (p *Plugin) SilenceGroup(groupID string) error {
	p.mu.Lock()
	p.silencedGroups[groupID] = true
	p.mu.Unlock()
	return p.persist(groupID, silencePlugin, true)
}

// ResumeGroup 解除指定群的静默状态，并持久化。
func (p *Plugin) ResumeGroup(groupID string) error {
	p.mu.Lock()
	delete(p.silencedGroups, groupID)
	p.mu.Unlock()
	return p.persist(groupID, silencePlugin, false)
}

// FlipDefault 翻转指定插件的运行时默认启用状态，并持久化。
//
// 若该插件尚无运行时覆盖，则以全局默认值（defaultEnabled）取反；
// 若已有运行时覆盖，则取反该覆盖值。
// 翻转结果优先级低于全局覆盖和群级状态，高于 opts.defaultEnabled。
func (p *Plugin) FlipDefault(pluginName string) error {
	p.mu.Lock()
	newDefault := !p.opts.defaultEnabled
	if cur, ok := p.pluginDefaults[pluginName]; ok {
		newDefault = !cur
	}
	p.pluginDefaults[pluginName] = newDefault
	p.mu.Unlock()
	return p.persist(flipGroupID, pluginName, newDefault)
}

// GetPluginDefault 返回指定插件当前的有效默认状态。
//
// 若已通过 FlipDefault 设置了运行时覆盖，返回覆盖值；
// 否则返回全局默认值（WithDefaultEnabled 设置）。
func (p *Plugin) GetPluginDefault(pluginName string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if def, ok := p.pluginDefaults[pluginName]; ok {
		return def
	}
	return p.opts.defaultEnabled
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

// 特殊 GroupID / PluginName 常量，用于区分持久化记录的语义。
const (
	globalGroupID   = "__global__"       // SetGlobalEnabled 使用
	flipGroupID     = "__flip_default__" // FlipDefault 使用
	silencePlugin   = "__silence__"      // SilenceGroup/ResumeGroup 使用（PluginName）
	globalBanPlugin = "__global_ban__"   // BanUser/UnbanUser 使用（PluginName，存储在 UserPluginState）
)

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
	// --- 群级状态（含静默、翻转默认） ---
	var groupStates []GroupPluginState
	if err := p.storage.Find(&groupStates); err != nil {
		logger.WithError(err).Warn("[pluginctrl] Failed to load group states from DB")
	} else {
		p.mu.Lock()
		for _, s := range groupStates {
			switch {
			case s.GroupID == globalGroupID:
				// 全局覆盖状态
				p.globalStates[s.PluginName] = s.Enabled
			case s.GroupID == flipGroupID:
				// 运行时默认翻转
				p.pluginDefaults[s.PluginName] = s.Enabled
			case s.PluginName == silencePlugin:
				// 群静默状态（Enabled=true 表示静默激活）
				if s.Enabled {
					p.silencedGroups[s.GroupID] = true
				} else {
					delete(p.silencedGroups, s.GroupID)
				}
			default:
				// 普通群级插件开关
				if _, ok := p.groupStates[s.GroupID]; !ok {
					p.groupStates[s.GroupID] = make(map[string]bool)
				}
				p.groupStates[s.GroupID][s.PluginName] = s.Enabled
			}
		}
		p.mu.Unlock()
		logger.Infof("[pluginctrl] Loaded %d group-plugin states from DB", len(groupStates))
	}

	// --- 用户级状态（含全局封禁） ---
	var userStates []UserPluginState
	if err := p.storage.Find(&userStates); err != nil {
		logger.WithError(err).Warn("[pluginctrl] Failed to load user states from DB")
	} else {
		p.mu.Lock()
		for _, s := range userStates {
			if s.PluginName == globalBanPlugin {
				// 全局封禁（Enabled=true 表示封禁激活）
				if s.Enabled {
					p.bannedUsers[s.UserID] = true
				} else {
					delete(p.bannedUsers, s.UserID)
				}
			} else {
				// 普通用户级插件禁用
				if _, ok := p.userStates[s.UserID]; !ok {
					p.userStates[s.UserID] = make(map[string]bool)
				}
				p.userStates[s.UserID][s.PluginName] = s.Enabled
			}
		}
		p.mu.Unlock()
		logger.Infof("[pluginctrl] Loaded %d user-plugin states from DB", len(userStates))
	}
}

// ----- 权限检查辅助函数 -----

// isGroupAdmin 判断发送者是否具有群管理权限。
//
// 判断顺序：
//  1. 超级管理员列表（superUsers）
//  2. platform.GroupRole（Discord 等平台通过 Member.Permissions 推断；
//     GroupRoleAdmin 或 GroupRoleOwner 视为群管理员）
func (p *Plugin) isGroupAdmin(ctx *eventctx.Context) bool {
	sender := ctx.GetSenderInfo()
	if p.IsSuperUser(sender.ID) {
		return true
	}
	return sender.GroupRole >= platform.GroupRoleAdmin
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

// handleSilence 处理"沉默 [群ID]"指令：将指定群（或当前群）设为静默状态。
func (p *Plugin) handleSilence(ctx *eventctx.Context) error {
	if !p.IsSuperUser(ctx.GetSenderInfo().ID) {
		_, _ = ctx.Reply(platform.TextMessage("❌ 权限不足，需要超级管理员权限"))
		return nil
	}
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil || len(args.Positional) == 0 {
		// 无参数时：若在群内则静默当前群
		chat := ctx.GetChatInfo()
		if chat.IsGroup {
			if err := p.SilenceGroup(chat.ID); err != nil {
				_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
				return nil
			}
			_, _ = ctx.Reply(platform.TextMessage("✅ 已将本群设置为静默状态"))
			return nil
		}
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 用法：/%s [群ID]", p.opts.silenceCmd)))
		return nil
	}
	groupID := args.Positional[0]
	if err := p.SilenceGroup(groupID); err != nil {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
		return nil
	}
	_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("✅ 已将群 %s 设置为静默状态", groupID)))
	return nil
}

// handleResume 处理"响应 [群ID]"指令：解除指定群（或当前群）的静默状态。
func (p *Plugin) handleResume(ctx *eventctx.Context) error {
	if !p.IsSuperUser(ctx.GetSenderInfo().ID) {
		_, _ = ctx.Reply(platform.TextMessage("❌ 权限不足，需要超级管理员权限"))
		return nil
	}
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil || len(args.Positional) == 0 {
		chat := ctx.GetChatInfo()
		if chat.IsGroup {
			if err := p.ResumeGroup(chat.ID); err != nil {
				_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
				return nil
			}
			_, _ = ctx.Reply(platform.TextMessage("✅ 已恢复本群的响应"))
			return nil
		}
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 用法：/%s [群ID]", p.opts.resumeCmd)))
		return nil
	}
	groupID := args.Positional[0]
	if err := p.ResumeGroup(groupID); err != nil {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
		return nil
	}
	_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("✅ 已恢复群 %s 的响应", groupID)))
	return nil
}

// handleFlipDefault 处理"反转默认 <插件名>"指令：翻转插件的默认启用状态。
func (p *Plugin) handleFlipDefault(ctx *eventctx.Context) error {
	if !p.IsSuperUser(ctx.GetSenderInfo().ID) {
		_, _ = ctx.Reply(platform.TextMessage("❌ 权限不足，需要超级管理员权限"))
		return nil
	}
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil || len(args.Positional) == 0 {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 用法：/%s <插件名>", p.opts.flipDefaultCmd)))
		return nil
	}
	pluginName := args.Positional[0]
	if err := p.FlipDefault(pluginName); err != nil {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
		return nil
	}
	newDefault := p.GetPluginDefault(pluginName)
	state := "开启"
	if !newDefault {
		state = "关闭"
	}
	_, _ = ctx.Reply(platform.TextMessage(
		fmt.Sprintf("✅ 插件「%s」的默认状态已翻转为：%s", pluginName, state),
	))
	return nil
}

func (p *Plugin) handleServiceList(ctx *eventctx.Context) error {
	chat := ctx.GetChatInfo()
	if !chat.IsGroup {
		_, _ = ctx.Reply(platform.TextMessage("❌ 该指令仅在群内使用"))
		return nil
	}
	states := p.GroupList(chat.ID)

	// 若有图像渲染器，优先尝试生成图像
	if p.opts.serviceListRenderer != nil {
		defEnabled := p.GetPluginDefault("") // 全局默认值（plugin 名为空表示全局）
		// 实际取全局 defaultEnabled（未被 Flip 时）
		p.mu.RLock()
		defEnabled = p.opts.defaultEnabled
		p.mu.RUnlock()

		imgData, mime, err := p.opts.serviceListRenderer(chat.ID, states, defEnabled)
		if err == nil && len(imgData) > 0 {
			_, _ = ctx.Reply(platform.OutboundMessage{
				Attachments: []platform.Attachment{{
					Kind:     platform.AttachmentKindImage,
					Data:     imgData,
					MimeType: mime,
					Name:     "service-list.png",
				}},
			})
			return nil
		}
		if err != nil {
			logger.Warnf("[pluginctrl] ServiceListRenderer failed: %v, falling back to text", err)
		}
	}

	// 文本输出（默认或渲染降级）
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

// defaultInfraPlugins 内置豁免列表：这些插件不会被自动注入 combinedGuard。
// 它们是框架基础设施插件，不应受业务管控逻辑的过滤。
var defaultInfraPlugins = map[string]bool{
	"pluginctrl": true,
	"storage":    true,
	"cooldown":   true,
	"logger":     true,
	"metrics":    true,
	"tracing":    true,
	"pprof":      true,
	"scheduler":  true,
	"auditlog":   true,
}

// isInfraPlugin 判断指定插件是否属于基础设施插件（豁免自动注入）。
func (p *Plugin) isInfraPlugin(name string) bool {
	if defaultInfraPlugins[name] {
		return true
	}
	for _, excluded := range p.opts.excludedPlugins {
		if excluded == name {
			return true
		}
	}
	return false
}

// combinedGuard 返回引擎分组级访问管控中间件，按顺序检查：
//
//  0. 超级管理员豁免（直接放行，确保超管始终可用管理命令）
//  1. 群静默检查（静默群：静默 drop，不发任何回复）
//  2. 全局封禁检查（封禁用户：回复提示后 drop）
//  3. 群级插件开关（插件关闭：回复提示后 drop）
//  4. 用户级插件禁用（用户被禁：回复提示后 drop）
//
// 注意：combinedGuard 不包含冷却检查。冷却属于 per-plugin UX 功能，
// 请通过 RegisterPolicy + Middleware（或 CooldownOnly）显式挂载。
// 这样可避免 combinedGuard 与 Middleware 共存时冷却计时器被重复推进的问题。
func (p *Plugin) combinedGuard(pluginName string) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			chatInfo := ctx.GetChatInfo()
			sender := ctx.GetSenderInfo()

			// 0. 超级管理员豁免：bypass 所有检查
			if sender.ID != "" && p.IsSuperUser(sender.ID) {
				return next(ctx)
			}

			// 1. 群静默：静默 drop（不回复，避免在静默群中产生任何噪音）
			if chatInfo.IsGroup && p.IsGroupSilenced(chatInfo.ID) {
				return nil
			}

			// 2. 全局封禁
			if sender.ID != "" && p.IsBanned(sender.ID) {
				_, _ = ctx.Reply(platform.TextMessage("❌ 你已被封禁，无法使用机器人服务"))
				return nil
			}

			// 3. 群级插件开关
			if chatInfo.IsGroup && !p.IsEnabled(chatInfo.ID, pluginName) {
				_, _ = ctx.Reply(platform.TextMessage("❌ 该功能在本群已关闭"))
				return nil
			}

			// 4. 用户级插件禁用
			if sender.ID != "" && !p.IsUserEnabled(sender.ID, pluginName) {
				_, _ = ctx.Reply(platform.TextMessage("❌ 你已被禁止使用该功能"))
				return nil
			}

			return next(ctx)
		}
	}
}

// CooldownOnly 返回仅执行冷却检查的中间件（不含其他访问控制逻辑）。
//
// 适用于已开启自动注入（combinedGuard）后仍需冷却控制的插件：
// combinedGuard 处理 silence/ban/on-off/user-disable，
// CooldownOnly 单独处理冷却，避免与 Middleware() 共存时的双重检查问题。
//
// 用法：
//
//	ctrl.RegisterPolicy("weather", pluginctrl.PluginPolicy{UserLimit: 10 * time.Second})
//	ctx.Reg.RegisterCommand(groupEvent, "/天气").
//	    Use(ctrl.CooldownOnly("weather")).
//	    Handle(handler)
func (p *Plugin) CooldownOnly(pluginName string) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			if pol, ok := p.GetPolicy(pluginName); ok {
				if !p.checkCooldown(ctx, pluginName, pol) {
					return nil
				}
			}
			return next(ctx)
		}
	}
}

// autoWireListener 实现 plugin.LifecycleListener，
// 在每个新业务插件加载时自动将 combinedGuard 注入其引擎分组中间件。
//
// 生命周期语义：
//   - OnPluginLoaded：先 Reset 再 wire，保证幂等——处理 unload 后重新注册的场景，
//     防止 engine.UseForGroup 追加第二个守卫导致重复执行
//   - OnPluginUnloaded：清除 groupMiddlewares 中的 phantom 条目，避免内存持续累积
//   - OnPluginReloaded：no-op。Reload 生命周期只触发 notifyReloaded（不触发 notifyLoaded），
//     旧守卫闭包读取的是 *Plugin 的实时状态，无需重新注入
type autoWireListener struct {
	p *Plugin
}

func (l *autoWireListener) OnPluginLoaded(name string) {
	if l.p.isInfraPlugin(name) || l.p.groupWireFn == nil {
		return
	}
	// Reset 先于 wire：保证幂等
	// 场景：插件曾被显式 Unregister 后再 Register，若跳过 Reset，
	// UseForGroup 会追加第二个 combinedGuard，导致用户收到两条"❌"回复。
	if l.p.groupResetFn != nil {
		l.p.groupResetFn(name)
	}
	l.p.groupWireFn(name, l.p.combinedGuard(name))
	logger.Debugf("[pluginctrl] Auto-wired combinedGuard for plugin %q", name)
}

func (l *autoWireListener) OnPluginUnloaded(name string) {
	if l.p.isInfraPlugin(name) || l.p.groupResetFn == nil {
		return
	}
	// 清除 phantom 条目：插件已卸载，其 Matcher 已通过 RemoveGroup 移除，
	// 但 engine.middlewareState.groupMiddlewares 里的条目仍然存在（minor map leak）。
	// 此处主动清理，确保内存不随插件频繁卸载而无限累积。
	l.p.groupResetFn(name)
	logger.Debugf("[pluginctrl] Cleaned up combinedGuard for unloaded plugin %q", name)
}

func (l *autoWireListener) OnPluginReloaded(_ string)                 {}
func (l *autoWireListener) OnPluginError(_ string, _ string, _ error) {}

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
		Privileged:   true, // 需要 ctx.Admin（AddLifecycleListener）和 ctx 引擎中间件注册能力
		OptionalDeps: []string{"storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "逐群插件开关管理，支持持久化；作为 Privileged 插件自动为所有业务插件注入访问管控中间件",
			Category:    "管理",
			Tags:        []string{"管理", "插件", "开关", "权限"},
			HelpText: fmt.Sprintf(`逐群插件开关管理指令：
  %s <插件名>  — 在本群开启插件（管理员）
  %s <插件名>  — 在本群关闭插件（管理员）
  %s           — 查看本群插件状态
  %s <插件名>  — 全局开启插件（超级管理员）
  %s <插件名>  — 全局关闭插件（超级管理员）
  %s [群ID]   — 将群设为静默状态（超级管理员）
  %s [群ID]   — 解除群静默状态（超级管理员）
  %s <用户ID> — 全局封禁用户（超级管理员）
  %s <用户ID> — 解除全局封禁（超级管理员）
  %s <插件名>  — 翻转插件默认启用状态（超级管理员）`,
				o.enableCmd, o.disableCmd, o.listCmd,
				o.globalEnableCmd, o.globalDisableCmd,
				o.silenceCmd, o.resumeCmd,
				o.banCmd, o.unbanCmd,
				o.flipDefaultCmd),
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			// 尝试获取存储（可选依赖）
			if client, ok := plugin.TryAs[storage.Client](ctx, "storage"); ok {
				p.storage = client
				ctx.Log.Info("Storage backend connected, plugin states will be persisted")
				if !ctx.DryRun {
					// 迁移表结构（含新增的 UserPluginState 表）
					if err := client.AutoMigrate(&GroupPluginState{}, &UserPluginState{}); err != nil {
						ctx.Log.Warnf("Failed to migrate pluginctrl tables: %v", err)
					} else {
						p.loadFromDB()
					}
				}
			} else {
				ctx.Log.Warn("No storage backend found, plugin states are in-memory only (lost on restart)")
			}

			if !ctx.DryRun {
				// ── 自动注入阶段 ──────────────────────────────────────────────
				// 1. 捕获引擎分组中间件注入/清除函数，供 autoWireListener 使用
				p.groupWireFn = ctx.NewGroupMiddlewareApplier()
				p.groupResetFn = ctx.NewGroupMiddlewareResetter()

				// 2. 为 pluginctrl 加载前已存在的业务插件补充 combinedGuard
				//    先 Reset 再 wire，保证幂等（处理 pluginctrl 热重载后重新注入的情况）
				for _, name := range ctx.Info.List() {
					if !p.isInfraPlugin(name) {
						p.groupResetFn(name)
						ctx.UseEngineForGroup(name, p.combinedGuard(name))
						ctx.Log.Debugf("Retroactively wired combinedGuard for existing plugin %q", name)
					}
				}

				// 3. 注册生命周期监听器，为后续加载的业务插件自动注入 combinedGuard
				ctx.Admin.AddLifecycleListener(&autoWireListener{p: p})
				ctx.Log.Info("Auto-wire enabled: combinedGuard will be injected for all user plugins")

				// ── 管理指令注册 ───────────────────────────────────────────────
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

	// /<enableCmd> <插件名>  ——  开启 weather（群管理员）
	ctx.Reg.RegisterCommand(groupEvent, "/"+o.enableCmd).Handle(p.handleEnable)
	// /<disableCmd> <插件名>  ——  关闭 weather（群管理员）
	ctx.Reg.RegisterCommand(groupEvent, "/"+o.disableCmd).Handle(p.handleDisable)
	// /<listCmd>  ——  服务列表
	ctx.Reg.RegisterCommand(groupEvent, "/"+o.listCmd).Handle(p.handleServiceList)
	// /<globalEnableCmd> <插件名>  ——  全局开启 weather（超级管理员，私聊或群均可）
	ctx.Reg.RegisterCommand("", "/"+o.globalEnableCmd).Handle(p.handleGlobalEnable)
	// /<globalDisableCmd> <插件名>  ——  全局关闭 weather
	ctx.Reg.RegisterCommand("", "/"+o.globalDisableCmd).Handle(p.handleGlobalDisable)
	// /<userDisableCmd> <userID> <插件名>  ——  禁用用户 u123 weather
	ctx.Reg.RegisterCommand("", "/"+o.userDisableCmd).Handle(p.handleUserDisable)
	// /<userEnableCmd> <userID> <插件名>  ——  启用用户 u123 weather
	ctx.Reg.RegisterCommand("", "/"+o.userEnableCmd).Handle(p.handleUserEnable)
	// /<silenceCmd> [群ID]  ——  沉默 / 沉默 group123
	ctx.Reg.RegisterCommand("", "/"+o.silenceCmd).Handle(p.handleSilence)
	// /<resumeCmd> [群ID]   ——  响应 / 响应 group123
	ctx.Reg.RegisterCommand("", "/"+o.resumeCmd).Handle(p.handleResume)
	// /<banCmd> <userID>    ——  封禁 u123
	ctx.Reg.RegisterCommand("", "/"+o.banCmd).Handle(p.handleBan)
	// /<unbanCmd> <userID>  ——  解封 u123
	ctx.Reg.RegisterCommand("", "/"+o.unbanCmd).Handle(p.handleUnban)
	// /<flipDefaultCmd> <插件名>  ——  反转默认 weather
	ctx.Reg.RegisterCommand("", "/"+o.flipDefaultCmd).Handle(p.handleFlipDefault)
}
