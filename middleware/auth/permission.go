package auth

import (
	"fmt"
	"slices"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─── 基于 core/permission 的权限中间件 ────────────────────────────────────────

// RequireRole 要求事件发送者具有指定角色。
//
// 若 Context 中未注入权限管理器（ctx.SetPermissionManager 未调用），
// 则拒绝所有请求（fail-closed）。
//
// 使用示例：
//
//	// 在全局权限管理器注入后
//	engine.OnCommand(dto.GroupMessage, "/admin").
//	    Use(middleware.RequireRole("admin")).
//	    Handle(adminHandler)
func RequireRole(role string) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			pm := ctx.GetPermissionManager()
			if pm == nil {
				logger.Warn("[Permission] No PermissionManager in context; denying (fail-closed)")
				ctx.Reply(platform.TextMessage("❌ 权限系统未初始化"))
				return nil
			}
			userID := ctx.GetSenderInfo().ID
			if slices.Contains(pm.GetUserRoles(userID), role) {
				return next(ctx)
			}
			logger.Debugf("[Permission] User %q missing role %q", userID, role)
			ctx.Reply(platform.TextMessage(fmt.Sprintf("❌ 权限不足，需要角色：%s", role)))
			return nil
		}
	}
}

// RequirePermission 要求事件发送者具有指定的资源+动作权限。
//
// 权限由 core/permission.Manager 管理；
// 若 Context 中未注入权限管理器则拒绝（fail-closed）。
//
// 使用示例：
//
//	engine.OnCommand(dto.GroupMessage, "/ban").
//	    Use(middleware.RequirePermission("admin", "kick")).
//	    Handle(banHandler)
func RequirePermission(resource, action string) eventctx.Middleware {
	perm := permission.Permission{Resource: resource, Action: action}
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			pm := ctx.GetPermissionManager()
			if pm == nil {
				logger.Warn("[Permission] No PermissionManager in context; denying (fail-closed)")
				ctx.Reply(platform.TextMessage("❌ 权限系统未初始化"))
				return nil
			}
			userID := ctx.GetSenderInfo().ID
			if !pm.HasPermission(userID, perm) {
				logger.Debugf("[Permission] User %q denied: missing %s", userID, perm)
				ctx.Reply(platform.TextMessage("❌ 权限不足"))
				return nil
			}
			return next(ctx)
		}
	}
}

// RequireAdmin 要求事件发送者具有 "admin" 角色。
//
// 是 RequireRole("admin") 的简写。
//
// 使用示例：
//
//	engine.OnCommand(dto.GroupMessage, "/reload").
//	    Use(middleware.RequireAdmin()).
//	    Handle(reloadHandler)
func RequireAdmin() eventctx.Middleware {
	return RequireRole("admin")
}

// RequireSuperUser 要求事件发送者的 ID 在 superUserIDs 列表中。
//
// 这是一种 hard-coded 的超级用户检查，不依赖权限管理器，
// 适合用于"紧急关闭"、"全局广播"等不能依赖持久化权限配置的场景。
//
// superUserIDs 应在初始化时从配置文件加载。
//
// 使用示例：
//
//	superUsers := cfg.GetStringSlice("bot.super_users")
//	engine.OnCommand(dto.GroupMessage, "/shutdown").
//	    Use(middleware.RequireSuperUser(superUsers...)).
//	    Handle(shutdownHandler)
func RequireSuperUser(superUserIDs ...string) eventctx.Middleware {
	allowed := make(map[string]struct{}, len(superUserIDs))
	for _, id := range superUserIDs {
		allowed[id] = struct{}{}
	}
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			userID := ctx.GetSenderInfo().ID
			if _, ok := allowed[userID]; !ok {
				logger.Debugf("[Permission] SuperUser check failed for user %q", userID)
				ctx.Reply(platform.TextMessage("❌ 仅超级管理员可使用此命令"))
				return nil
			}
			return next(ctx)
		}
	}
}

// RequireGroup 要求事件来自群组会话（IsGroup == true）。
//
// 用于过滤私聊消息，确保命令仅在群内有效。
//
// 使用示例：
//
//	engine.OnCommand(dto.Message, "/rank").
//	    Use(middleware.RequireGroup()).
//	    Handle(rankHandler)
func RequireGroup() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			if !ctx.GetChatInfo().IsGroup {
				ctx.Reply(platform.TextMessage("⚠️ 此命令仅在群组中有效"))
				return nil
			}
			return next(ctx)
		}
	}
}

// RequirePrivate 要求事件来自私聊会话（IsGroup == false）。
//
// 使用示例：
//
//	engine.OnCommand(dto.Message, "/register").
//	    Use(middleware.RequirePrivate()).
//	    Handle(registerHandler)
func RequirePrivate() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			if ctx.GetChatInfo().IsGroup {
				ctx.Reply(platform.TextMessage("⚠️ 此命令仅在私聊中有效"))
				return nil
			}
			return next(ctx)
		}
	}
}
