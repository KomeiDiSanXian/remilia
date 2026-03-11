package context

import (
	"slices"
)

// groupChatID extracts the group/chat ID in a platform-agnostic way.
// New path: reads from platform.Event.Chat().ID
// Old path (QQ): decodes GroupAtMessageCreateEvent.GroupOpenID
func groupChatID(ctx *Context) string {
	if e := ctx.GetPlatformEvent(); e != nil {
		return e.Chat().ID
	}
	var event struct {
		GroupOpenID string `json:"group_openid"`
	}
	if err := ctx.DecodeEvent(&event); err != nil {
		return ""
	}
	return event.GroupOpenID
}

// OnUserWhitelist 创建用户白名单规则
// 只有在白名单中的用户才能匹配
//
// 使用示例:
//
//	engine.On(
//	    OnGroupAtMessage(),
//	    OnUserWhitelist("user1", "user2", "user3"),
//	).Handle(handler)
func OnUserWhitelist(userIDs ...string) Rule {
	whitelist := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		whitelist[id] = true
	}

	return func(ctx *Context) bool {
		id := ctx.GetSenderInfo().ID
		if id == "" {
			return false
		}
		return whitelist[id]
	}
}

// OnUserBlacklist 创建用户黑名单规则
// 在黑名单中的用户将被拒绝
//
// 使用示例:
//
//	engine.On(
//	    OnGroupAtMessage(),
//	    OnUserBlacklist("banned1", "banned2"),
//	).Handle(handler)
func OnUserBlacklist(userIDs ...string) Rule {
	blacklist := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		blacklist[id] = true
	}

	return func(ctx *Context) bool {
		id := ctx.GetSenderInfo().ID
		if id == "" {
			return true // 没有发送者信息，放行
		}
		return !blacklist[id] // 不在黑名单中才放行
	}
}

// OnGroupWhitelist 创建群组白名单规则（仅对群组消息有效）。
// 只有在白名单中的群组才能匹配。
//
// 非群组消息（解码失败）视为「不适用此规则」，直接放行（返回 true），
// 由其他规则或 EventType 规则负责过滤。
// 若要严格限制仅处理群组消息，请在此规则之前添加 OnGroupAtMessage() 规则。
//
// 使用示例:
//
//	engine.On(
//	    OnGroupAtMessage(),
//	    OnGroupWhitelist("group1", "group2"),
//	).Handle(handler)
func OnGroupWhitelist(groupIDs ...string) Rule {
	whitelist := make(map[string]bool, len(groupIDs))
	for _, id := range groupIDs {
		whitelist[id] = true
	}

	return func(ctx *Context) bool {
		id := groupChatID(ctx)
		if id == "" {
			return true // 非群组消息，不适用此规则，放行
		}
		return whitelist[id]
	}
}

// OnGroupBlacklist 创建群组黑名单规则（仅对群组消息有效）。
// 在黑名单中的群组将被拒绝。
//
// 非群组消息（解码失败）视为「不适用此规则」，直接放行（返回 true），
// 由其他规则或 EventType 规则负责过滤。
// 若要严格限制仅处理群组消息，请在此规则之前添加 OnGroupAtMessage() 规则。
//
// 使用示例:
//
//	engine.On(
//	    OnGroupAtMessage(),
//	    OnGroupBlacklist("spam-group1", "spam-group2"),
//	).Handle(handler)
func OnGroupBlacklist(groupIDs ...string) Rule {
	blacklist := make(map[string]bool, len(groupIDs))
	for _, id := range groupIDs {
		blacklist[id] = true
	}

	return func(ctx *Context) bool {
		id := groupChatID(ctx)
		if id == "" {
			return true // 非群组消息，不适用此规则，放行
		}
		return !blacklist[id]
	}
}

// OnHasPermission 创建权限检查规则
// 检查用户是否拥有指定的资源和操作权限
//
// 使用示例:
//
//	engine.On(
//	    OnGroupAtMessage(),
//	    OnHasPermission("admin", "manage"),
//	).Handle(handler)
func OnHasPermission(resource, action string) Rule {
	perm := Permission{
		Resource: resource,
		Action:   action,
	}

	return func(ctx *Context) bool {
		pm := ctx.GetPermissionManager()
		if pm == nil {
			return false
		}

		userID := ctx.GetUserID()
		if userID == "" {
			return false
		}

		return pm.HasPermission(userID, perm)
	}
}

// OnHasRole 创建角色检查规则
// 检查用户是否拥有指定的角色
//
// 使用示例:
//
//	engine.On(
//	    OnGroupAtMessage(),
//	    OnHasRole("admin"),
//	).Handle(handler)
func OnHasRole(roleName string) Rule {
	return func(ctx *Context) bool {
		pm := ctx.GetPermissionManager()
		if pm == nil {
			return false
		}

		userID := ctx.GetUserID()
		if userID == "" {
			return false
		}

		roles := pm.GetUserRoles(userID)
		return slices.Contains(roles, roleName)
	}
}
