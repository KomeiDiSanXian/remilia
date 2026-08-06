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

// segmentToMessageSegment 将统一段逆向映射为 OneBot 段（出站，§4.2）。
//
// 已知段全量映射；forward 用 Extra["forward_id"] 还原；button/unknown 无
// 对应发送能力 → 返回零值（调用方跳过）。
func segmentToMessageSegment(s platform.Segment) (MessageSegment, bool) {
	switch s.Type {
	case platform.SegmentText:
		return textSegment(s.Text), true
	case platform.SegmentAt:
		return MessageSegment{Type: SegTypeAt, Data: map[string]string{"qq": s.UserID}}, true
	case platform.SegmentMentionAll:
		return MessageSegment{Type: SegTypeAt, Data: map[string]string{"qq": "all"}}, true
	case platform.SegmentImage:
		return mediaSegmentToChain(s.Attachment, SegTypeImage), true
	case platform.SegmentAudio:
		return mediaSegmentToChain(s.Attachment, SegTypeRecord), true
	case platform.SegmentVideo:
		return mediaSegmentToChain(s.Attachment, SegTypeVideo), true
	case platform.SegmentFile:
		return mediaSegmentToChain(s.Attachment, SegTypeFile), true
	case platform.SegmentFace:
		return MessageSegment{Type: SegTypeFace, Data: map[string]string{"id": s.FaceID}}, true
	case platform.SegmentReply:
		return MessageSegment{Type: SegTypeReply, Data: map[string]string{"id": s.ReplyToID}}, true
	case platform.SegmentForward:
		if id, ok := s.Extra["forward_id"].(string); ok && id != "" {
			return MessageSegment{Type: SegTypeForward, Data: map[string]string{"id": id}}, true
		}
		return MessageSegment{}, false
	default:
		return MessageSegment{}, false
	}
}

// mediaSegmentToChain 将统一附件段映射为 OneBot 媒体段。
func mediaSegmentToChain(att platform.Attachment, segType string) MessageSegment {
	data := map[string]string{"file": att.URL}
	if att.URL == "" {
		data["file"] = att.Name
	}
	if segType == SegTypeFile && att.Name != "" {
		data["name"] = att.Name
	}
	return MessageSegment{Type: segType, Data: data}
}

// OutboundChainFromSegments 将统一出站段转换为 OneBot MessageChain（保序）。
//
// 与 OutboundToChain 的便捷字段路径互补：段路径保留文本夹 at 的交错位置；
// 无法映射的段（button/unknown）跳过，不中断整体发送。
func OutboundChainFromSegments(segs []platform.Segment) MessageChain {
	var chain MessageChain
	for _, s := range segs {
		seg, ok := segmentToMessageSegment(s)
		if !ok {
			continue
		}
		chain = append(chain, seg)
	}
	return chain
}
