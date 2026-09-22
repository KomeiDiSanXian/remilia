// memory.go — 长期记忆节：把与当前查询最相关的长期事实注入上下文。
//
// 记忆的存储、去重合并与打分排序属于记忆存储自身（builtin/ai 的 memoryStore），
// 本包只声明注入所需的最小端口 [MemoryReader]，由装配侧注入。
// 作用域的存储键格式由装配侧决定，因此以参数传入。
package promptctx

import (
	"context"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// MemoryReader 长期事实记忆的检索端口。
//
// 只暴露注入需要的一个操作：按查询取前 N 条事实文本。
// 打分、去重与淘汰都在实现侧完成，本包不再二次排序；
// "是否启用"由调用方（动态上下文节的参与条件）判断，不进端口。
type MemoryReader interface {
	// RetrieveTexts 返回指定作用域下与 query 最相关的前 limit 条事实文本。
	RetrieveTexts(ctx context.Context, scope, query string, limit int) []string
}

// BotReplyContents 收集会话中 AI 已回复的内容集合。
//
// 开启 context_group_include_bot 时用于群聊窗口去重：机器人在会话历史中
// 已说过的内容不再重复注入窗口（两侧存的是同一文本）。session 为 nil 时返回 nil。
func BotReplyContents(cfg *config.Config, sess *session.Session) map[string]bool {
	if !cfg.ContextGroupIncludeBot || sess == nil {
		return nil
	}
	skip := make(map[string]bool)
	for _, m := range sess.SnapshotMessages() {
		if m.Role == protocol.RoleAssistant && m.Content != "" {
			skip[m.Content] = true
		}
	}
	return skip
}

// BuildMemoryContext 检索并格式化长期记忆注入文本。
// 群聊注入"用户相关记忆 + 群相关记忆"，私聊仅用户记忆；
// 按用户最近消息关键词对事实打分取 Top-N。
//
// userScope / groupScope 为两个作用域的存储键；query 为本轮检索用的用户消息文本。
func BuildMemoryContext(mem MemoryReader, ctx *eventctx.Context, query, userScope, groupScope string, limit int) string {
	sender := ctx.GetSenderInfo()
	if sender.ID == "" {
		return ""
	}
	if query == "" {
		return ""
	}
	if limit <= 0 {
		limit = 8
	}

	var facts []string
	if mem != nil {
		if hits := mem.RetrieveTexts(ctx.Context(), userScope, query, limit); len(hits) > 0 {
			facts = append(facts, "【用户相关记忆】")
			for _, text := range hits {
				facts = append(facts, "- "+text)
			}
		}
		if chat := ctx.GetChatInfo(); chat.IsGroup && chat.ID != "" {
			if hits := mem.RetrieveTexts(ctx.Context(), groupScope, query, limit); len(hits) > 0 {
				facts = append(facts, "【群相关记忆】")
				for _, text := range hits {
					facts = append(facts, "- "+text)
				}
			}
		}
	}
	if len(facts) == 0 {
		return ""
	}
	return "以下为长期对话中记住的关于用户/本群的事实，可据此提供个性化服务（如有冲突以用户当前说法为准）：\n" +
		strings.Join(facts, "\n")
}
