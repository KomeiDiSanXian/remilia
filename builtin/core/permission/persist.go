package permission

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/builtin/internal/jsonfile"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// 持久化存储 key 常量（保留，仍用于 JSON 文件内的字段名）
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

// permFile is the on-disk representation of all permission data.
type permFile struct {
	UserRoles userRolesSnapshot `json:"user_roles"`
	UserPerms userPermsSnapshot `json:"user_perms"`
	ACL       aclSnapshot       `json:"acl"`
}

// TryBindDataFile 绑定数据文件路径并加载持久化数据（Setup 时调用）。
// 若 path 为空则静默跳过（存储是可选的）。
func (p *Plugin) TryBindDataFile(path string) {
	if path == "" {
		return
	}
	p.dataFile = path
	p.loadFromFile()
	logger.Info("[PermissionPlugin] Persistence enabled, data loaded from file")
}

// loadFromFile 从 JSON 文件加载持久化的权限数据
func (p *Plugin) loadFromFile() {
	if p.dataFile == "" {
		return
	}
	f, err := jsonfile.Read[permFile](p.dataFile)
	if err != nil {
		return
	}

	// 加载用户角色映射
	if len(f.UserRoles.UserRoles) > 0 {
		p.manager.LoadUserRoles(f.UserRoles.UserRoles)
		logger.Infof("[PermissionPlugin] Loaded user roles for %d users", len(f.UserRoles.UserRoles))
	}

	// 加载用户直接权限
	for userID, perms := range f.UserPerms.UserPerms {
		for _, pe := range perms {
			p.manager.GrantPermission(userID, permission.Permission{Resource: pe.Resource, Action: pe.Action})
		}
	}
	if len(f.UserPerms.UserPerms) > 0 {
		logger.Infof("[PermissionPlugin] Loaded direct permissions for %d users", len(f.UserPerms.UserPerms))
	}

	// 加载 ACL 数据
	if f.ACL.List != nil {
		p.acl.LoadSnapshot(f.ACL.Mode, f.ACL.List, f.ACL.Notes)
		logger.Infof("[PermissionPlugin] Loaded ACL (mode=%d, users=%d)", f.ACL.Mode, len(f.ACL.List))
	}
}

// saveToFile 将当前权限数据持久化到 JSON 文件
func (p *Plugin) saveToFile() error {
	if p.dataFile == "" {
		return nil
	}

	rolesSnap := userRolesSnapshot{UserRoles: p.manager.ExportUserRoles()}

	rawPerms := p.manager.ExportUserPerms()
	permsSnap := userPermsSnapshot{UserPerms: make(map[string][]permEntry, len(rawPerms))}
	for userID, perms := range rawPerms {
		entries := make([]permEntry, len(perms))
		for i, perm := range perms {
			entries[i] = permEntry{Resource: perm.Resource, Action: perm.Action}
		}
		permsSnap.UserPerms[userID] = entries
	}

	aclMode, aclList, aclNotes := p.acl.ExportSnapshot()
	aclSnap := aclSnapshot{Mode: aclMode, List: aclList, Notes: aclNotes}

	f := permFile{
		UserRoles: rolesSnap,
		UserPerms: permsSnap,
		ACL:       aclSnap,
	}
	if err := jsonfile.Write(p.dataFile, f); err != nil {
		return fmt.Errorf("permission persist: write failed: %w", err)
	}
	return nil
}
