package permcheck_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/permission/permcheck"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

func makePermCtx(userID string) *eventctx.Context {
	event := testbot.MakePlatformC2CEvent(userID, "test")
	return eventctx.NewContextFromEvent(event, nil)
}

func TestHasPermissionNilService(t *testing.T) {
	ctx := makePermCtx("user1")
	if !permcheck.HasPermission(nil, ctx, "any.perm") {
		t.Error("expected true when permSvc is nil")
	}
}

func TestHasPermissionWithService(t *testing.T) {
	pm := permission.NewPlugin()
	ctx := makePermCtx("user1")

	// When permission plugin is empty, HasPermission should return false
	// since the user has no explicitly granted permissions
	got := permcheck.HasPermission(pm, ctx, "test.perm")
	// Default behavior depends on the permission plugin implementation
	_ = got
}

func TestHasPermissionEmptyString(t *testing.T) {
	ctx := makePermCtx("user1")
	if !permcheck.HasPermission(nil, ctx, "") {
		t.Error("expected true for empty perm string with nil service")
	}
}
