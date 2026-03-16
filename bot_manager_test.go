package remilia_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	qq "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeBot(t *testing.T) *remilia.Bot {
	t.Helper()
	adapter := qq.SimpleWebhookAdapter(0)
	bot, err := remilia.NewBotBuilder().
		WithAdapter(adapter).
		WithName("test-bot").
		Build()
	require.NoError(t, err)
	return bot
}
func TestBotManager_AddAndGet(t *testing.T) {
	mgr := remilia.NewBotManager()
	bot := makeBot(t)
	require.NoError(t, mgr.Add("alpha", bot))
	assert.Equal(t, 1, mgr.Len())
	got, ok := mgr.Get("alpha")
	assert.True(t, ok)
	assert.Equal(t, bot, got)
}
func TestBotManager_Add_DuplicateName(t *testing.T) {
	mgr := remilia.NewBotManager()
	require.NoError(t, mgr.Add("dup", makeBot(t)))
	err := mgr.Add("dup", makeBot(t))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}
func TestBotManager_Add_NilBot(t *testing.T) {
	mgr := remilia.NewBotManager()
	assert.Error(t, mgr.Add("nil-bot", nil))
}
func TestBotManager_Add_EmptyName(t *testing.T) {
	mgr := remilia.NewBotManager()
	assert.Error(t, mgr.Add("", makeBot(t)))
}
func TestBotManager_MustAdd_Chainable(t *testing.T) {
	mgr := remilia.NewBotManager()
	returned := mgr.MustAdd("a", makeBot(t)).MustAdd("b", makeBot(t))
	assert.Equal(t, mgr, returned)
	assert.Equal(t, 2, mgr.Len())
}
func TestBotManager_MustAdd_PanicsOnDuplicate(t *testing.T) {
	mgr := remilia.NewBotManager()
	mgr.MustAdd("x", makeBot(t))
	assert.Panics(t, func() { mgr.MustAdd("x", makeBot(t)) })
}
func TestBotManager_Get_Missing(t *testing.T) {
	mgr := remilia.NewBotManager()
	_, ok := mgr.Get("nonexistent")
	assert.False(t, ok)
}
func TestBotManager_MustGet_PanicsWhenMissing(t *testing.T) {
	mgr := remilia.NewBotManager()
	assert.Panics(t, func() { mgr.MustGet("ghost") })
}
func TestBotManager_Remove(t *testing.T) {
	mgr := remilia.NewBotManager()
	mgr.MustAdd("rem", makeBot(t))
	assert.True(t, mgr.Remove("rem"))
	assert.Equal(t, 0, mgr.Len())
	assert.False(t, mgr.Remove("rem"))
}
func TestBotManager_Names_PreservesOrder(t *testing.T) {
	mgr := remilia.NewBotManager()
	for _, n := range []string{"c", "a", "b"} {
		mgr.MustAdd(n, makeBot(t))
	}
	assert.Equal(t, []string{"c", "a", "b"}, mgr.Names())
}
func TestBotManager_Remove_UpdatesOrder(t *testing.T) {
	mgr := remilia.NewBotManager()
	mgr.MustAdd("first", makeBot(t))
	mgr.MustAdd("second", makeBot(t))
	mgr.MustAdd("third", makeBot(t))
	mgr.Remove("second")
	assert.Equal(t, []string{"first", "third"}, mgr.Names())
}
func TestBotManager_Status_Empty(t *testing.T) {
	mgr := remilia.NewBotManager()
	s := mgr.Status()
	assert.Equal(t, 0, s.Total)
	assert.Equal(t, 0, s.Running)
}
func TestBotManager_Status_AllStopped(t *testing.T) {
	mgr := remilia.NewBotManager()
	mgr.MustAdd("b1", makeBot(t))
	mgr.MustAdd("b2", makeBot(t))
	s := mgr.Status()
	assert.Equal(t, 2, s.Total)
	assert.Equal(t, 0, s.Running)
	assert.Equal(t, 2, s.Stopped)
	assert.Contains(t, s.Uptime, "b1")
	assert.Contains(t, s.Uptime, "b2")
}
func TestBotManager_RunningBots_Empty(t *testing.T) {
	mgr := remilia.NewBotManager()
	mgr.MustAdd("idle", makeBot(t))
	assert.Empty(t, mgr.RunningBots())
}
func TestBotError_Error(t *testing.T) {
	inner := errors.New("something broke")
	be := remilia.BotError{Name: "mybot", Err: inner}
	assert.Contains(t, be.Error(), "mybot")
	assert.Contains(t, be.Error(), "something broke")
	assert.Equal(t, inner, be.Unwrap())
}
func TestBotManagerError_Error_Single(t *testing.T) {
	err := &remilia.BotManagerError{
		Op:     "StartAll",
		Errors: []remilia.BotError{{Name: "bot1", Err: errors.New("oops")}},
	}
	assert.Contains(t, err.Error(), "StartAll")
	assert.Contains(t, err.Error(), "bot1")
}
func TestBotManagerError_Error_Multiple(t *testing.T) {
	err := &remilia.BotManagerError{
		Op: "StopAll",
		Errors: []remilia.BotError{
			{Name: "b1", Err: errors.New("e1")},
			{Name: "b2", Err: errors.New("e2")},
		},
	}
	assert.Contains(t, err.Error(), "2 bot(s) failed")
	assert.Equal(t, []string{"b1", "b2"}, err.FailedBots())
}
func TestBotManagerBuilder_AddBot(t *testing.T) {
	bot := makeBot(t)
	mgr, err := remilia.NewBotManagerBuilder().
		AddBot("main", bot).
		Build()
	require.NoError(t, err)
	assert.Equal(t, 1, mgr.Len())
	got, ok := mgr.Get("main")
	assert.True(t, ok)
	assert.Equal(t, bot, got)
}
func TestBotManagerBuilder_AddBuilder(t *testing.T) {
	builder := remilia.NewBotBuilder().
		WithAdapter(qq.SimpleWebhookAdapter(0)).
		WithName("built-bot")
	mgr, err := remilia.NewBotManagerBuilder().
		AddBuilder("lazy", builder).
		Build()
	require.NoError(t, err)
	assert.Equal(t, 1, mgr.Len())
}
func TestBotManagerBuilder_AddBuilder_FailsOnBadBuilder(t *testing.T) {
	_, err := remilia.NewBotManagerBuilder().
		AddBuilder("bad", remilia.NewBotBuilder()).
		Build()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad")
}
func TestBotManagerBuilder_MixedEntries(t *testing.T) {
	bot := makeBot(t)
	builder := remilia.NewBotBuilder().WithAdapter(qq.SimpleWebhookAdapter(0))
	mgr, err := remilia.NewBotManagerBuilder().
		AddBot("direct", bot).
		AddBuilder("from-builder", builder).
		Build()
	require.NoError(t, err)
	assert.Equal(t, 2, mgr.Len())
	assert.Equal(t, []string{"direct", "from-builder"}, mgr.Names())
}
func TestBotManagerBuilder_DuplicateName(t *testing.T) {
	_, err := remilia.NewBotManagerBuilder().
		AddBot("same", makeBot(t)).
		AddBot("same", makeBot(t)).
		Build()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}
func TestBotManagerBuilder_MustBuild_Panics(t *testing.T) {
	b := remilia.NewBotManagerBuilder().AddBuilder("x", remilia.NewBotBuilder())
	assert.Panics(t, func() { b.MustBuild() })
}
func TestBotManager_StartAll_Empty(t *testing.T) {
	mgr := remilia.NewBotManager()
	assert.NoError(t, mgr.StartAll(context.Background()))
}
func TestBotManager_StopAll_NoneRunning(t *testing.T) {
	mgr := remilia.NewBotManager()
	mgr.MustAdd("idle", makeBot(t))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NoError(t, mgr.StopAll(ctx))
}
func TestBotManager_HealthAll(t *testing.T) {
	mgr := remilia.NewBotManager()
	mgr.MustAdd("h1", makeBot(t))
	mgr.MustAdd("h2", makeBot(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := mgr.HealthAll(ctx)
	assert.Len(t, results, 2)
	for _, name := range []string{"h1", "h2"} {
		r, ok := results[name]
		assert.True(t, ok, "missing result for %s", name)
		assert.Equal(t, name, r.Name)
		assert.False(t, r.IsRunning)
	}
}
