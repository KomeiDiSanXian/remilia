package engine

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// metricsTestEvent 固定平台/类型的事件桩（内部包测试用）。
type metricsTestEvent struct{}

func (e *metricsTestEvent) Platform() string                 { return "qq" }
func (e *metricsTestEvent) Kind() platform.EventKind         { return platform.EventKindPrivateMessage }
func (e *metricsTestEvent) RawType() string                  { return string(platform.EventKindPrivateMessage) }
func (e *metricsTestEvent) Segments() []platform.Segment     { return nil }
func (e *metricsTestEvent) Chat() platform.ChatInfo          { return platform.ChatInfo{ID: "c1"} }
func (e *metricsTestEvent) Sender() platform.UserInfo        { return platform.UserInfo{ID: "u1"} }
func (e *metricsTestEvent) Timestamp() time.Time             { return time.Time{} }
func (e *metricsTestEvent) ID() string                       { return "evt-metrics-1" }
func (e *metricsTestEvent) RawPayload() any                  { return nil }

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
