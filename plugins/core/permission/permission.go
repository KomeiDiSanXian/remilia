package permission

import (
	"fmt"
	"slices"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Plugin 权限系统插件 API
type Plugin struct {
	manager         *eventctx.PermissionManager
	verificationMgr *VerificationManager
	acl             *AccessControlList
	cleanupStopChan chan struct{}
}

// New 创建权限插件（v2 API）
func New() *plugin.PluginDescriptor {
	// 创建核心组件（闭包捕获）
	permManager := eventctx.NewPermissionManager()
	verificationMgr := NewVerificationManager()
	acl := NewAccessControlList()
	cleanupStopChan := make(chan struct{})

	// 初始化预定义角色
	initExtraRoles(permManager)

	// 创建 Plugin API 包装器
	pluginAPI := &Plugin{
		manager:         permManager,
		verificationMgr: verificationMgr,
		acl:             acl,
		cleanupStopChan: cleanupStopChan,
	}

	return &plugin.PluginDescriptor{
		Name:        "permission",
		Version:     "3.0.0",
		Author:      "Remilia Team",
		Description: "基于角色的访问控制（RBAC）权限系统",
		Category:    "核心",
		Tags:        []string{"权限", "安全", "RBAC", "核心"},
		Deps:        []string{},
		HelpText: `权限系统使用说明：

基于角色的访问控制（RBAC）：
- 支持 Resource:Action 格式的权限
- 支持角色继承和权限组合
- 提供权限检查中间件
- 支持通配符匹配

预定义角色：
- admin - 管理员（所有权限）
- user  - 普通用户（命令执行权限）
- guest - 访客（仅查询权限）
- moderator - 版主（部分管理权限）

API 使用 (v2):
  perm := ctx.MustGet("permission").(*permission.Plugin)
  perm.HasPermission(userID, resource, action) - 检查权限
  perm.Grant(userID, resource, action) - 授予权限
  perm.Revoke(userID, resource, action) - 撤销权限
  perm.AssignRole(userID, role) - 分配角色`,

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[PermissionPlugin] Loading permission plugin (v2)...")

			// 获取所有角色
			roles := []string{"admin", "user", "guest", "moderator"}
			logger.Infof("[PermissionPlugin] Loaded %d default roles", len(roles))

			// 启动验证码清理协程
			go cleanupExpiredCodesRoutine(verificationMgr, cleanupStopChan)
			logger.Info("[PermissionPlugin] Started verification code cleanup routine")

			// 注册 API 包装器到容器
			ctx.Manager.GetContainer().Register("permission_api", pluginAPI)

			logger.Info("[PermissionPlugin] Permission plugin loaded successfully")
			return nil
		},

		Teardown: func() error {
			logger.Info("[PermissionPlugin] Unloading permission plugin...")

			// 停止清理协程
			close(cleanupStopChan)

			logger.Info("[PermissionPlugin] Permission plugin unloaded")
			return nil
		},
	}
}

// initExtraRoles 初始化额外的预定义角色（保持向后兼容）
func initExtraRoles(permManager *eventctx.PermissionManager) {
	// 重新定义 user 角色以匹配旧的权限格式
	user := eventctx.NewRole("user",
		eventctx.Permission{Resource: "command", Action: "use"},
		eventctx.Permission{Resource: "message", Action: "send"},
	)
	permManager.RegisterRole(user)

	// Moderator 角色 - 部分管理权限
	moderator := eventctx.NewRole("moderator",
		eventctx.Permission{Resource: "message", Action: "delete"},
		eventctx.Permission{Resource: "message", Action: "pin"},
		eventctx.Permission{Resource: "user", Action: "mute"},
		eventctx.Permission{Resource: "user", Action: "kick"},
		eventctx.Permission{Resource: "command", Action: "use"},
	)
	permManager.RegisterRole(moderator)
}

// cleanupExpiredCodesRoutine 定期清理过期的验证码（独立函数）
func cleanupExpiredCodesRoutine(verificationMgr *VerificationManager, stopChan chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			count := verificationMgr.CleanupExpired()
			if count > 0 {
				logger.Infof("[PermissionPlugin] Cleaned up %d expired verification codes", count)
			}
		case <-stopChan:
			logger.Info("[PermissionPlugin] Verification code cleanup routine stopped")
			return
		}
	}
}

// Load 加载插件（v1 API）
func (p *Plugin) Load(eng *engine.Engine) error {
	logger.Info("[PermissionPlugin] Loading permission plugin...")

	// 获取所有角色
	roles := []string{"admin", "user", "guest", "moderator"}
	logger.Infof("[PermissionPlugin] Loaded %d default roles", len(roles))

	// 启动验证码清理协程
	go p.cleanupExpiredCodes()
	logger.Info("[PermissionPlugin] Started verification code cleanup routine")

	return nil
}

// Unload 卸载插件（v1 API）
func (p *Plugin) Unload(eng *engine.Engine) error {
	logger.Info("[PermissionPlugin] Unloading permission plugin...")

	// 停止清理协程
	close(p.cleanupStopChan)

	return nil
}

// cleanupExpiredCodes 定期清理过期的验证码（v1 方法）
func (p *Plugin) cleanupExpiredCodes() {
	cleanupExpiredCodesRoutine(p.verificationMgr, p.cleanupStopChan)
}

// GetManager 获取底层的 PermissionManager
func (p *Plugin) GetManager() *eventctx.PermissionManager {
	return p.manager
}

// HasPermission 检查用户是否拥有指定权限
// 兼容旧 API：将字符串权限转换为 Resource:Action 格式
func (p *Plugin) HasPermission(userID, permission string) bool {
	// 解析权限字符串
	resource, action := parsePermission(permission)
	perm := eventctx.Permission{Resource: resource, Action: action}
	return p.manager.HasPermission(userID, perm)
}

// HasPermissionEx 检查用户是否拥有指定权限（新 API，使用 Resource:Action 格式）
func (p *Plugin) HasPermissionEx(userID, resource, action string) bool {
	perm := eventctx.Permission{Resource: resource, Action: action}
	return p.manager.HasPermission(userID, perm)
}

// Grant 授予用户权限（兼容旧 API）
func (p *Plugin) Grant(userID, permission string) error {
	resource, action := parsePermission(permission)
	perm := eventctx.Permission{Resource: resource, Action: action}
	p.manager.GrantPermission(userID, perm)
	logger.Infof("[PermissionPlugin] Granted permission '%s' to user '%s'", permission, userID)
	return nil
}

// GrantEx 授予用户权限（新 API）
func (p *Plugin) GrantEx(userID, resource, action string) error {
	perm := eventctx.Permission{Resource: resource, Action: action}
	p.manager.GrantPermission(userID, perm)
	logger.Infof("[PermissionPlugin] Granted permission '%s:%s' to user '%s'", resource, action, userID)
	return nil
}

// Revoke 撤销用户权限（兼容旧 API）
func (p *Plugin) Revoke(userID, permission string) error {
	resource, action := parsePermission(permission)
	perm := eventctx.Permission{Resource: resource, Action: action}
	p.manager.RevokePermission(userID, perm)
	logger.Infof("[PermissionPlugin] Revoked permission '%s' from user '%s'", permission, userID)
	return nil
}

// RevokeEx 撤销用户权限（新 API）
func (p *Plugin) RevokeEx(userID, resource, action string) error {
	perm := eventctx.Permission{Resource: resource, Action: action}
	p.manager.RevokePermission(userID, perm)
	logger.Infof("[PermissionPlugin] Revoked permission '%s:%s' from user '%s'", resource, action, userID)
	return nil
}

// parsePermission 解析权限字符串为 resource 和 action
// 支持格式: "resource:action", "resource.action", "resource"
func parsePermission(permission string) (resource, action string) {
	// 尝试 ":" 分隔符
	if idx := strings.Index(permission, ":"); idx > 0 {
		return permission[:idx], permission[idx+1:]
	}

	// 尝试 "." 分隔符（向后兼容）
	if idx := strings.LastIndex(permission, "."); idx > 0 {
		return permission[:idx], permission[idx+1:]
	}

	// 通配符
	if permission == "*" {
		return "*", "*"
	}

	// 默认：将整个字符串作为 resource，action 为 "*"
	return permission, "*"
}

// AssignRole 分配角色给用户
func (p *Plugin) AssignRole(userID, roleName string) error {
	err := p.manager.AssignRole(userID, roleName)
	if err != nil {
		return err
	}
	logger.Infof("[PermissionPlugin] Assigned role '%s' to user '%s'", roleName, userID)
	return nil
}

// RemoveRole 移除用户的角色
func (p *Plugin) RemoveRole(userID, roleName string) error {
	p.manager.RevokeRole(userID, roleName)
	logger.Infof("[PermissionPlugin] Removed role '%s' from user '%s'", roleName, userID)
	return nil
}

// GetUserPermissions 获取用户的所有权限
func (p *Plugin) GetUserPermissions(userID string) []string {
	perms := p.manager.GetUserPermissions(userID)

	// 转换为字符串列表
	result := make([]string, len(perms))
	for i, perm := range perms {
		result[i] = perm.String()
	}

	return result
}

// GetUserRoles 获取用户的所有角色
func (p *Plugin) GetUserRoles(userID string) []string {
	return p.manager.GetUserRoles(userID)
}

// DefineRole 定义新角色（兼容旧 API）
func (p *Plugin) DefineRole(roleName string, permissions []string) error {
	// 转换字符串权限为 Permission 对象
	perms := make([]eventctx.Permission, len(permissions))
	for i, perm := range permissions {
		resource, action := parsePermission(perm)
		perms[i] = eventctx.Permission{Resource: resource, Action: action}
	}

	role := eventctx.NewRole(roleName, perms...)
	p.manager.RegisterRole(role)

	logger.Infof("[PermissionPlugin] Defined role '%s' with %d permissions", roleName, len(permissions))
	return nil
}

// DefineRoleEx 定义新角色（新 API）
func (p *Plugin) DefineRoleEx(roleName string, permissions []eventctx.Permission) error {
	role := eventctx.NewRole(roleName, permissions...)
	p.manager.RegisterRole(role)
	logger.Infof("[PermissionPlugin] Defined role '%s' with %d permissions", roleName, len(permissions))
	return nil
}

// GetRole 获取角色的权限列表
func (p *Plugin) GetRole(roleName string) ([]string, error) {
	role, ok := p.manager.GetRole(roleName)
	if !ok {
		return nil, fmt.Errorf("role not found: %s", roleName)
	}

	// 转换为字符串列表
	perms := role.Permissions
	result := make([]string, len(perms))
	for i, perm := range perms {
		result[i] = perm.String()
	}

	return result, nil
}

// ListRoles 列出所有角色
func (p *Plugin) ListRoles() []string {
	// PermissionManager 没有直接的 ListRoles 方法
	// 返回已知的角色列表
	return []string{"admin", "user", "guest", "moderator"}
}

// RequirePermission 创建权限检查中间件（兼容旧 API）
func (p *Plugin) RequirePermission(permission string) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			userID := ctx.GetUserID()

			if !p.HasPermission(userID, permission) {
				logger.Warnf("[PermissionPlugin] User '%s' lacks permission '%s'", userID, permission)
				return fmt.Errorf("permission denied: %s", permission)
			}

			return next(ctx)
		}
	}
}

// RequirePermissionEx 创建权限检查中间件（新 API）
func (p *Plugin) RequirePermissionEx(resource, action string) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			userID := ctx.GetUserID()
			perm := eventctx.Permission{Resource: resource, Action: action}

			if !p.manager.HasPermission(userID, perm) {
				logger.Warnf("[PermissionPlugin] User '%s' lacks permission '%s:%s'", userID, resource, action)
				return eventctx.ErrPermissionDenied
			}

			return next(ctx)
		}
	}
}

// RequireRole 创建角色检查中间件
func (p *Plugin) RequireRole(roleName string) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			userID := ctx.GetUserID()
			roles := p.manager.GetUserRoles(userID)

			if slices.Contains(roles, roleName) {
				return next(ctx)
			}

			logger.Warnf("[PermissionPlugin] User '%s' lacks role '%s'", userID, roleName)
			return fmt.Errorf("role required: %s", roleName)
		}
	}
}

// === 验证码相关方法 ===

// GenerateVerificationCode 生成验证码
// role: 验证码授予的角色（如 "admin"）
// expiry: 过期时间（如 30*time.Minute）
// maxUses: 最大使用次数（0=一次性，-1=无限次）
func (p *Plugin) GenerateVerificationCode(role string, expiry time.Duration, maxUses int) (string, error) {
	return p.verificationMgr.GenerateCode(role, expiry, maxUses)
}

// VerifyAndGrantRole 验证码验证并授予角色
func (p *Plugin) VerifyAndGrantRole(code, userID string) (string, error) {
	role, success, err := p.verificationMgr.VerifyCode(code, userID)
	if err != nil {
		return "", err
	}

	if !success {
		return "", fmt.Errorf("verification failed")
	}

	// 授予角色
	if err := p.AssignRole(userID, role); err != nil {
		return "", fmt.Errorf("failed to assign role: %w", err)
	}

	logger.Infof("[PermissionPlugin] User '%s' verified with code and granted role '%s'", userID, role)
	return role, nil
}

// RevokeVerificationCode 撤销验证码
func (p *Plugin) RevokeVerificationCode(code string) error {
	return p.verificationMgr.RevokeCode(code)
}

// ListVerificationCodes 列出所有有效的验证码
func (p *Plugin) ListVerificationCodes() []*VerificationCode {
	return p.verificationMgr.ListCodes()
}

// GetVerificationCodeInfo 获取验证码信息
func (p *Plugin) GetVerificationCodeInfo(code string) (*VerificationCode, error) {
	return p.verificationMgr.GetCodeInfo(code)
}

// === 黑白名单相关方法 ===

// SetACLMode 设置黑白名单模式
func (p *Plugin) SetACLMode(mode ListMode) {
	p.acl.SetMode(mode)
	logger.Infof("[PermissionPlugin] ACL mode set to: %s", mode.String())
}

// GetACLMode 获取当前黑白名单模式
func (p *Plugin) GetACLMode() ListMode {
	return p.acl.GetMode()
}

// AddToACL 添加用户到黑白名单
func (p *Plugin) AddToACL(userID string, note string) {
	p.acl.Add(userID, note)
	mode := p.acl.GetMode()
	logger.Infof("[PermissionPlugin] Added user '%s' to %s", userID, mode.String())
}

// RemoveFromACL 从黑白名单移除用户
func (p *Plugin) RemoveFromACL(userID string) bool {
	removed := p.acl.Remove(userID)
	if removed {
		logger.Infof("[PermissionPlugin] Removed user '%s' from ACL", userID)
	}
	return removed
}

// IsUserAllowed 检查用户是否被允许访问
func (p *Plugin) IsUserAllowed(userID string) (bool, string) {
	return p.acl.IsAllowed(userID)
}

// ListACL 列出黑白名单中的所有用户
func (p *Plugin) ListACL() []UserInfo {
	return p.acl.List()
}

// GetACLCount 获取黑白名单中的用户数量
func (p *Plugin) GetACLCount() int {
	return p.acl.Count()
}

// ClearACL 清空黑白名单
func (p *Plugin) ClearACL() int {
	count := p.acl.Clear()
	logger.Infof("[PermissionPlugin] Cleared ACL (%d users removed)", count)
	return count
}

// GetACLStats 获取黑白名单统计信息
func (p *Plugin) GetACLStats() ACLStats {
	return p.acl.Stats()
}

// RequireACL 创建黑白名单检查中间件
func (p *Plugin) RequireACL() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			userID := ctx.GetUserID()

			allowed, reason := p.acl.IsAllowed(userID)
			if !allowed {
				logger.Warnf("[PermissionPlugin] User '%s' denied by ACL: %s", userID, reason)
				return fmt.Errorf("访问被拒绝: %s", reason)
			}

			return next(ctx)
		}
	}
}
