package catalog

import "github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"

// testInvocation 构造带能力端口的调用上下文（仅测试用）。
func testInvocation(caps Capabilities) *toolkit.Invocation {
	inv := &toolkit.Invocation{}
	inv.SetPort[Capabilities](caps)
	return inv
}
