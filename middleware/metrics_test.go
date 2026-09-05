package middleware

import (
	"context"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestHandlerPanicCounter 验证 Recover 中间件递增 panic 计数。
func TestHandlerPanicCounter(t *testing.T) {
	before := testutil.ToFloat64(handlerPanics)
	h := Recover()(mockPanicHandler("boom"))
	_ = h(createTestContext())
	if after := testutil.ToFloat64(handlerPanics); after != before+1 {
		t.Errorf("panic 计数: before=%v after=%v", before, after)
	}
}

// TestHandlerTimeoutCounter 验证 Timeout 中间件递增超时计数。
func TestHandlerTimeoutCounter(t *testing.T) {
	before := testutil.ToFloat64(handlerTimeouts)
	h := Timeout(10 * time.Millisecond)(func(ctx *eventctx.Context) error {
		// 睡过 deadline 后返回错误 → 判定为超时
		time.Sleep(30 * time.Millisecond)
		return context.DeadlineExceeded
	})
	_ = h(createTestContext())
	if after := testutil.ToFloat64(handlerTimeouts); after != before+1 {
		t.Errorf("timeout 计数: before=%v after=%v", before, after)
	}
}
