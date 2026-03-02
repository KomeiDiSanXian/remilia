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

// Role is a named set of Permissions.
type Role struct {
	Name        string
	Permissions []Permission
	mu          sync.RWMutex
}

// NewRole creates a Role with the given name and initial permission set.
func NewRole(name string, permissions ...Permission) *Role {
	return &Role{
		Name:        name,
		Permissions: permissions,
	}
}

// AddPermission appends a permission to the role.
func (r *Role) AddPermission(perm Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Permissions = append(r.Permissions, perm)
}

// RemovePermission removes all exact matches of perm from the role.
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

// HasPermission reports whether the role grants the target permission.
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

	// 2. role permissions
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
			role.mu.RLock()
			permissions = append(permissions, role.Permissions...)
			role.mu.RUnlock()
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
// filtered out.  Existing entries are preserved (this method does not replace).
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
