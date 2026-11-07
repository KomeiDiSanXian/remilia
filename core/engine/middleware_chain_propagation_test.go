package engine

// middleware_chain_propagation_test.go — 中间件链变更传播到临时 matcher
//
// 回归背景：invokeHandler 的快路径只比较 compiledVersion，不检查中间件代际号。
// Engine.Use 若不显式失效 TempManager 中活跃临时 matcher 的链，这些 matcher
// 会一直使用变更前的旧链（鉴权/日志中间件被整体跳过）。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestEngine_MiddlewareChangeReachesTempMatcher 验证运行期 Engine.Use 注册的
// 全局中间件会传播到 TempManager 中已存在的临时 matcher。
func TestEngine_MiddlewareChangeReachesTempMatcher(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var order []string
	m := eng.OnTemp(et)
	m.SetTempWithMaxUse(10)
	m.Handle(func(ctx *context.Context) error {
		order = append(order, "handler")
		return nil
	})

	evt := newTestPlatformEvent(platform.EventKindPrivateMessage)
	eng.ProcessEvent(context.NewContextFromEvent(evt, nil))
	if len(order) != 1 || order[0] != "handler" {
		t.Fatalf("first event should hit bare handler, got %v", order)
	}

	// 运行期新增全局中间件：已存在的临时 matcher 也必须重建链
	eng.Use(func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			order = append(order, "mw")
			return next(ctx)
		}
	})

	order = order[:0]
	eng.ProcessEvent(context.NewContextFromEvent(evt, nil))
	if len(order) != 2 || order[0] != "mw" || order[1] != "handler" {
		t.Fatalf("temp matcher should run through the new middleware chain, got %v", order)
	}
}
