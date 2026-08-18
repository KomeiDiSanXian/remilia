package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/scheduler"
	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer 构造带 Engine + FSM + Scheduler 的测试服务，返回路由 mux 与 API key。
func newTestServer(t *testing.T, configure func(eng *engine.Engine, fsmMgr *fsm.Manager, pm *plugin.Manager)) http.Handler {
	t.Helper()
	eng := engine.NewEngine()
	fsmMgr := fsm.NewManager(nil)
	pm := plugin.NewManager(eng)

	sched := scheduler.NewPlugin()
	require.NoError(t, pm.Register(sched.Descriptor()))

	if configure != nil {
		configure(eng, fsmMgr, pm)
	}

	deps := Deps{Engine: eng, FSMMgr: fsmMgr, PluginMgr: pm}
	srv := NewServer(":0", "test-key", deps)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	return mux
}

func getJSON(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer test-key")
	h.ServeHTTP(w, r)
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	return w.Code, resp
}

func TestEndpoint_MatcherGroups(t *testing.T) {
	h := newTestServer(t, func(eng *engine.Engine, _ *fsm.Manager, _ *plugin.Manager) {
		m := eng.On(string(platform.EventKindPrivateMessage))
		m.Handle(func(c *corectx.Context) error { return nil })
		eng.SetMatcherGroup(m, "grp-a", "test")
		m2 := eng.On(string(platform.EventKindGroupMessage))
		m2.Handle(func(c *corectx.Context) error { return nil })
		eng.SetMatcherGroup(m2, "grp-a", "test")
	})

	code, resp := getJSON(t, h, "/api/v1/engine/matchers/groups")
	assert.Equal(t, 200, code)
	assert.Equal(t, float64(0), resp["code"])
	data := resp["data"].(map[string]any)
	assert.Equal(t, float64(1), data["count"])
	groups := data["groups"].([]any)
	first := groups[0].(map[string]any)
	assert.Equal(t, "grp-a", first["name"])
	assert.Equal(t, float64(2), first["count"])
	assert.Equal(t, true, first["enabled"])
}

func TestEndpoint_MatcherGroups_AuthRequired(t *testing.T) {
	h := newTestServer(t, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/engine/matchers/groups", nil)
	h.ServeHTTP(w, r)
	assert.Equal(t, 401, w.Code)
}

func TestEndpoint_SchedulerJobs(t *testing.T) {
	h := newTestServer(t, func(_ *engine.Engine, _ *fsm.Manager, pm *plugin.Manager) {
		inst, ok := pm.Get("scheduler")
		require.True(t, ok)
		sched := inst.GetAPI().(*scheduler.Plugin)
		sched.EveryNamed("cleanup", time.Hour, func() {})
	})

	code, resp := getJSON(t, h, "/api/v1/scheduler/jobs")
	assert.Equal(t, 200, code)
	assert.Equal(t, float64(0), resp["code"])
	data := resp["data"].(map[string]any)
	assert.Equal(t, float64(1), data["count"])
	jobs := data["jobs"].([]any)
	job := jobs[0].(map[string]any)
	assert.Equal(t, "cleanup", job["name"])
	assert.Equal(t, "cron", job["kind"])
}

func TestEndpoint_FSMSessions(t *testing.T) {
	h := newTestServer(t, func(_ *engine.Engine, mgr *fsm.Manager, _ *plugin.Manager) {
		require.NoError(t, mgr.Register(&fsm.FSMDescriptor{
			Name: "demo",
			FSM: &fsm.FSM{
				Name: "demo", Initial: "s",
				Events: []fsm.Event{{Name: "e", From: "s", To: "d", Match: func(c *corectx.Context) bool { return true }}},
			},
		}))
		evt := &testAPIEvent{platform: "test", kind: platform.EventKindPrivateMessage, id: "evt-1"}
		require.NoError(t, mgr.Engine().StartSession(corectx.NewContextFromEvent(evt, &platform.NoopSender{}), "demo", "sess-1"))
	})

	code, resp := getJSON(t, h, "/api/v1/fsm/sessions")
	assert.Equal(t, 200, code)
	assert.Equal(t, float64(0), resp["code"])
	data := resp["data"].(map[string]any)
	assert.Equal(t, float64(1), data["count"])
	sessions := data["sessions"].([]any)
	sess := sessions[0].(map[string]any)
	assert.Equal(t, "sess-1", sess["id"])
	assert.Equal(t, "demo", sess["fsm_name"])
	assert.Equal(t, "s", sess["current"])
}

// testAPIEvent 构造一个满足 platform.Event 的最小事件。
type testAPIEvent struct {
	platform string
	kind     platform.EventKind
	id       string
}

func (e *testAPIEvent) Platform() string             { return e.platform }
func (e *testAPIEvent) ID() string                   { return e.id }
func (e *testAPIEvent) Kind() platform.EventKind     { return e.kind }
func (e *testAPIEvent) Timestamp() time.Time         { return time.Time{} }
func (e *testAPIEvent) Sender() platform.UserInfo    { return platform.UserInfo{ID: "user1"} }
func (e *testAPIEvent) Chat() platform.ChatInfo      { return platform.ChatInfo{ID: "chat1"} }
func (e *testAPIEvent) Segments() []platform.Segment { return nil }
