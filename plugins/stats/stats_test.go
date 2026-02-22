package stats_test

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/stats"
)

func newStatsPlugin(t *testing.T) *stats.Plugin {
	t.Helper()
	p, desc := stats.NewPlugin()
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)
	if err := pm.RegisterV2(desc); err != nil {
		t.Fatalf("register: %v", err)
	}
	return p
}
func makeCtxWithCommand(cmd, userID string) *context.Context {
	detail, _ := json.Marshal(dto.C2CMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			Content: cmd,
			Author:  dto.Author{UserOpenID: userID},
		},
	})
	return context.NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, nil)
}
func TestStats_Middleware_RecordsCommand(t *testing.T) {
	p := newStatsPlugin(t)
	mw := p.Middleware()
	noop := func(ctx *context.Context) error { return nil }
	handler := mw(noop)
	ctx := makeCtxWithCommand("/help", "user1")
	handler(ctx)
	handler(ctx)
	if p.CommandCount("/help") != 2 {
		t.Errorf("expected 2, got %d", p.CommandCount("/help"))
	}
}
func TestStats_Middleware_RecordsUser(t *testing.T) {
	p := newStatsPlugin(t)
	mw := p.Middleware()
	noop := func(ctx *context.Context) error { return nil }
	handler := mw(noop)
	handler(makeCtxWithCommand("hello", "stats_u1"))
	handler(makeCtxWithCommand("world", "stats_u2"))
	active := p.ActiveUsers(stats.AllTime)
	if len(active) < 2 {
		t.Errorf("expected >= 2 active users, got %d", len(active))
	}
}
func TestStats_TopCommands(t *testing.T) {
	p := newStatsPlugin(t)
	p.RecordCommand("/ping")
	p.RecordCommand("/ping")
	p.RecordCommand("/ping")
	p.RecordCommand("/help")
	p.RecordCommand("/help")
	top := p.TopCommands(2)
	if len(top) != 2 {
		t.Fatalf("expected 2, got %d", len(top))
	}
	if top[0].Command != "/ping" || top[0].Count != 3 {
		t.Errorf("expected /ping(3), got %+v", top[0])
	}
}
func TestStats_TotalMessages(t *testing.T) {
	p := newStatsPlugin(t)
	mw := p.Middleware()
	handler := mw(func(ctx *context.Context) error { return nil })
	for range 5 {
		handler(makeCtxWithCommand("msg", "u"))
	}
	if p.TotalMessages() != 5 {
		t.Errorf("expected 5, got %d", p.TotalMessages())
	}
}
func TestStats_Reset(t *testing.T) {
	p := newStatsPlugin(t)
	p.RecordCommand("/x")
	p.Reset()
	if p.CommandCount("/x") != 0 {
		t.Error("expected 0 after reset")
	}
}
