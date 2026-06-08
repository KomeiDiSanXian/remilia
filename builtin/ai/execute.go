// Package ai execute.go — 工具与技能（Skill）的执行逻辑。
//
// 本文件包含工具调用的执行链路：
//   - executeTool: 工具调度总入口，先检查 Skill 再检查真实命令，最后回退到 Execute 回调
//   - executeRealCommand: 通过 vevent 合成事件触发真实命令 handler 并捕获回复
//   - captureSender: 拦截 Sender 用于捕获 handler 输出
//   - executeSkill: Skill 内部工具调用循环（非流式）
//   - buildSkillTools: 构建 Skill 可见的工具列表（自有工具 + 其他 Skill）
//   - executeSkillTool: 执行 Skill 内部的工具调用
package ai

import (
	"context"
	"encoding/json"
	"fmt"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// captureSender 实现 platform.Sender，拦截 Send 调用并记录消息文本内容和附件。
// 命令 handler 的回复仅作为工具结果回填给 AI（不转发给真实用户），同时捕获生成的附件。
type captureSender struct {
	platform.NoopSender
	capturedText        string
	capturedAttachments []platform.Attachment
}

func (s *captureSender) Send(_ context.Context, req platform.SendRequest) (platform.SendResult, error) {
	text := req.Message.Text
	if text == "" {
		text = req.Message.Markdown
	}
	if text != "" {
		s.capturedText = text
	}
	if len(req.Message.Attachments) > 0 {
		s.capturedAttachments = append(s.capturedAttachments, req.Message.Attachments...)
	}
	return platform.SendResult{}, nil
}

// executeTool 执行一个工具调用并返回结果字符串。
//
// toolCtx 是调用方传入的超时 context，用于限制工具执行的最长时间。
// cs 用于捕获 real command 执行过程中产生的消息附件。
// 优先通过 vevent 触发真实命令执行并捕获其回复内容；
// 若捕获失败或工具无对应命令，回退到 tool.Execute 的占位结果。
func (p *Plugin) executeTool(ctx *eventctx.Context, tc ToolCall, toolCtx context.Context, cs *captureSender) string {
	if skill, ok := p.skillReg.Get(tc.Name); ok {
		result, err := p.executeSkill(toolCtx, skill, tc.Arguments)
		if err != nil {
			return fmt.Sprintf("错误: 技能 %q 执行失败: %v", tc.Name, err)
		}
		return result
	}

	tool, ok := p.reg.Get(tc.Name)
	if !ok {
		return fmt.Sprintf("错误: 未找到工具 %q", tc.Name)
	}

	if p.syncer != nil {
		if result := p.executeRealCommand(ctx, tc.Name, cs); result != "" {
			return result
		}
	}

	callerCtx := WithCallerInfo(toolCtx, ctx.GetSenderInfo())
	done := make(chan struct{}, 1)
	var result string
	var execErr error
	go func() {
		result, execErr = tool.Execute(callerCtx, tc.Arguments)
		close(done)
	}()
	select {
	case <-done:
		if execErr != nil {
			return fmt.Sprintf("错误: 工具 %q 执行失败: %v", tc.Name, execErr)
		}
		return result
	case <-toolCtx.Done():
		return fmt.Sprintf("错误: 工具 %q 执行超时", tc.Name)
	}
}

// executeRealCommand 通过 vevent 注入合成事件执行工具对应的真实命令，
// 返回命令 handler 的回复文本，并捕获产生的附件。
//
// 使用 captureSender 捕获 handler 的 ctx.Reply() 输出，不转发给真实用户。
// AI 会自行总结工具执行结果后回复用户，避免用户看到两条消息。
func (p *Plugin) executeRealCommand(origCtx *eventctx.Context, toolName string, cs *captureSender) string {
	pattern, ok := p.cmdPatterns[toolName]
	if !ok {
		return ""
	}

	originalEvent := origCtx.GetPlatformEvent()
	if originalEvent == nil {
		return ""
	}

	evt := platform.NewSyntheticEvent(
		originalEvent.Kind(),
		pattern,
		platform.WithSyntheticSender(originalEvent.Sender()),
		platform.WithSyntheticChat(originalEvent.Chat()),
	)
	p.syncer.ProcessPlatformEventSync(evt, cs)
	return cs.capturedText
}

// executeSkill 执行一个 Skill 的内部工具调用循环。
//
// 使用自己的 Prompt 和 Tools 做最多 SkillMaxDepth 轮的非流式 LLM 调用。
// 不持久化到 session，纯函数式。
func (p *Plugin) executeSkill(ctx context.Context, skill Skill, args map[string]any) (string, error) {
	argsJSON, _ := json.MarshalIndent(args, "", "  ")
	msgs := []Message{
		{Role: RoleSystem, Content: skill.Prompt},
		{Role: RoleUser, Content: string(argsJSON)},
	}
	tools := p.buildSkillTools(skill)

	skillCtx, cancel := context.WithTimeout(ctx, p.cfg.SkillTimeout)
	defer cancel()

	for depth := 0; depth < p.cfg.SkillMaxDepth; depth++ {
		resp, err := p.runSingleRound(skillCtx, msgs, tools)
		if err != nil {
			return "", err
		}

		if len(resp.ToolCalls) == 0 {
			return resp.Text, nil
		}

		msgs = append(msgs, Message{Role: RoleAssistant, Content: resp.Text, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			result := p.executeSkillTool(skillCtx, tc, tools)
			msgs = append(msgs, Message{Role: RoleTool, Content: result, ToolCallID: tc.ID})
		}
	}

	return "", fmt.Errorf("技能 %q 超过最大调用深度 (%d)", skill.Name, p.cfg.SkillMaxDepth)
}

// buildSkillTools 构建 Skill 可见的工具列表 = 自己的 Tools + 其他已注册的 Skill。
// 其他 Skill 按其自带的 Parameters 注入，无参数时使用默认 {"query": string}。
func (p *Plugin) buildSkillTools(skill Skill) []Tool {
	tools := make([]Tool, 0, len(skill.Tools)+len(p.skillReg.List()))
	tools = append(tools, skill.Tools...)

	for _, s := range p.skillReg.List() {
		if s.Name == skill.Name {
			continue
		}
		other := s
		params := other.Parameters
		if len(params.Properties) == 0 {
			params = ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"query": {Type: "string", Description: "需要该技能处理的问题"},
				},
				Required: []string{"query"},
			}
		}
		tools = append(tools, Tool{
			Name:        other.Name,
			Description: other.Description,
			Parameters:  params,
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				return p.executeSkill(ctx, other, args)
			},
		})
	}

	return tools
}

// executeSkillTool 执行 Skill 内部的工具调用。
// 不走 syncer/real command，直接调用工具自身的 Execute 回调。
func (p *Plugin) executeSkillTool(ctx context.Context, tc ToolCall, tools []Tool) string {
	for _, t := range tools {
		if t.Name == tc.Name {
			result, err := t.Execute(ctx, tc.Arguments)
			if err != nil {
				return fmt.Sprintf("错误: 工具 %q 执行失败: %v", tc.Name, err)
			}
			return result
		}
	}
	return fmt.Sprintf("错误: 未找到工具 %q", tc.Name)
}
