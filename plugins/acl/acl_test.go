package acl_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/plugins/acl"
)

func TestACL_Disabled(t *testing.T) {
	p := acl.NewPlugin()
	p.Add("user1", "blocked")

	// 默认 disabled 模式，所有用户放行
	if !p.IsAllowed("user1") {
		t.Fatal("disabled mode should allow all users")
	}
	if !p.IsAllowed("unknown") {
		t.Fatal("disabled mode should allow all users")
	}
}

func TestACL_Blacklist(t *testing.T) {
	p := acl.NewPlugin()
	p.SetMode(acl.ModeBlacklist)
	p.Add("baduser", "")

	if p.IsAllowed("baduser") {
		t.Fatal("blacklisted user should be blocked")
	}
	if !p.IsAllowed("gooduser") {
		t.Fatal("non-blacklisted user should be allowed")
	}
}

func TestACL_Whitelist(t *testing.T) {
	p := acl.NewPlugin()
	p.SetMode(acl.ModeWhitelist)
	p.Add("vip", "")

	if !p.IsAllowed("vip") {
		t.Fatal("whitelisted user should be allowed")
	}
	if p.IsAllowed("stranger") {
		t.Fatal("non-whitelisted user should be blocked")
	}
}

func TestACL_RemoveAndClear(t *testing.T) {
	p := acl.NewPlugin()
	p.Add("user1", "")
	p.Add("user2", "")

	p.Remove("user1")
	if p.Count() != 1 {
		t.Fatalf("expected 1 entry, got %d", p.Count())
	}

	p.Clear()
	if p.Count() != 0 {
		t.Fatal("after clear, count should be 0")
	}
}

func TestACL_ParseMode(t *testing.T) {
	tests := []struct {
		input string
		want  acl.Mode
		err   bool
	}{
		{"disabled", acl.ModeDisabled, false},
		{"blacklist", acl.ModeBlacklist, false},
		{"whitelist", acl.ModeWhitelist, false},
		{"off", acl.ModeDisabled, false},
		{"invalid", acl.ModeDisabled, true},
	}
	for _, tc := range tests {
		got, err := acl.ParseMode(tc.input)
		if (err != nil) != tc.err {
			t.Errorf("ParseMode(%q): unexpected error state: %v", tc.input, err)
		}
		if !tc.err && got != tc.want {
			t.Errorf("ParseMode(%q): got %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestACL_Descriptor(t *testing.T) {
	desc := acl.New()
	if desc == nil || desc.Name != "acl" {
		t.Fatal("invalid descriptor")
	}
}
