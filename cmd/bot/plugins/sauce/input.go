package sauce

import (
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
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
// 委托 platform.QuotedImage 统一提取：优先 reply 段
// Extra[platform.SegmentExtraQuoteAtts]（各平台适配器归一化填充的
// 被引用附件列表），QQ 兼容兜底 raw_quote / parallel_message 原始 JSON。
// Telegram 等平台被引用消息只有 file_id 无直链时返回空。
func findQuotedImageURL(event platform.Event) string {
	url, _ := platform.QuotedImage(event.Segments())
	return url
}
