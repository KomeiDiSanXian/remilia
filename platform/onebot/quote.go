package onebot

// quote.go — 引用消息附件回查。
//
// OneBot v11 的消息事件只携带 reply 段的目标消息 ID，不含被引用消息本体；
// 图片等富媒体引用需经 get_msg 回查。本文件提供事件分发前的尽力而为回查：
// 把被引用消息中的图片附件归一化后填充到引用段
// Extra[platform.SegmentExtraQuoteAtts]，供下游（AI 视觉、识图插件等）消费。

import (
	stdctx "context"
	"strconv"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// quotedMsgFetchTimeout 回查被引用消息（get_msg）的单次超时。
//
// 回查发生在分发路径上（WS 适配器的单分发 goroutine / HTTP 适配器的请求
// 处理），超时越短对后续事件的拖累越小；本地 OneBot 实现 get_msg 通常
// 毫秒级返回，1s 足够。
const quotedMsgFetchTimeout = 1 * time.Second

// enrichQuotedAttachments 回查被引用消息并把其中的图片附件填充到引用段。
//
// 仅当事件带引用段且尚无引用附件数据时触发；get_msg 失败、超时或实现方
// 不支持该 action 时静默跳过，不影响事件投递。只提取 image 段；URL 取
// 实现方给出的直链（如多媒体下载链），会过期，下游应及时消费。
//
// 调用时机约束：必须在独立于 WebSocket 读循环的 goroutine 中调用——
// wsAPIClient.Call 等待的响应由读循环的 routeResponse 投递，若在读循环内
// 同步调用会互相等待直至超时（死锁）。
func enrichQuotedAttachments(ctx stdctx.Context, sender *Sender, ev platform.Event) {
	if sender == nil || ev == nil {
		return
	}
	replyID := platform.GetReplyToID(ev)
	if replyID == "" {
		return
	}
	id, err := strconv.ParseInt(replyID, 10, 64)
	if err != nil || id <= 0 {
		return // 非数字回复标识（如合并转发的节点引用），无法 get_msg
	}

	// 早退判断与填充都锚定"首个 reply 段"（与 GetReplyToID 语义一致）：
	// 首个 reply 段已携带引用数据时不再回查；回查成功后也只填充该段，
	// 避免多 reply 段场景下行为不一致。
	segs := ev.Segments()
	var replySeg *platform.Segment
	for i := range segs {
		if segs[i].Type == platform.SegmentReply {
			replySeg = &segs[i]
			break
		}
	}
	if replySeg == nil {
		return
	}
	if _, ok := replySeg.Extra[platform.SegmentExtraQuoteAtts]; ok {
		return // 实现方已内嵌引用数据，无需回查
	}

	fetchCtx, cancel := stdctx.WithTimeout(ctx, quotedMsgFetchTimeout)
	defer cancel()
	res, err := sender.GetMsg(fetchCtx, id)
	if err != nil || res == nil {
		return
	}
	var atts []platform.Attachment
	for _, s := range res.Message.Segments() {
		if s.Type == platform.SegmentImage && s.Attachment.URL != "" {
			atts = append(atts, s.Attachment)
		}
	}
	if len(atts) == 0 {
		return
	}
	if replySeg.Extra == nil {
		replySeg.Extra = make(map[string]any)
	}
	replySeg.Extra[platform.SegmentExtraQuoteAtts] = atts
}
