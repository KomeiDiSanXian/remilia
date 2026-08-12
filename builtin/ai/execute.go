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
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// captureSender 实现 platform.Sender，拦截 Send 调用并记录消息文本内容和附件。
// 命令 handler 的回复仅作为工具结果回填给 AI（不转发给真实用户），同时捕获生成的附件。
type captureSender struct {
	platform.NoopSender
	mu                  sync.Mutex
	capturedText        string
	capturedAttachments []platform.Attachment
}

func (s *captureSender) Send(_ context.Context, req platform.SendRequest) (platform.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
// sender 非空时注入工具调用 context（含审批门控的 SendTo 能力）。
// 优先通过 vevent 触发真实命令执行并捕获其回复内容；
// 若捕获失败或工具无对应命令，回退到 tool.Execute 的占位结果。
func (p *Plugin) executeTool(ctx *eventctx.Context, tc ToolCall, toolCtx context.Context, cs *captureSender, sender ToolSender) string {
	callerCtx := WithCallerInfo(toolCtx, ctx.GetSenderInfo())
	if sender != nil {
		callerCtx = WithToolSender(callerCtx, sender)
	}
	if skill, ok := p.skillReg.GetByOwner(ctx.GetSenderInfo().ID, tc.Name); ok {
		result, err := p.executeSkill(callerCtx, skill, tc.Arguments)
		if err != nil {
			return fmt.Sprintf("错误: 技能 %q 执行失败: %v", tc.Name, err)
		}
		return result
	}
	if skill, ok := p.skillReg.GetSystem(tc.Name); ok {
		result, err := p.executeSkill(callerCtx, skill, tc.Arguments)
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
		// 并行工具执行时串行化真实命令路径（syncer 非线程安全）。
		p.realCmdMu.Lock()
		result := p.executeRealCommand(ctx, tc.Name, tc.Arguments, cs)
		p.realCmdMu.Unlock()
		if result != "" {
			return result
		}
	}

	// 工具级权限强制校验：工具声明了 Permissions 时，调用前校验调用者
	// RBAC 权限（任一命中即放行）。权限管理器缺失时拒绝（安全默认），
	// 不依赖插件自觉实现校验。
	if len(tool.Permissions) > 0 && !p.hasToolPermission(ctx, tool.Permissions) {
		return fmt.Sprintf("错误: 工具 %q 需要权限（%s），当前用户无权调用",
			tc.Name, strings.Join(tool.Permissions, ", "))
	}

	result, execErr := tool.Execute(callerCtx, tc.Arguments)
	if execErr != nil {
		if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
			return fmt.Sprintf("错误: 工具 %q 执行超时", tc.Name)
		}
		return fmt.Sprintf("错误: 工具 %q 执行失败: %v", tc.Name, execErr)
	}
	return result
}

// executeRealCommand 通过 vevent 注入合成事件执行工具对应的真实命令，
// 返回命令 handler 的回复文本，并捕获产生的附件。
//
// 使用 captureSender 捕获 handler 的 ctx.Reply() 输出，不转发给真实用户。
// AI 会自行总结工具执行结果后回复用户，避免用户看到两条消息。
func (p *Plugin) executeRealCommand(origCtx *eventctx.Context, toolName string, args map[string]any, cs *captureSender) string {
	p.cmdMu.RLock()
	pattern, ok := p.cmdPatterns[toolName]
	p.cmdMu.RUnlock()
	if !ok {
		return ""
	}

	originalEvent := origCtx.GetPlatformEvent()
	if originalEvent == nil {
		return ""
	}

	if rawArgs, ok := args["arguments"].(string); ok && rawArgs != "" {
		if isSafeCommandArg(rawArgs) {
			pattern += " " + rawArgs
		}
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

	skillTimeout := p.cfg.SkillTimeout
	if skillTimeout <= 0 {
		skillTimeout = 60 * time.Second
	}
	skillCtx, cancel := context.WithTimeout(ctx, skillTimeout)
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
			msgs = append(msgs, Message{Role: RoleTool, Content: truncateToolResult(result), ToolCallID: tc.ID})
		}
	}

	return "", fmt.Errorf("技能 %q 超过最大调用深度 (%d)", skill.Name, p.cfg.SkillMaxDepth)
}

// buildSkillTools 构建 Skill 可见的工具列表 = 自己的 Tools + 其他系统 Skill。
// 用户 Skill 不可见其他用户的 Skill，仅系统 Skill 被注入。
// 其他 Skill 按其自带的 Parameters 注入，无参数时使用默认 {"query": string}。
func (p *Plugin) buildSkillTools(skill Skill) []Tool {
	sysSkills := p.skillReg.ListByOwner(OwnerSystem)
	tools := make([]Tool, 0, len(skill.Tools)+len(sysSkills))
	tools = append(tools, skill.Tools...)

	for _, s := range sysSkills {
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

// isSafeCommandArg 校验 LLM 生成的命令参数是否安全。
// 只允许可打印的 ASCII 字符（含空格和 Tab），禁止控制字符和常见的 shell 注入字符。
func isSafeCommandArg(s string) bool {
	if len(s) > 4096 {
		return false
	}
	for _, r := range s {
		if r == '\t' {
			continue
		}
		if !unicode.IsPrint(r) {
			return false
		}
		if r > 0x7E {
			return false
		}
	}
	return true
}

// hasToolPermission 校验调用者是否拥有任一指定权限。
// 权限管理器缺失时返回 false（安全默认）。支持格式：
// "resource.action" / "resource:action" / "resource"（action 通配）。
func (p *Plugin) hasToolPermission(ctx *eventctx.Context, perms []string) bool {
	pm := ctx.GetPermissionManager()
	if pm == nil {
		return false
	}
	userID := ctx.GetUserID()
	for _, perm := range perms {
		perm = strings.TrimSpace(perm)
		if perm == "" {
			continue
		}
		resource, action := parseToolPermission(perm)
		if pm.HasPermission(userID, permission.Permission{Resource: resource, Action: action}) {
			return true
		}
	}
	return false
}

// parseToolPermission 解析工具权限字符串（与框架 parsePermission 同语义）。
func parseToolPermission(perm string) (resource, action string) {
	if idx := strings.Index(perm, ":"); idx > 0 {
		return perm[:idx], perm[idx+1:]
	}
	if idx := strings.LastIndex(perm, "."); idx > 0 {
		return perm[:idx], perm[idx+1:]
	}
	if perm == "*" {
		return "*", "*"
	}
	return perm, "*"
}
