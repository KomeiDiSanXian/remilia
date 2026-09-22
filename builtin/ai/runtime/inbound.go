// inbound.go — 入站消息归一化的纯形状工具。
//
// 这里的函数只做"把平台消息/附件翻译成协议消息"的判定与整形：附件类型判定、
// 待合并图片引用、多模态片段的拼装、命令样式识别、会话 ID 与稳定系统消息。
// 它们不读插件状态（触发前缀由调用方传入），因此从消息入口独立出来。
package runtime

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/textutil"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// BotMentioned 判断当前群消息是否 @ 了机器人自身。
//
// 判定委托 platform.MentionedBot：结构化 @ 列表（IsSelf）优先，平台级
// "事件本身即 @机器人" 标记兜底。仅扫 @ 列表会让 QQ 群 @机器人 消息
// （GROUP_AT_MESSAGE_CREATE 不带 mentions 数组）被判为"未 @ 机器人"，
// 群策略要求 @ 时也把用户 @机器人 的消息直接丢掉。
func BotMentioned(ctx *eventctx.Context) bool {
	if ctx == nil || ctx.GetPlatformEvent() == nil {
		return false
	}
	return platform.MentionedBot(ctx.GetPlatformEvent())
}

// HasImageAttachment 判断附件列表中是否包含图片附件。
//
// Kind 与 MimeType 双通道：部分平台（OneBot）只填 Kind、MimeType 为空，
// 另一些平台（QQ）只填 MimeType、Kind 为空，两者都缺失才算无图片。
func HasImageAttachment(atts []platform.Attachment) bool {
	for _, att := range atts {
		if att.Kind == platform.AttachmentKindImage || strings.HasPrefix(att.MimeType, "image/") {
			return true
		}
	}
	return false
}

// IsImageAttachment 判断附件是否为图片（Kind 优先，MimeType 兜底）。
func IsImageAttachment(att platform.Attachment) bool {
	return att.Kind == platform.AttachmentKindImage || strings.HasPrefix(att.MimeType, "image/")
}

// IsAudioAttachment 判断附件是否为音频（Kind 优先，MimeType 兜底）。
func IsAudioAttachment(att platform.Attachment) bool {
	return att.Kind == platform.AttachmentKindAudio || strings.HasPrefix(att.MimeType, "audio/")
}

// IsImageAttachmentMeta 判断附件元数据是否为图片（Type/MimeType 双通道，
// 与 HasImageAttachment 的平台附件判定语义一致）。
func IsImageAttachmentMeta(a *messagelog.AttachmentMeta) bool {
	return a.Type == string(platform.AttachmentKindImage) || strings.HasPrefix(a.MimeType, "image/")
}

// PendingImageRefsFromAttachments 提取事件中的图片附件为待合并引用。
// 判定与 HasImageAttachment 一致（Kind/MimeType 双通道）；跳过无 URL 项。
func PendingImageRefsFromAttachments(chatID, platformMsgID string, atts []platform.Attachment) []session.PendingImageRef {
	var refs []session.PendingImageRef
	for _, att := range atts {
		if att.URL == "" {
			continue
		}
		if !IsImageAttachment(att) {
			continue
		}
		refs = append(refs, session.PendingImageRef{
			ChatID:        chatID,
			PlatformMsgID: platformMsgID,
			URL:           att.URL,
			MimeType:      att.MimeType,
		})
	}
	return refs
}

// MergePendingImageParts 将未消费图片前置到当前 user 消息（图片在前，文字在后），
// 与文字合成一条多模态消息，保证模型强关联"图+文"。
func MergePendingImageParts(userMsg protocol.Message, pending []protocol.ContentPart) protocol.Message {
	parts := make([]protocol.ContentPart, 0, len(pending)+len(userMsg.ContentParts)+1)
	parts = append(parts, pending...)
	if len(userMsg.ContentParts) == 0 && userMsg.Content != "" {
		// 纯文本消息：把文字转为 text part，图片前置
		parts = append(parts, protocol.ContentPart{Type: protocol.ContentPartText, Text: userMsg.Content})
	} else {
		parts = append(parts, userMsg.ContentParts...)
	}
	userMsg.ContentParts = parts
	userMsg.Content = ""
	return userMsg
}

// mediaPlaceholderTokens 各平台无实质语义的媒体占位符文本。
// QQ 纯图片消息正文为 "[图片]"；Satori 渲染 HTML 时也会插入 "[图片]"/"[语音]" 等。
var mediaPlaceholderTokens = []string{"[图片]", "[表情]", "[动画表情]", "[语音]", "[视频]", "[文件]"}

// HasSubstantiveText 判断消息是否含实质文本（剔除媒体占位符后仍有内容）。
func HasSubstantiveText(content string) bool {
	s := strings.TrimSpace(content)
	if s == "" {
		return false
	}
	for _, t := range mediaPlaceholderTokens {
		s = strings.ReplaceAll(s, t, "")
	}
	return strings.TrimSpace(s) != ""
}

// InferPartType 根据 MIME 类型推断 ContentPartType。
func InferPartType(mimeType string) protocol.ContentPartType {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return protocol.ContentPartImage
	case strings.HasPrefix(mimeType, "audio/"):
		return protocol.ContentPartAudio
	default:
		return ""
	}
}

// InferAudioFormat 从 MIME 类型推断 OpenAI input_audio format。
func InferAudioFormat(mimeType string) string {
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

// HasTriggerPrefix 判断原始正文（未清洗）是否以触发命令开头。
//
// 匹配语义与命令路由一致：忽略前导空白后按前缀比较（见 [eventctx.OnCommand]
// 与 CleanMessage 的剥离逻辑），因此对 /ai 与"帮助"这类非前缀触发词都成立。
func HasTriggerPrefix(content, triggerCmd string) bool {
	if triggerCmd == "" {
		return false
	}
	return strings.HasPrefix(strings.TrimLeftFunc(content, unicode.IsSpace), triggerCmd)
}

// IsCommandMessage 判断消息是否为命令消息。
//
// 使用 [eventctx.SplitCommandPattern] 检测消息的首个单词是否带有
// 非字母数字前缀（如 "/"、"!"、"!!"、"$#" 等），有则视为命令消息。
//
// 这覆盖了所有自定义命令前缀场景，与框架的命令路由逻辑保持一致。
func IsCommandMessage(msg string) bool {
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

// CleanMessage 清洗消息内容，去除 @ 提及标记和触发命令前缀。
func CleanMessage(content, triggerCmd string) string {
	content = strings.TrimSpace(content)
	content = textutil.StripMentionMarkup(content)
	content = strings.TrimSpace(content)
	content = strings.TrimLeft(content, "@")
	content = strings.TrimSpace(content)
	if triggerCmd != "" {
		content = strings.TrimPrefix(content, triggerCmd)
	}
	content = strings.TrimSpace(content)
	return content
}

// AppendMentionInfo 在用户消息末尾追加结构化提及信息（排除机器人自身），
// 让 LLM 知道本条消息 @ 了哪些人，而非面对一串无意义的 ID 标记。
func AppendMentionInfo(content string, mentions []platform.UserInfo) string {
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

// SetSystemMessage 把稳定系统提示词写入会话的 System 消息（不存在时插在最前）。
//
// 会话始终只有这一条 System 消息（位置固定为消息数组第一个），且每轮重建的
// 内容逐字节一致——这是历史消息能被前缀缓存复用的前提。请求期才需要的
// 动态内容不得写进会话，见 InjectDynamicContext。
func SetSystemMessage(sess *session.Session, prompt string) {
	sess.Lock()
	defer sess.Unlock()
	if sess.Messages == nil {
		sess.Messages = make([]protocol.Message, 0)
	}
	for i, m := range sess.Messages {
		if m.Role == protocol.RoleSystem {
			sess.Messages[i].Content = prompt
			return
		}
	}
	sess.Messages = append([]protocol.Message{{Role: protocol.RoleSystem, Content: prompt}}, sess.Messages...)
}

// MakeSessionID 生成会话唯一标识。
// 格式: "{platform}:{chatID}:{userID}"
// 不同平台、不同群组、不同用户的会话相互隔离。
func MakeSessionID(platformName, chatID, userID string) string {
	return fmt.Sprintf("%s:%s:%s", platformName, chatID, userID)
}
