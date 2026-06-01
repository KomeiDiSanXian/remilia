package permission

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

// ─── GORM 模型 ───────────────────────────────────────────────────────────────

// UserRoleModel 用户-角色映射表。
type UserRoleModel struct {
	ID       uint   `gorm:"primaryKey;autoIncrement"`
	UserID   string `gorm:"uniqueIndex:idx_user_role;not null;size:512"`
	RoleName string `gorm:"uniqueIndex:idx_user_role;not null;size:100"`
}

// UserPermissionModel 用户直接权限表。
type UserPermissionModel struct {
	ID       uint   `gorm:"primaryKey;autoIncrement"`
	UserID   string `gorm:"uniqueIndex:idx_user_perm;not null;size:512"`
	Resource string `gorm:"uniqueIndex:idx_user_perm;not null;size:100"`
	Action   string `gorm:"uniqueIndex:idx_user_perm;not null;size:100"`
}

// ACLConfigModel 黑白名单模式（单行，ID=1）。
type ACLConfigModel struct {
	ID   uint `gorm:"primaryKey"`
	Mode int  `gorm:"not null;default:0"`
}

// ACLEntryModel 黑白名单条目表。
type ACLEntryModel struct {
	ID     uint   `gorm:"primaryKey;autoIncrement"`
	UserID string `gorm:"uniqueIndex;not null;size:512"`
	Note   string `gorm:"type:text"`
}

// ─── 持久化方法 ───────────────────────────────────────────────────────────────

func (p *Plugin) tryBindStorage(svc *storage.Plugin) {
	if svc == nil {
		return
	}
	p.storageSvc = svc
	if err := svc.AutoMigrate(
		&UserRoleModel{},
		&UserPermissionModel{},
		&ACLConfigModel{},
		&ACLEntryModel{},
	); err != nil {
		logger.WithError(err).Warn("[PermissionPlugin] AutoMigrate failed")
		return
	}
	p.loadFromDB()
	logger.Info("[PermissionPlugin] Persistence enabled via storage plugin (structured tables)")
}

func (p *Plugin) loadFromDB() {
	if p.storageSvc == nil {
		return
	}

	// 加载用户角色
	var roleModels []UserRoleModel
	if err := p.storageSvc.Find(&roleModels); err == nil && len(roleModels) > 0 {
		userRoles := make(map[string][]string)
		for _, m := range roleModels {
			userRoles[m.UserID] = append(userRoles[m.UserID], m.RoleName)
		}
		p.manager.LoadUserRoles(userRoles)
		logger.Infof("[PermissionPlugin] Loaded %d user roles", len(roleModels))
	}

	// 加载用户直接权限
	var permModels []UserPermissionModel
	if err := p.storageSvc.Find(&permModels); err == nil && len(permModels) > 0 {
		for _, m := range permModels {
			p.manager.GrantPermission(m.UserID, permission.Permission{Resource: m.Resource, Action: m.Action})
		}
		logger.Infof("[PermissionPlugin] Loaded %d direct permissions", len(permModels))
	}

	// 加载 ACL 配置
	var aclCfg ACLConfigModel
	if err := p.storageSvc.First(&aclCfg, "id = ?", 1); err == nil {
		var aclModels []ACLEntryModel
			if err := p.storageSvc.Find(&aclModels); err == nil {
			list := make(map[string]bool, len(aclModels))
			notes := make(map[string]string, len(aclModels))
			for _, m := range aclModels {
				list[m.UserID] = true
				notes[m.UserID] = m.Note
			}
			p.acl.LoadSnapshot(aclCfg.Mode, list, notes)
			logger.Infof("[PermissionPlugin] Loaded ACL (mode=%d, users=%d)", aclCfg.Mode, len(aclModels))
		}
	}
}

func (p *Plugin) saveToDB() error {
	if p.storageSvc == nil {
		return nil
	}

	// ── 用户角色 ──
	if err := p.storageSvc.Where("1 = 1").Delete(&UserRoleModel{}); err != nil {
		return fmt.Errorf("permission persist: clear roles failed: %w", err)
	}
	userRoles := p.manager.ExportUserRoles()
	for userID, roles := range userRoles {
		for _, role := range roles {
			m := UserRoleModel{UserID: userID, RoleName: role}
			if err := p.storageSvc.Create(&m); err != nil {
				return fmt.Errorf("permission persist: insert role failed: %w", err)
			}
		}
	}

	// ── 用户直接权限 ──
	if err := p.storageSvc.Where("1 = 1").Delete(&UserPermissionModel{}); err != nil {
		return fmt.Errorf("permission persist: clear perms failed: %w", err)
	}
	userPerms := p.manager.ExportUserPerms()
	for userID, perms := range userPerms {
		for _, perm := range perms {
			m := UserPermissionModel{UserID: userID, Resource: perm.Resource, Action: perm.Action}
			if err := p.storageSvc.Create(&m); err != nil {
				return fmt.Errorf("permission persist: insert perm failed: %w", err)
			}
		}
	}

	// ── ACL ──
	if err := p.storageSvc.Where("1 = 1").Delete(&ACLConfigModel{}); err != nil {
		return fmt.Errorf("permission persist: clear acl config failed: %w", err)
	}
	if err := p.storageSvc.Where("1 = 1").Delete(&ACLEntryModel{}); err != nil {
		return fmt.Errorf("permission persist: clear acl entries failed: %w", err)
	}

	aclMode, aclList, aclNotes := p.acl.ExportSnapshot()
	if err := p.storageSvc.Create(&ACLConfigModel{ID: 1, Mode: aclMode}); err != nil {
		return fmt.Errorf("permission persist: insert acl config failed: %w", err)
	}
	for userID := range aclList {
		m := ACLEntryModel{UserID: userID, Note: aclNotes[userID]}
		if err := p.storageSvc.Create(&m); err != nil {
			return fmt.Errorf("permission persist: insert acl entry failed: %w", err)
		}
	}

	logger.Infof("[PermissionPlugin] Saved %d roles, %d perms, %d ACL entries",
		len(userRoles), len(userPerms), len(aclList))
	return nil
}
