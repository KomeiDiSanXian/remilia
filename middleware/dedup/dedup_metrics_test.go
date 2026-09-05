package dedup

import (
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// dedupMetricEvent 固定平台名的测试事件（用于断言按平台细分的丢弃计数）。
type dedupMetricEvent struct {
	id string
}

func (e *dedupMetricEvent) Platform() string                 { return "testplat" }
func (e *dedupMetricEvent) Kind() platform.EventKind         { return platform.EventKindPrivateMessage }
func (e *dedupMetricEvent) RawType() string                  { return string(platform.EventKindPrivateMessage) }
func (e *dedupMetricEvent) Segments() []platform.Segment     { return nil }
func (e *dedupMetricEvent) Chat() platform.ChatInfo          { return platform.ChatInfo{ID: "c1"} }
func (e *dedupMetricEvent) Sender() platform.UserInfo        { return platform.UserInfo{ID: "u1"} }
func (e *dedupMetricEvent) Timestamp() time.Time             { return time.Time{} }
func (e *dedupMetricEvent) ID() string                       { return e.id }
func (e *dedupMetricEvent) RawPayload() any                  { return nil }

// TestDedupDroppedCounter 验证重复事件被阻断时递增丢弃计数（按平台）。
func TestDedupDroppedCounter(t *testing.T) {
	filter := NewDedupFilter(DefaultDedupConfig())
	defer filter.Stop()

	mw := Dedup(filter)
	handler := mw(func(ctx *eventctx.Context) error { return nil })

	mkCtx := func() *eventctx.Context {
		return eventctx.NewContextFromEvent(&dedupMetricEvent{id: "evt-1"}, &platform.NoopSender{})
	}

	before := testutil.ToFloat64(dedupDropped.WithLabelValues("testplat"))
	_ = handler(mkCtx()) // 首次：不丢弃
	if after := testutil.ToFloat64(dedupDropped.WithLabelValues("testplat")); after != before {
		t.Errorf("首次事件不应计数: %v -> %v", before, after)
	}
	_ = handler(mkCtx()) // 重复：丢弃并计数
	if after := testutil.ToFloat64(dedupDropped.WithLabelValues("testplat")); after != before+1 {
		t.Errorf("重复事件应计数: %v -> %v", before, after)
	}
}
