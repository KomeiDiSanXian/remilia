package engine

import (
	"testing"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestMatcherCostInjected 校验引擎在调用 handler 前注入了 matcher 的耗时画像，
// 供准入中间件按成本类别分预算。
func TestMatcherCostInjected(t *testing.T) {
	e := newEngineForTest(t)

	var lastSamples int
	var lastP50 time.Duration
	e.OnCommand(string(platform.EventKindPrivateMessage), "/slow").
		Handle(func(ctx *corectx.Context) error {
			c := ctx.MatcherCost()
			lastSamples, lastP50 = c.Samples, c.P50
			// 让画像有可测的耗时。
			time.Sleep(20 * time.Millisecond)
			return nil
		})

	// 引擎在 handler 返回后才 Record，因此第 N 次调用能看到前 N-1 次的样本。
	for range 4 {
		e.ProcessPlatformEvent(&engineTestEvent{
			kind:    platform.EventKindPrivateMessage,
			content: "/slow",
			id:      "e",
		}, &platform.NoopSender{})
	}

	if lastSamples < 3 {
		t.Fatalf("matcher cost samples = %d, want >= 3 (profile must be injected)", lastSamples)
	}
	if lastP50 <= 0 {
		t.Fatalf("matcher cost p50 = %v, want > 0", lastP50)
	}
}
