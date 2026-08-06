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
func buildTelegramSegments(msg *Message) []platform.Segment {
	var segs []platform.Segment
	if msg.ReplyToMsg != nil {
		segs = append(segs, platform.Segment{Type: platform.SegmentReply, ReplyToID: strconv.Itoa(msg.ReplyToMsg.MessageID)})
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

// buildTelegramOutboundText 将统一出站段映射为 Telegram 文本（§4.2）。
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
// 与 collectAttachments 一一对应，保证 Attachments() 派生视图与旧行为一致。
func attachmentSegments(msg *Message) []platform.Segment {
	var segs []platform.Segment
	appendMedia := func(t platform.SegmentType, att platform.Attachment) {
		segs = append(segs, platform.Segment{Type: t, Attachment: att})
	}
	if len(msg.Photo) > 0 {
		p := msg.Photo[len(msg.Photo)-1]
		appendMedia(platform.SegmentImage, platform.Attachment{
			Width:  p.Width,
			Height: p.Height,
			Size:   p.FileSize,
			Extra:  map[string]any{ExtraKeyFile: &FileMeta{FileID: p.FileID, FileUniqueID: p.FileUniqueID}},
		})
	}
	if msg.Audio != nil {
		appendMedia(platform.SegmentAudio, platform.Attachment{
			MimeType: msg.Audio.MimeType,
			Name:     msg.Audio.FileName,
			Size:     msg.Audio.FileSize,
			Extra:    map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Audio.FileID, FileUniqueID: msg.Audio.FileUniqueID}},
		})
	}
	if msg.Video != nil {
		appendMedia(platform.SegmentVideo, platform.Attachment{
			MimeType: msg.Video.MimeType,
			Name:     msg.Video.FileName,
			Width:    msg.Video.Width,
			Height:   msg.Video.Height,
			Size:     msg.Video.FileSize,
			Extra:    map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Video.FileID, FileUniqueID: msg.Video.FileUniqueID}},
		})
	}
	if msg.Document != nil {
		appendMedia(platform.SegmentFile, platform.Attachment{
			MimeType: msg.Document.MimeType,
			Name:     msg.Document.FileName,
			Size:     msg.Document.FileSize,
			Extra:    map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Document.FileID, FileUniqueID: msg.Document.FileUniqueID}},
		})
	}
	if msg.Voice != nil {
		appendMedia(platform.SegmentAudio, platform.Attachment{
			MimeType: msg.Voice.MimeType,
			Size:     msg.Voice.FileSize,
			Extra:    map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Voice.FileID, FileUniqueID: msg.Voice.FileUniqueID}},
		})
	}
	if msg.Animation != nil {
		appendMedia(platform.SegmentVideo, platform.Attachment{
			Width:  msg.Animation.Width,
			Height: msg.Animation.Height,
			Size:   msg.Animation.FileSize,
			Extra:  map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Animation.FileID, FileUniqueID: msg.Animation.FileUniqueID}},
		})
	}
	if msg.Sticker != nil {
		appendMedia(platform.SegmentImage, platform.Attachment{
			Width:  msg.Sticker.Width,
			Height: msg.Sticker.Height,
			Size:   msg.Sticker.FileSize,
			Extra:  map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Sticker.FileID, FileUniqueID: msg.Sticker.FileUniqueID}},
		})
	}
	return segs
}
