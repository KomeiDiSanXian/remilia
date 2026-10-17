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
//
// # 与 plugins/core/permission 的关系
//
// 本包是权限系统的**内核层**，只提供数据结构和算法，不依赖插件系统。
// 如需在 Bot 中通过命令动态管理权限（/acl add/rm/list），
// 请使用 plugins/core/permission 插件，它基于本包构建并提供命令界面。
package permission

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
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
//
// 安全约束：target（请求方）侧的 "*" 只按字面值处理，不再被当作通配符。
// 此前 target.Action=="*" / target.Resource=="*" 会被直接放行——
// 任何把用户可控字符串透传进 HasPermission 的调用方都可能被 "*" 绕过检查。
func (p Permission) Match(target Permission) bool {
	if p.Resource == target.Resource && p.Action == target.Action {
		return true
	}
	if p.Resource == "*" && p.Action == "*" {
		return true
	}
	resourceMatch := matchWithWildcard(p.Resource, target.Resource)
	actionMatch := p.Action == "*" || p.Action == target.Action
	return resourceMatch && actionMatch
}

// matchWithWildcard checks whether pattern (which may contain "*" or "prefix:*")
// matches value. Wildcards are only expanded on the pattern side; a "*" in
// value is treated literally (see Permission.Match).
//
// "prefix:*" 落在命名空间边界上：命中 "prefix" 本身或 "prefix:" 开头的值。
// 早前的裸前缀比较让 "command:*" 命中 "commander"、"admin:*" 命中
// "administrator"，授权范围被意外放大。
func matchWithWildcard(pattern, value string) bool {
	if pattern == "" || value == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	// prefix wildcard, e.g. "command:*"
	if len(pattern) > 2 && pattern[len(pattern)-2:] == ":*" {
		prefix := pattern[:len(pattern)-2]
		if value == prefix || strings.HasPrefix(value, prefix+":") {
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
	permissions map[string]Permission // key = permKey（无歧义内部编码）
	mu          sync.RWMutex

	// Level 是角色的等级（越大越高，0 表示未声明）。
	//
	// 内核不假设任何具体等级体系：它只提供比较与"受保护操作"的判定原语
	// （[Manager.UserLevel]/[Manager.HasLevelAtLeast]/[Manager.AssignRoleChecked]），
	// 让各命令层不再各自重复实现"谁是管理员"的判断（漏一处即提权）。
	Level int
}

// NewRole creates a Role with the given name and initial permission set.
func NewRole(name string, perms ...Permission) *Role {
	r := &Role{
		Name:        name,
		permissions: make(map[string]Permission, len(perms)),
	}
	for _, p := range perms {
		r.permissions[permKey(p)] = p
	}
	return r
}

// permKey 返回权限的无歧义内部 key。
//
// 早期实现直接用 Permission.String()（"resource:action"）作为 map key：
// {command:echo, execute} 与 {command, echo:execute} 会得到同一个 key
// （"command:echo:execute"），HasPermission 误命中、RemovePermission 误删。
// 这里用「长度前缀 + resource + action」，任意取值组合都不会碰撞；
// Permission.String() 保留为展示用格式（对外 API 不变）。
func permKey(p Permission) string {
	return strconv.Itoa(len(p.Resource)) + ":" + p.Resource + p.Action
}

// AddPermission appends a permission to the role (idempotent).
func (r *Role) AddPermission(perm Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.permissions == nil {
		r.permissions = make(map[string]Permission, 1)
	}
	r.permissions[permKey(perm)] = perm
}

// RemovePermission removes the exact permission from the role.
func (r *Role) RemovePermission(perm Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.permissions, permKey(perm))
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
	if _, ok := r.permissions[permKey(perm)]; ok {
		return true
	}
	// 2. 全局通配符 "*:*"（O(1)）
	if _, ok := r.permissions[permKey(Permission{Resource: "*", Action: "*"})]; ok {
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
	// providerErrorHandler 是外部权限源故障回调（见 SetProviderErrorHandler）。
	providerErrorHandler func(userID string, err error)
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
//
// nil 角色与空名角色被拒绝并告警：nil 会直接 panic，空名角色则永远无法被
// 授予（AssignRole 按名字查找），注册它等于"以为配好了权限、实际全不生效"。
func (pm *Manager) RegisterRole(role *Role) {
	if role == nil {
		logger.Warn("[permission] RegisterRole(nil) ignored")
		return
	}
	if role.Name == "" {
		logger.Warn("[permission] RegisterRole with empty name ignored")
		return
	}
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

// ListRoles returns a copy of all registered role names.
func (pm *Manager) ListRoles() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	names := make([]string, 0, len(pm.roles))
	for n := range pm.roles {
		names = append(names, n)
	}
	return names
}

// RemoveRole deletes a role definition and revokes it from every user.
// It reports whether the role existed.
func (pm *Manager) RemoveRole(name string) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, ok := pm.roles[name]; !ok {
		return false
	}
	delete(pm.roles, name)
	for userID, roles := range pm.userRoles {
		filtered := make([]string, 0, len(roles))
		for _, r := range roles {
			if r != name {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			delete(pm.userRoles, userID)
			continue
		}
		pm.userRoles[userID] = filtered
	}
	return true
}

// UserLevel 返回用户当前等级：其所有已注册角色中 Level 的最大值。
//
// 未授予任何角色（或角色未声明 Level）时返回 0。
func (pm *Manager) UserLevel(userID string) int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	level := 0
	for _, roleName := range pm.userRoles[userID] {
		if role, ok := pm.roles[roleName]; ok && role.Level > level {
			level = role.Level
		}
	}
	return level
}

// HasLevelAtLeast 报告用户等级是否达到 minLevel。
func (pm *Manager) HasLevelAtLeast(userID string, minLevel int) bool {
	return pm.UserLevel(userID) >= minLevel
}

// AssignRoleChecked 在等级约束下授予角色：调用方（actorID）的等级必须
//
//   - 不低于目标角色的 Level（不能授予高于自己的角色），且
//   - 不低于目标用户当前等级（不能改动与自己同级或更高的用户）。
//
// 这是给权限管理命令使用的提权防护原语：此前"管理员"判断散落在各命令层，
// 漏一处即可自我提权。返回错误说明被拒绝的原因，错误信息可直接回显。
func (pm *Manager) AssignRoleChecked(actorID, targetID, roleName string) error {
	if roleName == "" {
		return errors.New("role name is required")
	}
	pm.mu.RLock()
	role, ok := pm.roles[roleName]
	actorLevel := 0
	for _, rn := range pm.userRoles[actorID] {
		if r, ok := pm.roles[rn]; ok && r.Level > actorLevel {
			actorLevel = r.Level
		}
	}
	targetLevel := 0
	for _, rn := range pm.userRoles[targetID] {
		if r, ok := pm.roles[rn]; ok && r.Level > targetLevel {
			targetLevel = r.Level
		}
	}
	pm.mu.RUnlock()

	if !ok {
		return errors.New("role not found: " + roleName)
	}
	if role.Level > actorLevel {
		return fmt.Errorf("cannot assign role %q (level %d) above actor level %d", roleName, role.Level, actorLevel)
	}
	if targetLevel > actorLevel {
		return fmt.Errorf("cannot modify user %q at level %d from actor level %d", targetID, targetLevel, actorLevel)
	}
	if targetLevel > role.Level {
		return fmt.Errorf("cannot downgrade user %q (level %d) with role %q (level %d)", targetID, targetLevel, roleName, role.Level)
	}
	return pm.AssignRole(targetID, roleName)
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
//
// 结果为空时删除该用户的映射条目：否则对不存在/已无角色的用户调用会写入
// 空切片条目，userRoles 随调用次数无界增长，并被 ExportUserRoles 带进
// 持久化数据（对照 [Manager.RemoveRole] 的 delete 语义）。
func (pm *Manager) RevokeRole(userID, roleName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	roles, ok := pm.userRoles[userID]
	if !ok {
		return
	}
	filtered := make([]string, 0, len(roles))
	for _, r := range roles {
		if r != roleName {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		delete(pm.userRoles, userID)
		return
	}
	pm.userRoles[userID] = filtered
}

// GrantPermission grants a direct (non-role) permission to userID (idempotent).
func (pm *Manager) GrantPermission(userID string, perm Permission) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, p := range pm.userPerms[userID] {
		if p.Resource == perm.Resource && p.Action == perm.Action {
			return
		}
	}
	pm.userPerms[userID] = append(pm.userPerms[userID], perm)
}

// RevokePermission removes exact matches of perm from userID's direct permissions.
//
// 结果为空时删除该用户的映射条目（同 [Manager.RevokeRole]）：避免为不存在/
// 已无权限的用户留下空切片条目，污染 ExportUserPerms 的持久化数据。
func (pm *Manager) RevokePermission(userID string, perm Permission) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	perms, ok := pm.userPerms[userID]
	if !ok {
		return
	}
	filtered := make([]Permission, 0, len(perms))
	for _, p := range perms {
		if p.Resource != perm.Resource || p.Action != perm.Action {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		delete(pm.userPerms, userID)
		return
	}
	pm.userPerms[userID] = filtered
}

// HasPermission reports whether userID holds the requested permission,
// checking (in order): direct permissions, role permissions, and the
// optional Provider.
//
// Provider 可能访问外部系统（数据库/IAM），因此在释放 pm.mu 之后才调用，
// 避免慢查询长时间阻塞写操作，也避免 Provider 回调本 Manager 时死锁。
func (pm *Manager) HasPermission(userID string, perm Permission) bool {
	pm.mu.RLock()

	// 1. direct permissions
	for _, p := range pm.userPerms[userID] {
		if p.Match(perm) {
			pm.mu.RUnlock()
			return true
		}
	}

	// 2. role permissions（Role.HasPermission 内部 O(1) 精确匹配）
	roles := make([]*Role, 0, len(pm.userRoles[userID]))
	for _, roleName := range pm.userRoles[userID] {
		if role, ok := pm.roles[roleName]; ok {
			roles = append(roles, role)
		}
	}
	provider := pm.permProvider
	pm.mu.RUnlock()

	for _, role := range roles {
		if role.HasPermission(perm) {
			return true
		}
	}

	// 3. external provider（锁外调用）
	//
	// 失败必须可见：此前 provider 错误被静默吞掉，下游故障表现为"全员无权限"
	// 且没有任何日志；同时首个调用失败时不再继续调用第二个（provider 不可用时
	// 每次拒绝都要付两次外部请求的代价）。
	if provider != nil {
		perms, permsErr := provider.GetUserPermissions(userID)
		if permsErr != nil {
			pm.reportProviderError(userID, "GetUserPermissions", permsErr)
			return false
		}
		for _, p := range perms {
			if p.Match(perm) {
				return true
			}
		}

		roleNames, rolesErr := provider.GetUserRoles(userID)
		if rolesErr != nil {
			pm.reportProviderError(userID, "GetUserRoles", rolesErr)
			return false
		}
		for _, roleName := range roleNames {
			if role, ok := pm.GetRole(roleName); ok {
				if role.HasPermission(perm) {
					return true
				}
			}
		}
	}

	return false
}

// reportProviderError 记录外部权限源故障（日志 + 可选回调）。
func (pm *Manager) reportProviderError(userID, method string, err error) {
	logger.WithError(err).WithFields(logger.Fields{
		"user_id": userID,
		"method":  method,
	}).Warn("[permission] external provider call failed; permission denied by default")

	pm.mu.RLock()
	handler := pm.providerErrorHandler
	pm.mu.RUnlock()
	if handler != nil {
		handler(userID, err)
	}
}

// SetProviderErrorHandler 注册外部权限源故障回调（可选，用于告警/指标）。
//
// 回调在权限检查的调用方 goroutine 上执行，应保持轻量且不得回调本 Manager
// 的写方法（会与权限检查并发）。
func (pm *Manager) SetProviderErrorHandler(fn func(userID string, err error)) {
	pm.mu.Lock()
	pm.providerErrorHandler = fn
	pm.mu.Unlock()
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
