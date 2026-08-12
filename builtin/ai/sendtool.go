// Package ai sendtool.go — AI 消息发送工具（send_message / send_to）。
//
// 为 AI 补齐消息发送能力：
//   - send_message: 向当前会话发送中间进度消息（文本/Markdown/图片/@提及），
//     多步骤任务中向用户展示阶段性输出；无需审批
//   - send_to: 向指定用户/群推送消息；强制审批（AlwaysRequireApproval）
//     且需要 ai.message.send 权限
//
// send_to 的目标自动解析（无需配置），按优先级：
//  1. 内置别名：本群 / 我 / 对方（当前会话）
//  2. 当前会话近期发言者昵称（messagelog，全平台可用）
//  3. 机器人已加入群的群名（平台支持时）
//  4. 用户/群原始 ID 兜底（is_group 指定类型）
//
// 同名歧义时返回候选 ID 列表，由模型改用原始 ID 重试。
//
// 安全模型：
//   - ToolSender 经 context 注入（executeTool 构造 loopToolSender），
//     工具 Execute 回调不接触事件上下文，签名保持不变
//   - SendTo 能力仅在工具调用通过审批门后注入（sendToAllowed），
//     嵌套 Skill 工具调用继承同一 context，无法绕过审批
//   - 审批前预解析目标：审批消息展示解析后的目标（如 张三（12345）），
//     目标无效时模型可在批准前获知并调整
//   - 每次对话处理（processWithTools 一次运行）内发送次数受
//     max_sends_per_round 上限约束（原子计数，并行执行安全）
package ai

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

const (
	sendMessageToolName = "send_message"
	sendToToolName      = "send_to"

	// sendToPermission send_to 工具所需的 RBAC 权限（审批之上叠加）。
	sendToPermission = "ai.message.send"

	// maxSendMessageRunes 单条工具消息的最大字符数。
	maxSendMessageRunes = 4000

	// sendTimeout 单次发送的等待超时。
	sendTimeout = 15 * time.Second

	// recentSpeakerWindow 目标解析时查询的近期消息条数（messagelog）。
	recentSpeakerWindow = 300
)

// sendBudget 一次 processWithTools 运行内 AI 消息发送的次数预算。
// limit<=0 表示不限。
type sendBudget struct {
	limit int
	used  atomic.Int64
}

// tryUse 原子尝试占用一次发送额度，超限返回 false。
func (b *sendBudget) tryUse() bool {
	if b == nil || b.limit <= 0 {
		return true
	}
	return b.used.Add(1) <= int64(b.limit)
}

// loopToolSender 实现 ToolSender，将发送动作路由到事件上下文关联的平台。
type loopToolSender struct {
	ctx           *eventctx.Context
	p             *Plugin
	sendToAllowed bool
	budget        *sendBudget
}

// ReplyToChat 向当前会话发送消息（经 messagelog 记录，群聊窗口可见）。
func (s *loopToolSender) ReplyToChat(ctx context.Context, msg platform.OutboundMessage) (platform.SendResult, error) {
	if !s.budget.tryUse() {
		return platform.SendResult{}, fmt.Errorf("本轮对话发送消息次数已达上限")
	}
	if msg.IsEmpty() {
		return platform.SendResult{}, fmt.Errorf("消息内容为空")
	}
	return s.p.replyAndRecord(s.ctx, msg).Wait(ctx)
}

// SendTo 向指定用户/群推送消息。仅当工具调用通过审批时可用
// （sendToAllowed），否则返回错误——嵌套 Skill 调用继承同一 context，
// 无法绕过该门控。
func (s *loopToolSender) SendTo(ctx context.Context, target ChatTarget, msg platform.OutboundMessage) (platform.SendResult, error) {
	if !s.sendToAllowed {
		return platform.SendResult{}, fmt.Errorf("向其他会话发送消息需要用户审批，本次调用未获授权")
	}
	if target.ID == "" {
		return platform.SendResult{}, fmt.Errorf("目标 ID 不能为空")
	}
	if !s.budget.tryUse() {
		return platform.SendResult{}, fmt.Errorf("本轮对话发送消息次数已达上限")
	}
	if msg.IsEmpty() {
		return platform.SendResult{}, fmt.Errorf("消息内容为空")
	}
	sender := s.ctx.GetPlatformSender()
	if sender == nil {
		return platform.SendResult{}, fmt.Errorf("无法获取平台发送器")
	}

	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	// 优先主动推送接口（不依赖事件上下文路由），回退普通 Send。
	if sn, ok := sender.(platform.SessionNotifier); ok {
		var err error
		if target.IsGroup {
			err = sn.NotifyGroup(sendCtx, target.ID, msg)
		} else {
			err = sn.NotifyUser(sendCtx, target.ID, msg)
		}
		if err != nil {
			return platform.SendResult{}, err
		}
		return platform.SendResult{}, nil
	}
	return sender.Send(sendCtx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: target.ID, IsGroup: target.IsGroup},
		Message: msg,
	})
}

// ResolveTarget 将目标自动解析为 ChatTarget（见 resolveTarget）。
// isGroup 提示仅在按原始 ID 兜底时生效。
func (s *loopToolSender) ResolveTarget(ctx context.Context, raw string, isGroup bool) (ChatTarget, string, error) {
	return s.resolveTarget(ctx, raw, isGroup)
}

// resolveTarget 按优先级解析目标：
//  1. 内置别名（本群/我/对方）
//  2. 当前会话近期发言者昵称（messagelog）
//  3. 已加入群群名（平台支持时）
//  4. 原始 ID 兜底（仅 ASCII 标识符，isGroup 提示指定类型）
//
// 返回 (目标, 展示文本, 错误)。同名歧义时返回候选 ID 列表错误。
func (s *loopToolSender) resolveTarget(ctx context.Context, raw string, isGroupHint bool) (ChatTarget, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ChatTarget{}, "", fmt.Errorf("target 不能为空")
	}
	chat := s.ctx.GetChatInfo()

	// 1. 内置别名
	if target, display, matched, err := s.specialTarget(raw, chat); matched {
		return target, display, err
	}

	// 2. 当前会话近期发言者昵称
	if target, display, matched, err := s.matchRecentSpeaker(raw, chat); matched {
		return target, display, err
	}

	// 3. 已加入群群名（平台不支持时自动跳过）
	if target, display, matched, err := s.matchJoinedGroupName(ctx, raw); matched {
		return target, display, err
	}

	// 4. 原始 ID 兜底：仅接受 ASCII 标识符（数字/字母/_-），
	// 避免把中文昵称等未命中目标静默当作 ID 使用。
	if isPlainID(raw) {
		return ChatTarget{ID: raw, IsGroup: isGroupHint}, raw, nil
	}

	return ChatTarget{}, "", fmt.Errorf(
		"无法解析目标 %q：可用目标为本群/我/对方、本会话内近期发言者的昵称、已加入群的群名，或用户/群原始 ID", raw)
}

// isPlainID 判断字符串是否可作为原始目标 ID（ASCII 标识符，不含空白）。
func isPlainID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// specialTarget 处理内置别名（本群/我/对方）。
// matched=false 表示不是内置别名。
func (s *loopToolSender) specialTarget(raw string, chat platform.ChatInfo) (ChatTarget, string, bool, error) {
	switch raw {
	case "本群", "这个群", "当前群", "本群聊", "群里":
		if !chat.IsGroup || chat.ID == "" {
			return ChatTarget{}, "", true, fmt.Errorf("当前会话不是群聊，无法解析 %q", raw)
		}
		return ChatTarget{ID: chat.ID, IsGroup: true}, "本群（" + chat.ID + "）", true, nil
	case "我", "我自己", "给我":
		caller, ok := CallerInfoFromContext(s.ctx.Context())
		if !ok || caller.ID == "" {
			caller = s.ctx.GetSenderInfo()
		}
		if caller.ID == "" {
			return ChatTarget{}, "", true, fmt.Errorf("无法获取当前用户信息")
		}
		return ChatTarget{ID: caller.ID}, "我（" + caller.ID + "）", true, nil
	case "对方", "本会话", "这里":
		if chat.IsGroup || chat.ID == "" {
			return ChatTarget{}, "", true, fmt.Errorf("当前会话不是私聊，无法解析 %q", raw)
		}
		return ChatTarget{ID: chat.ID}, "对方（" + chat.ID + "）", true, nil
	}
	return ChatTarget{}, "", false, nil
}

// matchRecentSpeaker 在当前会话 messagelog 中按昵称匹配近期发言者。
// 先精确匹配，再大小写不敏感匹配；同名多人返回候选 ID 列表错误。
func (s *loopToolSender) matchRecentSpeaker(raw string, chat platform.ChatInfo) (ChatTarget, string, bool, error) {
	if s.p.history == nil {
		return ChatTarget{}, "", false, nil
	}
	var entries []messagelog.RecordEntry
	if chat.IsGroup {
		entries = s.p.history.QueryGroupRecent(chat.ID, recentSpeakerWindow)
	} else {
		entries = s.p.history.QueryUser(chat.ID, recentSpeakerWindow)
	}
	if len(entries) == 0 {
		return ChatTarget{}, "", false, nil
	}

	// 按用户去重（同一用户保留最早昵称），排除出站与空昵称。
	seen := make(map[string]string)
	for _, e := range entries {
		if e.IsOutbound || e.UserID == "" || e.UserName == "" {
			continue
		}
		if _, ok := seen[e.UserID]; !ok {
			seen[e.UserID] = e.UserName
		}
	}

	var exact, fuzzy []string
	for uid, name := range seen {
		if name == raw {
			exact = append(exact, uid)
		} else if strings.EqualFold(name, raw) {
			fuzzy = append(fuzzy, uid)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = fuzzy
	}
	slices.Sort(candidates)

	switch len(candidates) {
	case 0:
		return ChatTarget{}, "", false, nil
	case 1:
		return ChatTarget{ID: candidates[0]}, fmt.Sprintf("%s（%s）", raw, candidates[0]), true, nil
	default:
		return ChatTarget{}, "", true, fmt.Errorf(
			"「%s」匹配到多个近期发言者（%s），请使用原始 ID 指定目标", raw, strings.Join(candidates, "、"))
	}
}

// matchJoinedGroupName 在机器人已加入的群中按群名匹配（平台支持时）。
func (s *loopToolSender) matchJoinedGroupName(ctx context.Context, raw string) (ChatTarget, string, bool, error) {
	sender := s.ctx.GetPlatformSender()
	if sender == nil {
		return ChatTarget{}, "", false, nil
	}
	gip, ok := sender.(platform.GroupInfoProvider)
	if !ok {
		return ChatTarget{}, "", false, nil
	}
	groups, err := gip.GetJoinedGroups(ctx)
	if err != nil || len(groups) == 0 {
		return ChatTarget{}, "", false, nil
	}
	var matches []platform.GroupInfo
	for _, g := range groups {
		if g.Name == raw || strings.EqualFold(g.Name, raw) {
			matches = append(matches, g)
		}
	}
	if len(matches) == 0 {
		return ChatTarget{}, "", false, nil
	}
	if len(matches) > 1 {
		return ChatTarget{}, "", true, fmt.Errorf("「%s」匹配到多个已加入群，请使用群 ID 指定目标", raw)
	}
	g := matches[0]
	return ChatTarget{ID: g.ID, IsGroup: true}, fmt.Sprintf("%s（%s）", g.Name, g.ID), true, nil
}

// buildSendTools 构建 AI 消息发送工具列表（send_message / send_to，默认启用）。
func (p *Plugin) buildSendTools() []Tool {
	msgProps := map[string]ToolParamSchema{
		"message":     {Type: "string", Description: "消息文本内容"},
		"format":      {Type: "string", Description: "消息格式：text（默认）或 markdown", Enum: []string{"text", "markdown"}},
		"image_url":   {Type: "string", Description: "可选，附带一张图片的 URL"},
		"mention_ids": {Type: "array", Items: &ToolParamSchema{Type: "string"}, Description: "可选，需要 @ 的用户 ID 列表"},
	}

	sendMessage := Tool{
		Name:        sendMessageToolName,
		Categories:  []string{CategoryGeneral},
		Description: "向当前会话发送一条消息（文本/Markdown/图片/@提及）。用于多步骤任务中向用户展示阶段性进度或中间结果；最终答复请直接作为回复文本返回，不要使用本工具",
		Parameters: ToolParamSchema{
			Type:       "object",
			Properties: msgProps,
			Required:   []string{"message"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			sender, ok := ToolSenderFromContext(ctx)
			if !ok {
				return "", fmt.Errorf("当前上下文无消息发送能力")
			}
			msg, err := buildOutboundMessage(args)
			if err != nil {
				return "", err
			}
			sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
			defer cancel()
			if _, err := sender.ReplyToChat(sendCtx, msg); err != nil {
				return "", err
			}
			return "消息已发送", nil
		},
	}

	sendToProps := make(map[string]ToolParamSchema, len(msgProps)+2)
	maps.Copy(sendToProps, msgProps)
	sendToProps["target"] = ToolParamSchema{
		Type:        "string",
		Description: "目标：本群/我/对方、本会话内近期发言者的昵称、已加入群的群名，或用户/群原始 ID（is_group 指定类型）",
	}
	sendToProps["is_group"] = ToolParamSchema{
		Type:        "boolean",
		Description: "仅 target 为原始 ID 时生效：true=群聊目标，默认 false=私聊用户；昵称/群名解析时以匹配结果为准",
	}

	sendTo := Tool{
		Name:        sendToToolName,
		Categories:  []string{CategoryGeneral},
		Description: "向指定用户或群发送一条消息。target 自动解析：本群/我/对方、本会话内近期发言者的昵称（如\"给张三发消息\"）、已加入群的群名、或用户/群原始 ID。发送前需要发起者审批且具备 ai.message.send 权限",
		// RequiresApproval 兼容 restricted 审批模式；AlwaysRequireApproval
		// 保证 off 模式下也必须审批。
		RequiresApproval:      true,
		AlwaysRequireApproval: true,
		Permissions:           []string{sendToPermission},
		Parameters: ToolParamSchema{
			Type:       "object",
			Properties: sendToProps,
			Required:   []string{"target", "message"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			sender, ok := ToolSenderFromContext(ctx)
			if !ok {
				return "", fmt.Errorf("当前上下文无消息发送能力")
			}
			raw, _ := args["target"].(string)
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return "", fmt.Errorf("target 不能为空")
			}
			isGroup := false
			if v, ok := args["is_group"].(bool); ok {
				isGroup = v
			}
			target, display, err := sender.ResolveTarget(ctx, raw, isGroup)
			if err != nil {
				return "", err
			}
			msg, err := buildOutboundMessage(args)
			if err != nil {
				return "", err
			}
			sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
			defer cancel()
			if _, err := sender.SendTo(sendCtx, target, msg); err != nil {
				return "", err
			}
			return "消息已发送到 " + display, nil
		},
	}

	return []Tool{sendMessage, sendTo}
}

// buildOutboundMessage 将 LLM 参数组装为平台消息（文本/Markdown/图片/@提及）。
func buildOutboundMessage(args map[string]any) (platform.OutboundMessage, error) {
	text, _ := args["message"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return platform.OutboundMessage{}, fmt.Errorf("message 不能为空")
	}
	if n := len([]rune(text)); n > maxSendMessageRunes {
		return platform.OutboundMessage{}, fmt.Errorf("message 过长（最多 %d 字）", maxSendMessageRunes)
	}

	msg := platform.OutboundMessage{Text: text}
	if format, _ := args["format"].(string); format == "markdown" {
		msg.Markdown = text
	}
	if url, _ := args["image_url"].(string); strings.TrimSpace(url) != "" {
		msg.Attachments = append(msg.Attachments, platform.Attachment{
			Kind: platform.AttachmentKindImage,
			URL:  strings.TrimSpace(url),
		})
	}
	if mentions, ok := args["mention_ids"].([]any); ok {
		for _, m := range mentions {
			if s, ok := m.(string); ok && strings.TrimSpace(s) != "" {
				msg.Mentions = append(msg.Mentions, strings.TrimSpace(s))
			}
		}
	}
	return msg, nil
}
