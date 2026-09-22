// Package ai sendtool.go — 消息发送动作的发送器（ToolSender 实现）。
//
// send_message / send_to 两个动作本身在 builtin/ai/catalog（动作目录层），
// 本文件只实现它们依赖的 ToolSender：把动作的发送请求路由到事件上下文
// 关联的平台，并叠加审批授权与每轮发送预算。
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
//   - SendTo 能力仅在动作调用通过审批门后注入（sendToAllowed），
//     嵌套 Skill 调用继承同一 context，无法绕过审批
//   - 每次对话处理（一轮运行）内发送次数受 sendBudget 约束
//     （原子计数，并行执行安全）
package ai

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

const (
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
	ctx *eventctx.Context
	p   *Plugin
	// sendToAllowed 由审批门（decideToolApproval）一次性授予，SendTo 只消费
	// 该授权、不重新推导审批条件：授权只有一个来源。
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

// SendTo 向指定用户/群推送消息。仅当本次工具调用经审批门授予 SendTo 授权
// 时可用，否则返回错误——嵌套 Skill 调用继承同一 context，拿到的是未授权的
// sender，无法绕过该门控。
func (s *loopToolSender) SendTo(ctx context.Context, target toolkit.ChatTarget, msg platform.OutboundMessage) (platform.SendResult, error) {
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

	sendCtx, cancel := context.WithTimeout(ctx, catalog.SendTimeout)
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
func (s *loopToolSender) ResolveTarget(ctx context.Context, raw string, isGroup bool) (toolkit.ChatTarget, string, error) {
	return s.resolveTarget(ctx, raw, isGroup)
}

// resolveTarget 按优先级解析目标：
//  1. 内置别名（本群/我/对方）
//  2. 当前会话近期发言者昵称（messagelog）
//  3. 已加入群群名（平台支持时）
//  4. 原始 ID 兜底（仅 ASCII 标识符，isGroup 提示指定类型）
//
// 返回 (目标, 展示文本, 错误)。同名歧义时返回候选 ID 列表错误。
func (s *loopToolSender) resolveTarget(ctx context.Context, raw string, isGroupHint bool) (toolkit.ChatTarget, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return toolkit.ChatTarget{}, "", fmt.Errorf("target 不能为空")
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
	if s.isPlainID(raw) {
		return toolkit.ChatTarget{ID: raw, IsGroup: isGroupHint}, raw, nil
	}

	return toolkit.ChatTarget{}, "", fmt.Errorf(
		"无法解析目标 %q：可用目标为本群/我/对方、本会话内近期发言者的昵称、已加入群的群名，或用户/群原始 ID", raw)
}

// isPlainID 判断字符串是否可作为原始目标 ID（ASCII 标识符，不含空白）。
func (s *loopToolSender) isPlainID(raw string) bool {
	if raw == "" {
		return false
	}
	for _, r := range raw {
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
func (s *loopToolSender) specialTarget(raw string, chat platform.ChatInfo) (toolkit.ChatTarget, string, bool, error) {
	switch raw {
	case "本群", "这个群", "当前群", "本群聊", "群里":
		if !chat.IsGroup || chat.ID == "" {
			return toolkit.ChatTarget{}, "", true, fmt.Errorf("当前会话不是群聊，无法解析 %q", raw)
		}
		return toolkit.ChatTarget{ID: chat.ID, IsGroup: true}, "本群（" + chat.ID + "）", true, nil
	case "我", "我自己", "给我":
		caller, ok := toolkit.CallerInfoFromContext(s.ctx.Context())
		if !ok || caller.ID == "" {
			caller = s.ctx.GetSenderInfo()
		}
		if caller.ID == "" {
			return toolkit.ChatTarget{}, "", true, fmt.Errorf("无法获取当前用户信息")
		}
		return toolkit.ChatTarget{ID: caller.ID}, "我（" + caller.ID + "）", true, nil
	case "对方", "本会话", "这里":
		if chat.IsGroup || chat.ID == "" {
			return toolkit.ChatTarget{}, "", true, fmt.Errorf("当前会话不是私聊，无法解析 %q", raw)
		}
		return toolkit.ChatTarget{ID: chat.ID}, "对方（" + chat.ID + "）", true, nil
	}
	return toolkit.ChatTarget{}, "", false, nil
}

// matchRecentSpeaker 在当前会话 messagelog 中按昵称匹配近期发言者。
// 先精确匹配，再大小写不敏感匹配；同名多人返回候选 ID 列表错误。
func (s *loopToolSender) matchRecentSpeaker(raw string, chat platform.ChatInfo) (toolkit.ChatTarget, string, bool, error) {
	if s.p.history == nil {
		return toolkit.ChatTarget{}, "", false, nil
	}
	var entries []messagelog.RecordEntry
	if chat.IsGroup {
		entries = s.p.history.QueryGroupRecent(chat.ID, recentSpeakerWindow)
	} else {
		entries = s.p.history.QueryUser(chat.ID, recentSpeakerWindow)
	}
	if len(entries) == 0 {
		return toolkit.ChatTarget{}, "", false, nil
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
		return toolkit.ChatTarget{}, "", false, nil
	case 1:
		return toolkit.ChatTarget{ID: candidates[0]}, fmt.Sprintf("%s（%s）", raw, candidates[0]), true, nil
	default:
		return toolkit.ChatTarget{}, "", true, fmt.Errorf(
			"「%s」匹配到多个近期发言者（%s），请使用原始 ID 指定目标", raw, strings.Join(candidates, "、"))
	}
}

// matchJoinedGroupName 在机器人已加入的群中按群名匹配（平台支持时）。
func (s *loopToolSender) matchJoinedGroupName(ctx context.Context, raw string) (toolkit.ChatTarget, string, bool, error) {
	sender := s.ctx.GetPlatformSender()
	if sender == nil {
		return toolkit.ChatTarget{}, "", false, nil
	}
	gip, ok := sender.(platform.GroupInfoProvider)
	if !ok {
		return toolkit.ChatTarget{}, "", false, nil
	}
	groups, err := gip.GetJoinedGroups(ctx)
	if err != nil || len(groups) == 0 {
		return toolkit.ChatTarget{}, "", false, nil
	}
	var matches []platform.GroupInfo
	for _, g := range groups {
		if g.Name == raw || strings.EqualFold(g.Name, raw) {
			matches = append(matches, g)
		}
	}
	if len(matches) == 0 {
		return toolkit.ChatTarget{}, "", false, nil
	}
	if len(matches) > 1 {
		return toolkit.ChatTarget{}, "", true, fmt.Errorf("「%s」匹配到多个已加入群，请使用群 ID 指定目标", raw)
	}
	g := matches[0]
	return toolkit.ChatTarget{ID: g.ID, IsGroup: true}, fmt.Sprintf("%s（%s）", g.Name, g.ID), true, nil
}
