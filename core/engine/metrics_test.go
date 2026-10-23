package engine

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// metricsTestEvent 固定平台/类型的事件桩（内部包测试用）。
type metricsTestEvent struct{}

func (e *metricsTestEvent) Platform() string             { return "qq" }
func (e *metricsTestEvent) Kind() platform.EventKind     { return platform.EventKindPrivateMessage }
func (e *metricsTestEvent) RawType() string              { return string(platform.EventKindPrivateMessage) }
func (e *metricsTestEvent) Segments() []platform.Segment { return nil }
func (e *metricsTestEvent) Chat() platform.ChatInfo      { return platform.ChatInfo{ID: "c1"} }
func (e *metricsTestEvent) Sender() platform.UserInfo    { return platform.UserInfo{ID: "u1"} }
func (e *metricsTestEvent) Timestamp() time.Time         { return time.Time{} }
func (e *metricsTestEvent) ID() string                   { return "evt-metrics-1" }
func (e *metricsTestEvent) RawPayload() any              { return nil }

// TestEventsReceivedCounter 验证平台事件入口计数。
func TestEventsReceivedCounter(t *testing.T) {
	plt, kind := "qq", string(platform.EventKindPrivateMessage)
	before := testutil.ToFloat64(eventsReceived.WithLabelValues(plt, kind))

	eng := NewEngine()
	eng.ProcessPlatformEvent(&metricsTestEvent{}, &platform.NoopSender{})

	if after := testutil.ToFloat64(eventsReceived.WithLabelValues(plt, kind)); after != before+1 {
		t.Errorf("事件入口计数: before=%v after=%v", before, after)
	}
}

// TestEventsReceivedCounterRouterPath 验证 Router 转发路径同样计数。
//
// 实际部署里事件走 bot.handlePlatformEvent → Router.Dispatch → Engine.ProcessEvent，
// 不经过 ProcessPlatformEvent*；打点放在 processEventGuarded 才能覆盖两种用法。
func TestEventsReceivedCounterRouterPath(t *testing.T) {
	plt, kind := "qq", string(platform.EventKindPrivateMessage)
	before := testutil.ToFloat64(eventsReceived.WithLabelValues(plt, kind))

	eng := NewEngine()
	eng.ProcessEvent(context.NewContextFromEvent(&metricsTestEvent{}, &platform.NoopSender{}))

	if after := testutil.ToFloat64(eventsReceived.WithLabelValues(plt, kind)); after != before+1 {
		t.Errorf("Router 路径的事件入口计数: before=%v after=%v", before, after)
	}
}

// TestEventsInternalCounter 验证内部事件单独计数，不混入平台入口指标：
// 命令执行、定时任务等由插件自造的事件同样占用处理能力，值得观测，
// 但不是平台入口流量。
func TestEventsInternalCounter(t *testing.T) {
	kind := string(platform.EventKindPrivateMessage)
	beforeInternal := testutil.ToFloat64(eventsInternal.WithLabelValues(kind))
	beforeReceived := testutil.CollectAndCount(eventsReceived)

	eng := NewEngine()
	eng.ProcessPlatformEvent(platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "内部事件"), &platform.NoopSender{})

	if after := testutil.ToFloat64(eventsInternal.WithLabelValues(kind)); after != beforeInternal+1 {
		t.Errorf("内部事件计数: before=%v after=%v", beforeInternal, after)
	}
	if after := testutil.CollectAndCount(eventsReceived); after != beforeReceived {
		t.Errorf("内部事件不得计入平台入口: before=%d after=%d", beforeReceived, after)
	}
}
