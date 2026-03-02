// Package permission provides the role-based access control (RBAC) system
// for the Remilia framework.
//
// Core types:
//   - Permission — a (resource, action) pair, supports wildcards
//   - Role       — a named collection of Permissions
//   - Manager — maps users ↔ roles ↔ direct permissions
//   - Provider — optional external source for roles/permissions
//
// This package has no dependency on core/context; it can be used standalone
// (e.g. in HTTP services, CLI tools) without pulling in the full event context.
//
// The core/context package stores a *Manager in the typed-extension
// store via PermissionManagerExt and exposes ctx.GetPermissionManager() /
// ctx.SetPermissionManager() as convenience helpers.
package permission

import (
	"errors"
	"slices"
	"sync"
)

// Permission represents a capability defined by a resource and an action.
//
// Wildcard rules:
//   - Resource "*" matches any resource.
//   - Action "*" matches any action.
//   - Prefix wildcard "prefix:*" matches any resource with that prefix.
type Permission struct {
	Resource string // e.g. "command:weather", "admin:*"
	Action   string // e.g. "execute", "view", "manage"
}

// String returns the canonical "resource:action" string representation.
func (p Permission) String() string {
	return p.Resource + ":" + p.Action
}

// Match reports whether p grants the requested target permission.
// Wildcards in p are expanded; wildcards in target are treated literally.
func (p Permission) Match(target Permission) bool {
	if p.Resource == target.Resource && p.Action == target.Action {
		return true
	}
	if p.Resource == "*" && p.Action == "*" {
		return true
	}
	resourceMatch := matchWithWildcard(p.Resource, target.Resource)
	actionMatch := p.Action == "*" || target.Action == "*" || p.Action == target.Action
	return resourceMatch && actionMatch
}

// matchWithWildcard checks whether pattern (which may contain "*" or "prefix:*")
// matches value.
func matchWithWildcard(pattern, value string) bool {
	if pattern == "" || value == "" {
		return false
	}
	if pattern == "*" || value == "*" {
		return true
	}
	// prefix wildcard, e.g. "command:*"
	if len(pattern) > 2 && pattern[len(pattern)-2:] == ":*" {
		prefix := pattern[:len(pattern)-2]
		if len(value) >= len(prefix) && value[:len(prefix)] == prefix {
			return true
		}
	}
	return pattern == value
}

// Role is a named set of Permissions stored as a map for O(1) lookup.
//
// 变更（P2-5）：内部存储由 []Permission 改为 map[string]Permission，
// key 为 Permission.String()（"resource:action"）。
// HasPermission 从 O(n) 线性扫描降为 O(1) 精确匹配 + O(wildcards) 通配符回退。
//
// 外部 API 保持向后兼容：
//   - NewRole(name, permissions...)：签名不变
//   - AddPermission / RemovePermission：行为不变
//   - HasPermission：行为不变，性能提升
//   - Permissions()：返回 []Permission 切片副本（新增，替代直接访问字段）
type Role struct {
	Name        string
	permissions map[string]Permission // key = "resource:action"
	mu          sync.RWMutex
}

// NewRole creates a Role with the given name and initial permission set.
func NewRole(name string, perms ...Permission) *Role {
	r := &Role{
		Name:        name,
		permissions: make(map[string]Permission, len(perms)),
	}
	for _, p := range perms {
		r.permissions[p.String()] = p
	}
	return r
}

// AddPermission appends a permission to the role (idempotent).
func (r *Role) AddPermission(perm Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissions[perm.String()] = perm
}

// RemovePermission removes the exact permission from the role.
func (r *Role) RemovePermission(perm Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.permissions, perm.String())
}

// HasPermission reports whether the role grants the target permission.
//
// 查找顺序（O(1) 精确匹配优先，再通配符回退）：
//  1. 精确匹配（"resource:action"）
//  2. 通配全局（"*:*"）
//  3. 遍历通配符权限（含 "*" 的 Permission）
func (r *Role) HasPermission(perm Permission) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1. 精确 key 匹配（O(1)）
	if _, ok := r.permissions[perm.String()]; ok {
		return true
	}
	// 2. 全局通配符 "*:*"（O(1)）
	if _, ok := r.permissions["*:*"]; ok {
		return true
	}
	// 3. 遍历含通配符的 Permission（数量通常极少）
	for _, p := range r.permissions {
		if p.Resource == "*" || p.Action == "*" ||
			(len(p.Resource) > 2 && p.Resource[len(p.Resource)-2:] == ":*") {
			if p.Match(perm) {
				return true
			}
		}
	}
	return false
}

// Permissions returns a snapshot of all permissions in the role as a slice.
// The returned slice is a copy; modifying it does not affect the role.
func (r *Role) Permissions() []Permission {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Permission, 0, len(r.permissions))
	for _, p := range r.permissions {
		out = append(out, p)
	}
	return out
}

// Provider is an optional interface for external authorization sources
// (e.g. a database or IAM service). Implement it and call
// Manager.SetPermissionProvider to enable dynamic permission lookup.
type Provider interface {
	GetUserRoles(userID string) ([]string, error)
	GetUserPermissions(userID string) ([]Permission, error)
}

// Manager maps users to roles and direct permissions.
// All methods are safe for concurrent use.
type Manager struct {
	roles        map[string]*Role
	userRoles    map[string][]string
	userPerms    map[string][]Permission
	mu           sync.RWMutex
	permProvider Provider
}

// NewPermissionManager creates a Manager pre-loaded with the three
// default roles: "admin" (all permissions), "user" (command+query execute/view),
// and "guest" (query view only).
func NewPermissionManager() *Manager {
	pm := &Manager{
		roles:     make(map[string]*Role),
		userRoles: make(map[string][]string),
		userPerms: make(map[string][]Permission),
	}
	pm.RegisterDefaultRoles()
	return pm
}

// RegisterDefaultRoles registers the built-in admin / user / guest roles.
func (pm *Manager) RegisterDefaultRoles() {
	pm.RegisterRole(NewRole("admin", Permission{Resource: "*", Action: "*"}))
	pm.RegisterRole(NewRole("user",
		Permission{Resource: "command:*", Action: "execute"},
		Permission{Resource: "query:*", Action: "view"},
	))
	pm.RegisterRole(NewRole("guest",
		Permission{Resource: "query:*", Action: "view"},
	))
}

// RegisterRole adds or replaces a role definition.
func (pm *Manager) RegisterRole(role *Role) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.roles[role.Name] = role
}

// GetRole returns the Role with the given name, if registered.
func (pm *Manager) GetRole(name string) (*Role, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	role, ok := pm.roles[name]
	return role, ok
}

// AssignRole grants roleName to userID.
// Returns an error if the role is not registered.
func (pm *Manager) AssignRole(userID, roleName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, ok := pm.roles[roleName]; !ok {
		return errors.New("role not found: " + roleName)
	}
	if slices.Contains(pm.userRoles[userID], roleName) {
		return nil
	}
	pm.userRoles[userID] = append(pm.userRoles[userID], roleName)
	return nil
}

// RevokeRole removes roleName from userID.
func (pm *Manager) RevokeRole(userID, roleName string) {
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

// GrantPermission grants a direct (non-role) permission to userID.
func (pm *Manager) GrantPermission(userID string, perm Permission) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.userPerms[userID] = append(pm.userPerms[userID], perm)
}

// RevokePermission removes exact matches of perm from userID's direct permissions.
func (pm *Manager) RevokePermission(userID string, perm Permission) {
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

// HasPermission reports whether userID holds the requested permission,
// checking (in order): direct permissions, role permissions, and the
// optional Provider.
func (pm *Manager) HasPermission(userID string, perm Permission) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// 1. direct permissions
	for _, p := range pm.userPerms[userID] {
		if p.Match(perm) {
			return true
		}
	}

	// 2. role permissions（Role.HasPermission 内部 O(1) 精确匹配）
	for _, roleName := range pm.userRoles[userID] {
		if role, ok := pm.roles[roleName]; ok {
			if role.HasPermission(perm) {
				return true
			}
		}
	}

	// 3. external provider
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

// SetPermissionProvider sets an external authorization source.
func (pm *Manager) SetPermissionProvider(provider Provider) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.permProvider = provider
}

// GetUserRoles returns a copy of the role names assigned to userID.
func (pm *Manager) GetUserRoles(userID string) []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	roles := make([]string, len(pm.userRoles[userID]))
	copy(roles, pm.userRoles[userID])
	return roles
}

// GetUserPermissions returns all effective permissions for userID
// (direct permissions + permissions from all assigned roles).
func (pm *Manager) GetUserPermissions(userID string) []Permission {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	permissions := make([]Permission, 0)
	permissions = append(permissions, pm.userPerms[userID]...)
	for _, roleName := range pm.userRoles[userID] {
		if role, ok := pm.roles[roleName]; ok {
			permissions = append(permissions, role.Permissions()...)
		}
	}
	return permissions
}

// ExportUserRoles returns a deep copy of the user→roles mapping
// suitable for persistence.
func (pm *Manager) ExportUserRoles() map[string][]string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	out := make(map[string][]string, len(pm.userRoles))
	for userID, roles := range pm.userRoles {
		cp := make([]string, len(roles))
		copy(cp, roles)
		out[userID] = cp
	}
	return out
}

// ExportUserPerms returns a deep copy of the user→direct-permissions mapping
// suitable for persistence.
func (pm *Manager) ExportUserPerms() map[string][]Permission {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	out := make(map[string][]Permission, len(pm.userPerms))
	for userID, perms := range pm.userPerms {
		cp := make([]Permission, len(perms))
		copy(cp, perms)
		out[userID] = cp
	}
	return out
}

// LoadUserRoles merges a persisted user→roles mapping back into the manager.
// Roles that no longer exist in the manager's role registry are silently
// filtered out. Existing entries are preserved (this method does not replace).
func (pm *Manager) LoadUserRoles(userRoles map[string][]string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for userID, roles := range userRoles {
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

// ErrPermissionDenied is returned when an operation is rejected due to
// insufficient permissions.
var ErrPermissionDenied = errors.New("permission denied")
