// reply.go — 回复上下文：用户"回复某条消息再 @ 机器人"时，把被回复消息的
// 内容前置到用户消息。
//
// 消息历史来自 messagelog（框架层，非插件状态），因此本包直接消费它而不额外
// 声明端口。命中出站消息（机器人自己的回复）时以"机器人"标注发送者。
package promptctx

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/textutil"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/tidwall/gjson"
)

// ReplyChainLink 回复链上的一环。
type ReplyChainLink struct {
	Name    string
	Content string
}

// ReplyContextMaxDepth 回复上下文追溯的最大层数（含第一层被回复消息）。
const ReplyContextMaxDepth = 3

// PrependReplyContext 若本条消息是回复，将所回复消息的内容前置到用户消息。
// 命中出站消息（机器人自己的回复）时以"机器人"标注发送者。
// 支持回复链追溯（回复的回复），最多 [ReplyContextMaxDepth] 层。
// 未命中或关闭时不改变 content。
//
// 查询路径：messagelog 按事件 ID 查（各平台回复 ID = 消息 ID，可命中）；
// 逐层沿 ReplyToEventID / ReplyToMessageID 向上追溯；查不到时走段兜底——
// QQ 引用消息的回复标识是 ref_msg_idx（REFIDX_xxx，
// 平台内部引用标识，与 messagelog 的事件 ID 不对应），此时从 reply 段的
// Extra["parallel_message"] 提取被引用内容（v1.34.0 起事件解析时保留）。
func PrependReplyContext(history *messagelog.Logger, ctx *eventctx.Context, content string) string {
	if history == nil {
		return content
	}
	replyID := platform.GetReplyToID(ctx.GetPlatformEvent())
	if replyID == "" {
		return content
	}

	chain := ResolveReplyChain(history, ctx.GetChatInfo().ID, replyID)
	if len(chain) == 0 {
		// QQ 引用合并转发兜底（优先于 parallel_message 占位符）：被引用消息
		// 是合并转发时，并行视图只有 "[聊天记录]" 占位文本，AI 无法得知
		// 记录内容；reply 段 Extra 携带的结构化记录渲染为可读文本注入
		if rec := QuotedForwardRecordFromSegments(ctx.GetPlatformEvent().Segments()); rec != nil {
			if text := platform.ForwardRecordText(rec); text != "" {
				chain = append(chain, ReplyChainLink{Name: "对方", Content: text})
			}
		}
	}
	if len(chain) == 0 {
		// QQ 引用消息段兜底：被引用内容在 parallel_message.msg_nodes[0].content
		if q := QuoteFromSegments(ctx.GetPlatformEvent().Segments()); q != "" {
			chain = append(chain, ReplyChainLink{Name: "对方", Content: q})
		}
	}
	if len(chain) == 0 {
		return content
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[你正在回复 %s 的消息]\n", chain[0].Name)
	for i, link := range chain {
		if i == 0 {
			fmt.Fprintf(&b, "%s: %s\n", link.Name, link.Content)
			continue
		}
		fmt.Fprintf(&b, "[%s 在回复 %s 的消息]\n%s: %s\n",
			chain[i-1].Name, link.Name, link.Name, link.Content)
	}
	prefix := strings.TrimRight(b.String(), "\n")
	if content == "" {
		return prefix
	}
	return prefix + "\n\n" + content
}

// ResolveReplyChain 沿回复链向上追溯（被回复消息 → 其被回复消息……），
// 返回最新在前，最多 [ReplyContextMaxDepth] 层。每条内容剥 @ 标记并单行截断。
// 优先按 messagelog event_id 追溯，其次平台消息 ID（旧数据回填字段）。
func ResolveReplyChain(history *messagelog.Logger, chatID, replyID string) []ReplyChainLink {
	chain := make([]ReplyChainLink, 0, ReplyContextMaxDepth)
	curID := replyID
	for len(chain) < ReplyContextMaxDepth {
		entry, ok := history.QueryByEventID(chatID, curID)
		if !ok {
			break
		}
		text := strings.TrimSpace(textutil.StripMentionMarkup(entry.Content))
		text = textutil.TruncateRunes(text, 200)
		if text == "" {
			break
		}
		name := entry.UserName
		if entry.IsOutbound {
			name = "机器人"
		}
		if name == "" {
			name = entry.UserID
		}
		if name == "" {
			name = "未知"
		}
		chain = append(chain, ReplyChainLink{Name: name, Content: text})

		next := entry.ReplyToEventID
		if next == "" {
			next = entry.ReplyToMessageID
		}
		if next == "" || next == curID {
			break
		}
		curID = next
	}
	return chain
}

// QuoteFromSegments 从 reply 段 Extra 提取被引用消息文本。
//
// QQ 引用消息（message_type=103）的 parallel_message 是被引用消息的并行视图
// （msg_nodes[0].message_type 跟随内容类型：0=文本、7=富媒体；content 为
// 被引用消息完整文本，含 @ 占位）。图片等富媒体引用时 content 为占位文本
// （如 "[图片] "），返回非空但调用方会在净化后为空时跳过。
func QuoteFromSegments(segs []platform.Segment) string {
	for _, s := range segs {
		if s.Type != platform.SegmentReply {
			continue
		}
		raw, ok := s.Extra["parallel_message"].(string)
		if !ok || raw == "" {
			continue
		}
		return gjson.Get(raw, "msg_nodes.0.content").String()
	}
	return ""
}

// QuotedForwardRecordFromSegments 提取 reply 段携带的被引用合并转发记录
// （platform.SegmentExtraQuotedForward，*platform.ForwardRecord；QQ 103
// 引用 102 时由平台适配器解析填充）。
func QuotedForwardRecordFromSegments(segs []platform.Segment) *platform.ForwardRecord {
	for _, s := range segs {
		if s.Type != platform.SegmentReply {
			continue
		}
		if rec, ok := s.Extra[platform.SegmentExtraQuotedForward].(*platform.ForwardRecord); ok {
			return rec
		}
	}
	return nil
}
