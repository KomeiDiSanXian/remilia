package permission

import (
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/storage"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// 持久化存储 key 常量
const (
	permKeyUserRoles = "permission:user_roles"
	permKeyUserPerms = "permission:user_perms"
	permKeyACLMode   = "permission:acl_mode"
	permKeyACLList   = "permission:acl_list"
)

// userRolesSnapshot userRoles 的 JSON 序列化结构
type userRolesSnapshot struct {
	UserRoles map[string][]string `json:"user_roles"`
}

// userPermsSnapshot userPerms 的 JSON 序列化结构
type userPermsSnapshot struct {
	UserPerms map[string][]permEntry `json:"user_perms"`
}

type permEntry struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

// aclSnapshot ACL 的 JSON 序列化结构
type aclSnapshot struct {
	Mode  int               `json:"mode"`
	List  map[string]bool   `json:"list"`
	Notes map[string]string `json:"notes"`
}

// StorageBackend 存储后端接口（storage.Plugin 的子集）
type StorageBackend interface {
	GetJSON(key string, v any) error
	SetJSON(key string, v any, ttl time.Duration) error
}

// enablePersistence 将 storage 插件绑定到权限插件，并加载持久化数据
func (p *Plugin) enablePersistence(store StorageBackend) {
	p.store = store
	p.loadFromStorage()
	logger.Info("[PermissionPlugin] Persistence enabled, data loaded from storage")
}

// loadFromStorage 从 storage 加载持久化的权限数据
func (p *Plugin) loadFromStorage() {
	if p.store == nil {
		return
	}

	// 加载用户角色映射
	var rolesSnap userRolesSnapshot
	if err := p.store.GetJSON(permKeyUserRoles, &rolesSnap); err == nil {
		mgr := p.manager
		mgr.LoadUserRoles(rolesSnap.UserRoles)
		logger.Infof("[PermissionPlugin] Loaded user roles for %d users", len(rolesSnap.UserRoles))
	}

	// 加载用户直接权限
	var permsSnap userPermsSnapshot
	if err := p.store.GetJSON(permKeyUserPerms, &permsSnap); err == nil {
		mgr := p.manager
		for userID, perms := range permsSnap.UserPerms {
			for _, pe := range perms {
				mgr.GrantPermission(userID, permission.Permission{Resource: pe.Resource, Action: pe.Action})
			}
		}
		logger.Infof("[PermissionPlugin] Loaded direct permissions for %d users", len(permsSnap.UserPerms))
	}

	// 加载 ACL 数据
	var aclSnap aclSnapshot
	if err := p.store.GetJSON(permKeyACLMode, &aclSnap); err == nil {
		p.acl.LoadSnapshot(aclSnap.Mode, aclSnap.List, aclSnap.Notes)
		logger.Infof("[PermissionPlugin] Loaded ACL (mode=%d, users=%d)", aclSnap.Mode, len(aclSnap.List))
	}
}

// saveToStorage 将当前权限数据持久化到 storage
func (p *Plugin) saveToStorage() error {
	if p.store == nil {
		return nil
	}

	var errs []error

	// 保存用户角色映射
	rolesSnap := userRolesSnapshot{
		UserRoles: p.manager.ExportUserRoles(),
	}
	if err := p.store.SetJSON(permKeyUserRoles, rolesSnap, 0); err != nil {
		errs = append(errs, fmt.Errorf("save user roles: %w", err))
	}

	// 保存用户直接权限
	rawPerms := p.manager.ExportUserPerms()
	permsSnap := userPermsSnapshot{
		UserPerms: make(map[string][]permEntry, len(rawPerms)),
	}
	for userID, perms := range rawPerms {
		entries := make([]permEntry, len(perms))
		for i, perm := range perms {
			entries[i] = permEntry{Resource: perm.Resource, Action: perm.Action}
		}
		permsSnap.UserPerms[userID] = entries
	}
	if err := p.store.SetJSON(permKeyUserPerms, permsSnap, 0); err != nil {
		errs = append(errs, fmt.Errorf("save user perms: %w", err))
	}

	// 保存 ACL
	aclMode, aclList, aclNotes := p.acl.ExportSnapshot()
	aclSnap := aclSnapshot{
		Mode:  aclMode,
		List:  aclList,
		Notes: aclNotes,
	}
	if err := p.store.SetJSON(permKeyACLMode, aclSnap, 0); err != nil {
		errs = append(errs, fmt.Errorf("save acl: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("permission persist errors: %v", errs)
	}
	return nil
}

// TryBindStorage 尝试从 manager container 中自动绑定 storage 插件（Setup 时调用）
// 若 storage 插件未注册则静默跳过（存储是可选依赖）
func (p *Plugin) TryBindStorage(store StorageBackend) {
	if store == nil {
		return
	}
	p.enablePersistence(store)
}

// storageAdapterFromPlugin 将 *storage.Plugin 适配为 StorageBackend 接口
type storageAdapterFromPlugin struct {
	s *storage.Plugin
}

func (a *storageAdapterFromPlugin) GetJSON(key string, v any) error {
	return a.s.GetJSON(key, v)
}

func (a *storageAdapterFromPlugin) SetJSON(key string, v any, ttl time.Duration) error {
	return a.s.SetJSON(key, v, ttl)
}

// NewStorageAdapter 将 *storage.Plugin 包装为 StorageBackend
func NewStorageAdapter(s *storage.Plugin) StorageBackend {
	return &storageAdapterFromPlugin{s: s}
}
