// observability.go — 观测回调接线。
//
// 协议层（重试）与决策层（工具选择）只暴露回调点，不依赖指标库；
// 本文件把这两个点接到 builtin/ai 的 Prometheus 指标上。包级 init 只做
// 赋值，不产生额外 goroutine 或状态。
package ai

import (
	"github.com/KomeiDiSanXian/remilia/builtin/ai/decision"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

func init() {
	protocol.SetRetryObserver(recordLLMRetry)
	decision.SelectPathObserver = recordToolSelectPath
}
