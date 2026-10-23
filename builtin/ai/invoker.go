// Package ai invoker.go — 调用器与插件装配之间的适配器。
//
// 动作调用器本身（FuncInvoker / SkillInvoker / CommandInvoker）、结果类型
// [runtime.ActionResult] 与两个消费方端口（[runtime.CommandCatalog] /
// [runtime.SkillRunner]）见 builtin/ai/runtime。本文件只保留把插件侧能力适配为
// 端口的适配器：命令目录与平台事件处理器留在插件侧，执行入口将解析出的命令
// 模式绑定到本次调用；Skill 适配器交出子代理循环。
package ai

import (
	"context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// pluginCommandCatalog 把插件的命令重放能力适配为 runtime.CommandCatalog。
// 执行入口可将与工具定义一并解析出的命令模式绑定到适配器，目录更新不会改变进行中的调用。
type pluginCommandCatalog struct {
	p               *Plugin
	commandPattern  string
	patternResolved bool
}

// Pattern 返回动作名对应的命令模式（发现阶段写入，执行阶段只读）。
func (c pluginCommandCatalog) Pattern(toolName string) (string, bool) {
	c.p.catalogGen.mu.RLock()
	defer c.p.catalogGen.mu.RUnlock()
	c.p.cmdMu.RLock()
	defer c.p.cmdMu.RUnlock()
	pattern, ok := c.p.cmdPatterns[toolName]
	return pattern, ok
}

// Run 触发真实命令并返回捕获到的回复文本（错误语义见 execution.RunCommandWithPattern）。
func (c pluginCommandCatalog) Run(ctx *eventctx.Context, name string, args map[string]any, cs *execution.CaptureSender) (string, error) {
	pattern := c.commandPattern
	if !c.patternResolved {
		var ok bool
		pattern, ok = c.Pattern(name)
		if !ok {
			return "", fmt.Errorf("动作 %q 没有登记命令模式", name)
		}
	}
	if pattern == "" {
		return "", fmt.Errorf("动作 %q 没有登记命令模式", name)
	}
	return execution.RunCommandWithPattern(ctx, name, pattern, args, cs, c.p.syncer)
}

// pluginSkillRunner 把插件内的 Skill 循环适配为 runtime.SkillRunner。
type pluginSkillRunner struct{ p *Plugin }

// Run 跑完技能的子代理循环并返回最终文本。
func (r pluginSkillRunner) Run(ctx context.Context, inv *toolkit.Invocation, skill toolkit.Skill, args map[string]any) (string, error) {
	return r.p.executeSkill(ctx, inv, skill, args)
}
