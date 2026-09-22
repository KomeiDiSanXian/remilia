// group.go — 群聊消息窗口：把同群最近的若干条入站消息注入上下文，
// 让 AI 感知多人对话。
//
// 只读消息历史（messagelog）与配置（context_group_messages /
// context_group_include_bot），不含任何插件状态。
package promptctx

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/textutil"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// BuildGroupWindowN 组装同群最近 N 条消息（昵称: 内容，旧到新）。
// N 由调用方给定：无预算时取 context_group_messages，预算编排时可动态缩减。
//
// skipBotContents 为当前会话内 AI 已回复的内容集合——开启
// ContextGroupIncludeBot 时用于去重：机器人在会话历史中已说过的
// 内容不再重复注入群聊窗口（会话与窗口两侧存的是同一文本）。
func BuildGroupWindowN(history *messagelog.Logger, cfg *config.Config, ctx *eventctx.Context, skipBotContents map[string]bool, n int) string {
	if history == nil || n <= 0 {
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
	if cfg.ContextGroupIncludeBot {
		opts.Direction = messagelog.DirectionBoth
		opts.ExcludePending = true
	}
	entries := history.QueryChat(chat.ID, n, opts)
	if len(entries) == 0 {
		return ""
	}

	botName := ctx.GetBotName()
	if botName == "" {
		botName = "机器人"
	}

	var b strings.Builder
	if cfg.ContextGroupIncludeBot {
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
		text := strings.TrimSpace(textutil.StripMentionMarkup(e.Content))
		text = textutil.TruncateRunes(text, 120)
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
		if targetID := textutil.FirstNonEmpty(e.ReplyToEventID, e.ReplyToMessageID); targetID != "" && targetID != e.EventID {
			if target, ok := history.QueryByEventID(chat.ID, targetID); ok && target.Content != "" {
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
				tText := strings.TrimSpace(textutil.StripMentionMarkup(target.Content))
				tText = textutil.TruncateRunes(tText, 60)
				if tText != "" {
					suffix = "（回复 " + tName + ": " + tText + "）"
				}
			}
		}
		fmt.Fprintf(&b, "%s: %s%s%s\n", name, text, mentionSuffix, suffix)
	}

	return strings.TrimRight(b.String(), "\n")
}
