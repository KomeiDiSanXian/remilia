package telegram

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// segments.go — Telegram Message → 统一消息段映射
//
// 段是跨平台唯一真相源：Content()/Attachments() 均由本映射结果派生。
// 文本按 mention/text_mention 实体切分（UTF-16 单位，见 collectMentions 注释），
// 其余实体（粗体/斜体/链接等）为行内格式，统一段模型不表达，折叠为纯文本。

// buildTelegramSegments 将 Telegram Message 映射为保序统一段。
//
// 顺序：reply 段（引用在消息最前）→ 文本/at 交错段 → 媒体段（附属于正文之后）。
//
// 引用消息：Telegram update 内嵌完整被引用消息（reply_to_message），其媒体
// 以 file_id 归一化进 reply 段 Extra[platform.SegmentExtraQuoteAtts]；URL
// 由适配器的附件解析步骤统一换取（见 PollingAdapter.resolveAttachmentURLs）。
func buildTelegramSegments(msg *Message) []platform.Segment {
	var segs []platform.Segment
	if msg.ReplyToMsg != nil {
		seg := platform.Segment{Type: platform.SegmentReply, ReplyToID: strconv.Itoa(msg.ReplyToMsg.MessageID)}
		if qa := collectAttachments(msg.ReplyToMsg); len(qa) > 0 {
			seg.Extra = map[string]any{platform.SegmentExtraQuoteAtts: qa}
		}
		segs = append(segs, seg)
	}

	text := msg.Text
	if text == "" {
		text = msg.Caption
	}
	segs = append(segs, splitMentionEntities(text, msg.Entities)...)
	segs = append(segs, attachmentSegments(msg)...)
	return segs
}

// splitMentionEntities 按 mention/text_mention 实体把正文切成 text/at 交错段。
//
// 实体区间以 UTF-16 代码单元计（Telegram 官方语义），必须先编码再切分，
// 否则含 emoji/中文的消息会错位。越界或重叠的异常实体防御性跳过。
func splitMentionEntities(text string, entities []MessageEntity) []platform.Segment {
	type span struct {
		start, end int
		ent        MessageEntity
	}
	var spans []span
	for _, ent := range entities {
		if ent.Type == "mention" || ent.Type == "text_mention" {
			spans = append(spans, span{start: ent.Offset, end: ent.Offset + ent.Length, ent: ent})
		}
	}
	if len(spans) == 0 {
		if text == "" {
			return nil
		}
		return []platform.Segment{{Type: platform.SegmentText, Text: text}}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	u16 := utf16.Encode([]rune(text))
	var out []platform.Segment
	pos := 0
	for _, sp := range spans {
		if sp.start < pos || sp.start > len(u16) || sp.end > len(u16) {
			continue // 越界或重叠：报文异常，跳过而非 panic
		}
		if sp.start > pos {
			out = append(out, platform.Segment{Type: platform.SegmentText, Text: string(utf16.Decode(u16[pos:sp.start]))})
		}
		if sp.ent.Type == "text_mention" && sp.ent.User != nil {
			out = append(out, platform.Segment{
				Type:   platform.SegmentAt,
				UserID: strconv.FormatInt(sp.ent.User.ID, 10),
				Text:   sp.ent.User.DisplayName(),
			})
		} else {
			name := strings.TrimPrefix(string(utf16.Decode(u16[sp.start:sp.end])), "@")
			out = append(out, platform.Segment{Type: platform.SegmentAt, Text: name})
		}
		pos = sp.end
	}
	if pos < len(u16) {
		out = append(out, platform.Segment{Type: platform.SegmentText, Text: string(utf16.Decode(u16[pos:]))})
	}
	return out
}

// buildTelegramOutboundText 将统一出站段映射为 Telegram 文本。
//
// 注意：at 段无法还原为 text_mention 实体（实体需要完整 User 对象，出站仅有
// UserID）→ 按"尽力降级"渲染为 "@UserID" 文本；reply/face/forward/button/
// unknown 段无文本表达 → 跳过（reply 由调用方经 ReplyToID 单独传递）。
func buildTelegramOutboundText(segs []platform.Segment) string {
	var b strings.Builder
	for _, s := range segs {
		switch s.Type {
		case platform.SegmentText:
			b.WriteString(s.Text)
		case platform.SegmentAt:
			b.WriteString("@" + s.UserID)
		case platform.SegmentMentionAll:
			b.WriteString("@全体成员")
		}
	}
	return b.String()
}

// attachmentSegments 将媒体附件映射为媒体段（顺序：photo/audio/video/document/voice/animation/sticker）。
//
// 与 collectAttachments 共用同一套附件提取与 Kind 标注（单一真相源），
// 保证本条消息与引用消息的附件元数据一致。
func attachmentSegments(msg *Message) []platform.Segment {
	var segs []platform.Segment
	for _, att := range collectAttachments(msg) {
		segs = append(segs, platform.Segment{Type: segmentTypeFromKind(att.Kind), Attachment: att})
	}
	return segs
}

// segmentTypeFromKind 将附件 Kind 映射为统一段类型。
func segmentTypeFromKind(kind platform.AttachmentKind) platform.SegmentType {
	switch kind {
	case platform.AttachmentKindImage:
		return platform.SegmentImage
	case platform.AttachmentKindAudio:
		return platform.SegmentAudio
	case platform.AttachmentKindVideo:
		return platform.SegmentVideo
	default:
		return platform.SegmentFile
	}
}
