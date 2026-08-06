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
	SegTypeMface    = "mface"    // 商城表情（NapCat/LuckyLilliaBot 扩展，带 emoji_package_id / emoji_id / key）
	SegTypeMarkdown = "markdown" // Markdown 消息（NapCat 扩展，仅可在双层合并转发内发送）
	SegTypeFile     = "file"     // 文件（NapCat 扩展，含 url, path, file_size 等）
	// SegTypeKeyboard 为 LLOneBot/LuckyLilliaBot 扩展（接收侧）：
	// 来源 github.com/LLOneBot/LuckyLilliaBot src/onebot11/types.ts OB11MessageKeyboard
	// （2026-08 核验），NapCat/go-cqhttp/Lagrange.OneBot 无此段。
	// 数据字段：rows[].buttons[].{id, render_data{label,visited_label,style},
	// action{type,permission{type,specify_role_ids,specify_user_ids},
	// unsupport_tips,data,reply,enter}}；LLB 仅接收不上报 CQ:keyboard，发送侧不支持。
	SegTypeKeyboard = "keyboard" // 按钮交互
)

// SegmentData 是消息段的参数字典。
//
// 底层仍是 map[string]string，既有的 s.Data["text"] 读取与
// Data: map[string]string{...} 字面量写法都不受影响。
// 区别在于它自带一个容错的 UnmarshalJSON。
//
// 为什么需要容错：OneBot V11 各实现（NapCat / Lagrange / go-cqhttp …）在
// data 里混用类型非常普遍，例如 {"qq":123}（数字）、{"flash":true}（布尔）、
// node 段的 {"content":[...]}（数组）。若直接声明成 map[string]string，
// 这些值会让 json.Unmarshal 报错，而该错误会一路冒泡到 parseEvent，
// receiveLoop 只能打一行 "Failed to parse event" 然后丢弃**整条事件**——
// 一个无关紧要的字段类型不符，就吃掉了整条群消息。
//
// 现在标量一律按字面量转成字符串，数组/对象保留其原始 JSON 文本，
// 未知字段最差也只是"这一个字段不好用"，不会波及整条事件。
type SegmentData map[string]string

// UnmarshalJSON 实现 json.Unmarshaler，对值类型做宽容降级。
func (d *SegmentData) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		// data 不是对象（实测有实现会发 "data":"" 或 "data":[]）。
		// 这里同样不能返回错误：错误会冒泡到 parseEvent 并丢弃整条事件，
		// 正是本类型要消除的失败模式。降级为"无参数消息段"。
		return nil
	}
	out := make(SegmentData, len(raw))
	for k, v := range raw {
		out[k] = rawJSONToString(v)
	}
	*d = out
	return nil
}

// rawJSONToString 把任意 JSON 值降级为字符串表示。
//
//	"abc"      → abc          （解引号）
//	123 / true → 123 / true   （字面量原样）
//	[...] {...}→ 原始 JSON 文本（供调用方按需二次解析）
//	null       → 空字符串
func rawJSONToString(v json.RawMessage) string {
	s := strings.TrimSpace(string(v))
	if s == "" || s == "null" {
		return ""
	}
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal(v, &str); err == nil {
			return str
		}
	}
	return s
}

// MessageSegment 表示 OneBot V11 消息中的单个消息段。
// Data 字段包含与消息段类型相关的参数。
type MessageSegment struct {
	Type string      `json:"type"`
	Data SegmentData `json:"data"`

	// RawData 用于构造 data 中含有非字符串值的消息段（典型如 node 段的
	// content 数组）。非 nil 时序列化以它为准，Data 被忽略。
	//
	// 仅用于发送侧；接收侧一律填充 Data。
	RawData map[string]json.RawMessage `json:"-"`
}

// MarshalJSON 实现 json.Marshaler。
//
// RawData 非 nil 时按其内容原样输出，使 node 这类要求嵌套结构的消息段
// 能够被正确构造；否则退回普通的字符串字典。
func (s MessageSegment) MarshalJSON() ([]byte, error) {
	if s.RawData == nil {
		type plain struct {
			Type string      `json:"type"`
			Data SegmentData `json:"data"`
		}
		return json.Marshal(plain{Type: s.Type, Data: s.Data})
	}
	type rawSeg struct {
		Type string                     `json:"type"`
		Data map[string]json.RawMessage `json:"data"`
	}
	return json.Marshal(rawSeg{Type: s.Type, Data: s.RawData})
}

// NewNodeSegment 构造合并转发的 node 段。
//
// node 段的 content 字段在协议里是**消息**类型（字符串或消息段数组），
// 无法用 map[string]string 表达；此前 SegTypeNode 因此完全不可构造，
// 合并转发发不出去。这里通过 RawData 承载嵌套结构。
func NewNodeSegment(userID, nickname string, content MessageChain) (MessageSegment, error) {
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return MessageSegment{}, fmt.Errorf("onebot: marshal node content: %w", err)
	}
	uid, _ := json.Marshal(userID)
	nick, _ := json.Marshal(nickname)
	return MessageSegment{
		Type: SegTypeNode,
		RawData: map[string]json.RawMessage{
			"user_id":  uid,
			"nickname": nick,
			"content":  contentJSON,
		},
	}, nil
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
	// encoding/json 明确规定：字面量 null 也会调用 UnmarshalJSON，
	// 实现应将其视为空操作。缺少这一分支时，"message": null
	// 会以 "unexpected message format" 报错并丢弃整条事件。
	if string(b) == "null" {
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

// ToAttachments 从图片/语音/视频段中提取 platform.Attachment。
func (mc MessageChain) ToAttachments() []platform.Attachment {
	var result []platform.Attachment
	for _, s := range mc {
		switch s.Type {
		case SegTypeImage:
			u := s.ImageURL()
			if u != "" {
				result = append(result, platform.Attachment{
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
				result = append(result, platform.Attachment{
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
				result = append(result, platform.Attachment{
					URL:      u,
					MimeType: "video/*",
					Name:     s.Data["file"],
				})
			}
		case SegTypeMface:
			// 商城表情：若有 url 则作为附件处理
			if u := s.Data["url"]; u != "" {
				result = append(result, platform.Attachment{
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
				result = append(result, platform.Attachment{
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
// &#91; → [    &#93; → ]    &amp; → &
//
// &amp; 必须最后还原：否则 "&amp;#91;"（字面量文本 "&#91;" 的正确编码）
// 会先变成 "&#91;"，再被二次解码成 "["，凭空产生 CQ 码分隔符。
// 顺序与下方 unescapeCQValue 保持一致。
func unescapeText(s string) string {
	s = strings.ReplaceAll(s, "&#91;", "[")
	s = strings.ReplaceAll(s, "&#93;", "]")
	s = strings.ReplaceAll(s, "&amp;", "&")
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
