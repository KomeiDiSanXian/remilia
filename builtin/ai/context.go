// Package ai context.go — 回复上下文与群聊消息窗口。
//
// 本文件实现两个依赖 messagelog 消息历史的结构级上下文能力：
//   - 回复上下文：用户"回复某条消息再 @ 机器人"时，把被回复消息的内容前置到用户消息
//   - 群聊消息窗口：把同群最近的若干条入站消息注入系统提示，让 AI 感知多人对话
//
// 同时提供 replyAndRecord 薄封装：确保出站消息被 messagelog 的出站观察者记录，
// 从而使"回复机器人上一条消息"也能被回复上下文命中。
package ai

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/future"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/tidwall/gjson"
)

// replyAndRecord 发送消息并确保出站记录。
//
// 发送仍走 ctx.Reply 的异步调度器（提交即返回，不阻塞 handler）。
// 出站消息的记录由 messagelog 的 OutboundObserver 在发送完成后同步完成
// （见 messagelog.MessageLogger 中间件 / Logger.OnOutbound）。
//
// 此薄封装仅在上下文缺少观察者时（如 doSummary 用 NewContextFromEvent
// 新建的上下文）用 p.history 补上，保证 AI 的对话回复总能被记录。
// 记录不受 include_reply_context 控制——记录是 messagelog 级行为，
// 该配置只控制回复上下文的注入（见 prependReplyContext）。
func (p *Plugin) replyAndRecord(ctx *eventctx.Context, msg platform.OutboundMessage) *future.Future[platform.SendResult] {
	if _, ok := ctx.Ext().GetTyped[eventctx.OutboundObserverExt](); !ok && p.history != nil {
		ctx.Ext().SetTyped(eventctx.OutboundObserverExt{Observer: p.history})
	}
	return ctx.Reply(msg)
}

// replyFormatted 按 markdown 配置发送带格式的子命令回复。
//
// markdown=true 时使用 MarkdownMessage（平台不支持时由发送层自动降级为纯文本）；
// markdown=false 时始终发送纯文本。与主回复路径（handler/runner/sendtool）保持一致，
// 避免子命令文案（粗体/行内代码等）在支持 Markdown 的平台上一律以字面符号展示。
func (p *Plugin) replyFormatted(ctx *eventctx.Context, text string) *future.Future[platform.SendResult] {
	if p.cfg != nil && p.cfg.Markdown {
		return ctx.Reply(platform.MarkdownMessage(text))
	}
	return ctx.ReplyText(text)
}

// prependReplyContext 若本条消息是回复，将所回复消息的内容前置到用户消息。
// 命中出站消息（机器人自己的回复）时以"机器人"标注发送者。
// 支持回复链追溯（回复的回复），最多 replyContextMaxDepth 层。
// 未命中或关闭时不改变 content。
//
// 查询路径：messagelog 按事件 ID 查（各平台回复 ID = 消息 ID，可命中）；
// 逐层沿 ReplyToEventID / ReplyToMessageID 向上追溯；查不到时走段兜底——
// QQ 引用消息的回复标识是 ref_msg_idx（REFIDX_xxx，
// 平台内部引用标识，与 messagelog 的事件 ID 不对应），此时从 reply 段的
// Extra["parallel_message"] 提取被引用内容（v1.34.0 起事件解析时保留）。
func (p *Plugin) prependReplyContext(ctx *eventctx.Context, content string) string {
	if p.history == nil {
		return content
	}
	replyID := platform.GetReplyToID(ctx.GetPlatformEvent())
	if replyID == "" {
		return content
	}

	chain := p.resolveReplyChain(ctx.GetChatInfo().ID, replyID)
	if len(chain) == 0 {
		// QQ 引用消息段兜底：被引用内容在 parallel_message.msg_nodes[0].content
		if q := replyQuoteFromSegments(ctx.GetPlatformEvent().Segments()); q != "" {
			chain = append(chain, replyChainLink{Name: "对方", Content: q})
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

// replyChainLink 回复链上的一环。
type replyChainLink struct {
	Name    string
	Content string
}

// replyContextMaxDepth 回复上下文追溯的最大层数（含第一层被回复消息）。
const replyContextMaxDepth = 3

// resolveReplyChain 沿回复链向上追溯（被回复消息 → 其被回复消息……），
// 返回最新在前，最多 replyContextMaxDepth 层。每条内容剥 @ 标记并单行截断。
// 优先按 messagelog event_id 追溯，其次平台消息 ID（旧数据回填字段）。
func (p *Plugin) resolveReplyChain(chatID, replyID string) []replyChainLink {
	chain := make([]replyChainLink, 0, replyContextMaxDepth)
	curID := replyID
	for len(chain) < replyContextMaxDepth {
		entry, ok := p.history.QueryByEventID(chatID, curID)
		if !ok {
			break
		}
		text := strings.TrimSpace(stripMentionMarkup(entry.Content))
		text = truncateRunes(text, 200)
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
		chain = append(chain, replyChainLink{Name: name, Content: text})

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

// replyQuoteFromSegments 从 reply 段 Extra 提取被引用消息文本。
//
// QQ 引用消息（message_type=103）的 parallel_message 是被引用消息的并行视图
// （msg_nodes[0].message_type 跟随内容类型：0=文本、7=富媒体；content 为
// 被引用消息完整文本，含 @ 占位）。图片等富媒体引用时 content 为占位文本
// （如 "[图片] "），返回非空但调用方会在净化后为空时跳过。
func replyQuoteFromSegments(segs []platform.Segment) string {
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

// quotedImageFromSegments 从 reply 段提取被引用消息中的图片 URL 与 MIME 类型。
//
// 各平台适配器在解析引用消息时把被引用附件归一化存入
// Extra[platform.SegmentExtraQuoteAtts]（[]platform.Attachment，URL 为事件
// 时刻直链），优先消费；QQ 引用消息另以 Extra["raw_quote"]（msg_elements
// 原始 JSON）与 Extra["parallel_message"]（并行视图）保留原始数据，作为
// 约定键缺省时的兜底路径。实现委托 platform.QuotedImage，跨插件共享同一
// 套提取/类型判定语义。
func quotedImageFromSegments(segs []platform.Segment) (url, mimeType string) {
	return platform.QuotedImage(segs)
}

// buildGroupContext 组装同群最近 N 条消息（昵称: 内容，旧到新）。
// 默认只含入站消息；ContextGroupIncludeBot 开启时额外包含机器人的
// 出站回复（AI 与其他插件的回复），并以机器人自身名称标注
// （未注入名称时兜底"机器人"），同时窗口顶部加提示行说明这些
// 消息由本账号发出，避免 AI 误认为其他账号的发言。
// 跳过空内容与合成事件（AI 工具调用的内部命令），剥 @ 标记并单行截断。
//
// skipBotContents 为当前会话内 AI 已回复的内容集合——开启
// ContextGroupIncludeBot 时用于去重：机器人在会话历史中已说过的
// 内容不再重复注入群聊窗口（会话与窗口两侧存的是同一文本）。
func (p *Plugin) buildGroupContext(ctx *eventctx.Context, skipBotContents map[string]bool) string {
	return p.buildGroupContextN(ctx, skipBotContents, p.cfg.ContextGroupMessages)
}

// buildGroupContextN 组装同群最近 N 条消息（昵称: 内容，旧到新）。
// N 由调用方给定（预算编排时可动态缩减）。
func (p *Plugin) buildGroupContextN(ctx *eventctx.Context, skipBotContents map[string]bool, n int) string {
	if p.history == nil || n <= 0 {
		return ""
	}
	chat := ctx.GetChatInfo()
	if !chat.IsGroup {
		return ""
	}

	// 统一走新查询 API：热缓存 + SQLite 补齐，方向/出站状态在查询层过滤。
	// 仅入站消息时 Direction=Inbound；包含机器人回复时 Direction=Both 并
	// 排除 pending（未确认发送的出站不当作本账号发言）。
	opts := messagelog.QueryOptions{Direction: messagelog.DirectionInbound}
	if p.cfg.ContextGroupIncludeBot {
		opts.Direction = messagelog.DirectionBoth
		opts.ExcludePending = true
	}
	entries := p.history.QueryChat(chat.ID, n, opts)
	if len(entries) == 0 {
		return ""
	}

	botName := ctx.GetBotName()
	if botName == "" {
		botName = "机器人"
	}

	var b strings.Builder
	if p.cfg.ContextGroupIncludeBot {
		// 提示行：标注为机器人自身名称的消息由本账号发出
		// （AI 对话回复 + 其他插件命令输出），并非群内其他用户发言
		fmt.Fprintf(&b, "（标注「%s」的消息由本机器人账号发出——AI 对话回复或插件命令输出，均为你自己/本账号的发言，而非其他用户）\n", botName)
	}
	for _, e := range entries {
		if e.Content == "" || e.Platform == "synthetic" {
			continue
		}
		name := e.UserName
		if e.IsOutbound {
			// 只把"已确认发出"的机器人回复当作本账号发言：failed/unknown
			// 的失败回复不注入，避免模型误以为说过一句没发出去的话。
			// 旧数据 send_status 为空视为已发出（旧实现只记录成功的出站）。
			if e.SendStatus != "" && e.SendStatus != messagelog.SendStatusSent {
				continue
			}
			name = botName
			// 会话历史已包含 AI 自己的回复（assistant 轮次），
			// 内容一致的出站条目跳过，避免窗口与对话历史重复
			if skipBotContents[e.Content] {
				continue
			}
		}
		if name == "" {
			name = e.UserID
		}
		if name == "" {
			name = "未知"
		}
		text := strings.TrimSpace(stripMentionMarkup(e.Content))
		text = truncateRunes(text, 120)
		if text == "" {
			continue
		}
		// @ 提及标注：窗口历史不依赖当前消息的 IncludeMentionInfo，
		// 把记录中的 Mentions 结构化呈现（QQ 等平台的 @ 标记被剥除后
		// 文本无痕迹，补充标注避免 AI 漏看被提及对象）。跳过 @ 机器人自身
		// （窗口顶部已说明本账号发言），最多标注 3 个。
		mentionSuffix := ""
		if len(e.Mentions) > 0 {
			var names []string
			for _, m := range e.Mentions {
				if m.IsSelf {
					continue
				}
				if m.DisplayName != "" {
					names = append(names, m.DisplayName)
				} else if m.ID != "" {
					names = append(names, m.ID)
				}
				if len(names) >= 3 {
					break
				}
			}
			if len(names) > 0 {
				mentionSuffix = "（@" + strings.Join(names, "、@") + "）"
			}
		}
		// 回复内联：该条目是回复时，把被回复消息内容附在行尾
		// （仅追溯 1 层，保持窗口紧凑；目标通常在最近缓存内，开销可忽略）。
		suffix := ""
		if targetID := firstNonEmpty(e.ReplyToEventID, e.ReplyToMessageID); targetID != "" && targetID != e.EventID {
			if target, ok := p.history.QueryByEventID(chat.ID, targetID); ok && target.Content != "" {
				tName := target.UserName
				if target.IsOutbound {
					tName = botName
				}
				if tName == "" {
					tName = target.UserID
				}
				if tName == "" {
					tName = "未知"
				}
				tText := strings.TrimSpace(stripMentionMarkup(target.Content))
				tText = truncateRunes(tText, 60)
				if tText != "" {
					suffix = "（回复 " + tName + ": " + tText + "）"
				}
			}
		}
		fmt.Fprintf(&b, "%s: %s%s%s\n", name, text, mentionSuffix, suffix)
	}

	return strings.TrimRight(b.String(), "\n")
}

// firstNonEmpty 返回第一个非空字符串（回复目标 ID 解析的兜底顺序）。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
