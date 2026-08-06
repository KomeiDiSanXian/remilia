package onebot

import (
	"github.com/KomeiDiSanXian/remilia/platform"
)

// segments.go — OneBot MessageChain → 统一消息段映射
//
// 段是跨平台唯一真相源：Content()/Attachments()/Mentions()/ReplyToID()
// 均由本映射结果派生，保证视图一致（见 platform.SegmentsContent 等派生函数）。

// Segments 将 OneBot MessageChain 映射为统一段列表（保序）。
func (mc MessageChain) Segments() []platform.Segment {
	segs := make([]platform.Segment, 0, len(mc))
	for _, s := range mc {
		segs = append(segs, mapSegment(s))
	}
	return segs
}

// mapSegment 将单个 OneBot 段映射为统一段。
//
// 已知类型全量映射；无法映射的段（rps/dice/share/contact/location/music/
// xml/json/node/anonymous 等）→ SegmentUnknown + Extra 保留原始数据。
func mapSegment(s MessageSegment) platform.Segment {
	switch s.Type {
	case SegTypeText:
		return platform.Segment{Type: platform.SegmentText, Text: s.TextData()}
	case SegTypeAt:
		qq := s.AtQQ()
		if qq == "all" {
			return platform.Segment{Type: platform.SegmentMentionAll}
		}
		return platform.Segment{Type: platform.SegmentAt, UserID: qq, Text: s.Data["name"]}
	case SegTypeFace:
		return platform.Segment{Type: platform.SegmentFace, FaceID: s.Data["id"]}
	case SegTypeImage:
		return platform.Segment{Type: platform.SegmentImage, Attachment: mediaAttachment(s, platform.AttachmentKindImage)}
	case SegTypeRecord:
		return platform.Segment{Type: platform.SegmentAudio, Attachment: mediaAttachment(s, platform.AttachmentKindAudio)}
	case SegTypeVideo:
		return platform.Segment{Type: platform.SegmentVideo, Attachment: mediaAttachment(s, platform.AttachmentKindVideo)}
	case SegTypeFile:
		return platform.Segment{Type: platform.SegmentFile, Attachment: mediaAttachment(s, platform.AttachmentKindFile)}
	case SegTypeReply:
		return platform.Segment{Type: platform.SegmentReply, ReplyToID: s.Data["id"]}
	case SegTypeForward:
		// 合并转发：id 为平台原生转发消息 ID（同平台转发时可用 Extra 还原）
		return platform.Segment{
			Type:  platform.SegmentForward,
			Extra: map[string]any{"forward_id": s.Data["id"], "type": s.Type},
		}
	case SegTypeKeyboard:
		return platform.Segment{
			Type:  platform.SegmentButton,
			Extra: map[string]any{"type": s.Type, "data": s.Data},
		}
	default:
		extra := map[string]any{"type": s.Type, "data": s.Data}
		if len(s.RawData) > 0 {
			extra["raw_data"] = s.RawData
		}
		return platform.Segment{Type: platform.SegmentUnknown, Extra: extra}
	}
}

// mediaAttachment 从媒体段提取统一附件。
func mediaAttachment(s MessageSegment, kind platform.AttachmentKind) platform.Attachment {
	att := platform.Attachment{Kind: kind}
	if u := s.Data["url"]; u != "" {
		att.URL = u
	} else if f := s.Data["file"]; f != "" {
		att.URL = f
	}
	if kind == platform.AttachmentKindFile {
		att.Name = s.Data["name"]
		if att.Name == "" {
			att.Name = s.Data["file"]
		}
		att.MimeType = "application/octet-stream"
	} else {
		att.Name = s.Data["file"]
	}
	return att
}

// segmentsToMentions 从统一段派生被 @ 用户聚合视图（保序去重）。
//
// OneBot 无 payload 级自我标记，IsSelf 由 botID 判定；botID 为空时全部为 false。
func segmentsToMentions(segs []platform.Segment, botID string) []platform.UserInfo {
	return platform.SegmentsMentions(segs, botID)
}

// segmentsReplyToID 返回段中首个回复段的目标消息 ID。
func segmentsReplyToID(segs []platform.Segment) string {
	return platform.SegmentsReplyToID(segs)
}
