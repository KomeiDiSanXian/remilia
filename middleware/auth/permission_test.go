package auth

import (
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

type flexibleTestEvent struct {
	platform string
	kind     platform.EventKind
	content  string
	id       string
	chat     platform.ChatInfo
	sender   platform.UserInfo
}

func (e *flexibleTestEvent) Platform() string         { return e.platform }
func (e *flexibleTestEvent) Kind() platform.EventKind { return e.kind }
func (e *flexibleTestEvent) RawType() string          { return string(e.kind) }
func (e *flexibleTestEvent) Content() string          { return e.content }

func (e *flexibleTestEvent) Segments() []platform.Segment {
	if e.content == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: e.content}}
}
func (e *flexibleTestEvent) Chat() platform.ChatInfo            { return e.chat }
func (e *flexibleTestEvent) Sender() platform.UserInfo          { return e.sender }
func (e *flexibleTestEvent) Timestamp() time.Time               { return time.Time{} }
func (e *flexibleTestEvent) ID() string                         { return e.id }
func (e *flexibleTestEvent) RawPayload() any                    { return nil }
func (e *flexibleTestEvent) Attachments() []platform.Attachment { return nil }

func createPermCtx(senderID string, isGroup bool) *eventctx.Context {
	event := &flexibleTestEvent{
		platform: "test",
		kind:     platform.EventKindPrivateMessage,
		content:  "test",
		id:       "perm-test",
		chat:     platform.ChatInfo{ID: "chat-001", IsGroup: isGroup},
		sender:   platform.UserInfo{ID: senderID},
	}
	return eventctx.NewContextFromEvent(event, &platform.NoopSender{})
}

func createPermCtxWithPM(senderID string) *eventctx.Context {
	ctx := createPermCtx(senderID, false)
	pm := permission.NewPermissionManager()
	ctx.SetPermissionManager(pm)
	return ctx
}

// ── RequireRole ──────────────────────────────────────────────────────────

func TestRequireRole(t *testing.T) {
	t.Run("allows when role matches", func(t *testing.T) {
		ctx := createPermCtxWithPM("user-001")
		pm := ctx.GetPermissionManager()
		assert.NoError(t, pm.AssignRole("user-001", "admin"))

		called := false
		mw := RequireRole("admin")
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("blocks when role does not match", func(t *testing.T) {
		ctx := createPermCtxWithPM("user-001")
		pm := ctx.GetPermissionManager()
		assert.NoError(t, pm.AssignRole("user-001", "user"))

		called := false
		mw := RequireRole("admin")
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})

	t.Run("blocks when no permission manager", func(t *testing.T) {
		ctx := createPermCtx("user-001", false)

		called := false
		mw := RequireRole("admin")
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})
}

// ── RequirePermission ────────────────────────────────────────────────────

func TestRequirePermission(t *testing.T) {
	t.Run("allows when permission granted", func(t *testing.T) {
		ctx := createPermCtxWithPM("user-001")
		pm := ctx.GetPermissionManager()
		pm.GrantPermission("user-001", permission.Permission{Resource: "admin", Action: "kick"})

		called := false
		mw := RequirePermission("admin", "kick")
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("blocks when permission not granted", func(t *testing.T) {
		ctx := createPermCtxWithPM("user-001")

		called := false
		mw := RequirePermission("admin", "kick")
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})

	t.Run("blocks when no permission manager", func(t *testing.T) {
		ctx := createPermCtx("user-001", false)

		called := false
		mw := RequirePermission("admin", "kick")
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})
}

// ── RequireAdmin ─────────────────────────────────────────────────────────

func TestRequireAdmin(t *testing.T) {
	t.Run("allows admin user", func(t *testing.T) {
		ctx := createPermCtxWithPM("user-001")
		pm := ctx.GetPermissionManager()
		assert.NoError(t, pm.AssignRole("user-001", "admin"))

		called := false
		mw := RequireAdmin()
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("blocks non-admin user", func(t *testing.T) {
		ctx := createPermCtxWithPM("user-001")
		pm := ctx.GetPermissionManager()
		assert.NoError(t, pm.AssignRole("user-001", "user"))

		called := false
		mw := RequireAdmin()
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})

	t.Run("blocks when no permission manager", func(t *testing.T) {
		ctx := createPermCtx("user-001", false)

		called := false
		mw := RequireAdmin()
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})
}

// ── RequireSuperUser ─────────────────────────────────────────────────────

func TestRequireSuperUser(t *testing.T) {
	t.Run("allows user in super user list", func(t *testing.T) {
		ctx := createPermCtx("super-001", false)

		called := false
		mw := RequireSuperUser("super-001", "super-002")
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("allows user in list with multiple entries", func(t *testing.T) {
		ctx := createPermCtx("super-002", false)

		called := false
		mw := RequireSuperUser("super-001", "super-002", "super-003")
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("blocks user not in list", func(t *testing.T) {
		ctx := createPermCtx("unknown-user", false)

		called := false
		mw := RequireSuperUser("super-001", "super-002")
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})

	t.Run("blocks everyone with empty list", func(t *testing.T) {
		ctx := createPermCtx("any-user", false)

		called := false
		mw := RequireSuperUser()
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})
}

// ── RequireGroup ─────────────────────────────────────────────────────────

func TestRequireGroup(t *testing.T) {
	t.Run("allows group chat", func(t *testing.T) {
		ctx := createPermCtx("user-001", true)

		called := false
		mw := RequireGroup()
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("blocks private chat", func(t *testing.T) {
		ctx := createPermCtx("user-001", false)

		called := false
		mw := RequireGroup()
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})
}

// ── RequirePrivate ───────────────────────────────────────────────────────

func TestRequirePrivate(t *testing.T) {
	t.Run("allows private chat", func(t *testing.T) {
		ctx := createPermCtx("user-001", false)

		called := false
		mw := RequirePrivate()
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("blocks group chat", func(t *testing.T) {
		ctx := createPermCtx("user-001", true)

		called := false
		mw := RequirePrivate()
		handler := mw(func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		err := handler(ctx)
		assert.NoError(t, err)
		assert.False(t, called)
	})
}
