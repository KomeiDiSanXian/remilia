package remilia

import (
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

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
		author := ctx.GetAuthor()
		if author == nil {
			return false
		}
		return whitelist[author.UserOpenID]
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
		author := ctx.GetAuthor()
		if author == nil {
			return true // 没有作者信息，放行
		}
		return !blacklist[author.UserOpenID] // 不在黑名单中才放行
	}
}

// OnGroupWhitelist 创建群组白名单规则
// 只有在白名单中的群组才能匹配
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
		var event dto.GroupAtMessageCreateEvent
		if err := ctx.DecodeEvent(&event); err != nil {
			return false
		}
		return whitelist[event.GroupOpenID]
	}
}

// OnGroupBlacklist 创建群组黑名单规则
// 在黑名单中的群组将被拒绝
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
		var event dto.GroupAtMessageCreateEvent
		if err := ctx.DecodeEvent(&event); err != nil {
			return true // 解码失败，放行
		}
		return !blacklist[event.GroupOpenID] // 不在黑名单中才放行
	}
}

// OnHasPermission 创建权限检查规则
// 要求用户必须有指定的权限
//
// 使用示例:
//
//	engine.On(
//	    OnGroupAtMessage(),
//	    OnCommand("/admin"),
//	    OnHasPermission("admin", "manage"),
//	).Handle(handler)
func OnHasPermission(resource, action string) Rule {
	return func(ctx *Context) bool {
		return CheckPermission(ctx, resource, action)
	}
}

// OnHasRole 创建角色检查规则
// 要求用户必须有指定的角色
//
// 使用示例:
//
//	engine.On(
//	    OnGroupAtMessage(),
//	    OnCommand("/admin"),
//	    OnHasRole("admin"),
//	).Handle(handler)
func OnHasRole(roleName string) Rule {
	return func(ctx *Context) bool {
		permManager, ok := ctx.GetPermissionManager()
		if !ok || permManager == nil {
			return false
		}

		userID := GetUserID(ctx)
		if userID == "" {
			return false
		}

		// 检查用户是否有该角色
		userRoles := permManager.GetUserRoles(userID)
		for _, role := range userRoles {
			if role == roleName {
				return true
			}
		}

		return false
	}
}
