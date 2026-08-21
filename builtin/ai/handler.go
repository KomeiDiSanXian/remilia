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
//   - isCommandMessage: 命令消息检测
//   - cleanMessage: 消息清洗（去 @、去命令前缀）
//   - makeSessionID: 会话 ID 生成
package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

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
//  3. 检查 FSM 是否有当前用户的活跃会话（技能添加等两步流程）
//  4. 均非子命令时进入 AI 对话流程
//
// 注意：FSM 检查必须在 AI 对话之前，确保用户发送的内容被正确处理为技能 Prompt。
func (p *Plugin) handleAI(ctx *eventctx.Context) error {
	parsed := ctx.GetParsedCommand()

	if parsed != nil {
		if len(parsed.CommandPath) > 1 {
			return p.execSubCommand(ctx, parsed.CommandPath[1])
		}
		content := p.cleanMessage(ctx.GetMessageContent())
		if content == "" {
			return nil
		}
		// 命令路径不检查 FSM：避免 /ai cancel 这类消息被 FSM 的 cancel 事件消费
		return p.handleAIChat(ctx, content)
	}

	content := ctx.GetMessageContent()
	if content == "" {
		return nil
	}

	// per-group @ 触发要求（/ai group set mention on）：群策略显式要求必须 @ 时，
	// 未 @ 机器人的群消息（全局 GroupAutonomous 模式下会进入此路径）直接跳过。
	if require, ok := p.groupRequireMention(ctx); ok && require && !mentionedBot(ctx) {
		return nil
	}

	content = p.cleanMessage(content)
	if content == "" {
		return nil
	}
	if p.handleSubCommand(ctx, content) {
		return nil
	}
	if isCommandMessage(content) {
		return nil
	}

	if p.handleFSMTransition(ctx) {
		return nil
	}

	return p.handleAIChat(ctx, content)
}

// mentionedBot 判断当前群消息是否 @ 了机器人自身。
func mentionedBot(ctx *eventctx.Context) bool {
	if ctx == nil || ctx.GetPlatformEvent() == nil {
		return false
	}
	for _, m := range platform.GetMentions(ctx.GetPlatformEvent()) {
		if m.IsSelf {
			return true
		}
	}
	return false
}

// handleAIChat 执行 AI 对话流程：获取/创建会话、注入系统提示、追加用户消息（含附件）、调用 LLM。
func (p *Plugin) handleAIChat(ctx *eventctx.Context, content string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()

	sessionID := makeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)
	session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
	if session == nil {
		ctx.ReplyError("创建会话失败")
		return nil
	}

	// 用户抢占：若上一回合仍在执行（长任务/慢工具），请求中断让其尽快收尾，
	// 避免新消息被长时间阻塞（用户侧永远优先）。
	if session.TurnActive() {
		session.RequestInterrupt()
	}
	session.LockTurn()
	defer session.UnlockTurn()

	// 用户发消息重置计划后台推进预算（自动推进让位于用户）。
	session.ResetPlanAuto()

	// 标记回合活跃（中断信号生命周期：BeginTurn → 检查点 → EndTurn）。
	if !session.BeginTurn() {
		ctx.ReplyError("对话正在处理中，请稍后再试")
		return nil
	}
	defer session.EndTurn()

	systemPrompt := p.buildSystemPrompt(ctx, session)

	session.Lock()
	if session.Messages == nil {
		session.Messages = make([]Message, 0)
	}
	var foundSystem bool
	for i, m := range session.Messages {
		if m.Role == RoleSystem {
			session.Messages[i].Content = systemPrompt
			foundSystem = true
			break
		}
	}
	if !foundSystem {
		session.Messages = append([]Message{{Role: RoleSystem, Content: systemPrompt}}, session.Messages...)
	}
	session.Unlock()

	// 构建用户消息：前置回复上下文 → 追加 @ 提及的结构化信息 → 提取入站附件转为多模态 ContentParts
	if p.cfg.IncludeReplyContext {
		content = p.prependReplyContext(ctx, content)
	}
	if p.cfg.IncludeMentionInfo {
		content = appendMentionInfo(content, platform.GetMentions(ctx.GetPlatformEvent()))
	}
	userMsg := p.buildUserMessage(ctx, content, session)
	p.sm.AppendMessage(session, userMsg)

	// LLM 处理前发送"正在输入"状态（平台支持时），给用户即时反馈。
	// 平台不支持（如 QQ 群聊）时 TrySendTyping 静默 no-op。
	_ = ctx.TrySendTyping()

	// 生成回答并（开启 verify_enabled 时）经校验器校验，失败按反馈重新生成。
	result, err := p.generateVerified(ctx, session)
	if err != nil {
		ctx.ReplyError(formatAIError(err))
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

		p.replyAndRecord(ctx, msg)
	}

	// 对话回复完成后异步抽取长期记忆（memory_enabled 开启时）。
	// 不阻塞回复发送；节流与失败均由 maybeExtractMemory 内部处理。
	p.maybeExtractMemory(ctx, session)

	// 计划后台自动推进（plan_auto_continue 开启时）：计划未完成且
	// 用户无新消息时，按间隔自动继续执行并主动汇报。
	p.maybeContinuePlan(ctx, session)

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
func (p *Plugin) buildUserMessage(ctx *eventctx.Context, content string, session *Session) Message {
	msg := Message{Role: RoleUser, Content: content}

	atts := platform.Attachments(ctx.GetPlatformEvent())

	var quotedImg *ContentPart
	if p.cfg.VisionEnabled && !hasImageAttachment(atts) {
		quotedImg = p.quotedImagePart(ctx, session)
	}

	if len(atts) == 0 && quotedImg == nil {
		return msg
	}

	parts := make([]ContentPart, 0, 1+len(atts)+2)
	if content != "" {
		parts = append(parts, ContentPart{Type: ContentPartText, Text: content})
	}

	for _, att := range atts {
		if att.URL == "" {
			continue
		}

		// 平台已提供语音转写文本（如 QQ 官方 ASR asr_refer_text）时，
		// 直接注入转写文本，无需下载音频二进制再送 STT。
		// 这比把音频直传给多模态模型更通用：任何模型都能理解文本。
		if isAudioAttachment(att) {
			if asr := platform.AttachmentTranscript(att); asr != "" {
				parts = append(parts, ContentPart{Type: ContentPartText, Text: "[语音转写] " + asr})
				continue
			}
		}

		var cp *ContentPart
		switch {
		case isImageAttachment(att):
			if !p.cfg.VisionEnabled {
				continue
			}
			cp = p.downloadAttachment(att, session)
		case isAudioAttachment(att):
			if !p.cfg.AudioEnabled {
				continue
			}
			cp = p.downloadAttachment(att, session)
		}

		if cp != nil {
			parts = append(parts, *cp)
		}
	}

	if quotedImg != nil {
		parts = append(parts,
			ContentPart{Type: ContentPartText, Text: "[你回复（引用）的消息中包含这张图片]"},
			*quotedImg)
	}

	if len(parts) > 0 {
		msg.Content = "" // ContentParts 模式，清空 Content 避免内容重复
		msg.ContentParts = parts
	}
	return msg
}

// hasImageAttachment 判断附件列表中是否包含图片附件。
//
// Kind 与 MimeType 双通道：部分平台（OneBot）只填 Kind、MimeType 为空，
// 另一些平台（QQ）只填 MimeType、Kind 为空，两者都缺失才算无图片。
func hasImageAttachment(atts []platform.Attachment) bool {
	for _, att := range atts {
		if att.Kind == platform.AttachmentKindImage || strings.HasPrefix(att.MimeType, "image/") {
			return true
		}
	}
	return false
}

// isImageAttachment 判断附件是否为图片（Kind 优先，MimeType 兜底）。
func isImageAttachment(att platform.Attachment) bool {
	return att.Kind == platform.AttachmentKindImage || strings.HasPrefix(att.MimeType, "image/")
}

// isAudioAttachment 判断附件是否为音频（Kind 优先，MimeType 兜底）。
func isAudioAttachment(att platform.Attachment) bool {
	return att.Kind == platform.AttachmentKindAudio || strings.HasPrefix(att.MimeType, "audio/")
}

// quotedImagePart 提取引用消息中的被引用图片并下载；不可用时返回 nil。
//
// 下载复用 downloadAttachment 的缓存 / SSRF 防护 / 大小限制逻辑；
// 下载结果经内容嗅探校验为图片后才注入。
func (p *Plugin) quotedImagePart(ctx *eventctx.Context, session *Session) *ContentPart {
	evt := ctx.GetPlatformEvent()
	if evt == nil {
		return nil
	}
	url, mime := quotedImageFromSegments(evt.Segments())
	if url == "" {
		return nil
	}
	cp := p.downloadAttachment(platform.Attachment{URL: url, MimeType: mime}, session)
	if cp == nil || cp.Type != ContentPartImage {
		return nil
	}
	return cp
}

// downloadAttachment 下载附件并缓存到 session。超出大小限制或下载失败返回 nil。
func (p *Plugin) downloadAttachment(att platform.Attachment, session *Session) *ContentPart {
	// 先检查缓存
	if cached := session.getCachedContent(att.URL); cached != nil {
		return &ContentPart{
			Type:        inferPartType(cached.MimeType),
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

	cp := &ContentPart{
		Type:      inferPartType(mimeType),
		SourceURL: att.URL,
		Data:      data,
		MimeType:  mimeType,
	}

	// 检查是否真的 image/ audio，否则跳过
	if cp.Type == "" {
		return nil
	}

	// 推理音频格式
	if cp.Type == ContentPartAudio {
		cp.AudioFormat = inferAudioFormat(mimeType)
		if cp.AudioFormat == "" {
			return nil // 不支持的音频格式
		}
	}

	// 写入缓存
	session.setCachedContent(att.URL, data, mimeType, cp.AudioFormat)

	return cp
}

// inferPartType 根据 MIME 类型推断 ContentPartType。
func inferPartType(mimeType string) ContentPartType {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return ContentPartImage
	case strings.HasPrefix(mimeType, "audio/"):
		return ContentPartAudio
	default:
		return ""
	}
}

// inferAudioFormat 从 MIME 类型推断 OpenAI input_audio format。
func inferAudioFormat(mimeType string) string {
	switch mimeType {
	case "audio/wav", "audio/wave", "audio/x-wav":
		return "wav"
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/L16", "audio/l16":
		return "pcm"
	default:
		return ""
	}
}

// isCommandMessage 判断消息是否为命令消息。
//
// 使用 [context.SplitCommandPattern] 检测消息的首个单词是否带有
// 非字母数字前缀（如 "/"、"!"、"!!"、"$#" 等），有则视为命令消息。
//
// 这覆盖了所有自定义命令前缀场景，与框架的命令路由逻辑保持一致。
func isCommandMessage(msg string) bool {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return false
	}
	firstWord := trimmed
	if idx := strings.IndexFunc(trimmed, unicode.IsSpace); idx != -1 {
		firstWord = trimmed[:idx]
	}
	prefix, _ := eventctx.SplitCommandPattern(firstWord)
	return prefix != ""
}

// cleanMessage 清洗消息内容，去除 @ 提及标记和触发命令前缀。
func (p *Plugin) cleanMessage(content string) string {
	content = strings.TrimSpace(content)
	content = stripMentionMarkup(content)
	content = strings.TrimSpace(content)
	content = strings.TrimLeft(content, "@")
	content = strings.TrimSpace(content)
	if p.triggerCmd != "" {
		content = strings.TrimPrefix(content, p.triggerCmd)
	}
	content = strings.TrimSpace(content)
	return content
}

// mentionMarkupRegex 匹配各平台的 @ 提及标记：
//   - Discord: <@123> / <@!123> / <@&123>(角色) / <#123>(频道) / @everyone / @here
//   - onebot/QQ: @QQ号 / @all / @全体成员 / @所有人
//
// 不匹配 Telegram 的 @username（字母），避免误伤正文。
var mentionMarkupRegex = regexp.MustCompile(`<@!?&?\d+>|<#\d+>|@\d+|@everyone|@here|@all|@全体成员|@所有人`)

// stripMentionMarkup 从消息文本中去除所有平台的 @ 提及标记。
func stripMentionMarkup(content string) string {
	return mentionMarkupRegex.ReplaceAllString(content, "")
}

// appendMentionInfo 在用户消息末尾追加结构化提及信息（排除机器人自身），
// 让 LLM 知道本条消息 @ 了哪些人，而非面对一串无意义的 ID 标记。
func appendMentionInfo(content string, mentions []platform.UserInfo) string {
	var others []string
	for _, m := range mentions {
		if m.IsSelf || m.ID == "" {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		others = append(others, name)
	}
	if len(others) == 0 {
		return content
	}
	return content + "\n\n[本条消息 @ 提及了: " + strings.Join(others, ", ") + "]"
}

// buildSystemPrompt 构建复合系统提示词，由三层组成：
//
//  1. Framework Prompt — 硬编码的 AI 行为规则，不可被用户覆盖
//  2. User Custom Prompt — 配置文件 system_prompt 中的自定义指令
//     （群聊时被 per-group 策略的 prompt 覆盖，见 /ai group set prompt）
//  3. Runtime Context — 动态运行时环境信息（受 include_runtime_context 配置控制）
//
// session 提供当前会话的 assistant 回复内容，供群聊消息窗口包含
// 机器人回复（context_group_include_bot）时去重；nil 时不去重。
func (p *Plugin) buildSystemPrompt(ctx *eventctx.Context, session *Session) string {
	// 全局预算编排（context_window > 0 时启用）：按优先级动态缩减各注入节，
	// 防止小上下文模型被注入内容撑爆；非法预算回退默认构建。
	if p.cfg.ContextWindow > 0 {
		if budgeted := p.buildSystemPromptBudgeted(ctx, session); budgeted != "" {
			return budgeted
		}
	}

	var parts []string

	// 1. Framework Prompt
	parts = append(parts, DefaultFrameworkPrompt)

	// 2. User Custom Prompt（群聊时优先使用群策略 prompt）
	systemPrompt := p.cfg.SystemPrompt
	if gp := p.groupPolicyFor(ctx); gp != nil {
		if gpPrompt := gp.EffectiveSystemPrompt(); gpPrompt != "" {
			systemPrompt = gpPrompt
		}
	}
	if systemPrompt != "" {
		parts = append(parts, "===== 自定义指令 =====\n"+systemPrompt)
	}

	// 3. Runtime Context（可通过 include_runtime_context / context_fields 控制，
	//    避免用户 ID、群 ID 等隐私信息随请求发送给第三方 LLM）
	if p.cfg.IncludeRuntimeContext {
		parts = append(parts, "===== 运行时上下文 =====\n"+p.buildRuntimeContext(ctx))
	}

	// 4. 群聊最近消息窗口（context_group_messages > 0 时开启，默认 10，
	//    独立于 include_runtime_context，由自身配置控制）
	if p.cfg.ContextGroupMessages > 0 {
		// 会话历史已包含 AI 自己的回复（assistant 轮次）——
		// 开启 context_group_include_bot 时按内容去重，避免重复注入
		var skipBot map[string]bool
		if p.cfg.ContextGroupIncludeBot && session != nil {
			skipBot = make(map[string]bool)
			for _, m := range session.Messages {
				if m.Role == RoleAssistant && m.Content != "" {
					skipBot[m.Content] = true
				}
			}
		}
		if groupCtx := p.buildGroupContext(ctx, skipBot); groupCtx != "" {
			parts = append(parts, "===== 群聊最近消息 =====\n"+groupCtx)
		}
	}

	// 5. 长期记忆（memory_enabled 开启时，按用户消息关键词检索注入）
	if p.memory != nil && p.memory.Enabled() {
		if memCtx := p.buildMemoryContext(ctx, session); memCtx != "" {
			parts = append(parts, "===== 长期记忆 =====\n"+memCtx)
		}
	}

	// 6. 相关历史消息（context_rag_messages > 0 时开启，消息级 RAG）
	if p.cfg.ContextRAGMessages > 0 {
		if ragCtx := p.buildRAGContext(ctx, session); ragCtx != "" {
			parts = append(parts, "===== 相关历史消息 =====\n"+ragCtx)
		}
	}

	return strings.Join(parts, "\n\n")
}

// buildMemoryContext 检索并格式化长期记忆注入系统提示。
// 群聊注入"用户相关记忆 + 群相关记忆"，私聊仅用户记忆；
// 按用户最近消息关键词对事实打分取 Top-N。
func (p *Plugin) buildMemoryContext(ctx *eventctx.Context, session *Session) string {
	return p.buildMemoryContextN(ctx, session, p.cfg.MemoryInjectMax)
}

// buildMemoryContextN 同上，注入条数由调用方给定（预算编排时可动态缩减）。
func (p *Plugin) buildMemoryContextN(ctx *eventctx.Context, session *Session, limit int) string {
	sender := ctx.GetSenderInfo()
	if sender.ID == "" {
		return ""
	}
	query := getLastUserMessage(session)
	if query == "" {
		return ""
	}
	if limit <= 0 {
		limit = 8
	}

	var facts []string
	if hits := p.memory.Retrieve(ctx.Context(), userScope(sender.ID), query, limit); len(hits) > 0 {
		facts = append(facts, "【用户相关记忆】")
		for _, f := range hits {
			facts = append(facts, "- "+f.Text)
		}
	}
	if chat := ctx.GetChatInfo(); chat.IsGroup && chat.ID != "" {
		if hits := p.memory.Retrieve(ctx.Context(), groupScope(chat.ID), query, limit); len(hits) > 0 {
			facts = append(facts, "【群相关记忆】")
			for _, f := range hits {
				facts = append(facts, "- "+f.Text)
			}
		}
	}
	if len(facts) == 0 {
		return ""
	}
	return "以下为长期对话中记住的关于用户/本群的事实，可据此提供个性化服务（如有冲突以用户当前说法为准）：\n" +
		strings.Join(facts, "\n")
}

// buildRuntimeContext 组装当前事件的运行时上下文信息。
//
// 注入的字段由配置 context_fields 白名单控制：
// 为空表示注入全部字段；非空时仅注入列出的字段。
func (p *Plugin) buildRuntimeContext(ctx *eventctx.Context) string {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()

	allow := make(map[string]bool, len(p.cfg.ContextFields))
	for _, f := range p.cfg.ContextFields {
		allow[f] = true
	}
	in := func(key string) bool {
		if len(p.cfg.ContextFields) == 0 {
			return true
		}
		return allow[key]
	}

	var b strings.Builder
	if in("time") {
		fmt.Fprintf(&b, "当前时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	}
	if in("platform") {
		fmt.Fprintf(&b, "平台: %s\n", ctx.GetEventPlatform())
	}
	if in("bot_id") {
		if botID := ctx.GetBotID(); botID != "" {
			fmt.Fprintf(&b, "机器人 ID: %s\n", botID)
		}
	}
	if in("bot_name") {
		if botName := ctx.GetBotName(); botName != "" {
			fmt.Fprintf(&b, "机器人名称: %s\n", botName)
		}
	}
	if in("user_name") {
		fmt.Fprintf(&b, "用户昵称: %s\n", sender.DisplayName)
	}
	if in("user_id") {
		fmt.Fprintf(&b, "用户 ID: %s\n", sender.ID)
	}
	if in("user_is_bot") {
		isBot := "否"
		if sender.IsBot {
			isBot = "是"
		}
		fmt.Fprintf(&b, "发送者是否为机器人: %s\n", isBot)
	}
	if in("chat_type") {
		switch {
		case chat.IsGroup:
			fmt.Fprintf(&b, "聊天类型: 群聊\n")
		case chat.IsDM:
			fmt.Fprintf(&b, "聊天类型: 频道私信\n")
		default:
			fmt.Fprintf(&b, "聊天类型: 私聊\n")
		}
	}
	if chat.IsGroup {
		if in("chat_id") && chat.ID != "" {
			fmt.Fprintf(&b, "群 ID: %s\n", chat.ID)
		}
		if in("chat_name") && chat.Name != "" {
			fmt.Fprintf(&b, "群名称: %s\n", chat.Name)
		}
		if in("parent_id") && chat.ParentID != "" {
			fmt.Fprintf(&b, "所属服务器 ID: %s\n", chat.ParentID)
		}
		if in("group_role") {
			fmt.Fprintf(&b, "发送者群角色: %s\n", groupRoleName(sender.GroupRole))
		}
	} else {
		if in("chat_id") && chat.ID != "" {
			fmt.Fprintf(&b, "会话 ID: %s\n", chat.ID)
		}
		if in("chat_name") && chat.Name != "" {
			fmt.Fprintf(&b, "会话名称: %s\n", chat.Name)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// groupRoleName 将平台群角色转换为可读文本。
func groupRoleName(role platform.GroupRole) string {
	switch role {
	case platform.GroupRoleOwner:
		return "群主/所有者"
	case platform.GroupRoleAdmin:
		return "管理员"
	case platform.GroupRoleMember:
		return "普通成员"
	default:
		return "未知"
	}
}

// attachmentHTTPClient 是受 SSRF 防护的附件下载客户端（见 infra/netguard）：
// 连接目标须为公网 IP，重定向目标逐跳校验。
var attachmentHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: netguard.DialContext,
	},
	CheckRedirect: netguard.RedirectPolicy(10),
}

// makeSessionID 生成会话唯一标识。
// 格式: "{platform}:{chatID}:{userID}"
// 不同平台、不同群组、不同用户的会话相互隔离。
func makeSessionID(platform, chatID, userID string) string {
	return fmt.Sprintf("%s:%s:%s", platform, chatID, userID)
}

// handleFSMTransition 检查 FSM 引擎是否有当前用户的活跃会话。
// 有活跃会话时尝试迁移（处理技能添加等两步流程），返回 true 表示消息已被 FSM 消费。
func (p *Plugin) handleFSMTransition(ctx *eventctx.Context) bool {
	if p.fsmEngine == nil {
		return false
	}
	sessionID := makeSkillAddSessionID(ctx)
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
