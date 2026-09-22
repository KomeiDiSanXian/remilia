// forward.go — 合并转发（直发记录）的识别、触发与图片提取。
//
// 合并转发消息不带 @、正文为空，触发规则与普通消息不同，因此单独成篇；
// 这里只消费 platform.Event 的段结构，不读插件状态。
package runtime

import (
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ForwardRecordFromEvent 提取事件本体携带的合并转发记录
// （SegmentForward 段的 platform.SegmentExtraForwardNodes 载荷）。
func ForwardRecordFromEvent(ev platform.Event) *platform.ForwardRecord {
	if ev == nil {
		return nil
	}
	for _, s := range ev.Segments() {
		if s.Type != platform.SegmentForward {
			continue
		}
		if rec, ok := s.Extra[platform.SegmentExtraForwardNodes].(*platform.ForwardRecord); ok {
			return rec
		}
	}
	return nil
}

// ForwardTriggerContent 直发合并转发消息的 AI 触发决策。
//
// QQ 的合并转发消息无法携带 @：群聊里直发转发不存在"发给机器人"的显式
// 意图，自动回复会刷屏（自主发言群亦然），故不触发——记录仍由 messagelog
// 入库作为群聊窗口上下文，需要讨论时引用该记录（引用上下文会渲染记录全文）。
// 私聊直发视为直接对话，渲染记录文本后触发。
func ForwardTriggerContent(chat platform.ChatInfo, rec *platform.ForwardRecord) (string, bool) {
	if chat.IsGroup {
		return "", false
	}
	return platform.ForwardRecordText(rec), true
}

// ForwardRecordImageAtts 提取直发合并转发记录中的图片附件（节点顺序，
// 递归嵌套关联子条目），供视觉管线注入。
//
// maxImages 为注入上限（<=0 不限制）：超出截断——避免超出
// max_images_per_message 触发整条消息拒绝；截断部分的占位符仍在渲染
// 文本中，模型可感知"此处有图但未注入"。
func ForwardRecordImageAtts(ev platform.Event, maxImages int) []platform.Attachment {
	rec := ForwardRecordFromEvent(ev)
	if rec == nil {
		return nil
	}
	var out []platform.Attachment
	collectForwardRecordImages(rec.Nodes, &out)
	if maxImages > 0 && len(out) > maxImages {
		out = out[:maxImages]
	}
	return out
}

func collectForwardRecordImages(nodes []platform.ForwardNode, out *[]platform.Attachment) {
	for _, n := range nodes {
		for _, s := range n.Segments {
			if s.Type == platform.SegmentImage && s.Attachment.URL != "" {
				*out = append(*out, s.Attachment)
			}
		}
		collectForwardRecordImages(n.Related, out)
	}
}

// QuotedImageFromSegments 从 reply 段提取被引用消息中的图片 URL 与 MIME 类型。
//
// 各平台适配器在解析引用消息时把被引用附件归一化存入
// Extra[platform.SegmentExtraQuoteAtts]（[]platform.Attachment，URL 为事件
// 时刻直链），优先消费；QQ 引用消息另以 Extra["raw_quote"]（msg_elements
// 原始 JSON）与 Extra["parallel_message"]（并行视图）保留原始数据，作为
// 约定键缺省时的兜底路径。实现委托 platform.QuotedImage，跨插件共享同一
// 套提取/类型判定语义。
func QuotedImageFromSegments(segs []platform.Segment) (url, mimeType string) {
	return platform.QuotedImage(segs)
}
