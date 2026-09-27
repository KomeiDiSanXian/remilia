// Package ai options.go — 「插件配置 → 子包参数」的唯一映射点。
//
// 子包各自声明自己需要的最小参数结构（protocol.ProviderOptions、
// decision.SelectionOptions、promptctx.ContextOptions、runtime.Limits），
// 因此都不依赖插件配置包 builtin/ai/config：本文件是两者之间唯一的翻译层。
//
// 这样配置包只被装配根引用，子包也可以脱离整份插件配置独立构造与测试；
// 新增一个子包开关时，代价是显式在对应映射函数里加一行，而不是让子包伸手
// 去读配置对象的任意字段。
package ai

import (
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/decision"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/promptctx"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
)

// providerOptions 把插件配置映射为协议层的提供商参数。
func providerOptions(cfg *config.Config) protocol.ProviderOptions {
	return protocol.ProviderOptions{
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		MaxTokens:    cfg.MaxTokens,
		APITimeout:   cfg.APITimeout,
		MaxRetries:   cfg.MaxRetries,
		IncludeUsage: cfg.IncludeUsage,
	}
}

// selectionOptions 把插件配置映射为决策层的选择参数。
func selectionOptions(cfg *config.Config) decision.SelectionOptions {
	return decision.SelectionOptions{
		Max:       cfg.ToolSelectMax,
		Budget:    cfg.ToolBudget,
		Sticky:    cfg.ToolSetSticky,
		StickyMax: cfg.ToolSetStickyMax,
		StickyTTL: cfg.ToolSetTTL,
	}
}

// contextOptions 把插件配置映射为上下文各节的输入参数。
func contextOptions(cfg *config.Config) promptctx.ContextOptions {
	return promptctx.ContextOptions{
		RuntimeFields:   cfg.ContextFields,
		GroupIncludeBot: cfg.ContextGroupIncludeBot,
		GroupMessages:   cfg.ContextGroupMessages,
		RAGDays:         cfg.ContextRAGDays,
		RAGCandidates:   cfg.ContextRAGCandidates,
		RAGInjectMax:    cfg.ContextRAGInjectMax,
	}
}

// runtimeLimits 把插件配置映射为回合运行预算参数。
func runtimeLimits(cfg *config.Config) runtime.Limits {
	return runtime.Limits{
		ToolRetryLimit:  cfg.ToolRetryLimit,
		ApprovalTimeout: cfg.ApprovalTimeout,
		TurnTimeout:     cfg.TurnTimeout,
		APITimeout:      cfg.APITimeout,
		MaxDepth:        cfg.MaxDepth,
		PlanAutoRounds:  cfg.PlanAutoRounds,
	}
}
