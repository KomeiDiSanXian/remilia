package permission

import (
	"context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/builtin/core/storage"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// 持久化存储 key 常量
const (
	permKeyUserRoles = "user_roles"
	permKeyUserPerms = "user_perms"
	permKeyACL       = "acl"
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

// enablePersistence 绑定命名空间 Store 并加载持久化数据
func (p *Plugin) enablePersistence(store *storage.Store) {
	p.store = store
	p.loadFromStorage()
	logger.Info("[PermissionPlugin] Persistence enabled, data loaded from storage")
}

// loadFromStorage 从 storage 加载持久化的权限数据
func (p *Plugin) loadFromStorage() {
	if p.store == nil {
		return
	}
	ctx := context.Background()

	// 加载用户角色映射
	if rolesSnap, err := storage.Get[userRolesSnapshot](ctx, p.store, permKeyUserRoles); err == nil {
		p.manager.LoadUserRoles(rolesSnap.UserRoles)
		logger.Infof("[PermissionPlugin] Loaded user roles for %d users", len(rolesSnap.UserRoles))
	}

	// 加载用户直接权限
	if permsSnap, err := storage.Get[userPermsSnapshot](ctx, p.store, permKeyUserPerms); err == nil {
		for userID, perms := range permsSnap.UserPerms {
			for _, pe := range perms {
				p.manager.GrantPermission(userID, permission.Permission{Resource: pe.Resource, Action: pe.Action})
			}
		}
		logger.Infof("[PermissionPlugin] Loaded direct permissions for %d users", len(permsSnap.UserPerms))
	}

	// 加载 ACL 数据
	if aclSnap, err := storage.Get[aclSnapshot](ctx, p.store, permKeyACL); err == nil {
		p.acl.LoadSnapshot(aclSnap.Mode, aclSnap.List, aclSnap.Notes)
		logger.Infof("[PermissionPlugin] Loaded ACL (mode=%d, users=%d)", aclSnap.Mode, len(aclSnap.List))
	}
}

// saveToStorage 将当前权限数据持久化到 storage
func (p *Plugin) saveToStorage() error {
	if p.store == nil {
		return nil
	}
	ctx := context.Background()
	var errs []error

	// 保存用户角色映射
	rolesSnap := userRolesSnapshot{UserRoles: p.manager.ExportUserRoles()}
	if err := storage.Set(ctx, p.store, permKeyUserRoles, rolesSnap, 0); err != nil {
		errs = append(errs, fmt.Errorf("save user roles: %w", err))
	}

	// 保存用户直接权限
	rawPerms := p.manager.ExportUserPerms()
	permsSnap := userPermsSnapshot{UserPerms: make(map[string][]permEntry, len(rawPerms))}
	for userID, perms := range rawPerms {
		entries := make([]permEntry, len(perms))
		for i, perm := range perms {
			entries[i] = permEntry{Resource: perm.Resource, Action: perm.Action}
		}
		permsSnap.UserPerms[userID] = entries
	}
	if err := storage.Set(ctx, p.store, permKeyUserPerms, permsSnap, 0); err != nil {
		errs = append(errs, fmt.Errorf("save user perms: %w", err))
	}

	// 保存 ACL
	aclMode, aclList, aclNotes := p.acl.ExportSnapshot()
	aclSnap := aclSnapshot{Mode: aclMode, List: aclList, Notes: aclNotes}
	if err := storage.Set(ctx, p.store, permKeyACL, aclSnap, 0); err != nil {
		errs = append(errs, fmt.Errorf("save acl: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("permission persist errors: %v", errs)
	}
	return nil
}

// TryBindStorage 绑定命名空间化的 *storage.Store（Setup 时调用）。
// 若 storage 插件未注册则静默跳过（存储是可选依赖）。
//
//	if sb, ok := plugin.Try[storage.Plugin](ctx, "storage"); ok {
//	    pluginAPI.TryBindStorage(sb.NS("permission"))
//	}
func (p *Plugin) TryBindStorage(store *storage.Store) {
	if store == nil {
		return
	}
	p.enablePersistence(store)
}
