package platform

import (
	"strings"

	"github.com/tidwall/gjson"
)

// ────────────────────────────────────────────────────────────────────────────
// 引用消息（reply 段）共享助手
// ────────────────────────────────────────────────────────────────────────────

// QuoteAttachments 返回首个 reply 段携带的归一化被引用附件列表
// （Extra[SegmentExtraQuoteAtts]，类型恒为 []Attachment）。
// 无 reply 段或未填充时返回 nil。
func QuoteAttachments(segs []Segment) []Attachment {
	for _, s := range segs {
		if s.Type != SegmentReply {
			continue
		}
		if atts, ok := s.Extra[SegmentExtraQuoteAtts].([]Attachment); ok {
			return atts
		}
	}
	return nil
}

// QuotedImage 从引用消息段中提取被引用图片，返回 (URL, MimeType)。
//
// 提取顺序（对每个 reply 段）：
//  1. 归一化附件 Extra[SegmentExtraQuoteAtts]（跨平台统一路径）
//  2. QQ 兼容兜底 Extra["raw_quote"]（msg_elements 原始 JSON，图片位于
//     elements[].attachments[]）
//  3. QQ 兼容兜底 Extra["parallel_message"]（并行视图 msg_nodes[].attachments）
//
// 类型判定：优先 Kind 或 content_type 显式标注 image/* 的项；显式标注其他
// 类型（video/audio/file）的项跳过，不作为兜底；所有项均未标注类型时回退
// 首个带 URL 的项（真实类型由下载后的内容嗅探二次校验）。
// 无引用段或无可用的图片附件时返回空。
func QuotedImage(segs []Segment) (url, mimeType string) {
	for _, s := range segs {
		if s.Type != SegmentReply {
			continue
		}
		if atts, ok := s.Extra[SegmentExtraQuoteAtts].([]Attachment); ok {
			if u, mt := PickQuotedImage(atts); u != "" {
				return u, mt
			}
		}
		if raw, ok := s.Extra["raw_quote"].(string); ok && raw != "" && gjson.Valid(raw) {
			for _, elem := range gjson.Parse(raw).Array() {
				if u, mt := quotedImageFromJSONAtts(elem.Get("attachments")); u != "" {
					return u, mt
				}
			}
		}
		if raw, ok := s.Extra["parallel_message"].(string); ok && raw != "" && gjson.Valid(raw) {
			for _, node := range gjson.Get(raw, "msg_nodes").Array() {
				for _, elem := range node.Array() { // 对象节点 Array 返回单元素
					if u, mt := quotedImageFromJSONAtts(elem.Get("attachments")); u != "" {
						return u, mt
					}
				}
			}
		}
	}
	return "", ""
}

// PickQuotedImage 从归一化附件列表中取首个图片附件，返回 (URL, MimeType)。
//
// 优先 Kind 或 MimeType 显式标注 image 的项；显式标注其他类型的项跳过；
// 全部未标注类型时回退首个带 URL 的项（真实类型由下载后的内容嗅探二次校验）。
func PickQuotedImage(atts []Attachment) (string, string) {
	var fallbackURL string
	for _, att := range atts {
		if att.URL == "" {
			continue
		}
		if att.Kind == AttachmentKindImage || strings.HasPrefix(att.MimeType, "image/") {
			return att.URL, att.MimeType
		}
		if att.Kind != "" || att.MimeType != "" {
			continue // 显式标注的非图片类型，不作为兜底
		}
		if fallbackURL == "" {
			fallbackURL = att.URL
		}
	}
	if fallbackURL != "" {
		return fallbackURL, ""
	}
	return "", ""
}

// quotedImageFromJSONAtts 从被引用消息的附件 JSON 数组中取首个图片附件。
//
// 与 PickQuotedImage 同语义：显式非 image 类型跳过，未标注类型回退首个带 URL 项。
func quotedImageFromJSONAtts(atts gjson.Result) (string, string) {
	var fallbackURL string
	for _, att := range atts.Array() {
		u := att.Get("url").String()
		if u == "" {
			continue
		}
		mt := att.Get("content_type").String()
		if strings.HasPrefix(mt, "image/") {
			return u, mt
		}
		if mt != "" {
			continue // 显式标注的非图片类型，不作为兜底
		}
		if fallbackURL == "" {
			fallbackURL = u
		}
	}
	if fallbackURL != "" {
		return fallbackURL, ""
	}
	return "", ""
}
