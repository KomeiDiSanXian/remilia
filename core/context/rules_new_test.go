package context

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ---- mockPermissionChecker ------------------------------------------------

type mockPermChecker struct {
	allow bool
}

func (m *mockPermChecker) HasPermissionEx(_, _, _ string) bool {
	return m.allow
}

// ---- mockBannedChecker ---------------------------------------------------

type mockBannedChecker struct {
	banned map[string]bool
}

func (m *mockBannedChecker) IsBanned(userID string) bool {
	return m.banned[userID]
}

// ---- helpers -------------------------------------------------------------

func makeGroupAtContext(groupID string) *Context {
	return AcquireContextFromEvent(newMockGroupEvent(groupID), nil)
}

func makeC2CContextWithUserID(userID string) *Context {
	ctx := AcquireContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
	ctx.SetUserID(userID)
	return ctx
}

// ---- InGroup tests -------------------------------------------------------

func TestInGroup_MatchingGroup(t *testing.T) {
	rule := InGroup("grp-001", "grp-002")
	ctx := makeGroupAtContext("grp-001")
	if !rule(ctx) {
		t.Error("InGroup should match when group is in the allowed list")
	}
}

func TestInGroup_NonMatchingGroup(t *testing.T) {
	rule := InGroup("grp-001")
	ctx := makeGroupAtContext("grp-999")
	if rule(ctx) {
		t.Error("InGroup should not match when group is not in the allowed list")
	}
}

func TestInGroup_EmptyList_AlwaysFalse(t *testing.T) {
	rule := InGroup()
	ctx := makeGroupAtContext("grp-001")
	if rule(ctx) {
		t.Error("InGroup with empty list should always return false")
	}
}

func TestInGroup_WrongEventType_ReturnsFalse(t *testing.T) {
	rule := InGroup("grp-001")
	// C2C message �?cannot decode as GroupAtMessageCreateEvent
	ctx := AcquireContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
	if rule(ctx) {
		t.Error("InGroup should return false for non-group event types")
	}
}

// ---- HasPermission tests --------------------------------------------------

func TestHasPermission_Allowed(t *testing.T) {
	checker := &mockPermChecker{allow: true}
	rule := HasPermission(checker, "post", "create")
	ctx := makeC2CContextWithUserID("alice")
	if !rule(ctx) {
		t.Error("HasPermission should return true when checker allows")
	}
}

func TestHasPermission_Denied(t *testing.T) {
	checker := &mockPermChecker{allow: false}
	rule := HasPermission(checker, "post", "delete")
	ctx := makeC2CContextWithUserID("bob")
	if rule(ctx) {
		t.Error("HasPermission should return false when checker denies")
	}
}

func TestHasPermission_EmptyUserID_ReturnsFalse(t *testing.T) {
	checker := &mockPermChecker{allow: true}
	rule := HasPermission(checker, "post", "create")
	ctx := AcquireContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
	// No user ID set
	if rule(ctx) {
		t.Error("HasPermission should return false when userID is empty")
	}
}

// ---- NotBanned tests ------------------------------------------------------

func TestNotBanned_NotInBanList(t *testing.T) {
	checker := &mockBannedChecker{banned: map[string]bool{"badguy": true}}
	rule := NotBanned(checker)
	ctx := makeC2CContextWithUserID("goodguy")
	if !rule(ctx) {
		t.Error("NotBanned should return true for users not in ban list")
	}
}

func TestNotBanned_Banned(t *testing.T) {
	checker := &mockBannedChecker{banned: map[string]bool{"badguy": true}}
	rule := NotBanned(checker)
	ctx := makeC2CContextWithUserID("badguy")
	if rule(ctx) {
		t.Error("NotBanned should return false for banned users")
	}
}

func TestNotBanned_EmptyUserID_Allows(t *testing.T) {
	checker := &mockBannedChecker{banned: map[string]bool{"": true}}
	rule := NotBanned(checker)
	ctx := AcquireContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
	// Empty userID �?passthrough
	if !rule(ctx) {
		t.Error("NotBanned should allow when userID is empty (cannot determine identity)")
	}
}
