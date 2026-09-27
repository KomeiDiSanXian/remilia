// limits.go — 由参数推导的运行预算。
//
// 推导只读入参、不读插件状态，因此是纯函数：默认值与推导公式只有一处定义，
// 装配侧把插件配置映射成 [Limits] 后调用。本包因此不依赖配置包。
package runtime

import (
	"time"
)

// Limits 回合运行预算的输入参数（由装配侧从插件配置映射）。
type Limits struct {
	// ToolRetryLimit 工具失败重试预算（tool_retry_limit；<=0 用默认值 2）。
	ToolRetryLimit int
	// ApprovalTimeout 审批等待超时（approval_timeout；<=0 用默认值 60s）。
	ApprovalTimeout time.Duration
	// TurnTimeout 一次 AI 处理的独立时间预算（turn_timeout；<=0 时自动推导）。
	TurnTimeout time.Duration
	// APITimeout 单次 LLM 请求超时（api_timeout；推导 turn_timeout 与校验超时用）。
	APITimeout time.Duration
	// MaxDepth LLM 最大轮次（max_depth；推导 turn_timeout 用）。
	MaxDepth int
	// PlanAutoRounds 计划后台推进轮次上限（plan_auto_rounds；<=0 用默认值 3）。
	PlanAutoRounds int
}

// EffectiveToolRetryLimit 返回工具失败重试预算（<=0 时用默认值 2）。
func EffectiveToolRetryLimit(l Limits) int {
	if l.ToolRetryLimit <= 0 {
		return 2
	}
	return l.ToolRetryLimit
}

// EffectiveApprovalTimeout 返回生效的审批超时（未配置时用 60s）。
func EffectiveApprovalTimeout(l Limits) time.Duration {
	t := l.ApprovalTimeout
	if t <= 0 {
		t = 60 * time.Second
	}
	return t
}

// EffectiveTurnTimeout 返回一次 AI 处理的独立时间预算。
// turn_timeout 未配置时自动推导：api_timeout × max(2, min(max_depth, 5))，
// 既为多轮工具任务留足余量，又避免 max_depth 过大时预算失控。
func EffectiveTurnTimeout(l Limits) time.Duration {
	if l.TurnTimeout > 0 {
		return l.TurnTimeout
	}
	api := l.APITimeout
	if api <= 0 {
		api = 60 * time.Second
	}
	depth := l.MaxDepth
	if depth <= 0 {
		depth = 5
	}
	return api * time.Duration(max(2, min(depth, 5)))
}

// EffectivePlanAutoRounds 返回计划后台推进轮次上限（<=0 时用默认值 3）。
func EffectivePlanAutoRounds(l Limits) int {
	if l.PlanAutoRounds <= 0 {
		return 3
	}
	return l.PlanAutoRounds
}
