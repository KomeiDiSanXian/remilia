// Package ai invoker.go — 调用器与插件装配之间的适配器。
//
// 动作调用器本身（FuncInvoker / SkillInvoker / CommandInvoker）、结果类型
// [runtime.ActionResult] 与两个消费方端口（[runtime.CommandCatalog] /
// [runtime.SkillRunner]）见 builtin/ai/runtime。本文件只保留把插件侧能力适配为
// 端口的适配器：命令目录交出"发现期写入的模式映射 + 平台事件处理器"，
// Skill 适配器交出子代理循环。
package ai

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// pluginCommandCatalog 把插件的命令目录适配为 runtime.CommandCatalog 与消费方
// 端口 execution.CommandPatterns：目录数据（命令模式映射、发现期锁）与平台事件
// 处理器都留在插件侧，执行侧只按端口取用。
type pluginCommandCatalog struct{ p *Plugin }

// Pattern 返回动作名对应的命令模式（发现阶段写入，执行阶段只读）。
func (c pluginCommandCatalog) Pattern(toolName string) (string, bool) {
	c.p.cmdMu.RLock()
	defer c.p.cmdMu.RUnlock()
	pattern, ok := c.p.cmdPatterns[toolName]
	return pattern, ok
}

// Run 触发真实命令并返回捕获到的回复文本。
func (c pluginCommandCatalog) Run(ctx *eventctx.Context, name string, args map[string]any, cs *execution.CaptureSender) string {
	return execution.RunCommand(ctx, name, args, cs, c, c.p.syncer)
}

// pluginSkillRunner 把插件内的 Skill 循环适配为 runtime.SkillRunner。
type pluginSkillRunner struct{ p *Plugin }

// Run 跑完技能的子代理循环并返回最终文本。
func (r pluginSkillRunner) Run(ctx context.Context, skill toolkit.Skill, args map[string]any) (string, error) {
	return r.p.executeSkill(ctx, skill, args)
}
