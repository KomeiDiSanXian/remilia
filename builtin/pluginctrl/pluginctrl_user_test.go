package pluginctrl_test

// pluginctrl_user_test.go — P1 新功能测试：用户级禁用 / PluginPolicy / RuleFull / Middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KomeiDiSanXian/remilia/builtin/pluginctrl"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─── 测试事件桩 ────────────────────────────────────────────────────────────────

type pcTestEvent struct {
	groupID string
	userID  string
	isGroup bool
	content string
}

func (e *pcTestEvent) Platform() string { return "test" }
func (e *pcTestEvent) Kind() platform.EventKind {
	if e.isGroup {
		return platform.EventKindGroupMessage
	}
	return platform.EventKindPrivateMessage
}
func (e *pcTestEvent) RawType() string { return string(e.Kind()) }

func (e *pcTestEvent) Segments() []platform.Segment {
	if e.content == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: e.content}}
}
func (e *pcTestEvent) ID() string           { return "test-event-id" }
func (e *pcTestEvent) Timestamp() time.Time { return time.Time{} }
func (e *pcTestEvent) RawPayload() any      { return nil }
func (e *pcTestEvent) Chat() platform.ChatInfo {
	return platform.ChatInfo{ID: e.groupID, IsGroup: e.isGroup}
}
func (e *pcTestEvent) Sender() platform.UserInfo { return platform.UserInfo{ID: e.userID} }

func newPCCtx(groupID, userID string) *eventctx.Context {
	return eventctx.NewContextFromEvent(
		&pcTestEvent{groupID: groupID, userID: userID, isGroup: true},
		&platform.NoopSender{},
	)
}

func newPCPrivateCtx(userID string) *eventctx.Context {
	return eventctx.NewContextFromEvent(
		&pcTestEvent{userID: userID, isGroup: false},
		&platform.NoopSender{},
	)
}

// ─── 用户级开关 ────────────────────────────────────────────────────────────────

func TestIsUserEnabled_Default(t *testing.T) {
	p := newTestPlugin()
	assert.True(t, p.IsUserEnabled("any-user", "weather"))
}

func TestSetUserEnabled_Disable(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetUserEnabled("bad-user", "weather", false))
	assert.False(t, p.IsUserEnabled("bad-user", "weather"))
	assert.True(t, p.IsUserEnabled("good-user", "weather"))
}

func TestSetUserEnabled_ReEnable(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetUserEnabled("u1", "weather", false))
	require.NoError(t, p.SetUserEnabled("u1", "weather", true))
	assert.True(t, p.IsUserEnabled("u1", "weather"))
}

func TestSetUserEnabled_IndependentPlugins(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetUserEnabled("u1", "weather", false))
	assert.True(t, p.IsUserEnabled("u1", "news"))
}

func TestUserList(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetUserEnabled("u1", "weather", false))
	require.NoError(t, p.SetUserEnabled("u1", "news", true))
	assert.Len(t, p.UserList("u1"), 2)
	assert.Nil(t, p.UserList("unknown-user"))
}

// ─── 用户级持久化 ──────────────────────────────────────────────────────────────

func TestUserStatePersistence(t *testing.T) {
	db, err := storage.NewMemory()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&pluginctrl.GroupPluginState{}, &pluginctrl.UserPluginState{}))

	p := newTestPluginWithStorage(db)
	require.NoError(t, p.SetUserEnabled("bad-user", "weather", false))

	p2 := newTestPluginWithStorage(db)
	p2.LoadFromDB()
	assert.False(t, p2.IsUserEnabled("bad-user", "weather"))
	assert.True(t, p2.IsUserEnabled("other-user", "weather"))
}

// ─── RegisterPolicy / GetPolicy ───────────────────────────────────────────────

func TestRegisterPolicy_Chainable(t *testing.T) {
	p := newTestPlugin()
	p.RegisterPolicy("weather", pluginctrl.PluginPolicy{UserLimit: 10 * time.Second}).
		RegisterPolicy("news", pluginctrl.PluginPolicy{GroupLimit: time.Minute})

	pol, ok := p.GetPolicy("weather")
	require.True(t, ok)
	assert.Equal(t, 10*time.Second, pol.UserLimit)

	pol2, ok := p.GetPolicy("news")
	require.True(t, ok)
	assert.Equal(t, time.Minute, pol2.GroupLimit)
}

func TestGetPolicy_Missing(t *testing.T) {
	p := newTestPlugin()
	_, ok := p.GetPolicy("nonexistent")
	assert.False(t, ok)
}

func TestRegisterPolicy_Overwrite(t *testing.T) {
	p := newTestPlugin()
	p.RegisterPolicy("cmd", pluginctrl.PluginPolicy{UserLimit: 5 * time.Second})
	p.RegisterPolicy("cmd", pluginctrl.PluginPolicy{UserLimit: 10 * time.Second})
	pol, _ := p.GetPolicy("cmd")
	assert.Equal(t, 10*time.Second, pol.UserLimit)
}

// ─── RuleFull ─────────────────────────────────────────────────────────────────

func TestRuleFull_GroupDisabled(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetGroupEnabled("g1", "weather", false))
	rule := p.RuleFull("weather")

	assert.False(t, rule(newPCCtx("g1", "user1")))
	assert.True(t, rule(newPCCtx("g2", "user1")))
}

func TestRuleFull_UserDisabled(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetUserEnabled("bad-user", "weather", false))
	rule := p.RuleFull("weather")

	assert.False(t, rule(newPCCtx("g1", "bad-user")))
	assert.True(t, rule(newPCCtx("g1", "good-user")))
}

func TestRuleFull_UserDisabledOverridesGroupEnabled(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetGroupEnabled("g1", "weather", true))
	require.NoError(t, p.SetUserEnabled("bad-user", "weather", false))
	assert.False(t, p.RuleFull("weather")(newPCCtx("g1", "bad-user")))
}

func TestRuleFull_PrivateMessage_UserCheckStillApplies(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetUserEnabled("bad-user", "weather", false))
	rule := p.RuleFull("weather")
	// 私聊中用户级禁用仍然生效
	assert.False(t, rule(newPCPrivateCtx("bad-user")))
	// 私聊中正常用户放行
	assert.True(t, rule(newPCPrivateCtx("normal-user")))
}

// ─── Middleware ───────────────────────────────────────────────────────────────

func TestMiddleware_GroupDisabled(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetGroupEnabled("g1", "weather", false))

	called := false
	handler := p.Middleware("weather")(func(_ *eventctx.Context) error { called = true; return nil })

	require.NoError(t, handler(newPCCtx("g1", "user1")))
	assert.False(t, called)
}

func TestMiddleware_UserDisabled(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetUserEnabled("bad-user", "weather", false))

	called := false
	handler := p.Middleware("weather")(func(_ *eventctx.Context) error { called = true; return nil })

	require.NoError(t, handler(newPCCtx("g1", "bad-user")))
	assert.False(t, called)
}

func TestMiddleware_PassThrough(t *testing.T) {
	p := newTestPlugin()
	called := false
	handler := p.Middleware("weather")(func(_ *eventctx.Context) error { called = true; return nil })

	require.NoError(t, handler(newPCCtx("g1", "user1")))
	assert.True(t, called)
}

func TestMiddleware_WithCooldown_UserLimit(t *testing.T) {
	p := newTestPlugin()
	p.RegisterPolicy("sign", pluginctrl.PluginPolicy{UserLimit: time.Minute})

	count := 0
	handler := p.Middleware("sign")(func(_ *eventctx.Context) error { count++; return nil })

	require.NoError(t, handler(newPCCtx("g1", "user1")))
	assert.Equal(t, 1, count)

	// 同用户第二次：冷却中
	require.NoError(t, handler(newPCCtx("g1", "user1")))
	assert.Equal(t, 1, count, "should be blocked by user cooldown")

	// 不同用户不受影响
	require.NoError(t, handler(newPCCtx("g1", "user2")))
	assert.Equal(t, 2, count)
}

func TestMiddleware_WithCooldown_GroupLimit(t *testing.T) {
	p := newTestPlugin()
	p.RegisterPolicy("news", pluginctrl.PluginPolicy{GroupLimit: time.Minute})

	count := 0
	handler := p.Middleware("news")(func(_ *eventctx.Context) error { count++; return nil })

	require.NoError(t, handler(newPCCtx("g1", "user1")))
	assert.Equal(t, 1, count)

	// 同群不同用户：群冷却中
	require.NoError(t, handler(newPCCtx("g1", "user2")))
	assert.Equal(t, 1, count, "second user in same group should be blocked by group cooldown")

	// 不同群放行
	require.NoError(t, handler(newPCCtx("g2", "user1")))
	assert.Equal(t, 2, count)
}

func TestMiddleware_NoPolicyRegistered(t *testing.T) {
	p := newTestPlugin()
	count := 0
	handler := p.Middleware("weather")(func(_ *eventctx.Context) error { count++; return nil })

	require.NoError(t, handler(newPCCtx("g1", "user1")))
	require.NoError(t, handler(newPCCtx("g1", "user1")))
	assert.Equal(t, 2, count, "no policy → no cooldown, both calls succeed")
}

func TestMiddleware_UserDisable_PrecedesGroupEnable(t *testing.T) {
	p := newTestPlugin()
	require.NoError(t, p.SetGroupEnabled("g1", "cmd", true))
	require.NoError(t, p.SetUserEnabled("bad-user", "cmd", false))

	called := false
	handler := p.Middleware("cmd")(func(_ *eventctx.Context) error { called = true; return nil })

	_ = handler(newPCCtx("g1", "bad-user"))
	assert.False(t, called)
}

// ─── WithUserCommands ─────────────────────────────────────────────────────────

func TestWithUserCommands(t *testing.T) {
	d := pluginctrl.New(
		pluginctrl.WithSuperUsers("admin"),
		pluginctrl.WithUserCommands("ban", "unban"),
	)
	assert.Equal(t, "pluginctrl", d.Name)
}
