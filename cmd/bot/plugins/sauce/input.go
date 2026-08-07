package sauce

import (
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/tidwall/gjson"
)

// ── 图片输入解析 ───────────────────────────────────────────────────────
//
// 支持三种输入方式（按优先级）：
//  1. 本条消息携带的图片附件（URL）
//  2. 引用（回复）消息中携带的图片——QQ 平台在事件解析时把被引用消息的
//     原始 msg_elements 存进 reply 段 Extra["raw_quote"]，其中
//     attachments[].url 即为被引用图片的下载地址
//  3. 都不满足时进入等待流程（见 wait.go），由用户随后补发图片

// resolveImageSource 解析本次命令的图片来源。
//
// 返回 (imageURL, true) 表示已取得图片并可继续；返回 ("", false) 表示
// 已进入等待流程或无可恢复的错误，调用方应直接返回。
func (p *Plugin) resolveImageSource(ctx *eventctx.Context, engines engineSet) (string, bool) {
	event := ctx.GetPlatformEvent()
	if event == nil {
		ctx.ReplyError("无法获取消息内容")
		return "", false
	}

	// 1. 本条消息图片附件
	if url := findImageURL(event); url != "" {
		return url, true
	}

	// 2. 引用消息中的图片（reply 段 Extra["raw_quote"]，QQ 等平台携带）
	if url := findQuotedImageURL(event); url != "" {
		return url, true
	}

	// 3. 等待用户补发图片（60 秒超时；发送非图片消息即取消）
	p.beginImageWait(ctx, engines)
	return "", false
}

// findQuotedImageURL 从引用消息段中提取被引用图片的 URL。
//
// 各平台实现：
//   - QQ：reply 段 Extra["raw_quote"] 为被引用消息的 msg_elements 原始 JSON，
//     取其中首个 attachments[].url；Extra["parallel_message"] 为并行视图兜底
//   - 其他平台：尽力从 reply 段 Extra 中查找 "raw_quote"/"parallel_message"
//     同构数据；Telegram 等平台被引用消息只有 file_id 无直链时返回空
func findQuotedImageURL(event platform.Event) string {
	for _, s := range event.Segments() {
		if s.Type != platform.SegmentReply {
			continue
		}
		if raw, ok := s.Extra["raw_quote"].(string); ok && raw != "" {
			if url := quotedImageFromRawQuote(raw); url != "" {
				return url
			}
		}
		if raw, ok := s.Extra["parallel_message"].(string); ok && raw != "" {
			if url := quotedImageFromParallel(raw); url != "" {
				return url
			}
		}
	}
	return ""
}

// quotedImageFromRawQuote 从 QQ msg_elements JSON 中提取被引用图片 URL。
//
// 结构：msg_elements 为数组，其元素（被引用消息）的 attachments 数组内
// 每条含 url/content_type 等字段。优先取 image/* 类型的附件。
func quotedImageFromRawQuote(raw string) string {
	if !gjson.Valid(raw) {
		return ""
	}
	for _, elem := range gjson.Parse(raw).Array() {
		atts := elem.Get("attachments").Array()
		if len(atts) == 0 {
			continue
		}
		for _, att := range atts {
			ct := att.Get("content_type").String()
			u := att.Get("url").String()
			if ct != "" && !strings.HasPrefix(ct, "image/") {
				continue
			}
			if u != "" {
				return u
			}
		}
		// 无 content_type 时取第一个带 url 的附件
		for _, att := range atts {
			if u := att.Get("url").String(); u != "" {
				return u
			}
		}
	}
	return ""
}

// quotedImageFromParallel 从 QQ parallel_message（msg_nodes 并行视图）中提取图片 URL。
//
// parallel_message 是被引用消息的并行视图，结构：
// {"msg_nodes": [{"message_type": 7, "attachments": [{"url": ...}]}]}
// 富媒体消息（message_type=7）的 content 仅为 "[图片]" 占位文本，图片在
// attachments 内。作为 raw_quote 解析失败时的兜底。
func quotedImageFromParallel(raw string) string {
	if !gjson.Valid(raw) {
		return ""
	}
	nodes := gjson.Get(raw, "msg_nodes").Array()
	for _, n := range nodes {
		// 节点本身可能是富媒体元素数组（元素带 message_type/attachments）
		if u := quotedImageFromRawQuote(n.Raw); u != "" {
			return u
		}
		if n.IsObject() {
			atts := n.Get("attachments").Array()
			for _, att := range atts {
				ct := att.Get("content_type").String()
				u := att.Get("url").String()
				if ct != "" && !strings.HasPrefix(ct, "image/") {
					continue
				}
				if u != "" {
					return u
				}
			}
		}
	}
	return ""
}
