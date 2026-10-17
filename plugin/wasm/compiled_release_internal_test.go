package wasm

// compiled_release_internal_test.go — 白盒断言：Close 必须释放编译产物

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalModule 生成最小 WASM 模块（plugin_init 与 plugin_handle 都返回 0）。
//
// 内部测试包的手写二进制：只需通过实例化与必需导出校验，避免依赖
// wasm_test 包的模块生成器。
func minimalModule() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version
		// type section: [0] ()→i32, [1] (i32,i32)→i32
		0x01, 0x0b, 0x02, 0x60, 0x00, 0x01, 0x7f, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
		// function section: [0]=type0 (plugin_init), [1]=type1 (plugin_handle)
		0x03, 0x03, 0x02, 0x00, 0x01,
		// export section: plugin_init, plugin_handle
		0x07, 0x1f, 0x02,
		0x0b, 'p', 'l', 'u', 'g', 'i', 'n', '_', 'i', 'n', 'i', 't', 0x00, 0x00,
		0x0d, 'p', 'l', 'u', 'g', 'i', 'n', '_', 'h', 'a', 'n', 'd', 'l', 'e', 0x00, 0x01,
		// code section: [0] i32.const 0, [1] i32.const 0
		0x0a, 0x0b, 0x02,
		0x04, 0x00, 0x41, 0x00, 0x0b,
		0x04, 0x00, 0x41, 0x00, 0x0b,
	}
}

// TestModule_CloseNilCompiledField 直接检查编译产物字段：实例关闭后
// mod.compiled 必须为 nil。外部只能观察到计数（见 wasm_test 的对应测试），
// 这里从内部固定同一条不变式。
func TestModule_CloseNilCompiledField(t *testing.T) {
	ctx := context.Background()
	rt, err := NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, "compiled-release", minimalModule(), nil)
	require.NoError(t, err)
	require.NotNil(t, mod.compiled, "实例化后应持有编译产物")
	assert.Equal(t, 1, rt.OpenModules())

	require.NoError(t, mod.Close(ctx))
	assert.Nil(t, mod.compiled, "Close 必须释放编译产物")
	assert.True(t, mod.Closed())
	assert.Equal(t, 0, rt.OpenModules())
}
