// handler.go — AI 消息路由入口与对话流程控制。
//
// 本文件处理用户消息的三种触发路径：
//   - /ai 命令路径：通过 GetParsedCommand 解析子命令
//   - @机器人 / 私聊路径：通过消息内容清洗后匹配子命令
//   - 均非子命令时进入 AI 对话（handleAIChat）
//
// 包含函数：
//   - handleAI: 消息路由总入口
//   - handleAIChat: AI 对话主流程（会话管理 + 系统提示注入 + LLM 调用）
//   - buildUserMessage: 入站附件归一化（下载、引用图片、多模态拼装）
//
// 消息清洗、会话 ID、命令样式识别、附件类型判定等纯形状工具已归位到
// builtin/ai/runtime（见 runtime/inbound.go）。

package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/promptctx"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/outbound"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// handleAI AI 消息处理器的总入口。
//
// 处理流程：
//  1. 若通过 /ai 命令触发，使用 GetParsedCommand 检测子命令
//  2. 若通过 @bot 或私聊触发，使用 cleanMessage 清洗后检测子命令
//  3. 命令样式正文（/help 等）仅在未显式点名 AI 时被跳过（其他插件的命令）
//  4. 检查 FSM 是否有当前用户的活跃会话（技能添加等两步流程）
//  5. 均非子命令时进入 AI 对话流程
//
// 注意：
//   - FSM 检查必须在 AI 对话之前，确保用户发送的内容被正确处理为技能 Prompt。
//   - “显式点名 AI”（/ai 前缀、私聊直发合并转发）时正文原样交给 AI，
//     不把以符号开头的正文当作其他插件的命令丢弃（见下方 explicit）。
func (p *Plugin) handleAI(ctx *eventctx.Context) error {
	parsed := ctx.GetParsedCommand()
	atts := platform.Attachments(ctx.GetPlatformEvent())

	// explicit：本条消息是否“显式指向 AI”（命令路径 /ai、@机器人+触发前缀、
	// 私聊直发合并转发）。只有显式指向时，正文里的命令样式内容（/help、
	// #tag、C:\path）才原样交给 AI；自主发言/私聊兜底路径仍需跳过其他插件
	// 的命令（见末尾 isCommandMessage 检查），避免抢答。
	explicit := parsed != nil

	if parsed != nil {
		if len(parsed.CommandPath) > 1 {
			return p.execSubCommand(ctx, parsed.CommandPath[1])
		}
		content := runtime.CleanMessage(ctx.GetMessageContent(), p.triggerPrefix())
		if content == "" && len(atts) == 0 {
			// 裸 /ai（无正文、无附件）：沉默会让用户以为插件失灵，
			// 回一句用法提示（与其他子命令路径的提示一致）。
			p.replyFormatted(ctx, p.aiUsageHint())
			return nil
		}
		// 命令路径不检查 FSM：避免 /ai cancel 这类消息被 FSM 的 cancel 事件消费。
		// 纯附件消息（无文本）没有命令语义，直接进入对话。
		if content == "" {
			return p.handleAIChat(ctx, "")
		}
		return p.handleAIChat(ctx, content)
	}

	content := ctx.GetMessageContent()
	if content == "" {
		// 直发合并转发消息：Content 为空（forward 段不进入派生文本），
		// 按会话类型决定是否触发对话（forwardTriggerContent）。
		if rec := runtime.ForwardRecordFromEvent(ctx.GetPlatformEvent()); rec != nil {
			text, trigger := runtime.ForwardTriggerContent(ctx.GetChatInfo(), rec)
			if !trigger {
				return nil
			}
			content = text
			// 转发内容已由 forwardTriggerContent 判定为“应回复”（私聊直发
			// 视为直接对话），不再按命令样式跳过。
			explicit = true
		}
	}
	if content == "" && len(atts) == 0 {
		return nil
	}
	// 触发前缀由 AI 插件独占注册（见 buildAIDefinition），因此“正文以触发
	// 前缀开头”本身就意味着用户显式点名 AI。不能依赖 mentionedBot：QQ
	// GROUP_AT_MESSAGE_CREATE 报文不带 mentions 数组，mentionedBot 恒为
	// false，@机器人+触发前缀 的 matcher 路径会漏判。
	explicit = explicit || runtime.HasTriggerPrefix(content, p.triggerPrefix())

	// per-group @ 触发要求（/ai group set mention on）：群策略显式要求必须 @ 时，
	// 未 @ 机器人的群消息（全局 GroupAutonomous 模式下会进入此路径）直接跳过。
	if require, ok := p.groupRequireMention(ctx); ok && require && !runtime.BotMentioned(ctx) {
		return nil
	}

	// 群自主接话策略（group_reply_policy=smart）：未被显式点名的群消息按
	// 分层决策决定是否插话——强信号（@/触发词/计划推进/在跟 bot 对话）
	// 必回；纯社交/复读/嫌弃/超短消息不回；其余弱信号按概率抽签并受
	// 插话冷静期约束。all（默认）时不做任何过滤，行为与旧版一致。
	// gate 放在 pending-image 记录之前：smart 拒绝时纯图片也不进合并窗口。
	if !explicit && p.autonomousReplyDenied(ctx, content) {
		return nil
	}

	// 纯图片消息（无实质文本）：群聊且未 @/未引用时记录合并窗口，不回复。
	if p.maybeRecordPendingImage(ctx, content, atts) {
		return nil
	}

	content = runtime.CleanMessage(content, p.triggerPrefix())
	if content == "" && len(atts) == 0 {
		return nil
	}
	// 纯附件消息（无文本）不进子命令/FSM 路径，直接进入对话。
	if content == "" {
		return p.handleAIChat(ctx, "")
	}
	if p.handleSubCommand(ctx, content) {
		return nil
	}
	// 命令样式正文默认跳过：自主发言/私聊兜底路径下这通常是其他插件的
	// 命令（如 /help），AI 不应抢答。但显式点名 AI 时正文就是交给 AI 的内容。
	//
	// 本检查位于 parsed == nil 的路径上（命令解析路径在上方已直接进入对话），
	// 主要是 QQ 群 @机器人 这种无法产生 Parsed 的入口：旧行为会把
	// “@机器人 /ai /tmp 是什么”清洗后的 “/tmp 是什么” 当作外部命令丢弃，
	// 用户侧只能看到“发了消息没反应”。
	if !explicit && runtime.IsCommandMessage(content) {
		return nil
	}

	if p.handleFSMTransition(ctx) {
		return nil
	}

	return p.handleAIChat(ctx, content)
}

// handleAIChat 执行 AI 对话流程：获取/创建会话、注入系统提示、追加用户消息（含附件）、调用 LLM。
//
// 准入是**有序链**（见 admitMessage）：每一步都可能提前收尾，顺序本身即语义。
// 本函数只负责回合级编排：会话与回合锁 → 准入 → 落库 → 生成 → 收尾副作用。
func (p *Plugin) handleAIChat(ctx *eventctx.Context, content string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()

	sessionID := runtime.MakeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)
	sess := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)

	// 用户抢占：若上一回合仍在执行（长任务/慢工具），请求中断让其尽快收尾，
	// 避免新消息被长时间阻塞（用户侧永远优先）。
	if sess.TurnActive() {
		sess.RequestInterrupt()
	}
	sess.LockTurn()
	defer sess.UnlockTurn()

	// 用户发消息重置计划后台推进预算（自动推进让位于用户）。
	sess.ResetPlanAuto()

	// 标记回合活跃（中断信号生命周期：BeginTurn → 检查点 → EndTurn）。
	if !sess.BeginTurn() {
		ctx.ReplyError("对话正在处理中，请稍后再试")
		return nil
	}
	defer sess.EndTurn()

	// 稳定系统提示词（框架+自定义指令）：每轮重建同一内容，作为前缀缓存
	// 的可复用起点；逐轮变化的上下文由 processWithTools 挂到本轮用户消息上。
	runtime.SetSystemMessage(sess, p.buildStaticSystemPrompt(ctx))

	// 构建用户消息：前置回复上下文 → 追加 @ 提及的结构化信息 → 提取入站附件转为多模态 ContentParts
	if p.cfg.IncludeReplyContext {
		content = promptctx.PrependReplyContext(p.history, ctx, content)
	}
	if p.cfg.IncludeMentionInfo {
		content = runtime.AppendMentionInfo(content, platform.GetMentions(ctx.GetPlatformEvent()))
	}
	userMsg := p.buildUserMessage(ctx, content, sess)
	userMsg.Timestamp = time.Now()

	// 准入：任一检查不通过即收尾（已给出用户可见反馈），不进入落库与 LLM。
	admitted, err := p.admitMessage(ctx, sess, &userMsg)
	if !admitted {
		return err
	}

	p.sm.AppendMessage(sess, userMsg)

	// LLM 处理前发送"正在输入"状态（平台支持时），给用户即时反馈。
	// 平台不支持（如 QQ 群聊）时 TrySendTyping 静默 no-op。
	_ = ctx.TrySendTyping()

	return p.runAdmittedTurn(ctx, sess)
}

// admitMessage 执行对话前的准入检查，返回是否可以继续落库并调用 LLM。
//
// 顺序即语义（admission_order_test.go 逐条固定）：
//
//  1. 合并窗口：把"先发图再发字"的未消费图片并入本条。引用/回复消息优先级
//     更高，跳过合并以免双图语义混乱；窗口关闭时仍要消费 pending，否则这些
//     引用会一直留到下一条消息，变成跨回合的幽灵图片。
//  2. 总图片上限：必须在合并之后判定——合并与引用注入都会增加图片数。
//  3. 空消息：文本与附件都没有产出时收尾。必须在图片检查之后，
//     否则"附件处理失败"会被误报成"空消息"。
//
// 三步都只读会话与事件，不写历史；userMsg 通过指针就地修改。
func (p *Plugin) admitMessage(ctx *eventctx.Context, sess *session.Session, userMsg *protocol.Message) (bool, error) {
	// 合并必须先于预算检查，且必须把"未能合并的待处理图片"计入本轮计数：
	// 那些图没有进入 ContentParts，但它们本会随本条消息发送，用户看到的是
	// "我发了两张图"。只按 ContentParts 判定会漏掉它们。
	unmerged := p.mergePendingImages(ctx, sess, userMsg)

	if !p.imageBudgetAllows(userMsg.ContentParts, unmerged) {
		logger.Warnf("[AI] Rejected message with %d images (max_images_per_message=%d)",
			runtime.CountImageParts(userMsg.ContentParts)+unmerged, p.cfg.MaxImagesPerMessage)
		ctx.ReplyText(fmt.Sprintf("图片数量超出限制（最多 %d 张），请重新编辑后再发送", p.cfg.MaxImagesPerMessage))
		return false, nil
	}

	return p.rejectEmptyMessage(ctx, userMsg)
}

// mergePendingImages 把未消费的待合并图片并入本条消息。
//
// 返回**未能并入**的图片数。调用方必须把这个数计入本轮图片预算：这些图虽然没
// 进 ContentParts，但它们本会随本条消息发送（用户看到的是"我发了两张图"），
// 只按 ContentParts 判定会漏掉它们。
//
// 顺序依赖（见 admission_order_test.go）：本函数必须在 imageBudgetAllows 之前
// 调用，且返回值必须交给它。
func (p *Plugin) mergePendingImages(ctx *eventctx.Context, sess *session.Session, userMsg *protocol.Message) int {
	pendingRefs, pendingExtra := sess.ConsumePendingImage(p.cfg.ImageMergeWindow, userMsg.Timestamp)
	if len(pendingRefs) == 0 && pendingExtra == 0 {
		return 0
	}
	// 窗口关闭（热重载为 <=0）或本条是引用/回复：消费了但不合并，
	// 也不计入本轮待处理图片。
	if p.cfg.ImageMergeWindow <= 0 || platform.GetReplyToID(ctx.GetPlatformEvent()) != "" {
		return 0
	}

	pendingCount := len(pendingRefs) + pendingExtra
	// 合并后若超过总上限：不合并，把数量交回去让预算检查统一拒绝，
	// 保证"超限"只有一条提示文案与一处判定。
	if runtime.CountImageParts(userMsg.ContentParts)+pendingCount > p.cfg.MaxImagesPerMessage {
		return pendingCount
	}

	// 引用 → 多模态 ContentPart（messagelog 存储优先，URL 兜底）；
	// 解析失败的图片跳过，不影响本条文字。
	if parts := p.hydratePendingImageRefs(ctx, sess, pendingRefs); len(parts) > 0 {
		*userMsg = runtime.MergePendingImageParts(*userMsg, parts)
		return pendingExtra // 纯计数部分（无引用）仍只以数量形式存在
	}
	if pendingCount > 0 && userMsg.Content != "" {
		// 合并目标图片全部解析失败：给用户可见提示，避免发了字却看不到图、
		// 无从知道为什么。
		ctx.ReplyText(fmt.Sprintf("（此前发出的 %d 张图片已无法加载，本次仅处理文字）", pendingCount))
	}
	return pendingExtra
}

// imageBudgetAllows 判断本条消息的图片数是否在 max_images_per_message 之内。
//
// 视觉禁用时附件不会进入请求，因此不参与上限判定：否则关掉 vision 之后
// 发多图会被一条与视觉无关的规则拦住。
func (p *Plugin) imageBudgetAllows(parts []protocol.ContentPart, extra int) bool {
	if !p.cfg.VisionEnabled {
		return true
	}
	return runtime.CountImageParts(parts)+extra <= p.cfg.MaxImagesPerMessage
}

// rejectEmptyMessage 收尾"没有任何可送入模型的内容"的消息。
//
// 无文本且无附件片段时不能继续（否则是一条空 user 消息）。若原始事件确实
// 带了附件或引用了消息，说明附件处理全失败（SSRF/超限/超时/格式不支持），
// 必须给用户可见反馈，避免"发了语音/图片没反应"；原始事件本无附件且无文本
// 的场景（群兜底误触发等）保持静默。
func (p *Plugin) rejectEmptyMessage(ctx *eventctx.Context, userMsg *protocol.Message) (bool, error) {
	if userMsg.Content != "" || len(userMsg.ContentParts) > 0 {
		return true, nil
	}
	evtAtts := platform.Attachments(ctx.GetPlatformEvent())
	if len(evtAtts) > 0 || platform.GetReplyToID(ctx.GetPlatformEvent()) != "" {
		logger.Debugf("[AI] Message had %d attachment(s) but none processed", len(evtAtts))
		ctx.ReplyText("这条消息没有我能处理的内容（仅支持文本、图片和语音）；请检查附件是否下载失败或格式不受支持")
	}
	return false, nil
}

// runAdmittedTurn 执行已通过准入的回合：生成回复、发送、触发收尾副作用。
func (p *Plugin) runAdmittedTurn(ctx *eventctx.Context, sess *session.Session) error {
	// 生成回答并（开启 verify_enabled 时）经校验器校验，失败按反馈重新生成。
	result, err := p.generateVerified(ctx, sess)
	if err != nil {
		// 多轮工具循环中可能已经捕获到可用回复文本（如超深提前收尾），
		// 先把它发出去再附错误提示，避免一整轮 LLM/工具工作凭空蒸发。
		if result != nil && result.Text != "" {
			p.replyAndRecord(ctx, p.formatReplyMessage(result.Text))
		}
		ctx.ReplyError(runtime.FormatAIError(err))
		return nil
	}

	if result.Text != "" || len(result.Attachments) > 0 {
		msg := p.formatReplyMessage(result.Text)
		if len(result.Attachments) > 0 {
			msg.Attachments = result.Attachments
		}

		p.replyAndRecord(ctx, p.maybeAttachQQButtons(ctx, msg.WithQuoteTrigger()))
	}

	// 对话回复完成后异步抽取长期记忆（memory_enabled 开启时）。
	// 不阻塞回复发送；节流与失败均由 maybeExtractMemory 内部处理。
	p.maybeExtractMemory(ctx, sess)

	// 计划后台自动推进（plan_auto_continue 开启时）：计划未完成且
	// 用户无新消息时，按间隔自动继续执行并主动汇报。
	p.maybeContinuePlan(ctx, sess)

	return nil
}

// buildUserMessage 构建用户消息，包含文本内容及入站图片/音频附件。
//
// 附件下载后通过 session.contentCache 缓存（TTL 10 分钟），同一附件多次使用不需重复下载。
// 超出大小限制或下载失败的附件会被静默跳过（日志 Debug 记录）。
//
// 引用消息中的被引用图片：用户"回复一张图片并 @ 机器人"时本条消息自身没有
// 图片附件（如 QQ 引用富媒体消息 message_type=103），此时从 reply 段
// Extra（raw_quote / parallel_message）提取被引用图片作为视觉输入注入，
// 使模型能看到被回复的图。本条消息自带图片附件时不注入，避免重复上传与
// 语义混淆。
func (p *Plugin) buildUserMessage(ctx *eventctx.Context, content string, sess *session.Session) protocol.Message {
	msg := protocol.Message{Role: protocol.RoleUser, Content: content}

	atts := platform.Attachments(ctx.GetPlatformEvent())

	if p.cfg.VisionEnabled && !runtime.HasImageAttachment(atts) {
		// 直发合并转发记录内的图片：作为本条消息的视觉输入注入
		// （上限 max_images_per_message，超出截断；截断部分仍以 [图片]
		// 占位符出现在渲染文本中）。
		atts = append(atts, runtime.ForwardRecordImageAtts(ctx.GetPlatformEvent(), p.cfg.MaxImagesPerMessage)...)
	}

	var quotedImg *protocol.ContentPart
	if p.cfg.VisionEnabled && !runtime.HasImageAttachment(atts) {
		quotedImg = p.quotedImagePart(ctx, sess)
	}

	if len(atts) == 0 && quotedImg == nil {
		return msg
	}

	parts := make([]protocol.ContentPart, 0, 1+len(atts)+2)
	if content != "" {
		parts = append(parts, protocol.ContentPart{Type: protocol.ContentPartText, Text: content})
	}

	for _, att := range atts {
		if att.URL == "" {
			continue
		}

		// 平台已提供语音转写文本（如 QQ 官方 ASR asr_refer_text）时，
		// 直接注入转写文本，无需下载音频二进制再送 STT。
		// 这比把音频直传给多模态模型更通用：任何模型都能理解文本。
		if runtime.IsAudioAttachment(att) {
			if asr := platform.AttachmentTranscript(att); asr != "" {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentPartText, Text: "[语音转写] " + asr})
				continue
			}
		}

		var cp *protocol.ContentPart
		switch {
		case runtime.IsImageAttachment(att):
			if !p.cfg.VisionEnabled {
				continue
			}
			cp = p.downloadAttachment(att, sess)
		case runtime.IsAudioAttachment(att):
			if !p.cfg.AudioEnabled {
				continue
			}
			cp = p.downloadAttachment(att, sess)
		}

		if cp != nil {
			parts = append(parts, *cp)
		}
	}

	if quotedImg != nil {
		parts = append(parts,
			protocol.ContentPart{Type: protocol.ContentPartText, Text: "[你回复（引用）的消息中包含这张图片]"},
			*quotedImg)
	}

	if len(parts) > 0 {
		msg.Content = "" // ContentParts 模式，清空 Content 避免内容重复
		msg.ContentParts = parts
	}
	return msg
}

// maybeRecordPendingImage 处理"纯图片消息"（无实质文本）在群聊中的合并窗口记录。
//
// 判定为纯图片且可记录时返回 true（消息被消费，不触发回复）：
//   - 群聊、未 @ 机器人、未引用其他消息（无明确意图，表情包场景）
//   - 有图片附件、无实质文本（QQ 的 "[图片]" 占位符视为无实质文本）
//   - image_merge_window > 0 且 vision_enabled
//
// 私聊或已 @/引用的图片消息走正常对话流程（明确意图，立即处理）。
func (p *Plugin) maybeRecordPendingImage(ctx *eventctx.Context, content string, atts []platform.Attachment) bool {
	if p.cfg.ImageMergeWindow <= 0 || !p.cfg.VisionEnabled {
		return false
	}
	if !runtime.HasImageAttachment(atts) {
		return false
	}
	if runtime.HasSubstantiveText(content) {
		return false
	}
	chat := ctx.GetChatInfo()
	if !chat.IsGroup {
		return false
	}
	if runtime.BotMentioned(ctx) {
		return false
	}
	if platform.GetReplyToID(ctx.GetPlatformEvent()) != "" {
		return false
	}

	sender := ctx.GetSenderInfo()
	sess := p.sm.GetOrCreate(runtime.MakeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID), sender.ID, chat.ID)
	sess.LockTurn()
	defer sess.UnlockTurn()

	// 记录引用而非二进制：不下载表情包，等待窗口内文字消息合并时才水合
	// （messagelog 存储优先，URL 直链兜底）。SSRF/大小校验在水合阶段执行。
	evt := ctx.GetPlatformEvent()
	refs := runtime.PendingImageRefsFromAttachments(chat.ID, evt.ID(), atts)
	if len(refs) == 0 {
		// 判定为纯图片场景但无可用附件引用：仍静默消费，避免表情包走旧路径
		// 用 "[图片]" 占位内容回复。
		return true
	}
	sess.ExtendPendingImage(refs, time.Now(), p.cfg.ImageMergeWindow, p.cfg.MaxImagesPerMessage)
	// 立即持久化引用：重启后窗口内 follow-up 仍可合并（二进制由
	// messagelog 附件存储兜底，不依赖会话内存）。
	p.sm.SaveSession(sess)
	logger.Debugf("[AI] Recorded pending image refs for merge (chat=%s, refs=%d)", chat.ID, len(refs))
	return true
}

// hydratePendingImageRefs 把未消费图片引用解析为多模态 ContentPart。
// 每个引用按"messagelog 存储 → URL 直链"顺序解析；解析失败跳过该图。
func (p *Plugin) hydratePendingImageRefs(ctx *eventctx.Context, sess *session.Session, refs []session.PendingImageRef) []protocol.ContentPart {
	parts := make([]protocol.ContentPart, 0, len(refs))
	for _, ref := range refs {
		if cp := p.resolvePendingImageRef(ctx, sess, ref); cp != nil {
			parts = append(parts, *cp)
		}
	}
	return parts
}

// resolvePendingImageRef 解析单个图片引用。
//
// 三层回退，**顺序即语义**：
//
//  1. 本机附件存储：PlatformMsgID → event_id → 附件行，URL 完全一致的那张优先，
//     否则退而取该消息的第一张图；
//  2. 原始 URL 直连下载：存储未启用/未落库/已清理时的兜底（合并窗口很短，
//     直链通常仍有效）；
//  3. 放弃：返回 nil，由调用方降级为"本次仅处理文字"。
//
// 三层写成"成功即返回、失败即下沉"的三个函数，而不是嵌套 if：嵌套会让
// "第二层到底在什么条件下执行"需要逐层反推，而这正是本函数此前的状态。
func (p *Plugin) resolvePendingImageRef(ctx *eventctx.Context, sess *session.Session, ref session.PendingImageRef) *protocol.ContentPart {
	if cp := p.pendingImageFromStore(ctx.Context(), sess, ref); cp != nil {
		return cp
	}
	if ref.URL == "" {
		return nil
	}
	return p.downloadAttachment(platform.Attachment{URL: ref.URL, MimeType: ref.MimeType}, sess)
}

// pendingImageFromStore 从本机附件存储取回该引用对应的图片（第 1 层）。
//
// 返回 nil 表示存储路径不可用或不命中，调用方应继续尝试 URL 直连。
func (p *Plugin) pendingImageFromStore(ctx context.Context, sess *session.Session,
	ref session.PendingImageRef) *protocol.ContentPart {
	if p.history == nil || ref.PlatformMsgID == "" {
		return nil
	}
	entry, ok := p.history.QueryByEventID(ref.ChatID, ref.PlatformMsgID)
	if !ok || entry.EventID == "" {
		return nil
	}
	rows, err := p.history.AttachmentsByEventID(entry.EventID)
	if err != nil {
		return nil
	}

	// 同一条消息可能带多张图：URL 完全一致的那张最可能是用户所指；
	// 其余只作为兜底（用户连着发多图时，URL 与引用记录的未必一致）。
	var fallback *messagelog.AttachmentMeta
	for i := range rows {
		if !runtime.IsImageAttachmentMeta(&rows[i]) {
			continue
		}
		if ref.URL != "" && rows[i].URL == ref.URL {
			if cp := p.fetchStoredAttachment(ctx, &rows[i], sess); cp != nil {
				return cp
			}
			continue
		}
		if fallback == nil {
			fallback = &rows[i]
		}
	}
	if fallback == nil {
		return nil
	}
	return p.fetchStoredAttachment(ctx, fallback, sess)
}

// quotedImagePart 提取引用消息中的被引用图片并下载；不可用时返回 nil。
//
// 下载复用 downloadAttachment 的缓存 / SSRF 防护 / 大小限制逻辑；
// 下载结果经内容嗅探校验为图片后才注入。
func (p *Plugin) quotedImagePart(ctx *eventctx.Context, sess *session.Session) *protocol.ContentPart {
	evt := ctx.GetPlatformEvent()
	if evt == nil {
		return nil
	}
	// 优先：被引用消息在 messagelog 的已落库附件（URL 过期免疫、内容去重）。
	// 被引用消息是历史消息（已记录/已 flush），按平台消息 ID 解析 event_id 后
	// 从附件存储读取二进制；未命中再走段提取的直链。
	if replyID := platform.GetReplyToID(evt); replyID != "" && p.history != nil {
		chat := ctx.GetChatInfo()
		if entry, ok := p.history.QueryByEventID(chat.ID, replyID); ok && entry.EventID != "" {
			if cp := p.storedImagePart(ctx.Context(), entry.EventID, sess); cp != nil {
				return cp
			}
		}
	}
	// 兜底：从 reply 段 Extra 提取被引用图片 URL（存储未启用/未落库时）。
	url, mime := platform.QuotedImage(evt.Segments())
	if url == "" {
		return nil
	}
	cp := p.downloadAttachment(platform.Attachment{URL: url, MimeType: mime}, sess)
	if cp == nil || cp.Type != protocol.ContentPartImage {
		return nil
	}
	return cp
}

// storedImagePart 从 messagelog 附件存储读取指定消息的第一张图片（含同步下载）。
// 存储不可用 / 无图片行 / 下载失败返回 nil（调用方回退 URL 直链）。
func (p *Plugin) storedImagePart(ctx context.Context, eventID string, sess *session.Session) *protocol.ContentPart {
	rows, err := p.history.AttachmentsByEventID(eventID)
	if err != nil || len(rows) == 0 {
		return nil
	}
	for i := range rows {
		if !runtime.IsImageAttachmentMeta(&rows[i]) {
			continue
		}
		if cp := p.fetchStoredAttachment(ctx, &rows[i], sess); cp != nil {
			return cp
		}
	}
	return nil
}

// fetchStoredAttachment 经 messagelog 附件存储读取单个附件，返回**只带引用**的
// ContentPart（AttachmentID + SourceURL）。
//
// 二进制写入会话缓存，由请求构建阶段按需装入请求副本（见 hydrateAttachmentParts）：
// 会话历史因此永远不持有图片/音频字节，AttachmentID 又让重启后仍能取回。
// 未 ready 的行由 FetchContext 同步触发下载（带 30s 超时）；失败返回 nil。
func (p *Plugin) fetchStoredAttachment(ctx context.Context, a *messagelog.AttachmentMeta, sess *session.Session) *protocol.ContentPart {
	// 缓存命中：只需还原引用，不必再读一次。
	if cached := sess.CachedContent(a.URL); cached != nil {
		return &protocol.ContentPart{
			Type:         runtime.InferPartType(cached.MimeType),
			SourceURL:    a.URL,
			AttachmentID: a.ID,
			MimeType:     cached.MimeType,
			AudioFormat:  cached.AudioFormat,
		}
	}
	if a.ID == 0 {
		return nil
	}

	data, ok := p.readStoredAttachment(ctx, a.ID)
	if !ok {
		return nil
	}

	mimeType := a.MimeType
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	cp := &protocol.ContentPart{
		Type:         runtime.InferPartType(mimeType),
		SourceURL:    a.URL,
		AttachmentID: a.ID,
		MimeType:     mimeType,
	}
	if cp.Type == "" {
		return nil
	}
	if cp.Type == protocol.ContentPartAudio {
		cp.AudioFormat = runtime.InferAudioFormat(mimeType)
		if cp.AudioFormat == "" {
			return nil
		}
	}
	sess.SetCachedContentWithBudget(a.URL, data, mimeType, cp.AudioFormat, p.imageCacheBudget())
	return cp
}

// readStoredAttachment 从 messagelog 附件存储读取二进制（强制大小上限）。
func (p *Plugin) readStoredAttachment(ctx context.Context, attachmentID int64) ([]byte, bool) {
	if p.history == nil || attachmentID <= 0 {
		return nil, false
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rc, err := p.history.FetchContext(fetchCtx, attachmentID)
	if err != nil {
		logger.Debugf("[AI] stored attachment fetch failed (id=%d): %v", attachmentID, err)
		return nil, false
	}
	defer rc.Close()
	return p.readAttachmentBody(rc, fmt.Sprintf("attachment id=%d", attachmentID))
}

// readAttachmentBody 读取附件正文并强制大小上限（max_attachment_size）。
func (p *Plugin) readAttachmentBody(r io.Reader, what string) ([]byte, bool) {
	maxBytes := int64(20 * 1024 * 1024)
	if p != nil && p.cfg != nil && p.cfg.MaxAttachmentSize > 0 {
		maxBytes = p.cfg.MaxAttachmentSize
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		logger.Debugf("[AI] failed to read %s: %v", what, err)
		return nil, false
	}
	if int64(len(data)) > maxBytes {
		logger.Debugf("[AI] %s exceeded size limit (%d bytes), skipped", what, maxBytes)
		return nil, false
	}
	return data, true
}

// downloadAttachment 下载附件，返回**只带引用**的 ContentPart（SourceURL），
// 二进制写入会话缓存，由请求构建阶段按需装入。超出大小限制或下载失败返回 nil。
func (p *Plugin) downloadAttachment(att platform.Attachment, sess *session.Session) *protocol.ContentPart {
	// AI 消息通道只吃图片/语音：视频/文件 kind 显式声明时提前跳过——
	// 此前会把整个附件（最高 20MB）下载完才被 InferPartType 弃置，白费带宽与时间。
	if att.Kind == platform.AttachmentKindVideo || att.Kind == platform.AttachmentKindFile {
		logger.Debugf("[AI] skipped unsupported attachment kind %s: %s", att.Kind, att.URL)
		return nil
	}
	// 先检查缓存
	if cached := sess.CachedContent(att.URL); cached != nil {
		return &protocol.ContentPart{
			Type:        runtime.InferPartType(cached.MimeType),
			SourceURL:   att.URL,
			MimeType:    cached.MimeType,
			AudioFormat: cached.AudioFormat,
		}
	}

	data, mimeType, ok := p.downloadAttachmentBytes(att.URL, att.MimeType)
	if !ok {
		return nil
	}

	cp := &protocol.ContentPart{
		Type:      runtime.InferPartType(mimeType),
		SourceURL: att.URL,
		MimeType:  mimeType,
	}

	// 检查是否真的 image/ audio，否则跳过
	if cp.Type == "" {
		return nil
	}

	// 推理音频格式
	if cp.Type == protocol.ContentPartAudio {
		cp.AudioFormat = runtime.InferAudioFormat(mimeType)
		if cp.AudioFormat == "" {
			// 不支持的音频格式：留 Warn 便于归因"发了语音没反应"。
			logger.Warnf("[AI] Unsupported audio mime %q dropped: %s", mimeType, att.URL)
			return nil
		}
	}

	// 写入缓存
	sess.SetCachedContentWithBudget(att.URL, data, mimeType, cp.AudioFormat, p.imageCacheBudget())

	return cp
}

// downloadAttachmentBytes 下载附件正文（SSRF 白名单 + 30s 超时 + 大小上限）。
// 下载挂到插件生命周期上下文：既不随（可能很快超时的）事件上下文取消，
// 又能在插件关闭时中止在途下载；生命周期上下文缺失（测试）时回退到 Background。
func (p *Plugin) downloadAttachmentBytes(url, mimeHint string) ([]byte, string, bool) {
	if err := p.attachment().Allow(url); err != nil {
		logger.Debugf("[AI] Attachment URL blocked: %v", err)
		return nil, "", false
	}

	downloadBase := p.lifecycleCtx
	if downloadBase == nil {
		downloadBase = context.Background()
	}
	dlCtx, dlCancel := context.WithTimeout(downloadBase, 30*time.Second)
	defer dlCancel()
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		logger.Debugf("[AI] Failed to create download request: %v", err)
		return nil, "", false
	}
	resp, err := p.attachment().Do(req)
	if err != nil {
		logger.Debugf("[AI] Failed to download attachment: %v", err)
		return nil, "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("[AI] Download failed with status %d: %s", resp.StatusCode, url)
		return nil, "", false
	}

	data, ok := p.readAttachmentBody(resp.Body, url)
	if !ok {
		return nil, "", false
	}

	mimeType := mimeHint
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	return data, mimeType, true
}

// imageCacheBudget 返回单会话附件内存缓存的字节预算。
// 配置以 MB 为单位（image_cache_max_mb），0/负数回退到 session.MaxContentCacheBytes。
func (p *Plugin) imageCacheBudget() int64 {
	if p.cfg == nil || p.cfg.ImageCacheMaxMB <= 0 {
		return session.MaxContentCacheBytes
	}
	return int64(p.cfg.ImageCacheMaxMB) << 20
}

// releaseStaleAttachmentBytes 释放历史消息里不再随请求发送的附件二进制。
//
// 请求构建只保留 image_context_window 内的图片/音频（见
// runtime.PrepareRequestMessages），更早的二进制留在会话内存里毫无用处。
// 回合结束时调用一次：只处理带时间戳且早于窗口的消息，当前轮的附件不受影响。
func (p *Plugin) releaseStaleAttachmentBytes(sess *session.Session) {
	if p.cfg == nil || sess == nil {
		return
	}
	window := p.cfg.ImageContextWindow
	if window <= 0 {
		return
	}
	if n := sess.StripBinaryPartsOlderThan(time.Now().Add(-window)); n > 0 {
		logger.Debugf("[AI] Released %d stale attachment part(s) from session %s", n, sess.ID)
	}
}

// buildStaticSystemPrompt 构建 Stable System Prompt —— 唯一的 System 消息。
//
// 只包含两节：
//
//  1. Framework Prompt — 硬编码的 AI 行为规则，不可被用户覆盖
//  2. User Custom Prompt — 配置文件 system_prompt 中的自定义指令
//     （群聊时被 per-group 策略的 prompt 覆盖，见 /ai group set prompt）
//
// 这两节只随配置/群策略变化，在同一会话的连续请求之间字节稳定，因此
// 放在请求最前面作为 LLM 前缀缓存的可复用段。
//
// 任何逐轮变化的内容（运行时上下文、群聊窗口、长期记忆、相关历史）一律
// 不得进入本函数——它们由 [buildDynamicContext] 构建；执行计划则由
// processWithTools 单独构建。两者都在请求构建时挂到本轮用户消息尾部。
// 把动态内容混进 System 消息会让缓存前缀在很靠前的位置失效，历史消息
// 随之整段无法复用（cache hit 长期只有个位数~十几个百分点）。
func (p *Plugin) buildStaticSystemPrompt(ctx *eventctx.Context) string {
	parts := []string{defaultFrameworkPrompt}
	if custom := p.effectiveCustomPrompt(ctx); custom != "" {
		parts = append(parts, "===== 自定义指令 =====\n"+custom)
	}
	return strings.Join(parts, "\n\n")
}

// defaultAttachmentClient 是默认（严格公网）附件下载客户端；插件初始化时
// 按 allow_private_media_hosts 与平台适配器自声明的媒体来源换成受信版本。
//
// 策略见 infra/outbound：直连目标须为公网 IP（拨号前对解析结果二次校验并
// 直连已校验 IP），重定向逐跳校验，代理由 Transport 统一处理（HTTP(S)_PROXY
// 常指向回环/内网，代理拨号不受公网校验约束）。
var defaultAttachmentClient = outbound.MustNew(outbound.Policy{
	Component:    "ai.attachment",
	Class:        outbound.TrustedOrigin,
	Timeout:      30 * time.Second,
	MaxRedirects: 5,
})

// attachmentOrigins 合并附件下载的受信来源：运维名单（allow_private_media_hosts）
// + 平台适配器自声明的媒体源（见 [platform.MediaOriginSource]）。
func attachmentOrigins(hosts []string, src platform.MediaOriginSource) []string {
	origins := append([]string{}, hosts...)
	if src != nil {
		origins = append(origins, src.MediaOrigins()...)
	}
	return origins
}

// newAttachmentClient 按受信媒体来源构建附件下载客户端：命中所声明来源的
// 主机可走内网/明文，其余目标保持严格公网校验。
func newAttachmentClient(origins []string) (*outbound.Client, error) {
	return outbound.New(outbound.Policy{
		Component:    "ai.attachment",
		Class:        outbound.TrustedOrigin,
		Origins:      origins,
		Timeout:      30 * time.Second,
		MaxRedirects: 5,
	})
}

// attachment 返回附件下载客户端：未按配置初始化（测试/降级）时用严格默认值。
func (p *Plugin) attachment() *outbound.Client {
	if p != nil && p.attachmentClient != nil {
		return p.attachmentClient
	}
	return defaultAttachmentClient
}

// handleFSMTransition 检查 FSM 引擎是否有当前用户的活跃会话。
// 有活跃会话时尝试迁移（处理技能添加等两步流程），返回 true 表示消息已被 FSM 消费。
func (p *Plugin) handleFSMTransition(ctx *eventctx.Context) bool {
	if p.fsmEngine == nil {
		return false
	}
	sessionID := p.skillAddSessionID(ctx)
	_, ok, err := p.fsmEngine.TryTransition(ctx, sessionID)
	if err != nil {
		logger.Errorf("[AI] FSM transition error: %v", err)
	}
	return ok
}

// downloadTextAttachment 下载文本附件内容，使用传入的 context 控制生命周期。
func (p *Plugin) downloadTextAttachment(ctx context.Context, att platform.Attachment) string {
	if err := p.attachment().Allow(att.URL); err != nil {
		return ""
	}
	dlCtx, dlCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dlCancel()
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, att.URL, nil)
	if err != nil {
		return ""
	}
	resp, err := p.attachment().Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(p.cfg.MaxUserSkillPromptLen)+1))
	if err != nil {
		return ""
	}
	if int64(len(data)) > int64(p.cfg.MaxUserSkillPromptLen) {
		return ""
	}
	return string(data)
}
