package fsm

import (
	"testing"
	"testing/synctest"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testEvent struct {
	platform string
	content  string
	chatID   string
	kind     platform.EventKind
}

func (e *testEvent) Platform() string          { return e.platform }
func (e *testEvent) ID() string                { return "" }
func (e *testEvent) Kind() platform.EventKind  { return e.kind }
func (e *testEvent) Sender() platform.UserInfo { return platform.UserInfo{ID: "user1"} }
func (e *testEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{ID: e.chatID} }

func (e *testEvent) Segments() []platform.Segment {
	if e.content == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: e.content}}
}
func (e *testEvent) Timestamp() time.Time { return time.Time{} }

func newTestContext(content string) *corectx.Context {
	evt := &testEvent{
		platform: "test",
		content:  content,
		chatID:   "test_chat",
		kind:     platform.EventKindPrivateMessage,
	}
	return corectx.NewContextFromEvent(evt, &platform.NoopSender{})
}

func TestEngine_Register_ValidFSM(t *testing.T) {
	eng := NewEngine(nil)
	fsm := &FSM{
		Name:    "test",
		Initial: "start",
		Events: []Event{
			{Name: "next", From: "start", To: "done", Match: func(ctx *corectx.Context) bool { return true }},
		},
	}
	err := eng.Register(fsm)
	assert.NoError(t, err)
}

func TestEngine_Register_NilFSM(t *testing.T) {
	eng := NewEngine(nil)
	err := eng.Register(nil)
	assert.Error(t, err)
}

func TestEngine_Register_Duplicate(t *testing.T) {
	eng := NewEngine(nil)
	fsm := &FSM{
		Name: "dup", Initial: "s",
		Events: []Event{{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }}},
	}
	require.NoError(t, eng.Register(fsm))
	err := eng.Register(fsm)
	assert.Error(t, err)
}

func TestEngine_Register_NoEvents(t *testing.T) {
	eng := NewEngine(nil)
	err := eng.Register(&FSM{Name: "empty", Initial: "s"})
	assert.Error(t, err)
}

func TestEngine_Register_NoMatchFunc(t *testing.T) {
	eng := NewEngine(nil)
	err := eng.Register(&FSM{
		Name: "bad", Initial: "s",
		Events: []Event{{Name: "e", From: "s", To: "d"}},
	})
	assert.Error(t, err)
}

func TestEngine_TryTransition_NoSession(t *testing.T) {
	eng := NewEngine(nil)
	ctx := newTestContext("hello")
	to, ok, err := eng.TryTransition(ctx, "nonexistent")
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, to)
}

func TestEngine_TryTransition_HappyPath(t *testing.T) {
	eng := NewEngine(nil)
	fsm := &FSM{
		Name: "form", Initial: "idle",
		Events: []Event{
			{Name: "start", From: "idle", To: "asking_name", Match: func(ctx *corectx.Context) bool {
				return ctx.GetMessageContent() == "/form"
			}},
			{Name: "provide_name", From: "asking_name", To: "confirming", Match: func(ctx *corectx.Context) bool {
				return ctx.GetMessageContent() != ""
			}},
		},
	}
	require.NoError(t, eng.Register(fsm))

	ctx := newTestContext("/form")
	require.NoError(t, eng.StartSession(ctx, "form", "sess1"))

	to, ok, err := eng.TryTransition(ctx, "sess1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, State("asking_name"), to)
}

func TestEngine_TryTransition_OnEnterOnExit(t *testing.T) {
	eng := NewEngine(nil)

	var entered, exited bool

	fsm := &FSM{
		Name: "track", Initial: "s",
		Events: []Event{
			{Name: "go", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }},
		},
		OnExit: map[State]func(ctx *FSMContext) error{
			"s": func(ctx *FSMContext) error { exited = true; return nil },
		},
		OnEnter: map[State]func(ctx *FSMContext) error{
			"d": func(ctx *FSMContext) error { entered = true; return nil },
		},
	}
	require.NoError(t, eng.Register(fsm))

	ctx := newTestContext("anything")
	require.NoError(t, eng.StartSession(ctx, "track", "sess2"))

	to, ok, err := eng.TryTransition(ctx, "sess2")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, State("d"), to)
	assert.True(t, exited)
	assert.True(t, entered)
}

func TestEngine_TryTransition_OnEnterError_RollsBack(t *testing.T) {
	eng := NewEngine(nil)

	fsm := &FSM{
		Name: "rollback", Initial: "s",
		Events: []Event{
			{Name: "go", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }},
		},
		OnEnter: map[State]func(ctx *FSMContext) error{
			"d": func(ctx *FSMContext) error { return assert.AnError },
		},
	}
	require.NoError(t, eng.Register(fsm))

	ctx := newTestContext("x")
	require.NoError(t, eng.StartSession(ctx, "rollback", "sess3"))

	to, ok, err := eng.TryTransition(ctx, "sess3")
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Equal(t, State("s"), to)

	sess := eng.GetSession("sess3")
	require.NotNil(t, sess)
	assert.Equal(t, State("s"), sess.Current)
}

func TestEngine_TryTransition_WildcardFrom(t *testing.T) {
	eng := NewEngine(nil)

	fsm := &FSM{
		Name: "wild", Initial: "a",
		Events: []Event{
			{Name: "to_b", From: "a", To: "b", Match: func(ctx *corectx.Context) bool { return true }},
			{Name: "back", From: "*", To: "a", Match: func(ctx *corectx.Context) bool { return ctx.GetMessageContent() == "reset" }},
		},
	}
	require.NoError(t, eng.Register(fsm))

	ctx := newTestContext("go")
	require.NoError(t, eng.StartSession(ctx, "wild", "sess4"))

	to, ok, err := eng.TryTransition(ctx, "sess4")
	require.True(t, ok)
	assert.Equal(t, State("b"), to)
	assert.NoError(t, err)

	to, ok, err = eng.TryTransition(newTestContext("reset"), "sess4")
	require.True(t, ok)
	assert.Equal(t, State("a"), to)
	assert.NoError(t, err)
}

func TestEngine_StartSession_RejectsDuplicate(t *testing.T) {
	eng := NewEngine(nil)
	fsm := &FSM{
		Name: "uniq", Initial: "s",
		Events: []Event{{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }}},
	}
	require.NoError(t, eng.Register(fsm))

	ctx := newTestContext("x")
	require.NoError(t, eng.StartSession(ctx, "uniq", "sess5"))
	err := eng.StartSession(ctx, "uniq", "sess5")
	assert.Error(t, err)
}

func TestEngine_EndSession(t *testing.T) {
	eng := NewEngine(nil)
	fsm := &FSM{
		Name: "eps", Initial: "s",
		Events: []Event{{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }}},
	}
	require.NoError(t, eng.Register(fsm))

	ctx := newTestContext("x")
	require.NoError(t, eng.StartSession(ctx, "eps", "sess6"))
	assert.NotNil(t, eng.GetSession("sess6"))

	eng.EndSession("sess6")
	assert.Nil(t, eng.GetSession("sess6"))
}

func TestEngine_ListFSMs(t *testing.T) {
	eng := NewEngine(nil)
	r := func(name string) {
		eng.Register(&FSM{Name: name, Initial: "s", Events: []Event{{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }}}})
	}
	r("a")
	r("b")
	names := eng.ListFSMs()
	assert.ElementsMatch(t, []string{"a", "b"}, names)
}

func TestEngine_GetSession_Nil(t *testing.T) {
	eng := NewEngine(nil)
	assert.Nil(t, eng.GetSession("nonexistent"))
}

func TestEngine_Unregister(t *testing.T) {
	eng := NewEngine(nil)
	fsm := &FSM{
		Name: "gone", Initial: "s",
		Events: []Event{{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }}},
	}
	require.NoError(t, eng.Register(fsm))
	eng.Unregister("gone")
	assert.Nil(t, eng.GetFSM("gone"))
}

func TestEngine_ListSessions(t *testing.T) {
	eng := NewEngine(nil)
	fsm := &FSM{
		Name: "listme", Initial: "s",
		Events: []Event{{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }}},
	}
	require.NoError(t, eng.Register(fsm))

	assert.Empty(t, eng.ListSessions())

	require.NoError(t, eng.StartSession(newTestContext("x"), "listme", "sess-a"))
	require.NoError(t, eng.StartSession(newTestContext("x"), "listme", "sess-b"))

	sessions := eng.ListSessions()
	assert.Len(t, sessions, 2)
	seen := make(map[string]bool)
	for _, s := range sessions {
		seen[s.ID] = true
		assert.Equal(t, "listme", s.FSMName)
		assert.Equal(t, State("s"), s.Current)
	}
	assert.True(t, seen["sess-a"] && seen["sess-b"], "expected both sessions, got %v", seen)

	eng.EndSession("sess-a")
	sessions = eng.ListSessions()
	assert.Len(t, sessions, 1)
	assert.Equal(t, "sess-b", sessions[0].ID)
}

func TestMemoryStorage_Concurrent(t *testing.T) {
	s := NewMemoryStorage()
	done := make(chan struct{})
	go func() {
		for i := range 100 {
			s.Save(&Session{ID: "a", Data: map[string]any{"n": i}})
			s.Get("a")
		}
		done <- struct{}{}
	}()
	go func() {
		for i := range 100 {
			s.Save(&Session{ID: "b", Data: map[string]any{"n": i}})
			s.Get("b")
		}
		done <- struct{}{}
	}()
	go func() {
		for range 50 {
			s.Delete("a")
			s.Delete("b")
		}
		done <- struct{}{}
	}()
	for range 3 {
		<-done
	}
}

func TestMemoryStorage_Cleanup(t *testing.T) {
	s := NewMemoryStorage()
	now := time.Now().Unix()
	s.Save(&Session{ID: "expired", ExpireAt: now - 10})
	s.Save(&Session{ID: "valid", ExpireAt: now + 3600})
	s.Save(&Session{ID: "no_expiry"})

	count := s.Cleanup(now)
	assert.Equal(t, 1, count)
	assert.Nil(t, s.Get("expired"))
	assert.NotNil(t, s.Get("valid"))
	assert.NotNil(t, s.Get("no_expiry"))
}

func TestMemoryStorage_Get_DoesNotReturnExpired(t *testing.T) {
	s := NewMemoryStorage()
	s.Save(&Session{ID: "gone", ExpireAt: time.Now().Unix() - 5})
	assert.Nil(t, s.Get("gone"))
}

func TestFSMDescriptor_Validate(t *testing.T) {
	d := &FSMDescriptor{Name: "test", FSM: &FSM{
		Name: "test", Initial: "s",
		Events: []Event{{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }}},
	}}
	assert.NoError(t, d.Validate())
}

func TestFSMDescriptor_Validate_NoName(t *testing.T) {
	d := &FSMDescriptor{FSM: &FSM{
		Name: "test", Initial: "s",
		Events: []Event{{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }}},
	}}
	assert.ErrorIs(t, d.Validate(), ErrFSMDescriptorNameRequired)
}

func TestFSMDescriptor_Validate_NilFSM(t *testing.T) {
	d := &FSMDescriptor{Name: "test"}
	assert.ErrorIs(t, d.Validate(), ErrFSMDescriptorNilFSM)
}

func TestEngine_Timeout_Session(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eng := NewEngine(nil)
		fsm := &FSM{
			Name: "timed", Initial: "s",
			Events: []Event{
				{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return ctx.GetMessageContent() == "go" }},
			},
			Timeout: 1 * time.Millisecond,
		}
		require.NoError(t, eng.Register(fsm))

		ctx := newTestContext("go")
		require.NoError(t, eng.StartSession(ctx, "timed", "sess_timeout"))

		time.Sleep(5 * time.Millisecond)

		assert.Nil(t, eng.GetSession("sess_timeout"), "session should be expired")
	})
}

func TestEngine_StartCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewMemoryStorage()
		eng := NewEngine(s)
		fsm := &FSM{
			Name: "clean", Initial: "s",
			Events:  []Event{{Name: "e", From: "s", To: "d", Match: func(ctx *corectx.Context) bool { return true }}},
			Timeout: 10 * time.Millisecond,
		}
		require.NoError(t, eng.Register(fsm))

		ctx := newTestContext("x")
		require.NoError(t, eng.StartSession(ctx, "clean", "to_clean"))

		stop := make(chan struct{})
		eng.StartCleanup(5*time.Millisecond, stop)
		time.Sleep(50 * time.Millisecond)
		close(stop)

		assert.Nil(t, eng.GetSession("to_clean"), "cleanup should remove expired session")
	})
}

func TestEngine_FSMContext_Embedded(t *testing.T) {
	eng := NewEngine(nil)
	fsm := &FSM{
		Name: "embed", Initial: "s",
		Events: []Event{
			{Name: "go", From: "s", To: "d", Match: func(ctx *corectx.Context) bool {
				return ctx.GetMessageContent() == "ping"
			}, Action: func(ctx *FSMContext) error {
				ctx.Data["result"] = ctx.GetMessageContent() + "_pong"
				return nil
			}},
		},
	}
	require.NoError(t, eng.Register(fsm))

	ctx := newTestContext("ping")
	require.NoError(t, eng.StartSession(ctx, "embed", "sess7"))

	_, ok, err := eng.TryTransition(ctx, "sess7")
	require.True(t, ok)
	require.NoError(t, err)

	sess := eng.GetSession("sess7")
	require.NotNil(t, sess)
	assert.Equal(t, "ping_pong", sess.Data["result"])
}
