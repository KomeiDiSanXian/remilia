package wasm_test

// adapter_handler_test.go — ToDescriptor handler 的空平台事件防御回归测试
//
// 回归背景：ToDescriptor 生成的 handler 直接调用 ectx.GetPlatformEvent().ID()，
// 合成事件/无事件绑定的 Context 会 nil 解引用 panic（Bridge 路径早已修过同类
// 问题，adapter 漏了）。事件编码与调用上下文现已抽成桥接层共用实现。

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

func TestToDescriptor_HandlerWithoutPlatformEventDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Shutdown(shutdownCtx)
	})

	pm := plugin.NewManager(eng)
	require.NoError(t, pm.RegisterContext(ctx, wasm.ToDescriptor(&wasm.Descriptor{
		Name:     "adapter-nil-event",
		Commands: []wasm.CommandDef{{Command: "/adapter"}},
	}, testWasmModule, nil)))

	inst, ok := pm.Get("adapter-nil-event")
	require.True(t, ok)
	matchers := inst.GetMatchers()
	require.Len(t, matchers, 1)
	require.NotNil(t, matchers[0].Handler)

	bare := corectx.NewContextFromEvent(nil, nil)
	require.NotPanics(t, func() { _ = matchers[0].Handler(bare) })
}
