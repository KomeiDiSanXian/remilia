package permission

import (
	"fmt"
	"maps"
	"sync"
)

// ListMode 黑白名单模式
type ListMode int

const (
	// ModeDisabled 禁用模式（不启用黑白名单）
	ModeDisabled ListMode = iota
	// ModeBlacklist 黑名单模式（只禁止列表中的用户）
	ModeBlacklist
	// ModeWhitelist 白名单模式（只允许列表中的用户）
	ModeWhitelist
)

// String 返回模式的字符串表示
func (m ListMode) String() string {
	switch m {
	case ModeDisabled:
		return "禁用"
	case ModeBlacklist:
		return "黑名单"
	case ModeWhitelist:
		return "白名单"
	default:
		return "未知"
	}
}

// AccessControlList 访问控制列表（黑白名单）
type AccessControlList struct {
	mu    sync.RWMutex
	mode  ListMode          // 当前模式
	list  map[string]bool   // 用户列表
	notes map[string]string // 用户备注
}

// NewAccessControlList 创建访问控制列表
func NewAccessControlList() *AccessControlList {
	return &AccessControlList{
		mode:  ModeDisabled,
		list:  make(map[string]bool),
		notes: make(map[string]string),
	}
}

// SetMode 设置模式
func (acl *AccessControlList) SetMode(mode ListMode) {
	acl.mu.Lock()
	defer acl.mu.Unlock()
	acl.mode = mode
}

// GetMode 获取当前模式
func (acl *AccessControlList) GetMode() ListMode {
	acl.mu.RLock()
	defer acl.mu.RUnlock()
	return acl.mode
}

// Add 添加用户到列表
func (acl *AccessControlList) Add(userID string, note string) {
	acl.mu.Lock()
	defer acl.mu.Unlock()
	acl.list[userID] = true
	if note != "" {
		acl.notes[userID] = note
	}
}

// Remove 从列表中移除用户
func (acl *AccessControlList) Remove(userID string) bool {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	if !acl.list[userID] {
		return false
	}

	delete(acl.list, userID)
	delete(acl.notes, userID)
	return true
}

// Contains 检查用户是否在列表中
func (acl *AccessControlList) Contains(userID string) bool {
	acl.mu.RLock()
	defer acl.mu.RUnlock()
	return acl.list[userID]
}

// IsAllowed 检查用户是否被允许访问
// 返回: (允许, 原因)
func (acl *AccessControlList) IsAllowed(userID string) (bool, string) {
	acl.mu.RLock()
	defer acl.mu.RUnlock()

	switch acl.mode {
	case ModeDisabled:
		// 禁用模式：允许所有用户
		return true, ""

	case ModeBlacklist:
		// 黑名单模式：只禁止列表中的用户
		if acl.list[userID] {
			note := acl.notes[userID]
			if note != "" {
				return false, fmt.Sprintf("用户在黑名单中（原因: %s）", note)
			}
			return false, "用户在黑名单中"
		}
		return true, ""

	case ModeWhitelist:
		// 白名单模式：只允许列表中的用户
		if acl.list[userID] {
			return true, ""
		}
		return false, "用户不在白名单中"

	default:
		return true, ""
	}
}

// List 列出所有用户
func (acl *AccessControlList) List() []UserInfo {
	acl.mu.RLock()
	defer acl.mu.RUnlock()

	result := make([]UserInfo, 0, len(acl.list))
	for userID := range acl.list {
		result = append(result, UserInfo{
			UserID: userID,
			Note:   acl.notes[userID],
		})
	}

	return result
}

// Count 返回列表中的用户数量
func (acl *AccessControlList) Count() int {
	acl.mu.RLock()
	defer acl.mu.RUnlock()
	return len(acl.list)
}

// Clear 清空列表
func (acl *AccessControlList) Clear() int {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	count := len(acl.list)
	acl.list = make(map[string]bool)
	acl.notes = make(map[string]string)

	return count
}

// GetNote 获取用户备注
func (acl *AccessControlList) GetNote(userID string) string {
	acl.mu.RLock()
	defer acl.mu.RUnlock()
	return acl.notes[userID]
}

// SetNote 设置用户备注
func (acl *AccessControlList) SetNote(userID string, note string) {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	if acl.list[userID] {
		acl.notes[userID] = note
	}
}

// UserInfo 用户信息
type UserInfo struct {
	UserID string
	Note   string
}

// Stats 返回统计信息
func (acl *AccessControlList) Stats() ACLStats {
	acl.mu.RLock()
	defer acl.mu.RUnlock()

	return ACLStats{
		Mode:      acl.mode,
		UserCount: len(acl.list),
	}
}

// ACLStats 访问控制列表统计
type ACLStats struct {
	Mode      ListMode
	UserCount int
}

// ExportSnapshot 导出 ACL 快照（用于持久化）
// 返回 (mode, list, notes)，均为值拷贝
func (acl *AccessControlList) ExportSnapshot() (mode int, list map[string]bool, notes map[string]string) {
	acl.mu.RLock()
	defer acl.mu.RUnlock()

	listCopy := make(map[string]bool, len(acl.list))
	maps.Copy(listCopy, acl.list)
	notesCopy := make(map[string]string, len(acl.notes))
	maps.Copy(notesCopy, acl.notes)
	return int(acl.mode), listCopy, notesCopy
}

// LoadSnapshot 从持久化快照恢复 ACL 数据（替换当前数据）
func (acl *AccessControlList) LoadSnapshot(mode int, list map[string]bool, notes map[string]string) {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	acl.mode = ListMode(mode)
	acl.list = make(map[string]bool, len(list))
	maps.Copy(acl.list, list)
	acl.notes = make(map[string]string, len(notes))
	maps.Copy(acl.notes, notes)
}
