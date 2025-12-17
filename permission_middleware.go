package remilia

import "fmt"

// RequirePermissionMiddleware 创建一个权限检查中间件
// 使用示例:
//
//	engine.Use(RequirePermissionMiddleware(permManager))
func RequirePermissionMiddleware(pm *PermissionManager) HandlerMiddleware {
	return func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			// 将权限管理器注入 Context
			ctx.SetState("permission_manager", pm)
			return next(ctx)
		}
	}
}

// RequirePermission 创建一个要求特定权限的中间件
// 使用示例:
//
//	engine.On(...).Use(RequirePermission("command:admin", "execute")).HandleE(...)
func RequirePermission(resource, action string) HandlerMiddleware {
	return func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			if err := ctx.RequirePermission(resource, action); err != nil {
				return fmt.Errorf("permission check failed for %s:%s: %w", resource, action, err)
			}
			return next(ctx)
		}
	}
}

// RequireRole 创建一个要求特定角色的中间件
// 使用示例:
//
//	engine.On(...).Use(RequireRole("admin")).HandleE(...)
func RequireRole(roleName string) HandlerMiddleware {
	return func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			pm, ok := ctx.GetState("permission_manager")
			if !ok {
				return ErrPermissionDenied
			}

			permManager, ok := pm.(*PermissionManager)
			if !ok {
				return ErrPermissionDenied
			}

			userID := ctx.getUserID()
			if userID == "" {
				return ErrPermissionDenied
			}

			// 检查用户是否有该角色
			userRoles := permManager.GetUserRoles(userID)
			for _, role := range userRoles {
				if role == roleName {
					return next(ctx)
				}
			}

			return fmt.Errorf("role %s required: %w", roleName, ErrPermissionDenied)
		}
	}
}

// RequireAnyPermission 创建一个要求任意权限的中间件（OR 逻辑）
// 使用示例:
//
//	engine.On(...).Use(RequireAnyPermission(
//	    Permission{Resource: "admin:*", Action: "*"},
//	    Permission{Resource: "moderator:*", Action: "*"},
//	)).HandleE(...)
func RequireAnyPermission(perms ...Permission) HandlerMiddleware {
	return func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			pm, ok := ctx.GetState("permission_manager")
			if !ok {
				return ErrPermissionDenied
			}

			permManager, ok := pm.(*PermissionManager)
			if !ok {
				return ErrPermissionDenied
			}

			userID := ctx.getUserID()
			if userID == "" {
				return ErrPermissionDenied
			}

			// 检查是否有任意一个权限
			for _, perm := range perms {
				if permManager.HasPermission(userID, perm) {
					return next(ctx)
				}
			}

			return fmt.Errorf("at least one of the required permissions needed: %w", ErrPermissionDenied)
		}
	}
}

// RequireAllPermissions 创建一个要求所有权限的中间件（AND 逻辑）
// 使用示例:
//
//	engine.On(...).Use(RequireAllPermissions(
//	    Permission{Resource: "data", Action: "read"},
//	    Permission{Resource: "data", Action: "write"},
//	)).HandleE(...)
func RequireAllPermissions(perms ...Permission) HandlerMiddleware {
	return func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			pm, ok := ctx.GetState("permission_manager")
			if !ok {
				return ErrPermissionDenied
			}

			permManager, ok := pm.(*PermissionManager)
			if !ok {
				return ErrPermissionDenied
			}

			userID := ctx.getUserID()
			if userID == "" {
				return ErrPermissionDenied
			}

			// 检查是否有所有权限
			for _, perm := range perms {
				if !permManager.HasPermission(userID, perm) {
					return fmt.Errorf("permission %s:%s required: %w", perm.Resource, perm.Action, ErrPermissionDenied)
				}
			}

			return next(ctx)
		}
	}
}
