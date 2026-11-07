package wasm_test

// hostfunc_probe_test.go — 宿主函数自描述清单的回归测试
//
// 回归背景：ListFunctionNames 只列 funcs，不含 callerFuncs。纯 RegisterWithCaller
// 注册的宿主函数（Runtime 注入的 get_config 就是这种）因此对 __host_functions
// 探测不可见，guest 无法感知宿主能力。

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

func TestListFunctionNames_IncludesCallerFuncs(t *testing.T) {
	reg := wasm.NewHostFuncRegistry()
	reg.Register("plain", func([]byte) ([]byte, error) { return nil, nil })
	reg.RegisterWithCaller("caller-only", func(string, []byte) ([]byte, error) { return nil, nil })

	names := reg.ListFunctionNames()
	assert.Contains(t, names, "plain")
	assert.Contains(t, names, "caller-only", "caller-only 函数同样可用，必须可被探测")

	// 同名只列一次
	reg.RegisterWithCaller("plain", func(string, []byte) ([]byte, error) { return nil, nil })
	count := 0
	for _, n := range reg.ListFunctionNames() {
		if n == "plain" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}
