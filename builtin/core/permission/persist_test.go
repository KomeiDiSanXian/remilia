package permission_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	permission0 "github.com/KomeiDiSanXian/remilia/core/permission"
)

// mockStorageBackend 模拟存储后端（内存实现）
type mockStorageBackend struct {
	data map[string][]byte
}

func newMockStorage() *mockStorageBackend {
	return &mockStorageBackend{data: make(map[string][]byte)}
}

func (m *mockStorageBackend) GetJSON(key string, v any) error {
	b, ok := m.data[key]
	if !ok {
		return fmt.Errorf("key not found: %s", key)
	}
	return json.Unmarshal(b, v)
}

func (m *mockStorageBackend) SetJSON(key string, v any, _ time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.data[key] = b
	return nil
}

// Ensure mockStorageBackend satisfies permission.StorageBackend at compile time
var _ permission.StorageBackend = (*mockStorageBackend)(nil)

// TestPermissionManager_ExportLoadUserRoles 验证用户角色的导出和加载
func TestPermissionManager_ExportLoadUserRoles(t *testing.T) {
	mgr := eventctx.NewPermissionManager()

	// 分配角色
	if err := mgr.AssignRole("user1", "admin"); err != nil {
		t.Fatalf("AssignRole admin: %v", err)
	}
	if err := mgr.AssignRole("user2", "user"); err != nil {
		t.Fatalf("AssignRole user: %v", err)
	}

	// 导出
	exported := mgr.ExportUserRoles()
	if len(exported) != 2 {
		t.Fatalf("expected 2 users in export, got %d", len(exported))
	}
	if len(exported["user1"]) == 0 || exported["user1"][0] != "admin" {
		t.Errorf("user1 should have admin role, got %v", exported["user1"])
	}

	// 创建新的 Manager 并加载
	mgr2 := eventctx.NewPermissionManager()
	mgr2.LoadUserRoles(exported)

	roles := mgr2.GetUserRoles("user1")
	if len(roles) == 0 || roles[0] != "admin" {
		t.Errorf("user1 should have admin role after load, got %v", roles)
	}
	roles2 := mgr2.GetUserRoles("user2")
	if len(roles2) == 0 || roles2[0] != "user" {
		t.Errorf("user2 should have user role after load, got %v", roles2)
	}
}

// TestPermissionManager_ExportUserPerms 验证直接权限导出
func TestPermissionManager_ExportUserPerms(t *testing.T) {
	mgr := eventctx.NewPermissionManager()

	mgr.GrantPermission("alice", permission0.Permission{Resource: "post", Action: "create"})
	mgr.GrantPermission("alice", permission0.Permission{Resource: "post", Action: "delete"})

	exported := mgr.ExportUserPerms()
	alicePerms, ok := exported["alice"]
	if !ok {
		t.Fatal("alice should be in exported perms")
	}
	if len(alicePerms) != 2 {
		t.Errorf("expected 2 perms for alice, got %d", len(alicePerms))
	}
}

// TestACL_ExportLoadSnapshot 验证 ACL 快照导出和加载
func TestACL_ExportLoadSnapshot(t *testing.T) {
	acl := permission.NewAccessControlList()
	acl.SetMode(permission.ModeBlacklist)
	acl.Add("baduser1", "spam")
	acl.Add("baduser2", "abuse")

	mode, list, notes := acl.ExportSnapshot()
	if mode != int(permission.ModeBlacklist) {
		t.Errorf("expected blacklist mode, got %d", mode)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 users in ACL, got %d", len(list))
	}
	if notes["baduser1"] != "spam" {
		t.Errorf("expected note 'spam' for baduser1, got %q", notes["baduser1"])
	}

	// 加载到新的 ACL
	acl2 := permission.NewAccessControlList()
	acl2.LoadSnapshot(mode, list, notes)

	allowed, reason := acl2.IsAllowed("baduser1")
	if allowed {
		t.Errorf("baduser1 should be blocked after ACL load, reason: %s", reason)
	}

	allowed2, _ := acl2.IsAllowed("gooduser")
	if !allowed2 {
		t.Error("gooduser should be allowed (not in blacklist)")
	}
}
