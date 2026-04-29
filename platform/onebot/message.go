package onebot

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ────────────────────────────────────────────────────────────────────────────
// 消息段类型
// ────────────────────────────────────────────────────────────────────────────

// 消息段类型常量，与 OneBot V11 规范中的定义一致。
const (
	SegTypeText      = "text"
	SegTypeFace      = "face"
	SegTypeImage     = "image"
	SegTypeRecord    = "record" // 语音
	SegTypeVideo     = "video"
	SegTypeAt        = "at"
	SegTypeRPS       = "rps"       // 猜拳魔法表情
	SegTypeDice      = "dice"      // 掷骰子魔法表情
	SegTypeShake     = "shake"     // 窗口抖动（戳一戳快捷方式）
	SegTypePoke      = "poke"      // 戳一戳
	SegTypeAnonymous = "anonymous" // 匿名发消息
	SegTypeShare     = "share"     // 链接分享
	SegTypeContact   = "contact"   // 推荐好友/群
	SegTypeLocation  = "location"  // 位置
	SegTypeMusic     = "music"     // 音乐分享
	SegTypeReply     = "reply"     // 回复引用
	SegTypeForward   = "forward"   // 合并转发（接收）
	SegTypeNode      = "node"      // 合并转发节点（发送）
	SegTypeXML       = "xml"
	SegTypeJSON      = "json"

	// 扩展段类型（常见 OneBot 实现提供的扩展）
	SegTypeMface    = "mface"    // 商城表情（带 emoji_package_id / emoji_id / key）
	SegTypeMarkdown = "markdown" // Markdown 消息
	SegTypeKeyboard = "keyboard" // 按钮交互
	SegTypeFile     = "file"     // 文件（含 url, path, file_size 等）
)

// MessageSegment 表示 OneBot V11 消息中的单个消息段。
// Data 字段包含与消息段类型相关的参数。
type MessageSegment struct {
	Type string            `json:"type"`
	Data map[string]string `json:"data"`
}

// TextData 返回文本段中的 "text" 字段值。
func (s MessageSegment) TextData() string {
	return s.Data["text"]
}

// ImageURL 返回图片段的图片 URL 或文件路径。
func (s MessageSegment) ImageURL() string {
	if u := s.Data["url"]; u != "" {
		return u
	}
	return s.Data["file"]
}

// AtQQ 返回 at 段中的 QQ 号（或 "all"）。
func (s MessageSegment) AtQQ() string {
	return s.Data["qq"]
}

// ReplyID 返回回复段中的消息 ID。
func (s MessageSegment) ReplyID() string {
	return s.Data["id"]
}

// ────────────────────────────────────────────────────────────────────────────
// MessageChain — 自定义 JSON 处理，支持数组和字符串两种格式
// ────────────────────────────────────────────────────────────────────────────

// MessageChain 是 MessageSegment 的切片。
//
// OneBot V11 的 "message" 字段可以是：
//   - JSON 数组：  [{"type":"text","data":{"text":"hello"}}]
//   - CQ 码字符串："hello[CQ:face,id=1]"
//
// 两种格式均可透明解析为 MessageChain。
type MessageChain []MessageSegment

// UnmarshalJSON 实现 json.Unmarshaler，支持两种消息格式。
func (mc *MessageChain) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	switch b[0] {
	case '[':
		// 数组格式：直接反序列化
		var segs []MessageSegment
		if err := json.Unmarshal(b, &segs); err != nil {
			return err
		}
		*mc = segs
		return nil

	case '"':
		// 字符串格式：先反转义 JSON 字符串，再解析 CQ 码
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*mc = parseCQString(s)
		return nil

	default:
		return fmt.Errorf("onebot: unexpected message format (first byte %q)", b[0])
	}
}

// MarshalJSON 实现 json.Marshaler — 始终以数组格式序列化。
func (mc MessageChain) MarshalJSON() ([]byte, error) {
	if len(mc) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal([]MessageSegment(mc))
}

// Text 返回所有文本段拼接后的纯文本内容。
func (mc MessageChain) Text() string {
	var sb strings.Builder
	for _, s := range mc {
		if s.Type == SegTypeText {
			sb.WriteString(s.TextData())
		}
	}
	return sb.String()
}

// FullText 返回合并了文本和 @mention 的可读字符串。
func (mc MessageChain) FullText() string {
	var sb strings.Builder
	for _, s := range mc {
		switch s.Type {
		case SegTypeText:
			sb.WriteString(s.TextData())
		case SegTypeAt:
			sb.WriteString("@" + s.AtQQ())
		}
	}
	return sb.String()
}

// ToAttachments 从图片/语音/视频段中提取 platform.InboundAttachment。
func (mc MessageChain) ToAttachments() []platform.InboundAttachment {
	var result []platform.InboundAttachment
	for _, s := range mc {
		switch s.Type {
		case SegTypeImage:
			u := s.ImageURL()
			if u != "" {
				result = append(result, platform.InboundAttachment{
					URL:      u,
					MimeType: "image/*",
					Name:     s.Data["file"],
				})
			}
		case SegTypeRecord:
			u := s.Data["url"]
			if u == "" {
				u = s.Data["file"]
			}
			if u != "" {
				result = append(result, platform.InboundAttachment{
					URL:      u,
					MimeType: "audio/*",
					Name:     s.Data["file"],
				})
			}
		case SegTypeVideo:
			u := s.Data["url"]
			if u == "" {
				u = s.Data["file"]
			}
			if u != "" {
				result = append(result, platform.InboundAttachment{
					URL:      u,
					MimeType: "video/*",
					Name:     s.Data["file"],
				})
			}
		case SegTypeMface:
			// 商城表情：若有 url 则作为附件处理
			if u := s.Data["url"]; u != "" {
				result = append(result, platform.InboundAttachment{
					URL:      u,
					MimeType: "image/*",
					Name:     s.Data["summary"],
				})
			}
		case SegTypeFile:
			// 文件段：优先使用 url，回退到本地 path
			u := s.Data["url"]
			if u == "" {
				u = s.Data["path"]
			}
			if u != "" {
				result = append(result, platform.InboundAttachment{
					URL:      u,
					MimeType: "application/octet-stream",
					Name:     s.Data["name"],
				})
			}
		}
	}
	return result
}

// ────────────────────────────────────────────────────────────────────────────
// CQ 码解析器（字符串格式 → MessageChain）
// ────────────────────────────────────────────────────────────────────────────

// parseCQString 将 CQ 码字符串解析为 MessageChain。
//
// CQ 码格式：  [CQ:type,key=value,key2=value2]
// 纯文本中的特殊字符使用 HTML 实体：&amp; &#91; &#93;
// CQ 码参数值中的特殊字符：&amp; &#91; &#93; &#44;
func parseCQString(s string) MessageChain {
	var result MessageChain
	for len(s) > 0 {
		// 查找下一个 CQ 码
		start := strings.Index(s, "[CQ:")
		if start < 0 {
			// 没有更多 CQ 码；剩余部分为纯文本
			if len(s) > 0 {
				result = append(result, textSegment(unescapeText(s)))
			}
			break
		}
		// 当前 CQ 码之前的文本
		if start > 0 {
			result = append(result, textSegment(unescapeText(s[:start])))
		}
		// 查找当前 CQ 码的结束位置
		end := strings.Index(s[start:], "]")
		if end < 0 {
			// 格式错误；将剩余部分作为文本处理
			result = append(result, textSegment(unescapeText(s[start:])))
			break
		}
		end += start                  // 绝对索引
		cqContent := s[start+4 : end] // 去掉 "[CQ:" 和 "]" 之间的内容
		s = s[end+1:]

		seg := parseCQContent(cqContent)
		result = append(result, seg)
	}
	return result
}

// parseCQContent 解析 CQ 码的内部内容（"[CQ:" 之后的部分）。
// 输入格式："type,key=value,key2=value2"
func parseCQContent(content string) MessageSegment {
	parts := strings.SplitN(content, ",", 2)
	segType := parts[0]
	data := make(map[string]string)

	if len(parts) == 2 {
		for _, kv := range splitCQParams(parts[1]) {
			before, after, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			k := before
			v := unescapeCQValue(after)
			data[k] = v
		}
	}
	return MessageSegment{Type: segType, Data: data}
}

// splitCQParams 按逗号分割 CQ 码参数，正确处理转义字符。
// 用于处理参数值中以 &#44; 形式出现的逗号。
func splitCQParams(s string) []string {
	var parts []string
	var cur strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == ',' {
			parts = append(parts, cur.String())
			cur.Reset()
			i++
		} else if i+5 <= len(s) && s[i:i+5] == "&#44;" {
			cur.WriteByte(',')
			i += 5
		} else {
			cur.WriteByte(s[i])
			i++
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// unescapeText 还原纯文本（CQ 码之间的文本）的转义。
// &amp; → &    &#91; → [    &#93; → ]
func unescapeText(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&#91;", "[")
	s = strings.ReplaceAll(s, "&#93;", "]")
	return s
}

// unescapeCQValue 还原 CQ 码参数值中的转义。
// &amp; → &    &#91; → [    &#93; → ]    &#44; → ,
func unescapeCQValue(s string) string {
	s = strings.ReplaceAll(s, "&#44;", ",")
	s = strings.ReplaceAll(s, "&#91;", "[")
	s = strings.ReplaceAll(s, "&#93;", "]")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}

// escapeCQValue 对 CQ 码参数值中的特殊字符进行转义。
func escapeCQValue(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "[", "&#91;")
	s = strings.ReplaceAll(s, "]", "&#93;")
	s = strings.ReplaceAll(s, ",", "&#44;")
	return s
}

// escapeText 对 CQ 码之间的纯文本中的特殊字符进行转义。
func escapeText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "[", "&#91;")
	s = strings.ReplaceAll(s, "]", "&#93;")
	return s
}

// textSegment 创建一个文本类型的 MessageSegment。
func textSegment(text string) MessageSegment {
	return MessageSegment{Type: SegTypeText, Data: map[string]string{"text": text}}
}

// ────────────────────────────────────────────────────────────────────────────
// CQ 码格式化器（MessageChain → CQ 字符串）
// ────────────────────────────────────────────────────────────────────────────

// ToCQString 将 MessageChain 转换为 CQ 码字符串格式。
func (mc MessageChain) ToCQString() string {
	var sb strings.Builder
	for _, s := range mc {
		if s.Type == SegTypeText {
			sb.WriteString(escapeText(s.TextData()))
			continue
		}
		sb.WriteString("[CQ:")
		sb.WriteString(s.Type)
		for k, v := range s.Data {
			sb.WriteByte(',')
			sb.WriteString(k)
			sb.WriteByte('=')
			sb.WriteString(escapeCQValue(v))
		}
		sb.WriteByte(']')
	}
	return sb.String()
}

// ────────────────────────────────────────────────────────────────────────────
// 出站消息构建器
// ────────────────────────────────────────────────────────────────────────────

// OutboundToChain 将 platform.OutboundMessage 转换为 OneBot MessageChain。
//
// 转换规则：
//   - Message.Text / Message.Markdown → 文本段
//   - Message.ReplyToID              → 回复段（前置）
//   - Message.Mentions               → at 段（回复段之后前置）
//   - Message.Attachments (URL)      → 图片段
func OutboundToChain(msg platform.OutboundMessage) MessageChain {
	var chain MessageChain

	// 回复段在前
	if msg.ReplyToID != "" {
		chain = append(chain, MessageSegment{
			Type: SegTypeReply,
			Data: map[string]string{"id": msg.ReplyToID},
		})
	}

	// AT 提及
	for _, uid := range msg.Mentions {
		chain = append(chain, MessageSegment{
			Type: SegTypeAt,
			Data: map[string]string{"qq": uid},
		})
	}

	// 主文本内容（优先使用 Markdown 段类型，否则回退到文本段）
	if msg.Markdown != "" {
		chain = append(chain, MessageSegment{
			Type: SegTypeMarkdown,
			Data: map[string]string{"content": msg.Markdown},
		})
	} else if msg.Text != "" {
		chain = append(chain, textSegment(msg.Text))
	}

	// 附件 — 根据 MIME 类型映射为图片/语音/视频
	for _, att := range msg.Attachments {
		if att.URL != "" {
			seg := attachmentToSegment(att)
			if seg.Type != "" {
				chain = append(chain, seg)
			}
		}
	}

	return chain
}

// attachmentToSegment 将 platform.Attachment 转换为 OneBot MessageSegment。
func attachmentToSegment(att platform.Attachment) MessageSegment {
	mime := strings.ToLower(att.MimeType)
	switch {
	case strings.HasPrefix(mime, "image/") || mime == "":
		return MessageSegment{
			Type: SegTypeImage,
			Data: map[string]string{"file": att.URL},
		}
	case strings.HasPrefix(mime, "audio/"):
		return MessageSegment{
			Type: SegTypeRecord,
			Data: map[string]string{"file": att.URL},
		}
	case strings.HasPrefix(mime, "video/"):
		return MessageSegment{
			Type: SegTypeVideo,
			Data: map[string]string{"file": att.URL},
		}
	case strings.HasPrefix(mime, "application/") || strings.HasPrefix(mime, "text/"):
		return MessageSegment{
			Type: SegTypeFile,
			Data: map[string]string{"file": att.URL, "name": att.Name},
		}
	default:
		// 不支持的类型；尽力当作图片处理
		return MessageSegment{
			Type: SegTypeImage,
			Data: map[string]string{"file": att.URL},
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 辅助：从 MessageChain 中提取回复 ID
// ────────────────────────────────────────────────────────────────────────────

// GetReplyToID 返回消息链中第一个回复段的 ID，若不存在则返回 ""。
func (mc MessageChain) GetReplyToID() string {
	for _, s := range mc {
		if s.Type == SegTypeReply {
			return s.ReplyID()
		}
	}
	return ""
}

// GetAtList 返回所有通过 @ 段提及的 QQ 号。
func (mc MessageChain) GetAtList() []string {
	var result []string
	for _, s := range mc {
		if s.Type == SegTypeAt {
			result = append(result, s.AtQQ())
		}
	}
	return result
}
