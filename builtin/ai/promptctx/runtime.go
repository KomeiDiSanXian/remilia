// runtime.go — 运行时上下文节：把当前事件的环境信息（时间、平台、机器人、
// 发送者、会话）渲染成一行行文本。
//
// 只依赖事件上下文与配置白名单，不读任何插件状态。
//
// 取值与渲染已拆开：取值见 facts.go 的 [FactsFromEvent]，渲染见
// [RenderEnvProse]。本文件只保留对外入口与群角色名映射。

package promptctx

import (
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// BuildRuntimeContext 组装当前事件的运行时上下文信息。
//
// 注入的字段由配置 context_fields 白名单控制：
// 为空表示注入全部字段；非空时仅注入列出的字段。
//
// 等价于 [RenderEnvProse] + [FactsFromEvent]：取值与格式分离之后，这条
// 路径的默认输出与改造前逐字节一致（新增的 weekday/timezone/location/language
// 需要被 context_fields 显式列出才会出现）。
func BuildRuntimeContext(opts ContextOptions, ctx *eventctx.Context) string {
	return RenderEnvProse(opts, FactsFromEvent(opts, ctx))
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

// unknownSpeaker 发言人名称的兜底文本：既无昵称也无 ID 时使用。
const unknownSpeaker = "未知"

// displayName 把一个发言人的显示名解析成可注入提示词的形式。
//
// 三级回落（昵称 → 原始 ID → "未知"）在群聊窗口、相关历史、回复链里各写
// 过一遍。这不只是重复：三处必须给出同一个名字，否则同一个人在窗口里叫
// "张三"、在回复链里叫 "12345"，模型会当成两个人。
func displayName(name, userID string) string {
	if name != "" {
		return name
	}
	if userID != "" {
		return userID
	}
	return unknownSpeaker
}
