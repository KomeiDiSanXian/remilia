// runtime.go — 运行时上下文节：把当前事件的环境信息（时间、平台、机器人、
// 发送者、会话）渲染成一行行文本。
//
// 只依赖事件上下文与配置白名单，不读任何插件状态。
package promptctx

import (
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// BuildRuntimeContext 组装当前事件的运行时上下文信息。
//
// 注入的字段由配置 context_fields 白名单控制：
// 为空表示注入全部字段；非空时仅注入列出的字段。
func BuildRuntimeContext(cfg *config.Config, ctx *eventctx.Context) string {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()

	allow := make(map[string]bool, len(cfg.ContextFields))
	for _, f := range cfg.ContextFields {
		allow[f] = true
	}
	in := func(key string) bool {
		if len(cfg.ContextFields) == 0 {
			return true
		}
		return allow[key]
	}

	var b strings.Builder
	if in("time") {
		fmt.Fprintf(&b, "当前时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	}
	if in("platform") {
		fmt.Fprintf(&b, "平台: %s\n", ctx.GetEventPlatform())
	}
	if in("bot_id") {
		if botID := ctx.GetBotID(); botID != "" {
			fmt.Fprintf(&b, "机器人 ID: %s\n", botID)
		}
	}
	if in("bot_name") {
		if botName := ctx.GetBotName(); botName != "" {
			fmt.Fprintf(&b, "机器人名称: %s\n", botName)
		}
	}
	if in("user_name") {
		fmt.Fprintf(&b, "用户昵称: %s\n", sender.DisplayName)
	}
	if in("user_id") {
		fmt.Fprintf(&b, "用户 ID: %s\n", sender.ID)
	}
	if in("user_is_bot") {
		isBot := "否"
		if sender.IsBot {
			isBot = "是"
		}
		fmt.Fprintf(&b, "发送者是否为机器人: %s\n", isBot)
	}
	if in("chat_type") {
		switch {
		case chat.IsGroup:
			fmt.Fprintf(&b, "聊天类型: 群聊\n")
		case chat.IsDM:
			fmt.Fprintf(&b, "聊天类型: 频道私信\n")
		default:
			fmt.Fprintf(&b, "聊天类型: 私聊\n")
		}
	}
	if chat.IsGroup {
		if in("chat_id") && chat.ID != "" {
			fmt.Fprintf(&b, "群 ID: %s\n", chat.ID)
		}
		if in("chat_name") && chat.Name != "" {
			fmt.Fprintf(&b, "群名称: %s\n", chat.Name)
		}
		if in("parent_id") && chat.ParentID != "" {
			fmt.Fprintf(&b, "所属服务器 ID: %s\n", chat.ParentID)
		}
		if in("group_role") {
			fmt.Fprintf(&b, "发送者群角色: %s\n", GroupRoleName(sender.GroupRole))
		}
	} else {
		if in("chat_id") && chat.ID != "" {
			fmt.Fprintf(&b, "会话 ID: %s\n", chat.ID)
		}
		if in("chat_name") && chat.Name != "" {
			fmt.Fprintf(&b, "会话名称: %s\n", chat.Name)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// GroupRoleName 将平台群角色转换为可读文本。
func GroupRoleName(role platform.GroupRole) string {
	switch role {
	case platform.GroupRoleOwner:
		return "群主/所有者"
	case platform.GroupRoleAdmin:
		return "管理员"
	case platform.GroupRoleMember:
		return "普通成员"
	default:
		return "未知"
	}
}
