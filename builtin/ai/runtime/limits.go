// limits.go — 由配置推导的运行预算。
//
// 三个推导都只读配置、不读插件状态，因此从插件方法收拢为纯函数：默认值
// 与推导公式只有一处定义，装配侧与测试直接以配置调用。
package runtime

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
)

// EffectiveToolRetryLimit 返回工具失败重试预算（<=0 时用默认值 2）。
func EffectiveToolRetryLimit(cfg *config.Config) int {
	if cfg.ToolRetryLimit <= 0 {
		return 2
	}
	return cfg.ToolRetryLimit
}

// EffectiveApprovalTimeout 返回生效的审批超时（未配置时用 60s）。
func EffectiveApprovalTimeout(cfg *config.Config) time.Duration {
	t := cfg.ApprovalTimeout
	if t <= 0 {
		t = 60 * time.Second
	}
	return t
}

// EffectiveTurnTimeout 返回一次 AI 处理的独立时间预算。
// turn_timeout 未配置时自动推导：api_timeout × max(2, min(max_depth, 5))，
// 既为多轮工具任务留足余量，又避免 max_depth 过大时预算失控。
func EffectiveTurnTimeout(cfg *config.Config) time.Duration {
	if cfg.TurnTimeout > 0 {
		return cfg.TurnTimeout
	}
	api := cfg.APITimeout
	if api <= 0 {
		api = 60 * time.Second
	}
	depth := cfg.MaxDepth
	if depth <= 0 {
		depth = 5
	}
	return api * time.Duration(max(2, min(depth, 5)))
}

// EffectivePlanAutoRounds 返回计划后台推进轮次上限（<=0 时用默认值 3）。
func EffectivePlanAutoRounds(cfg *config.Config) int {
	if cfg.PlanAutoRounds <= 0 {
		return 3
	}
	return cfg.PlanAutoRounds
}
