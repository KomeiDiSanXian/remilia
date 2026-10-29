// observability.go — 观测回调接线。
//
// 协议层（重试）只暴露一个回调点，不依赖指标库；本文件把它接到 builtin/ai 的
// Prometheus 指标上。
//
// 接线通过 [Plugin] 的构造入口显式完成，而不是包级 init：
// init 里的跨包赋值让"谁注册了观察者"无法靠 grep 找到，测试之间也会互相污染
// （先 import 的包决定了全局状态）。工具选择路径的观测已经改为函数参数
// （见 decision.SelectToolsForTurn 的 observePath），不需要全局变量。
package ai

import "github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"

// installObservers 把包内观测点接到指标实现上。由 [New] 在装配时调用。
func installObservers() {
	protocol.SetRetryObserver(recordLLMRetry)
}
