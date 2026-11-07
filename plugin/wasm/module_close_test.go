package wasm_test

// module_close_test.go — 模块关闭与在飞调用互斥的回归测试
//
// 回归背景：
//   - CallHandle/CallInit 先读 m.instance 再取 callMu，而 Close 不持 callMu 就把
//     instance 置 nil：与在飞调用并发时，handler 会在已关闭的实例上继续读内存。
//   - Closed() 直接读 m.instance，与 Close 的写入竞争。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

// TestModule_CallAfterCloseReturnsError 关闭后的调用必须返回明确错误，
// 而不是 nil 解引用。
func TestModule_CallAfterCloseReturnsError(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, "closed-call", testWasmModule, nil)
	require.NoError(t, err)
	require.NoError(t, mod.CallInit(ctx))

	require.NoError(t, mod.Close(ctx))
	assert.True(t, mod.Closed())

	_, err = mod.CallHandle(ctx, tlvEvent("/ping"))
	require.Error(t, err, "已关闭模块的调用必须返回错误")
	assert.Contains(t, err.Error(), "closed")

	assert.Error(t, mod.CallInit(ctx))
}

// TestModule_CloseIsSerializedWithCalls 关闭与调用并发时不得 panic。
func TestModule_CloseIsSerializedWithCalls(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, "close-race", testWasmModule, nil)
	require.NoError(t, err)
	require.NoError(t, mod.CallInit(ctx))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 20 {
			// 结果可能是成功或 "closed"，但绝不能 panic
			_, _ = mod.CallHandle(ctx, tlvEvent("/ping"))
		}
	}()
	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond)
		_ = mod.Close(ctx)
	}()

	require.NotPanics(t, func() { wg.Wait() })
}
