package context

import (
	"errors"
	"slices"
	"sync"
)

// Permission 表示一个权限（资源 + 操作）
type Permission struct {
	Resource string // 资源名称，如 "command:weather", "admin:*"
	Action   string // 操作类型，如 "execute", "view", "manage"
}

// String 返回权限的字符串表示
func (p Permission) String() string {
	return p.Resource + ":" + p.Action
}

// Match 检查权限是否匹配
// 支持通配符 "*" 和前缀通配符（如 "command:*"）
func (p Permission) Match(target Permission) bool {
	// 完全匹配
	if p.Resource == target.Resource && p.Action == target.Action {
		return true
	}

	// 全通配符
	if p.Resource == "*" && p.Action == "*" {
		return true
	}

	// 资源匹配检查（支持前缀通配符）
	resourceMatch := matchWithWildcard(p.Resource, target.Resource)

	// 操作匹配检查（支持通配符）
	actionMatch := p.Action == "*" || target.Action == "*" || p.Action == target.Action

	return resourceMatch && actionMatch
}

// matchWithWildcard 匹配带通配符的字符串
// 支持 "*" 匹配所有，"prefix:*" 匹配前缀
func matchWithWildcard(pattern, value string) bool {
	// 空字符串不应该匹配
	if pattern == "" || value == "" {
		return false
	}

	if pattern == "*" || value == "*" {
		return true
	}

	// 检查前缀通配符，如 "command:*"
	if len(pattern) > 2 && pattern[len(pattern)-2:] == ":*" {
		prefix := pattern[:len(pattern)-2]
		if len(value) >= len(prefix) && value[:len(prefix)] == prefix {
			return true
		}
	}

	// 精确匹配
	return pattern == value
}

// Role 表示一个角色，包含多个权限
type Role struct {
	Name        string
	Permissions []Permission
	mu          sync.RWMutex
}

// NewRole 创建一个新角色
func NewRole(name string, permissions ...Permission) *Role {
	return &Role{
		Name:        name,
		Permissions: permissions,
	}
}

// AddPermission 添加权限到角色
func (r *Role) AddPermission(perm Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Permissions = append(r.Permissions, perm)
}

// RemovePermission 从角色移除权限
func (r *Role) RemovePermission(perm Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()

	filtered := make([]Permission, 0, len(r.Permissions))
	for _, p := range r.Permissions {
		if p.Resource != perm.Resource || p.Action != perm.Action {
			filtered = append(filtered, p)
		}
	}
	r.Permissions = filtered
}

// HasPermission 检查角色是否有指定权限
func (r *Role) HasPermission(perm Permission) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.Permissions {
		if p.Match(perm) {
			return true
		}
	}
	return false
}

// PermissionManager 权限管理器
type PermissionManager struct {
	roles        map[string]*Role        // 角色定义
	userRoles    map[string][]string     // 用户角色映射
	userPerms    map[string][]Permission // 用户自定义权限
	mu           sync.RWMutex
	permProvider PermissionProvider // 权限提供者（可选）
}

// PermissionProvider 权限提供者接口，用于从外部系统获取权限
type PermissionProvider interface {
	GetUserRoles(userID string) ([]string, error)
	GetUserPermissions(userID string) ([]Permission, error)
}

// NewPermissionManager 创建权限管理器
func NewPermissionManager() *PermissionManager {
	pm := &PermissionManager{
		roles:     make(map[string]*Role),
		userRoles: make(map[string][]string),
		userPerms: make(map[string][]Permission),
	}

	// 注册默认角色
	pm.RegisterDefaultRoles()

	return pm
}

// RegisterDefaultRoles 注册默认角色
func (pm *PermissionManager) RegisterDefaultRoles() {
	// 管理员：拥有所有权限
	admin := NewRole("admin", Permission{Resource: "*", Action: "*"})
	pm.RegisterRole(admin)

	// 普通用户：基础命令权限
	user := NewRole("user",
		Permission{Resource: "command:*", Action: "execute"},
		Permission{Resource: "query:*", Action: "view"},
	)
	pm.RegisterRole(user)

	// 访客：仅查询权限
	guest := NewRole("guest",
		Permission{Resource: "query:*", Action: "view"},
	)
	pm.RegisterRole(guest)
}

// RegisterRole 注册角色
func (pm *PermissionManager) RegisterRole(role *Role) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.roles[role.Name] = role
}

// GetRole 获取角色
func (pm *PermissionManager) GetRole(name string) (*Role, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	role, ok := pm.roles[name]
	return role, ok
}

// AssignRole 给用户分配角色
func (pm *PermissionManager) AssignRole(userID string, roleName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// 检查角色是否存在
	if _, ok := pm.roles[roleName]; !ok {
		return errors.New("role not found: " + roleName)
	}

	// 检查是否已分配
	if slices.Contains(pm.userRoles[userID], roleName) {
		return nil // 已存在
	}

	pm.userRoles[userID] = append(pm.userRoles[userID], roleName)
	return nil
}

// RevokeRole 撤销用户角色
func (pm *PermissionManager) RevokeRole(userID string, roleName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	roles := pm.userRoles[userID]
	filtered := make([]string, 0, len(roles))
	for _, r := range roles {
		if r != roleName {
			filtered = append(filtered, r)
		}
	}
	pm.userRoles[userID] = filtered
}

// GrantPermission 直接授予用户权限（不通过角色）
func (pm *PermissionManager) GrantPermission(userID string, perm Permission) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.userPerms[userID] = append(pm.userPerms[userID], perm)
}

// RevokePermission 撤销用户权限
func (pm *PermissionManager) RevokePermission(userID string, perm Permission) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	perms := pm.userPerms[userID]
	filtered := make([]Permission, 0, len(perms))
	for _, p := range perms {
		if p.Resource != perm.Resource || p.Action != perm.Action {
			filtered = append(filtered, p)
		}
	}
	pm.userPerms[userID] = filtered
}

// HasPermission 检查用户是否有指定权限
func (pm *PermissionManager) HasPermission(userID string, perm Permission) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// 1. 检查用户直接权限
	for _, p := range pm.userPerms[userID] {
		if p.Match(perm) {
			return true
		}
	}

	// 2. 检查角色权限
	for _, roleName := range pm.userRoles[userID] {
		if role, ok := pm.roles[roleName]; ok {
			if role.HasPermission(perm) {
				return true
			}
		}
	}

	// 3. 使用权限提供者（如果配置）
	if pm.permProvider != nil {
		if perms, err := pm.permProvider.GetUserPermissions(userID); err == nil {
			for _, p := range perms {
				if p.Match(perm) {
					return true
				}
			}
		}

		if roles, err := pm.permProvider.GetUserRoles(userID); err == nil {
			for _, roleName := range roles {
				if role, ok := pm.roles[roleName]; ok {
					if role.HasPermission(perm) {
						return true
					}
				}
			}
		}
	}

	return false
}

// SetPermissionProvider 设置权限提供者
func (pm *PermissionManager) SetPermissionProvider(provider PermissionProvider) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.permProvider = provider
}

// GetUserRoles 获取用户的所有角色
func (pm *PermissionManager) GetUserRoles(userID string) []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	roles := make([]string, len(pm.userRoles[userID]))
	copy(roles, pm.userRoles[userID])
	return roles
}

// GetUserPermissions 获取用户的所有权限（包括角色权限）
func (pm *PermissionManager) GetUserPermissions(userID string) []Permission {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	permissions := make([]Permission, 0)

	// 用户直接权限
	permissions = append(permissions, pm.userPerms[userID]...)

	// 角色权限
	for _, roleName := range pm.userRoles[userID] {
		if role, ok := pm.roles[roleName]; ok {
			role.mu.RLock()
			permissions = append(permissions, role.Permissions...)
			role.mu.RUnlock()
		}
	}

	return permissions
}

// PermissionManagerExt stores PermissionManager in Context typed extensions.
// This type is meant to be used with ExtSet/ExtGet.
type PermissionManagerExt struct {
	PM *PermissionManager
}

// ErrPermissionDenied 权限拒绝错误
var ErrPermissionDenied = errors.New("permission denied")

// ExportUserRoles 导出所有用户的角色映射（用于持久化）
func (pm *PermissionManager) ExportUserRoles() map[string][]string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	out := make(map[string][]string, len(pm.userRoles))
	for userID, roles := range pm.userRoles {
		rolesCopy := make([]string, len(roles))
		copy(rolesCopy, roles)
		out[userID] = rolesCopy
	}
	return out
}

// ExportUserPerms 导出所有用户的直接权限映射（用于持久化）
func (pm *PermissionManager) ExportUserPerms() map[string][]Permission {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	out := make(map[string][]Permission, len(pm.userPerms))
	for userID, perms := range pm.userPerms {
		permsCopy := make([]Permission, len(perms))
		copy(permsCopy, perms)
		out[userID] = permsCopy
	}
	return out
}

// LoadUserRoles 批量恢复用户角色映射（用于从持久化存储恢复）
// 此方法会替换当前的用户角色映射（不合并）
func (pm *PermissionManager) LoadUserRoles(userRoles map[string][]string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for userID, roles := range userRoles {
		// 过滤掉不存在的角色
		filtered := make([]string, 0, len(roles))
		for _, r := range roles {
			if _, ok := pm.roles[r]; ok {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			pm.userRoles[userID] = filtered
		}
	}
}
