package pluginctrl

// pluginctrl_user.go — 用户级禁用 / 全局封禁 / PluginPolicy / Middleware / RuleFull
//
// 本文件扩展 Plugin，实现以下功能：
//  1. 用户级插件禁用（SetUserEnabled / IsUserEnabled / UserList）
//  2. 全局用户封禁（BanUser / UnbanUser / IsBanned）
//  3. 插件冷却策略注册（RegisterPolicy / GetPolicy）
//  4. Middleware：一步完成"群静默 + 全局封禁 + 群开关 + 用户禁用 + 冷却"全检查
//  5. RuleFull：群+用户双检 Rule（用于引擎层过滤）
//  6. 用户级指令处理（handleUserDisable / handleUserEnable / handleBan / handleUnban）

import (
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─── 用户级开关 API ────────────────────────────────────────────────────────────

// SetUserEnabled 设置指定用户对某插件的访问状态，并持久化。
//
// enabled=false 表示禁止该用户使用该插件（不受群开关状态影响）。
// 此操作仅影响目标用户，不影响其他用户或群配置。
func (p *Plugin) SetUserEnabled(userID, pluginName string, enabled bool) error {
	p.mu.Lock()
	if _, ok := p.userStates[userID]; !ok {
		p.userStates[userID] = make(map[string]bool)
	}
	p.userStates[userID][pluginName] = enabled
	p.mu.Unlock()
	return p.persistUser(userID, pluginName, enabled)
}

// IsUserEnabled 查询指定用户是否被允许使用某插件。
//
// 未设置（无记录）时默认返回 true（允许）。
// 此方法独立于群级开关：即使群开关为开启，用户被禁用后仍返回 false。
func (p *Plugin) IsUserEnabled(userID, pluginName string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if us, ok := p.userStates[userID]; ok {
		if enabled, ok := us[pluginName]; ok {
			return enabled
		}
	}
	return true // 默认允许
}

// UserList 返回指定用户所有已设置的插件状态列表。
func (p *Plugin) UserList(userID string) []UserPluginState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	us, ok := p.userStates[userID]
	if !ok {
		return nil
	}
	result := make([]UserPluginState, 0, len(us))
	for name, enabled := range us {
		result = append(result, UserPluginState{
			UserID:     userID,
			PluginName: name,
			Enabled:    enabled,
		})
	}
	return result
}

// ─── 全局用户封禁 API ─────────────────────────────────────────────────────────

// BanUser 全局封禁指定用户，禁止其使用机器人的任何功能，并持久化。
//
// 全局封禁的优先级高于所有单插件状态检查。
// Middleware 和 RuleFull 会在任何其他检查之前拒绝被封禁的用户。
// 解除封禁请调用 [Plugin.UnbanUser]。
func (p *Plugin) BanUser(userID string) error {
	p.mu.Lock()
	p.bannedUsers[userID] = true
	p.mu.Unlock()
	return p.persistUser(userID, globalBanPlugin, true)
}

// UnbanUser 解除对指定用户的全局封禁，并持久化。
func (p *Plugin) UnbanUser(userID string) error {
	p.mu.Lock()
	delete(p.bannedUsers, userID)
	p.mu.Unlock()
	return p.persistUser(userID, globalBanPlugin, false)
}

// IsBanned 检查指定用户是否被全局封禁。
func (p *Plugin) IsBanned(userID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.bannedUsers[userID]
}

// ─── 持久化（用户级）─────────────────────────────────────────────────────────

func (p *Plugin) persistUser(userID, pluginName string, enabled bool) error {
	if p.storage == nil {
		return nil
	}
	state := &UserPluginState{
		UserID:     userID,
		PluginName: pluginName,
		Enabled:    enabled,
	}
	var existing UserPluginState
	err := p.storage.Where("user_id = ? AND plugin_name = ?", userID, pluginName).First(&existing)
	if err == nil {
		existing.Enabled = enabled
		return p.storage.Save(&existing)
	}
	return p.storage.Create(state)
}

// ─── PluginPolicy ─────────────────────────────────────────────────────────────

// RegisterPolicy 为指定插件名注册冷却策略。
//
// 注册后，[Plugin.Middleware] 会在群开关和用户禁用检查通过后，
// 自动按策略执行 GlobalLimit → GroupLimit → UserLimit 三层冷却检查。
//
// 返回 *Plugin 支持链式调用：
//
//	ctrl.
//	    RegisterPolicy("weather", pluginctrl.PluginPolicy{UserLimit: 10*time.Second, GroupLimit: 2*time.Second}).
//	    RegisterPolicy("news", pluginctrl.PluginPolicy{GlobalLimit: time.Minute})
func (p *Plugin) RegisterPolicy(pluginName string, policy PluginPolicy) *Plugin {
	p.mu.Lock()
	cp := policy // value copy, avoid aliasing
	p.policies[pluginName] = &cp
	p.mu.Unlock()
	return p
}

// GetPolicy 返回已注册的插件冷却策略（若无则返回 nil, false）。
func (p *Plugin) GetPolicy(pluginName string) (*PluginPolicy, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	pol, ok := p.policies[pluginName]
	return pol, ok
}

// ─── RuleFull ─────────────────────────────────────────────────────────────────

// RuleFull 返回一个全链路 Rule，按顺序检查：
//  0. 群静默（silencedGroups）
//  1. 全局用户封禁（bannedUsers）
//  2. 用户级插件禁用（userStates）
//  3. 群级插件开关（groupStates / globalStates）
//
// 与 [Plugin.Rule] 的区别：
//   - Rule：仅检查群级开关和群静默（非群消息直接放行）
//   - RuleFull：额外检查全局用户封禁和用户级禁用
//
// 适合在 engine.On() 中使用，不需要自动回复拒绝消息的场景。
// 若需要自动回复（如"你已被封禁"），请改用 [Plugin.Middleware]。
//
//	ctx.Reg.On(string(platform.EventKindGroupMessage), ctrl.RuleFull("weather")).Handle(h)
func (p *Plugin) RuleFull(pluginName string) eventctx.Rule {
	return func(ctx *eventctx.Context) bool {
		sender := ctx.GetSenderInfo()
		chatInfo := ctx.GetChatInfo()

		// 0. 群静默检查（优先于所有其他检查）
		if chatInfo.IsGroup && p.IsGroupSilenced(chatInfo.ID) {
			return false
		}
		// 1. 全局用户封禁检查
		if sender.ID != "" && p.IsBanned(sender.ID) {
			return false
		}
		// 2. 用户级禁用检查（优先：用户被禁则直接拒绝，不论群状态）
		if sender.ID != "" && !p.IsUserEnabled(sender.ID, pluginName) {
			return false
		}
		// 3. 群级开关检查
		if chatInfo.IsGroup && !p.IsEnabled(chatInfo.ID, pluginName) {
			return false
		}
		return true
	}
}

// ─── Middleware ───────────────────────────────────────────────────────────────

// Middleware 返回一个一体化中间件，按顺序执行：
//
//  0. 群静默检查（群消息）→ 静默时静默放行（不回复）
//  1. 全局用户封禁检查   → 失败时回复 "❌ 你已被封禁，无法使用机器人服务"
//  2. 群级开关检查（群消息）→ 失败时回复 "❌ 该功能在本群已关闭"
//  3. 用户级禁用检查       → 失败时回复 "❌ 你已被禁止使用该功能"
//  4. 冷却策略检查         → 若已通过 [Plugin.RegisterPolicy] 注册了策略，
//     按 GlobalLimit → GroupLimit → UserLimit 顺序检查，失败时回复冷却提示
//
// 这是推荐的使用方式（对标 zbpctrl 的 ctrl.Handler），
// 相比分别使用 Rule + cooldown 中间件，更简洁且保证一致性：
//
//	ctrl.RegisterPolicy("weather", pluginctrl.PluginPolicy{
//	    UserLimit:  10 * time.Second,
//	    GroupLimit: 2 * time.Second,
//	})
//	ctx.Reg.On(string(platform.EventKindGroupMessage)).
//	    Use(ctrl.Middleware("weather")).
//	    Handle(weatherHandler)
func (p *Plugin) Middleware(pluginName string) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			chatInfo := ctx.GetChatInfo()
			sender := ctx.GetSenderInfo()

			// 0. 群静默检查（静默时直接 drop，不回复任何内容）
			if chatInfo.IsGroup && p.IsGroupSilenced(chatInfo.ID) {
				return nil
			}

			// 1. 全局用户封禁检查
			if sender.ID != "" && p.IsBanned(sender.ID) {
				_, _ = ctx.Reply(platform.TextMessage("❌ 你已被封禁，无法使用机器人服务"))
				return nil
			}

			// 2. 群级开关
			if chatInfo.IsGroup && !p.IsEnabled(chatInfo.ID, pluginName) {
				_, _ = ctx.Reply(platform.TextMessage("❌ 该功能在本群已关闭"))
				return nil
			}

			// 3. 用户级禁用
			if sender.ID != "" && !p.IsUserEnabled(sender.ID, pluginName) {
				_, _ = ctx.Reply(platform.TextMessage("❌ 你已被禁止使用该功能"))
				return nil
			}

			// 4. 冷却检查（若有注册策略）
			if pol, ok := p.GetPolicy(pluginName); ok {
				if !p.checkCooldown(ctx, pluginName, pol) {
					return nil // 冷却消息已由 checkCooldown 发送
				}
			}

			return next(ctx)
		}
	}
}

// checkCooldown 按策略顺序检查冷却（GlobalLimit → GroupLimit → UserLimit）。
//
// 返回 true 表示通过（未冷却）；返回 false 表示冷却中（已向用户发送提示消息）。
func (p *Plugin) checkCooldown(ctx *eventctx.Context, pluginName string, pol *PluginPolicy) bool {
	cd := p.cd

	// --- 全局冷却 ---
	if pol.GlobalLimit > 0 {
		if !cd.GlobalAllow(pluginName, pol.GlobalLimit) {
			rem := cd.Remaining("__global__", pluginName, pol.GlobalLimit)
			_, _ = ctx.Reply(platform.TextMessage(
				fmt.Sprintf("⏱ 全局冷却中，请在 %s 后再试", rem.Round(time.Second)),
			))
			return false
		}
	}

	// --- 群级冷却 ---
	if pol.GroupLimit > 0 {
		chatInfo := ctx.GetChatInfo()
		if chatInfo.IsGroup && chatInfo.ID != "" {
			if !cd.GroupAllow(chatInfo.ID, pluginName, pol.GroupLimit) {
				rem := cd.GroupRemaining(chatInfo.ID, pluginName, pol.GroupLimit)
				_, _ = ctx.Reply(platform.TextMessage(
					fmt.Sprintf("⏱ 该群冷却中，请在 %s 后再试", rem.Round(time.Second)),
				))
				return false
			}
		}
	}

	// --- 用户级冷却 ---
	if pol.UserLimit > 0 {
		sender := ctx.GetSenderInfo()
		if sender.ID != "" {
			if !cd.Allow(sender.ID, pluginName, pol.UserLimit) {
				rem := cd.Remaining(sender.ID, pluginName, pol.UserLimit)
				logger.Debugf("[pluginctrl] User %s cooldown for %s: %s remaining", sender.ID, pluginName, rem)
				_, _ = ctx.Reply(platform.TextMessage(
					fmt.Sprintf("⏱ 操作太频繁，请在 %s 后再试", rem.Round(time.Second)),
				))
				return false
			}
		}
	}

	return true
}

// ─── 用户级指令处理 ────────────────────────────────────────────────────────────

func (p *Plugin) handleUserDisable(ctx *eventctx.Context) error {
	return p.handleUserToggle(ctx, false)
}

func (p *Plugin) handleUserEnable(ctx *eventctx.Context) error {
	return p.handleUserToggle(ctx, true)
}

func (p *Plugin) handleUserToggle(ctx *eventctx.Context, enable bool) error {
	if !p.IsSuperUser(ctx.GetSenderInfo().ID) {
		_, _ = ctx.Reply(platform.TextMessage("❌ 权限不足，需要超级管理员权限"))
		return nil
	}

	verb := p.opts.userDisableCmd
	if enable {
		verb = p.opts.userEnableCmd
	}
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil || len(args.Positional) < 2 {
		_, _ = ctx.Reply(platform.TextMessage(
			fmt.Sprintf("❌ 用法：/%s <用户ID> <插件名>", verb),
		))
		return nil
	}

	targetUserID := args.Positional[0]
	pluginName := args.Positional[1]

	if err := p.SetUserEnabled(targetUserID, pluginName, enable); err != nil {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
		return nil
	}

	action := "禁用"
	if enable {
		action = "启用"
	}
	_, _ = ctx.Reply(platform.TextMessage(
		fmt.Sprintf("✅ 已%s用户 %s 对插件「%s」的访问", action, targetUserID, pluginName),
	))
	return nil
}

// ─── 全局封禁指令处理 ─────────────────────────────────────────────────────────

// handleBan 处理"封禁 <userID>"指令：全局封禁指定用户。
func (p *Plugin) handleBan(ctx *eventctx.Context) error {
	if !p.IsSuperUser(ctx.GetSenderInfo().ID) {
		_, _ = ctx.Reply(platform.TextMessage("❌ 权限不足，需要超级管理员权限"))
		return nil
	}
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil || len(args.Positional) == 0 {
		_, _ = ctx.Reply(platform.TextMessage(
			fmt.Sprintf("❌ 用法：/%s <用户ID>", p.opts.banCmd),
		))
		return nil
	}
	targetUserID := args.Positional[0]
	if err := p.BanUser(targetUserID); err != nil {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
		return nil
	}
	_, _ = ctx.Reply(platform.TextMessage(
		fmt.Sprintf("✅ 已全局封禁用户 %s，该用户无法使用机器人的任何功能", targetUserID),
	))
	return nil
}

// handleUnban 处理"解封 <userID>"指令：解除对指定用户的全局封禁。
func (p *Plugin) handleUnban(ctx *eventctx.Context) error {
	if !p.IsSuperUser(ctx.GetSenderInfo().ID) {
		_, _ = ctx.Reply(platform.TextMessage("❌ 权限不足，需要超级管理员权限"))
		return nil
	}
	args, err := command.ParseCommandLine(ctx.GetMessageContent())
	if err != nil || len(args.Positional) == 0 {
		_, _ = ctx.Reply(platform.TextMessage(
			fmt.Sprintf("❌ 用法：/%s <用户ID>", p.opts.unbanCmd),
		))
		return nil
	}
	targetUserID := args.Positional[0]
	if err := p.UnbanUser(targetUserID); err != nil {
		_, _ = ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 操作失败：%v", err)))
		return nil
	}
	_, _ = ctx.Reply(platform.TextMessage(
		fmt.Sprintf("✅ 已解除用户 %s 的全局封禁", targetUserID),
	))
	return nil
}
