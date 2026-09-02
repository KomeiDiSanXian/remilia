// Package ai qqaction.go — QQ"重新生成/清空会话"操作按钮接入。
//
// QQ v2 公开文档中的消息按钮只有 keyboard（挂在 markdown 消息上的按钮行，
// 见 https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/msg-btn.html）；
// action_button（含模板 "1"/"10"）无对应公开文档，真机冒烟（2026-09）验证：
//   - 模板 "1"（ReGenerate=true）下客户端只渲染"赞/踩"反馈行（type=13 回调，
//     feedback_opt=LIKE/DISLIKE，并把 callback_data 原样回传为 button_data），
//     没有"重新生成"控件；
//   - 模板 "10"（重新生成/停止生成按钮）直接被服务端拒绝：HTTP 400 code=50061
//     "请求参数不支持"，导致整条回复发送失败。
//
// 按钮走官方 keyboard 通道（与 builtin/about 的按钮同一通道，客户端可正常
// 渲染），但**不用 type=1 回调按钮**：QQ webhook 下 INTERACTION_CREATE 投递
// 存在秒级延迟（真机冒烟约 2s），挤占互动事件 3s 回应窗口——即使服务端
// PUT /interactions/{id} 成功，客户端仍显示"请求第三方失败"（2026-09 真机
// 验证，与 builtin/about 现象一致）。因此改用 **type=2 指令按钮**（与
// builtin/about 同方案，见官方《消息按钮》action.type=2 / action.enter）：
//
//   - Command 字段下发文本命令，点击后 QQ 把命令填入输入框；ButtonExtra 设
//     Enter=true 时手机端（客户端 8983+）点击后自动作为用户消息发送，桌面端
//     填入输入框由用户手动发送（Enter 仅 QQ 单聊可用，群聊点击一律填入
//     "@bot <命令>" 由用户手动发送）；
//   - 点击不产生 INTERACTION_CREATE，无 loading / "请求第三方失败"；
//   - 命令走常规消息管线（等价用户手输文本命令），可靠、可被 /ai stop 与
//     用户新消息抢占等既有机制约束。
//
// 接入策略：
//   - QQ 单聊与群聊（频道除外）的 Markdown AI 回复附加按钮（QQ 官方按钮
//     只能挂在 markdown 消息上；纯文本回复不附加，避免把用户显式配置的纯
//     文本强制迁移成 markdown 消息类型；富媒体/频道消息不附加）。
//   - "重新生成"（qq_regen_button，默认开）：指令按钮命令 /ai retry，复用
//     /ai retry 路径（重新生成上一条回复）；忙时与文本命令一致——等待当前
//     回合结束后再执行。
//   - "清空会话"（qq_clear_button，默认开）：指令按钮命令 /ai reset，语义与
//     /ai reset 一致：忙时（生成中的回合仍持有会话指针，此时删除会让进行中
//     回合失去历史且仍会输出）拒绝并节流提示，空闲时删除本插件会话历史并
//     确认。QQ 客户端自带的"清空会话"入口是官方原生互动（type=14，
//     CLEAR_SESSION，见《互动事件》文档
//     https://bot.q.qq.com/wiki/develop/api-v2/autogen/event/interaction_create.html），
//     其 data.resolved 无按钮数据，平台层为其合成内容 "clear_session"
//     （platform/qq/event.go 的 clearSessionContent）；同样分派到
//     handleClearAction，语义一致。
//   - 防重复触发：指令按钮点击等价连发文本命令，故 /ai retry、/ai reset 子
//     命令路径带会话级触发冷却（qqActionClickAllowed，与回调共用动作键），
//     冷却窗口内重复触发静默忽略；忙时提示按会话节流，避免生成期间反复
//     触发造成提示刷屏。
//   - 计划消息按钮（qq_plan_button，默认开）：AI 创建任务计划（create_plan）
//     时同步推送的"计划已创建"消息、以及 /ai plan 状态回复，在 QQ 单聊
//     与群聊 Markdown 场景附带"查看计划"（Command=/ai plan）与"停止生成"
//     （Command=/ai stop，仅回合进行中时附加）指令按钮，长任务执行期间可
//     一键刷新进度或中断（见 maybeAttachQQPlanButtons）。
//   - 自动发送策略：仅"重新生成/查看计划"在 QQ 单聊（Enter=true，手机端
//     8983+）自动发送；"清空会话/停止生成"不自动发送——点击后仅填入输入框
//     由用户手动发送，避免误触（清空/停止属破坏性动作）。
//   - "停止生成"与计划取消：/ai stop 在中断进行中回合的同时，若会话存在
//     进行中的任务计划会一并取消（半途中断会让下一轮按旧计划继续自动推进，
//     见 handleStopCommand / subcommand.go）。
//   - type=1 回调兜底：handleInteraction 仍识别 ai:regenerate / ai:clear
//     （改版前已下发消息上的旧按钮点击、其他平台回调），approval 审批按钮
//     亦走该通道（见 approval.go）。
//   - "停止生成"入口：QQ 单聊与群聊均为整条发送（本插件未接 C2C 流式接口），
//     无法在流式渲染期间提供原生停止按钮；停止以计划消息上的"停止生成"指令
//     按钮（Command=/ai stop，见上）与文本命令 /ai stop 提供（中断管线见
//     Session.TurnCtx / process.go，用户新消息抢占亦走同一管线）。
//   - 文本命令 /ai retry、/ai stop、/ai reset 始终可用，按钮只是快捷入口。
package ai

import (
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	qq "github.com/KomeiDiSanXian/remilia/platform/qq"
)

const (
	// regenButtonData 是"重新生成"键盘按钮的 ID 与限流动作键。指令按钮以
	// Command=regenButtonCommand 下发（ID 不参与点击交互）；type=1 旧按钮
	// 回调（改版前已下发消息上的按钮）经 INTERACTION_CREATE 的 button_data
	// 原样回传时也以此值识别。
	regenButtonData = "ai:regenerate"
	// regenButtonCommand 是"重新生成"指令按钮下发的命令文本：点击后（手机端
	// 自动发送/桌面端填入输入框）进入 /ai retry 子命令路径。
	regenButtonCommand = "/ai retry"
	// clearButtonData 是"清空会话"键盘按钮的 ID 与限流动作键（同上机制）。
	clearButtonData = "ai:clear"
	// clearButtonCommand 是"清空会话"指令按钮下发的命令文本（→ /ai reset）。
	clearButtonCommand = "/ai reset"
	// planViewButtonData 是计划消息"查看计划"按钮的 ID（限流/兼容识别用）。
	planViewButtonData = "ai:plan"
	// planViewButtonCommand 是"查看计划"指令按钮下发的命令文本（→ /ai plan，
	// 查看当前计划进度）。
	planViewButtonCommand = "/ai plan"
	// planStopButtonData 是计划消息"停止生成"按钮的 ID（同上）。
	planStopButtonData = "ai:stop"
	// planStopButtonCommand 是"停止生成"指令按钮下发的命令文本（→ /ai stop，
	// 中断当前正在生成的回合并取消进行中的任务计划，见 handleStopCommand）。
	planStopButtonCommand = "/ai stop"
	// clearSessionNativeContent 是平台层为原生"清空会话"互动（type=14，
	// CLEAR_SESSION）合成的内容标记（platform/qq/event.go 的 clearSessionContent）。
	// 与清空会话指令按钮语义一致：用户想清空本插件会话历史，故共用同一处理。
	clearSessionNativeContent = "clear_session"
)

// qqActionClickCooldown 是同一会话同一动作两次触发之间的最短间隔。
// 指令按钮连点会连发多条文本命令，此窗口内重复触发被静默忽略，
// 避免触发多轮重复操作刷屏。
const qqActionClickCooldown = 3 * time.Second

// qqActionBusyNoticeInterval 是同一会话"正在生成，请稍后再试"提示的最小间隔。
// 生成可能持续较久，忙时每次触发都回一句提示同样会刷屏，故按会话节流。
const qqActionBusyNoticeInterval = 10 * time.Second

// qqActionRateState 记录同一会话同一动作的限流状态。
type qqActionRateState struct {
	lastClick      time.Time // 最近一次被接受的触发（空闲路径）
	lastBusyNotice time.Time // 最近一次忙时提示
}

// qqActionRateKey 组装限流 map 的键：动作（ai:regenerate / ai:clear /
// "重新生成" 等）与会话 ID 以 NUL 分隔，保证不同动作互不干扰。
func qqActionRateKey(action, sessionID string) string {
	return action + "\x00" + sessionID
}

// shouldAttachQQButtons 判断是否给消息附加 QQ 操作键盘按钮（重新生成/清空会话）。
//
// 条件：至少一个按钮配置开启 + QQ 平台 + 单聊/群聊（频道除外）+ Markdown 正文且
// 无附件/已有按钮/出站段（QQ 官方 keyboard 只能挂在 markdown 消息上，其余场景
// 会被平台忽略或整条发送失败）。频道（ParentID 非空，含频道私信）不附加：
// QQ 频道消息场景暂不接入操作按钮。
func shouldAttachQQButtons(anyEnabled bool, platformName string, chat platform.ChatInfo, msg platform.OutboundMessage) bool {
	if !anyEnabled || platformName != "qq" {
		return false
	}
	if chat.ParentID != "" {
		return false
	}
	if msg.Markdown == "" {
		return false
	}
	return len(msg.Attachments) == 0 && len(msg.Buttons) == 0 && len(msg.Segments) == 0
}

// maybeAttachQQButtons 在满足条件时给 AI 回复消息附加 QQ 操作键盘按钮。
//
// "重新生成"与"清空会话"排在同一行（Row=1），各按钮按对应配置独立开关。
// 按钮为 QQ 指令按钮（平台层映射为 action.type=2，见 newQQActionButton）：
// Command 下发 /ai retry、/ai reset 文本命令——点击不产生互动回调，规避 QQ
// webhook 下回调按钮"请求第三方失败"的问题（与 builtin/about 一致）。
// "重新生成"在单聊携带 Enter=true（手机端 8983+ 点击自动发送，群聊不支持
// Enter 故仅填入输入框）；"清空会话"始终不自动发送（破坏性动作，点击仅填入
// 输入框由用户手动发送，避免误触）。
// ID 保留用于限流动作键与旧回调兼容识别。
func (p *Plugin) maybeAttachQQButtons(ctx *context.Context, msg platform.OutboundMessage) platform.OutboundMessage {
	if p.cfg == nil {
		return msg
	}
	anyEnabled := p.cfg.QQRegenButton || p.cfg.QQClearButton
	chat := ctx.GetChatInfo()
	if !shouldAttachQQButtons(anyEnabled, ctx.GetEventPlatform(), chat, msg) {
		return msg
	}
	// QQ 的 Enter（点击自动发送）仅单聊可用（手机端 8983+）：群聊点击后
	// 客户端自动在输入框填入 "@bot <命令>"，由用户手动发送，故不设 Enter。
	autoSend := !chat.IsGroup
	var buttons []platform.Button
	if p.cfg.QQRegenButton {
		buttons = append(buttons, newQQActionButton(regenButtonData, "重新生成", regenButtonCommand, platform.ButtonStylePrimary, autoSend))
	}
	if p.cfg.QQClearButton {
		buttons = append(buttons, newQQActionButton(clearButtonData, "清空会话", clearButtonCommand, platform.ButtonStyleSecondary, false))
	}
	return msg.WithButtons(buttons...)
}

// maybeAttachQQPlanButtons 在 QQ 单聊/群聊（频道除外）计划消息（"计划已创建"
// 推送、/ai plan 状态回复）上附加操作按钮：
//   - "查看计划"（Command=/ai plan）：长任务执行期间一键刷新计划进度；
//   - "停止生成"（Command=/ai stop）：仅 running=true（回合进行中）时附加，
//     点击中断当前正在生成的回合（等价输入 /ai stop）。
//
// 与重新生成/清空会话按钮同一通道与约束（指令按钮 type=2，QQ 单聊/群聊
// Markdown，见 shouldAttachQQButtons / newQQActionButton）。"查看计划"在单聊
// 携带 Enter=true（手机端自动发送）；"停止生成"始终不自动发送（破坏性动作，
// 点击仅填入输入框由用户手动发送，避免误触）。
func (p *Plugin) maybeAttachQQPlanButtons(ctx *context.Context, msg platform.OutboundMessage, running bool) platform.OutboundMessage {
	if p.cfg == nil || !p.cfg.QQPlanButton {
		return msg
	}
	chat := ctx.GetChatInfo()
	if !shouldAttachQQButtons(p.cfg.QQPlanButton, ctx.GetEventPlatform(), chat, msg) {
		return msg
	}
	autoSend := !chat.IsGroup
	buttons := []platform.Button{
		newQQActionButton(planViewButtonData, "查看计划", planViewButtonCommand, platform.ButtonStylePrimary, autoSend),
	}
	if running {
		buttons = append(buttons, newQQActionButton(planStopButtonData, "停止生成", planStopButtonCommand, platform.ButtonStyleSecondary, false))
	}
	return msg.WithButtons(buttons...)
}

// newQQActionButton 构造一个 QQ 指令按钮：Command 下发文本命令（平台层映射
// 为 action.type=2，点击把命令填入输入框）；autoSend=true 时 Extra 携带
// Enter=true（仅 QQ 单聊手机端 8983+ 生效，点击后自动发送 data），false 时
// 点击仅填入输入框由用户手动发送（桌面端与群聊始终如此）。点击不产生
// INTERACTION_CREATE。
func newQQActionButton(id, label, command string, style platform.ButtonStyle, autoSend bool) platform.Button {
	return platform.Button{
		Row:     1,
		ID:      id,
		Label:   label,
		Style:   style,
		Command: command,
		Extra: map[string]any{
			qq.ExtraKeyButton: &qq.ButtonExtra{Enter: autoSend},
		},
	}
}

// handleInteraction 处理 QQ 平台互动事件（EventKindInteraction）回调。
//
// 当前下发的操作按钮为指令按钮（type=2），点击不产生互动事件；本入口处理
// 剩余的回调通道。单一 matcher 内按回调内容分派（引擎首个命中 matcher 即
// 阻断，审批 matcher 无 Where 过滤会吞掉所有互动事件，因此两类回调必须
// 共用一个 matcher）：
//   - "ai:regenerate" → 重新生成（等价 /ai retry；改版前 type=1 旧按钮兜底）
//   - "ai:clear" / "clear_session" → 清空会话（等价 /ai reset；后者是平台层
//     为原生 type=14 清空会话事件合成的内容）
//   - ai:approve:* / ai:deny:* → 工具执行审批（见 approval.go）
func (p *Plugin) handleInteraction(ctx *context.Context) error {
	switch platform.Content(ctx.GetPlatformEvent()) {
	case regenButtonData:
		return p.handleRegenAction(ctx)
	case clearButtonData, clearSessionNativeContent:
		return p.handleClearAction(ctx)
	default:
		return p.handleApprovalButton(ctx)
	}
}

// handleRegenAction 处理"重新生成"回调（type=1 旧按钮/其他平台回调兜底）。
//
// 会话 ID 由回调事件按点击者重建（与 /ai retry 一致）。防护顺序：
//  1. 会话忙（TurnActive）时不排队、不消耗触发冷却，仅节流提示一次——
//     回合结束后再触发一次即可立即生效，避免忙时触发被误吞；
//  2. 空闲时触发冷却窗口内的重复触发/重复投递直接静默忽略；
//  3. 兜底：忙时判断与点击之间的竞态（判断后回合才启动）由 retryLastReply
//     的 TryLockTurn 拒绝，同样走节流提示。
func (p *Plugin) handleRegenAction(ctx *context.Context) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()
	sessionID := makeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)
	if p.sessionTurnActive(sessionID) {
		p.notifyQQActionBusy(ctx, "重新生成", sessionID)
		return nil
	}
	if !p.qqActionClickAllowed(regenButtonData, sessionID) {
		return nil
	}
	return p.retryLastReply(ctx, sessionID, sender.ID, chat.ID, false)
}

// handleClearAction 处理"清空会话"，/ai reset 文本命令、QQ"清空会话"指令
// 按钮、type=1 旧按钮回调与原生 type=14 清空会话共用此路径，语义一致。
//
// 来源有三，语义一致共用此路径：
//   - /ai reset 文本命令与"清空会话"指令按钮（点击后自动发送 /ai reset）；
//   - "清空会话"旧按钮回调（clearButtonData，键盘回调 type=11）；
//   - QQ 客户端原生"清空会话"入口（clearSessionNativeContent，type=14）。
//
// 防护顺序与 handleRegenAction 相同：会话忙时不清空（生成中的回合持有会话
// 指针，此时删除会让进行中的回合失去历史且仍会输出），仅节流提示一次；
// 空闲时冷却窗口内重复触发静默忽略；通过后删除会话历史，并以确认文案回复。
func (p *Plugin) handleClearAction(ctx *context.Context) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()
	sessionID := makeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)
	if p.sessionTurnActive(sessionID) {
		p.notifyQQActionBusy(ctx, "清空会话", sessionID)
		return nil
	}
	if !p.qqActionClickAllowed(clearButtonData, sessionID) {
		return nil
	}
	if p.sm != nil {
		p.sm.Delete(sessionID)
	}
	ctx.ReplyText(sessionClearedText)
	return nil
}

// sessionTurnActive 返回会话当前是否处于生成回合（TurnActive）。
func (p *Plugin) sessionTurnActive(sessionID string) bool {
	if p.sm == nil {
		return false
	}
	if s := p.sm.Peek(sessionID); s != nil && s.TurnActive() {
		return true
	}
	return false
}

// qqActionClickAllowed 原子地检查并记录一次动作触发（按动作 + 会话独立计，
// 同一会话"重新生成"与"清空会话"互不占用对方冷却）。窗口内重复触发返回
// false；首次触发记录时间戳并返回 true。
//
// 指令按钮点击（自动发送文本命令）与 type=1 回调共用此门闩：/ai retry、
// /ai reset 子命令入口（subcommand.go execSubCommand）先调用本方法再执行。
func (p *Plugin) qqActionClickAllowed(action, sessionID string) bool {
	p.actionMu.Lock()
	defer p.actionMu.Unlock()
	p.pruneQQActionRateLocked()
	key := qqActionRateKey(action, sessionID)
	st := p.actionRate[key]
	now := time.Now()
	if now.Sub(st.lastClick) < qqActionClickCooldown {
		return false
	}
	st.lastClick = now
	p.actionRate[key] = st
	return true
}

// qqActionBusyNoticeAllowed 原子地检查忙时提示是否可发送（按会话节流）。
func (p *Plugin) qqActionBusyNoticeAllowed(action, sessionID string) bool {
	p.actionMu.Lock()
	defer p.actionMu.Unlock()
	p.pruneQQActionRateLocked()
	key := qqActionRateKey(action, sessionID)
	st := p.actionRate[key]
	now := time.Now()
	if now.Sub(st.lastBusyNotice) < qqActionBusyNoticeInterval {
		return false
	}
	st.lastBusyNotice = now
	p.actionRate[key] = st
	return true
}

// notifyQQActionBusy 在会话忙时给出节流后的提示；节流窗口内静默忽略。
// 忙时拒绝不消耗触发冷却（lastClick），回合结束后用户再触发一次即可立即
// 生效。
func (p *Plugin) notifyQQActionBusy(ctx *context.Context, label, sessionID string) {
	if !p.qqActionBusyNoticeAllowed(label, sessionID) {
		return
	}
	ctx.ReplyText(fmt.Sprintf("⏳ 当前还有回复正在生成，稍后再试“%s”即可", label))
}

// pruneQQActionRateLocked 清理超过节流窗口的旧条目，避免 map 无界增长。
// 调用方须持有 actionMu；actionRate 为 nil 时先初始化。
func (p *Plugin) pruneQQActionRateLocked() {
	if p.actionRate == nil {
		p.actionRate = make(map[string]qqActionRateState)
	}
	now := time.Now()
	for key, st := range p.actionRate {
		if now.Sub(st.lastClick) > qqActionClickCooldown &&
			now.Sub(st.lastBusyNotice) > qqActionBusyNoticeInterval {
			delete(p.actionRate, key)
		}
	}
}
