// Package ai usage.go — 管理员按权限查询指定用户的使用状态。
//
// /ai status、/ai stats 原本只能读取调用者自己的会话（会话按
// "{platform}:{chatID}:{userID}" 严格隔离），管理员无法了解某个用户的用量。
// 本文件补齐"带目标用户"的查询入口，权限分两档：
//
//   - 群管理员（平台群主/管理员）：仅限本群成员
//   - RBAC admin / superadmin：任意会话（跨群）
//   - 持有 ai.usage.view 权限者：可委托
//
// 目标用户解析按序取第一个可用来源：结构化 @ 列表 → 显式用户 ID。
// 解析不出目标时返回 handled=false，调用方回落原有"查询自己"的逻辑，
// 保证既有行为逐字不变；也避免把自然语言聊天（如"@bot status 是什么"）
// 误判成查询命令。
//
// 隐私边界：只输出会话级计数与时间（消息数/调用次数/时长/记忆条数），
// 全程不读取对话正文，也不返回工具调用追踪。
package ai

import (
	"fmt"
	"slices"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// aiUsageViewPermission 委托"查询他人使用状态"能力所需的 RBAC 权限。
// 与 ai.message.send 同构：resource.action，可授予非 admin 的运维/客服角色。
const aiUsageViewPermission = "ai.usage.view"

// resolveUsageTarget 解析"使用状态查询"的目标用户。
//
// 解析顺序：
//  1. 事件的结构化 @ 列表（跳过机器人自身与空 ID）
//  2. 子命令之后正文里的首个 token；允许带前导 @（如 "@张三"）
//
// ok=false 表示无法确定目标，调用方应回落原有自身查询逻辑。
// name 仅在来自 @ 列表时非空（平台提供了显示名）。
func (p *Plugin) resolveUsageTarget(ctx *eventctx.Context, rest string) (id, name string, ok bool) {
	for _, u := range platform.GetMentions(ctx.GetPlatformEvent()) {
		if u.IsSelf || u.ID == "" {
			continue
		}
		return u.ID, u.DisplayName, true
	}

	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", false
	}
	token := strings.TrimSpace(strings.TrimLeft(fields[0], "@"))
	if !looksLikeUserID(token) {
		return "", "", false
	}
	return token, "", true
}

// looksLikeUserID 粗略判断 token 是否像平台用户 ID。
//
// 目的是把"显式 ID"与自然语言正文区分开：命令解析层对多余位置参数一律
// 静默接受（parseArguments 只遍历已声明的参数），若不设门槛，
// "@bot status 是什么" 这类正常聊天会被误当成查询他人。
//
// 平台 ID 实测形态：QQ/Telegram 为纯数字，Discord 为雪花号（数字），
// OneBot/Milky 为带分隔符的 openid，均至少含一个数字。因此这里要求：
// 只允许 ASCII 字母数字与 _-. :#@ 分隔符，且必须含至少一个数字。
//
// 纯字母 token（"update"、"detailed"）一律按自然语言处理——各平台真实的
// 用户 ID 都含数字，收紧到"必须含数字"可以彻底消除英文误判。个别平台若
// 使用纯字母 ID，仍可通过结构化 @ 列表定位（那是首要交互方式）。
func looksLikeUserID(token string) bool {
	if len(token) < 4 {
		return false
	}
	digits := 0
	for _, r := range token {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '_', r == '-', r == '.', r == ':', r == '#', r == '@':
		default:
			// CJK、空白或其他符号：按自然语言处理。
			return false
		}
	}
	return digits > 0
}

// rolesOf 返回用户的 RBAC 角色。
//
// 优先使用事件上下文里的权限管理器（由 Bot 在事件入口注入，生产路径），
// 未注入时回退到插件的权限服务（测试或权限插件单独接线的场景）。
// 两者都不可用时返回 nil（安全默认：查不到任何角色）。
func (p *Plugin) rolesOf(ctx *eventctx.Context, userID string) []string {
	if pm := ctx.GetPermissionManager(); pm != nil {
		return pm.GetUserRoles(userID)
	}
	if p.perms != nil {
		return p.perms.GetUserRoles(userID)
	}
	return nil
}

// havePermissionSource 返回是否存在可用的 RBAC 权限来源。
func (p *Plugin) havePermissionSource(ctx *eventctx.Context) bool {
	return ctx.GetPermissionManager() != nil || p.perms != nil
}

// hasAdminRole 判断 userID 是否持有 RBAC admin / superadmin 角色。
// 与 [Plugin.isAdmin] 的区别：角色来源经 [Plugin.rolesOf] 解析，因此在
// 权限插件单独接线（未注入事件上下文）的场景下同样可用。
func (p *Plugin) hasAdminRole(ctx *eventctx.Context, userID string) bool {
	for _, r := range p.rolesOf(ctx, userID) {
		if r == "admin" || r == "superadmin" {
			return true
		}
	}
	return false
}

// hasSuperAdminRole 判断 userID 是否持有 superadmin 角色。
func (p *Plugin) hasSuperAdminRole(ctx *eventctx.Context, userID string) bool {
	return slices.Contains(p.rolesOf(ctx, userID), "superadmin")
}

// usageSubCommandAliases 返回可与"目标用户"组合使用的 status/stats 别名。
func usageSubCommandAliases(sub string) []string {
	switch sub {
	case "status":
		return []string{"status", "状态"}
	case "stats":
		return []string{"stats", "统计"}
	}
	return nil
}

// matchUsageSubCommand 判断清洗后的正文是否以 status/stats 子命令开头
// （整词前缀："status"、"status 12345"、"统计 @张三"）。
//
// 用于 @机器人 自然语言路径——该路径的子命令分派是整串精确匹配，带参数的
// "status 12345" 原本会落入 AI 对话。整词匹配（要求后跟空格）避免误命中
// "statuses" 这类普通正文。返回规范子命令名，不匹配返回 ""。
func matchUsageSubCommand(cmd string) string {
	for _, sub := range []string{"status", "stats"} {
		for _, alias := range usageSubCommandAliases(sub) {
			if cmd == alias || strings.HasPrefix(cmd, alias+" ") {
				return sub
			}
		}
	}
	return ""
}

// usageRest 返回"查询他人使用状态"命令中的目标参数原文。
//
// 优先取命令解析结果里已声明的 target 位置参数——/ai status 12345 这类
// 命令路径（GetParsedCommand 非空）由此确定，不依赖触发前缀的清洗行为；
// @机器人 自然语言路径没有 Parsed，退回按子命令前缀剥离原文。
func (p *Plugin) usageRest(ctx *eventctx.Context, sub string) string {
	if parsed := ctx.GetParsedCommand(); parsed != nil {
		if v := strings.TrimSpace(parsed.GetString("target")); v != "" {
			return v
		}
	}
	return p.subCommandRest(ctx, usageSubCommandAliases(sub)...)
}

// authorizeUsageQuery 判断调用者是否有权查询 targetID 的使用状态。
//
// 授权矩阵（调用者视角）：
//
//	平台群角色 >= 群管理员   群聊内允许（仅限本群）；私聊无依据 → 拒绝
//	RBAC admin / superadmin  允许（任意会话，即跨群）
//	持有 ai.usage.view       允许
//	普通成员                 拒绝
//
// 附加保护：目标用户若持有 superadmin 角色，除 superadmin 本人外一律拒绝，
// 与 /perm 的 checkTargetNotSuperadmin 分级保持一致。
//
// fail-closed：既无 RBAC 权限来源、又无平台群管理员身份时拒绝。
// 返回 ok=false 时 reason 为可直接回复给用户的原因文本。
func (p *Plugin) authorizeUsageQuery(ctx *eventctx.Context, targetID string) (bool, string) {
	chat := ctx.GetChatInfo()
	sender := ctx.GetSenderInfo()
	groupAdmin := chat.IsGroup && sender.GroupRole >= platform.GroupRoleAdmin

	if !p.havePermissionSource(ctx) && !groupAdmin {
		return false, "❌ 权限系统未初始化，无法查询他人使用状态"
	}

	// 目标保护：非 superadmin 不得窥探 superadmin 的用量。
	if !p.hasSuperAdminRole(ctx, sender.ID) && p.hasSuperAdminRole(ctx, targetID) {
		return false, "❌ 只有超级管理员才能查询超级管理员的使用状态"
	}

	if p.hasAdminRole(ctx, sender.ID) { // admin / superadmin
		return true, ""
	}
	if p.hasToolPermission(ctx, []string{aiUsageViewPermission}) {
		return true, ""
	}
	if groupAdmin {
		return true, ""
	}
	if chat.IsGroup {
		return false, "❌ 需要群管理员（群主/管理员）或管理员权限才能查询本群成员的使用状态"
	}
	return false, "❌ 只有管理员才能查询他人使用状态"
}

// usageSessionID 返回目标用户在当前会话中的会话 ID。
//
// 群聊：目标在本群的会话（{platform}:{群ID}:{目标}）。
// 私聊：框架语义中 ChatInfo.ID 私聊即用户 ID，故目标与机器人的私聊会话为
// {platform}:{目标}:{目标}。
func usageSessionID(platformName string, chat platform.ChatInfo, targetID string) string {
	if chat.IsGroup {
		return makeSessionID(platformName, chat.ID, targetID)
	}
	return makeSessionID(platformName, targetID, targetID)
}

// formatUsageTime 按本地时区格式化时间戳（零值显示为 "-"）。
func formatUsageTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

// usageSummaryText 渲染目标用户在当前会话中的使用状态摘要。
//
// 只读：经 [SessionManager.PeekOrLoad] 读取，不创建会话、不写库、不触碰 LRU。
// 只输出会话级计数与时间，不读取任何对话正文。
func (p *Plugin) usageSummaryText(ctx *eventctx.Context, targetID, targetName string) string {
	chat := ctx.GetChatInfo()

	var b strings.Builder
	if chat.IsGroup {
		fmt.Fprintf(&b, "📊 **用户使用状态**（本群 `%s`）\n\n", chat.ID)
	} else {
		b.WriteString("📊 **用户使用状态**（私聊）\n\n")
	}
	if targetName != "" {
		fmt.Fprintf(&b, "  - 用户：%s（`%s`）\n", targetName, targetID)
	} else {
		fmt.Fprintf(&b, "  - 用户：`%s`\n", targetID)
	}

	session := p.peekUsageSession(ctx, targetID)
	if session == nil {
		b.WriteString("  - 会话记录：无（该用户在此会话还没有与 AI 交互过）\n")
	} else {
		session.Lock()
		msgCount := len(session.Messages)
		sysCount := 0
		for _, m := range session.Messages {
			if m.Role == RoleSystem {
				sysCount++
			}
		}
		callCount := session.CallCount
		toolCount := session.ToolCount
		createdAt := session.CreatedAt
		updatedAt := session.UpdatedAt
		session.Unlock()

		b.WriteString("  - 会话记录：有\n")
		fmt.Fprintf(&b, "  - 消息数：`%d`（含 %d 条系统提示）\n", msgCount, sysCount)
		fmt.Fprintf(&b, "  - LLM 调用次数：`%d`\n", callCount)
		fmt.Fprintf(&b, "  - 工具调用次数：`%d`\n", toolCount)
		fmt.Fprintf(&b, "  - 会话创建：`%s`，最后活跃：`%s`（距今 `%s`）\n",
			formatUsageTime(createdAt), formatUsageTime(updatedAt),
			formatDuration(time.Since(updatedAt)))
	}

	if p.memory != nil {
		fmt.Fprintf(&b, "  - 长期记忆：`%d` 条\n", len(p.memory.Facts(userScope(targetID))))
	}

	b.WriteString("\n（仅用量摘要，不含对话正文）\n")
	return b.String()
}

// peekUsageSession 只读读取目标用户在当前会话的会话对象；不存在时返回 nil。
// session 管理器未初始化（测试场景）时同样返回 nil，不 panic。
func (p *Plugin) peekUsageSession(ctx *eventctx.Context, targetID string) *Session {
	if p.sm == nil {
		return nil
	}
	return p.sm.PeekOrLoad(usageSessionID(ctx.GetEventPlatform(), ctx.GetChatInfo(), targetID))
}

// handleUsageQuery 处理"查询指定用户使用状态"。
//
// 返回 handled=false 表示本条消息不属于该场景（未给出可识别的目标，或目标
// 就是调用者自己），调用方应回落原有的"查询自己"逻辑，使既有行为逐字不变。
func (p *Plugin) handleUsageQuery(ctx *eventctx.Context, rest string) bool {
	targetID, targetName, ok := p.resolveUsageTarget(ctx, rest)
	if !ok {
		return false
	}
	if targetID == ctx.GetSenderInfo().ID {
		return false // 查询自己：不施加额外权限，走原路径
	}

	allowed, reason := p.authorizeUsageQuery(ctx, targetID)
	if !allowed {
		p.replyFormatted(ctx, reason)
		return true
	}
	p.replyFormatted(ctx, p.usageSummaryText(ctx, targetID, targetName))
	return true
}
