package stats_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/stats"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

// stats.Plugin 无 Setup 初始化逻辑，直接 NewPlugin() 即可，无需走 manager 注册流程。
func newStatsPlugin() *stats.Plugin {
	return stats.NewPlugin()
}

// makeCtxWithCommandPlatform 使用平台无关路径创建测试 Context（推荐）
func makeCtxWithCommandPlatform(cmd, userID string) *context.Context {
	event := testbot.MakePlatformC2CEvent(userID, cmd)
	return context.NewContextFromEvent(event, nil)
}

// makeCtxWithCommand 使用平台无关路径创建测试 Context
func makeCtxWithCommand(cmd, userID string) *context.Context {
	event := testbot.MakePlatformC2CEvent(userID, cmd)
	return context.NewContextFromEvent(event, nil)
}

func TestStats_Middleware_RecordsCommand(t *testing.T) {
	p := newStatsPlugin()
	handler := p.Middleware()(func(ctx *context.Context) error { return nil })
	ctx := makeCtxWithCommandPlatform("/help", "user1")
	handler(ctx)
	handler(ctx)
	if p.CommandCount("/help") != 2 {
		t.Errorf("expected 2, got %d", p.CommandCount("/help"))
	}
}

func TestStats_Middleware_RecordsUser(t *testing.T) {
	p := newStatsPlugin()
	handler := p.Middleware()(func(ctx *context.Context) error { return nil })
	handler(makeCtxWithCommandPlatform("hello", "stats_u1"))
	handler(makeCtxWithCommandPlatform("world", "stats_u2"))
	if len(p.ActiveUsers(stats.AllTime)) < 2 {
		t.Errorf("expected >= 2 active users, got %d", len(p.ActiveUsers(stats.AllTime)))
	}
}

func TestStats_Middleware_RecordsUser_QQ(t *testing.T) {
	p := newStatsPlugin()
	handler := p.Middleware()(func(ctx *context.Context) error { return nil })
	handler(makeCtxWithCommand("hello", "stats_u3"))
	handler(makeCtxWithCommand("world", "stats_u4"))
	if len(p.ActiveUsers(stats.AllTime)) < 2 {
		t.Errorf("expected >= 2 active users (QQ path), got %d", len(p.ActiveUsers(stats.AllTime)))
	}
}

func TestStats_TopCommands(t *testing.T) {
	p := newStatsPlugin()
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
	p := newStatsPlugin()
	handler := p.Middleware()(func(ctx *context.Context) error { return nil })
	for range 5 {
		handler(makeCtxWithCommandPlatform("msg", "u"))
	}
	if p.TotalMessages() != 5 {
		t.Errorf("expected 5, got %d", p.TotalMessages())
	}
}
func TestStats_Reset(t *testing.T) {
	p := newStatsPlugin()
	p.RecordCommand("/x")
	p.Reset()
	if p.CommandCount("/x") != 0 {
		t.Error("expected 0 after reset")
	}
}
