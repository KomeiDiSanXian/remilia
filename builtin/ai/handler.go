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
	"strings"
	"time"
	"unicode"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// handleAI AI 消息处理器的入口。
//
// 处理流程：
//  1. 若通过 /ai 命令触发，使用 GetParsedCommand 检测子命令
//  2. 若通过 @bot 或私聊触发，使用 cleanMessage 清洗后检测子命令
//  3. 均非子命令时进入 AI 对话流程
//  4. 注入/更新系统提示词
//  5. 追加用户消息到会话
//  6. 进入工具调用循环（processWithTools）
//  7. 发送最终回复
func (p *Plugin) handleAI(ctx *eventctx.Context) error {
	parsed := ctx.GetParsedCommand()

	if parsed != nil {
		if len(parsed.CommandPath) > 1 {
			return p.execSubCommand(ctx, parsed.CommandPath[1])
		}
		return p.handleAIChat(ctx, p.cleanMessage(ctx.GetMessageContent()))
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

	return p.handleAIChat(ctx, content)
}

// handleAIChat 执行 AI 对话流程：获取/创建会话、注入系统提示、追加用户消息（含附件）、调用 LLM。
func (p *Plugin) handleAIChat(ctx *eventctx.Context, content string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()

	sessionID := makeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)
	session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
	if session == nil {
		return ctx.ReplyError("创建会话失败")
	}

	if session.Messages == nil {
		session.Messages = make([]Message, 0)
	}

	systemPrompt := p.buildSystemPrompt(ctx)

	session.Lock()
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

	// 构建用户消息：提取入站附件转为多模态 ContentParts
	userMsg := p.buildUserMessage(ctx, content, session)
	p.sm.AppendMessage(session, userMsg)

	result, err := p.processWithTools(ctx, session)
	if err != nil {
		return ctx.ReplyError(formatAIError(err))
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

		_, err = ctx.Reply(msg)
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

	atts := ctx.GetPlatformEvent().Attachments()
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
func (p *Plugin) downloadAttachment(att platform.InboundAttachment, session *Session) *ContentPart {
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
	resp, err := http.DefaultClient.Do(req)
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

// cleanMessage 清洗消息内容，去除 @ 提及和触发命令前缀。
func (p *Plugin) cleanMessage(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimLeft(content, "@")
	content = strings.TrimSpace(content)
	if p.triggerCmd != "" {
		content = strings.TrimPrefix(content, p.triggerCmd)
	}
	content = strings.TrimSpace(content)
	return content
}

// buildSystemPrompt 构建复合系统提示词，由三层组成：
//
//  1. Framework Prompt — 硬编码的 AI 行为规则，不可被用户覆盖
//  2. User Custom Prompt — 配置文件 system_prompt 中的自定义指令
//  3. Runtime Context — 动态运行时环境信息
func (p *Plugin) buildSystemPrompt(ctx *eventctx.Context) string {
	var parts []string

	// 1. Framework Prompt
	parts = append(parts, DefaultFrameworkPrompt)

	// 2. User Custom Prompt
	if p.cfg.SystemPrompt != "" {
		parts = append(parts, "===== 自定义指令 =====\n"+p.cfg.SystemPrompt)
	}

	// 3. Runtime Context
	parts = append(parts, "===== 运行时上下文 =====\n"+p.buildRuntimeContext(ctx))

	return strings.Join(parts, "\n\n")
}

// buildRuntimeContext 组装当前事件的运行时上下文信息。
func (p *Plugin) buildRuntimeContext(ctx *eventctx.Context) string {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()
	now := time.Now().Format("2006-01-02 15:04:05")

	var b strings.Builder
	fmt.Fprintf(&b, "当前时间: %s\n", now)
	fmt.Fprintf(&b, "平台: %s\n", ctx.GetEventPlatform())

	botID := ctx.GetBotID()
	if botID != "" {
		fmt.Fprintf(&b, "机器人 ID: %s\n", botID)
	}
	botName := ctx.GetBotName()
	if botName != "" {
		fmt.Fprintf(&b, "机器人名称: %s\n", botName)
	}

	fmt.Fprintf(&b, "用户昵称: %s\n", sender.DisplayName)
	fmt.Fprintf(&b, "用户 ID: %s\n", sender.ID)

	if chat.IsGroup {
		fmt.Fprintf(&b, "聊天类型: 群聊\n")
		if chat.Name != "" {
			fmt.Fprintf(&b, "群名称: %s\n", chat.Name)
		}
	} else if chat.IsDM {
		fmt.Fprintf(&b, "聊天类型: 频道私信\n")
	} else {
		fmt.Fprintf(&b, "聊天类型: 私聊\n")
	}
	if chat.Name != "" && !chat.IsGroup {
		fmt.Fprintf(&b, "会话名称: %s\n", chat.Name)
	}

	return strings.TrimRight(b.String(), "\n")
}

// isAllowedDownloadURL 检查附件下载 URL 是否合法（SSRF 防护）。
// 只允许 https 协议，禁止内网地址。
func isAllowedDownloadURL(rawURL string) bool {
	if !strings.HasPrefix(rawURL, "https://") {
		return false
	}
	return true
}

// makeSessionID 生成会话唯一标识。
// 格式: "{platform}:{chatID}:{userID}"
// 不同平台、不同群组、不同用户的会话相互隔离。
func makeSessionID(platform, chatID, userID string) string {
	return fmt.Sprintf("%s:%s:%s", platform, chatID, userID)
}
