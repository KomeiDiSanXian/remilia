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
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
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
	session.LockTurn()
	defer session.UnlockTurn()

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

	result, err := p.processWithTools(ctx, session)
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
		return err
	}
	return nil
}

// buildUserMessage 构建用户消息，包含文本内容及入站图片/音频附件。
//
// 附件下载后通过 session.contentCache 缓存（TTL 10 分钟），同一附件多次使用不需重复下载。
// 超出大小限制或下载失败的附件会被静默跳过（日志 Debug 记录）。
func (p *Plugin) buildUserMessage(ctx *eventctx.Context, content string, session *Session) Message {
	msg := Message{Role: RoleUser, Content: content}

	atts := platform.Attachments(ctx.GetPlatformEvent())
	if len(atts) == 0 {
		return msg
	}

	parts := make([]ContentPart, 0, 1+len(atts))
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
		if strings.HasPrefix(att.MimeType, "audio/") {
			if asr := platform.AttachmentTranscript(att); asr != "" {
				parts = append(parts, ContentPart{Type: ContentPartText, Text: "[语音转写] " + asr})
				continue
			}
		}

		var cp *ContentPart
		switch {
		case strings.HasPrefix(att.MimeType, "image/"):
			if !p.cfg.VisionEnabled {
				continue
			}
			cp = p.downloadAttachment(att, session)
		case strings.HasPrefix(att.MimeType, "audio/"):
			if !p.cfg.AudioEnabled {
				continue
			}
			cp = p.downloadAttachment(att, session)
		}

		if cp != nil {
			parts = append(parts, *cp)
		}
	}

	if len(parts) > 0 {
		msg.Content = "" // ContentParts 模式，清空 Content 避免内容重复
		msg.ContentParts = parts
	}
	return msg
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

	if !isAllowedDownloadURL(att.URL) {
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
//  3. Runtime Context — 动态运行时环境信息（受 include_runtime_context 配置控制）
//
// session 提供当前会话的 assistant 回复内容，供群聊消息窗口包含
// 机器人回复（context_group_include_bot）时去重；nil 时不去重。
func (p *Plugin) buildSystemPrompt(ctx *eventctx.Context, session *Session) string {
	var parts []string

	// 1. Framework Prompt
	parts = append(parts, DefaultFrameworkPrompt)

	// 2. User Custom Prompt
	if p.cfg.SystemPrompt != "" {
		parts = append(parts, "===== 自定义指令 =====\n"+p.cfg.SystemPrompt)
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

	return strings.Join(parts, "\n\n")
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

// isAllowedDownloadURL 检查附件下载 URL 是否合法（SSRF 防护）。
// 只允许 https 协议，禁止内网地址。对域名会执行 DNS 解析检查目标 IP。
func isAllowedDownloadURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return false
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		return isPublicIP(ip)
	}
	// 域名：执行 DNS 解析，检查所有解析结果是否为公网 IP
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", u.Hostname())
	if err != nil {
		return false
	}
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return false
		}
	}
	return true
}

var attachmentHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: safeAttachmentDialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !isAllowedDownloadURL(req.URL.String()) {
			return fmt.Errorf("redirect to unsafe attachment URL blocked")
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

func safeAttachmentDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return nil, fmt.Errorf("connection to non-public address blocked")
		}
	} else {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP address found for attachment host")
		}
		for _, ip := range ips {
			if !isPublicIP(ip) {
				return nil, fmt.Errorf("attachment host resolves to non-public address")
			}
		}
	}
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
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
	if !isAllowedDownloadURL(att.URL) {
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
