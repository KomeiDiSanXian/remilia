// Package ai handler.go — AI 消息路由入口与对话流程控制。
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
	"github.com/KomeiDiSanXian/remilia/infra/netguard"
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
		content := runtime.CleanMessage(ctx.GetMessageContent(), p.triggerCmd)
		if content == "" && len(atts) == 0 {
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
	explicit = explicit || runtime.HasTriggerPrefix(content, p.triggerCmd)

	// per-group @ 触发要求（/ai group set mention on）：群策略显式要求必须 @ 时，
	// 未 @ 机器人的群消息（全局 GroupAutonomous 模式下会进入此路径）直接跳过。
	if require, ok := p.groupRequireMention(ctx); ok && require && !runtime.BotMentioned(ctx) {
		return nil
	}

	// 纯图片消息（无实质文本）：群聊且未 @/未引用时记录合并窗口，不回复。
	if p.maybeRecordPendingImage(ctx, content, atts) {
		return nil
	}

	content = runtime.CleanMessage(content, p.triggerCmd)
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
func (p *Plugin) handleAIChat(ctx *eventctx.Context, content string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()

	sessionID := runtime.MakeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)
	sess := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
	if sess == nil {
		ctx.ReplyError("创建会话失败")
		return nil
	}

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

	// 合并窗口：消费未消费图片并入本条（引用/回复消息优先级更高，跳过合并，
	// 避免双图语义混乱）。消费即清除 pending，防止跨回合误合并。
	if pendingRefs, pendingExtra := sess.ConsumePendingImage(p.cfg.ImageMergeWindow, userMsg.Timestamp); len(pendingRefs) > 0 || pendingExtra > 0 {
		// 窗口被热重载关闭（<=0）时同样消费 pending，但不合并。
		if p.cfg.ImageMergeWindow > 0 && platform.GetReplyToID(ctx.GetPlatformEvent()) == "" {
			if total := len(pendingRefs) + pendingExtra + runtime.CountImageParts(userMsg.ContentParts); total > p.cfg.MaxImagesPerMessage {
				logger.Warnf("[AI] Rejected merged message with %d images (max_images_per_message=%d)",
					total, p.cfg.MaxImagesPerMessage)
				ctx.ReplyText(fmt.Sprintf("图片数量超出限制（最多 %d 张），请重新编辑后再发送", p.cfg.MaxImagesPerMessage))
				return nil
			}
			// 引用 → 多模态 ContentPart（messagelog 存储优先，URL 兜底）；
			// 解析失败（存储未启用/URL 过期）的图片跳过，不影响本条文字。
			if parts := p.hydratePendingImageRefs(ctx, sess, pendingRefs); len(parts) > 0 {
				userMsg = runtime.MergePendingImageParts(userMsg, parts)
			}
		}
	}

	// 单条消息图片数上限：超限拒绝本次请求，不调用 LLM（合并累加后同样生效）。
	if imgCount := runtime.CountImageParts(userMsg.ContentParts); imgCount > p.cfg.MaxImagesPerMessage {
		logger.Warnf("[AI] Rejected message with %d images (max_images_per_message=%d)",
			imgCount, p.cfg.MaxImagesPerMessage)
		ctx.ReplyText(fmt.Sprintf("图片数量超出限制（最多 %d 张），请重新编辑后再发送", p.cfg.MaxImagesPerMessage))
		return nil
	}

	// 无文本且附件全部下载失败（SSRF/超限/超时）：无可处理内容，静默跳过。
	if userMsg.Content == "" && len(userMsg.ContentParts) == 0 {
		return nil
	}

	p.sm.AppendMessage(sess, userMsg)

	// LLM 处理前发送"正在输入"状态（平台支持时），给用户即时反馈。
	// 平台不支持（如 QQ 群聊）时 TrySendTyping 静默 no-op。
	_ = ctx.TrySendTyping()

	// 生成回答并（开启 verify_enabled 时）经校验器校验，失败按反馈重新生成。
	result, err := p.generateVerified(ctx, sess)
	if err != nil {
		ctx.ReplyError(runtime.FormatAIError(err))
		return nil
	}

	if result.Text != "" || len(result.Attachments) > 0 {
		msg := platform.OutboundMessage{}

		if p.cfg.Markdown {
			msg.Markdown = result.Text
		} else {
			msg.Text = result.Text
		}

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
	if sess == nil {
		return false
	}
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
//   - 存储路径：PlatformMsgID → messagelog event → 附件行（URL 匹配优先）→ Fetch；
//   - 未命中回退 URL 直链下载（合并窗口内直链通常仍有效）。
func (p *Plugin) resolvePendingImageRef(ctx *eventctx.Context, sess *session.Session, ref session.PendingImageRef) *protocol.ContentPart {
	if p.history != nil && ref.PlatformMsgID != "" {
		if entry, ok := p.history.QueryByEventID(ref.ChatID, ref.PlatformMsgID); ok && entry.EventID != "" {
			if rows, err := p.history.AttachmentsByEventID(entry.EventID); err == nil {
				var fallback *messagelog.AttachmentMeta
				for i := range rows {
					if !runtime.IsImageAttachmentMeta(&rows[i]) {
						continue
					}
					if ref.URL != "" && rows[i].URL == ref.URL {
						if cp := p.fetchStoredAttachment(ctx.Context(), &rows[i], sess); cp != nil {
							return cp
						}
					} else if fallback == nil {
						fallback = &rows[i]
					}
				}
				if fallback != nil {
					if cp := p.fetchStoredAttachment(ctx.Context(), fallback, sess); cp != nil {
						return cp
					}
				}
			}
		}
	}
	if ref.URL != "" {
		return p.downloadAttachment(platform.Attachment{URL: ref.URL, MimeType: ref.MimeType}, sess)
	}
	return nil
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
	url, mime := runtime.QuotedImageFromSegments(evt.Segments())
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

// fetchStoredAttachment 经 messagelog 附件存储读取单个附件二进制并构造 ContentPart。
// 未 ready 的行由 FetchContext 同步触发下载（带 30s 超时）；失败返回 nil。
// 结果写入会话缓存（按 URL key，与 URL 下载路径共用，避免同一附件重复读盘）。
func (p *Plugin) fetchStoredAttachment(ctx context.Context, a *messagelog.AttachmentMeta, sess *session.Session) *protocol.ContentPart {
	if cached := sess.CachedContent(a.URL); cached != nil {
		return &protocol.ContentPart{
			Type:        runtime.InferPartType(cached.MimeType),
			SourceURL:   a.URL,
			Data:        cached.Data,
			MimeType:    cached.MimeType,
			AudioFormat: cached.AudioFormat,
		}
	}
	if a.ID == 0 {
		return nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rc, err := p.history.FetchContext(fetchCtx, a.ID)
	if err != nil {
		logger.Debugf("[AI] stored attachment fetch failed (id=%d): %v", a.ID, err)
		return nil
	}
	defer rc.Close()

	maxBytes := p.cfg.MaxAttachmentSize
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}
	data, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
	if err != nil {
		return nil
	}
	if int64(len(data)) > maxBytes {
		return nil
	}

	mimeType := a.MimeType
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	cp := &protocol.ContentPart{
		Type:      runtime.InferPartType(mimeType),
		SourceURL: a.URL,
		Data:      data,
		MimeType:  mimeType,
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
	sess.SetCachedContent(a.URL, data, mimeType, cp.AudioFormat)
	return cp
}

// downloadAttachment 下载附件并缓存到 session。超出大小限制或下载失败返回 nil。
func (p *Plugin) downloadAttachment(att platform.Attachment, sess *session.Session) *protocol.ContentPart {
	// 先检查缓存
	if cached := sess.CachedContent(att.URL); cached != nil {
		return &protocol.ContentPart{
			Type:        runtime.InferPartType(cached.MimeType),
			SourceURL:   att.URL,
			Data:        cached.Data,
			MimeType:    cached.MimeType,
			AudioFormat: cached.AudioFormat,
		}
	}

	if !netguard.AllowURL(att.URL) {
		logger.Debugf("[AI] Attachment URL blocked (SSRF prevention): %s", att.URL)
		return nil
	}

	dlCtx, dlCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dlCancel()
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, att.URL, nil)
	if err != nil {
		logger.Debugf("[AI] Failed to create download request: %v", err)
		return nil
	}
	resp, err := attachmentHTTPClient.Do(req)
	if err != nil {
		logger.Debugf("[AI] Failed to download attachment: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("[AI] Download failed with status %d: %s", resp.StatusCode, att.URL)
		return nil
	}

	// 用 LimitReader 强控大小，防止 att.Size==0 绕过或服务器返回超量数据
	maxBytes := p.cfg.MaxAttachmentSize
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		logger.Debugf("[AI] Failed to read attachment body: %v", err)
		return nil
	}
	if int64(len(data)) > maxBytes {
		logger.Debugf("[AI] Attachment exceeded size limit, skip: %s", att.URL)
		return nil
	}

	mimeType := att.MimeType
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}

	cp := &protocol.ContentPart{
		Type:      runtime.InferPartType(mimeType),
		SourceURL: att.URL,
		Data:      data,
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
			return nil // 不支持的音频格式
		}
	}

	// 写入缓存
	sess.SetCachedContent(att.URL, data, mimeType, cp.AudioFormat)

	return cp
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
// 任何逐轮变化的内容（运行时上下文、群聊窗口、长期记忆、相关历史、
// 执行计划）一律不得进入本函数——它们由 [buildDynamicContext] 构建，
// 并在请求构建时挂到本轮用户消息尾部（见 processWithTools）。把动态
// 内容混进 System 消息会让缓存前缀在很靠前的位置失效，历史消息随之
// 整段无法复用（cache hit 长期只有个位数~十几个百分点）。
func (p *Plugin) buildStaticSystemPrompt(ctx *eventctx.Context) string {
	parts := []string{DefaultFrameworkPrompt}
	if custom := p.effectiveCustomPrompt(ctx); custom != "" {
		parts = append(parts, "===== 自定义指令 =====\n"+custom)
	}
	return strings.Join(parts, "\n\n")
}

// attachmentHTTPClient 是受 SSRF 防护的附件下载客户端（见 infra/netguard）：
// 连接目标须为公网 IP，重定向目标逐跳校验。
var attachmentHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: netguard.DialContext,
	},
	CheckRedirect: netguard.RedirectPolicy(10),
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
	if !netguard.AllowURL(att.URL) {
		return ""
	}
	dlCtx, dlCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dlCancel()
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, att.URL, nil)
	if err != nil {
		return ""
	}
	resp, err := attachmentHTTPClient.Do(req)
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
