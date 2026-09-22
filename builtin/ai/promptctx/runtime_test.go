package promptctx

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestGroupRoleName(t *testing.T) {
	cases := []struct {
		role platform.GroupRole
		want string
	}{
		{platform.GroupRoleOwner, "群主/所有者"},
		{platform.GroupRoleAdmin, "管理员"},
		{platform.GroupRoleMember, "普通成员"},
		{platform.GroupRoleUnknown, "未知"},
		{platform.GroupRole(99), "未知"},
	}
	for _, c := range cases {
		if got := GroupRoleName(c.role); got != c.want {
			t.Errorf("GroupRoleName(%v) = %q, want %q", c.role, got, c.want)
		}
	}
}
