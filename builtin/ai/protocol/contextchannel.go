// contextchannel.go — 原生上下文通道：把"框架事实"作为独立消息投递给模型。
//
// 为什么需要这一层：日期、时区、星期、平台等是框架掌握的事实，不是用户说的
// 话。把它们拼进 user 文本（改造前的做法）有两个代价——模型无法区分二者，
// 而且每一轮的 user 消息字节都不同，导致上一轮问答的尾部永久不可缓存。
//
// 三家端点能接受的形状不同，且都没有"通用环境元信息协议"：
//
//	DeepSeek（OpenAI 兼容 / Anthropic 兼容）  role: "latest_reminder"
//	Anthropic 官方                            messages 数组内的 role: "system"
//	OpenAI 及兼容网关                          role: "developer"
//
// 硬约束（官方文档，违规即 4xx）：
//   - latest_reminder 后面必须紧跟 user 消息，且不得夹在 tool_use 与
//     tool_result 之间；content 必须是字符串。
//   - Anthropic 中途 system 不能是首条、必须紧跟 user（或以 server tool result
//     结尾的 assistant）、不得夹在 tool_use 与 tool_result 之间，且只有部分
//     模型支持（Sonnet 5 不支持）。
//
// 因此本层只提供"放到哪里"的语法，策略（何时投递、失败如何降级）在装配侧。
//
// 存储与编码分离：会话历史里保存 [RoleEnvContext]（provider 中立），适配器
// 在编码时把它翻译成自家形状。这样换端点不需要改写已落库的历史。

package protocol

import "strings"

// 原生上下文角色的内部取值（只在原生通道启用时出现）。
const (
	// RoleDeveloper OpenAI 的应用级指令角色。部分兼容网关不认。
	RoleDeveloper Role = "developer"

	// RoleLatestReminder DeepSeek 的环境元信息角色（OpenAI 兼容与
	// Anthropic 兼容端点都支持）。内容格式为
	// "<date>,<weekday>,<location>,<platform>,<language>"。
	RoleLatestReminder Role = "latest_reminder"

	// RoleSystemMid Anthropic 的对话中途系统消息（内部名）。
	// Anthropic 适配器把它映射成数组内的 "system"；其余适配器不得原样透传。
	RoleSystemMid Role = "system_mid"

	// RoleEnvContext 框架事实的**存储角色**（provider 中立）。
	//
	// 会话历史里存这个角色、而不是某家的线格式角色：历史一旦落库就会长期
	// 存在，而换模型/换端点只需要在编码时重新映射。适配器负责把它翻译成
	// 自己的原生形状（DeepSeek → latest_reminder；Anthropic → 数组内 system；
	// 其余 OpenAI 兼容 → developer）。
	//
	// 它是本仓库唯一允许随历史回放的上下文角色（见 docs/notes/33）。
	RoleEnvContext Role = "env_context"
)

// NativeContext 端点支持的原生上下文形状。
type NativeContext int

const (
	// NativeContextNone 只能把事实拼进 user 文本（兜底形态，永远可用）。
	NativeContextNone NativeContext = iota
	// NativeContextDeveloper OpenAI 的 developer 角色。
	NativeContextDeveloper
	// NativeContextReminder DeepSeek 的 latest_reminder 角色。
	NativeContextReminder
	// NativeContextMidSystem Anthropic 的对话中途 system 消息。
	NativeContextMidSystem
)

// String 返回形状名（日志与指标标签用）。
func (n NativeContext) String() string {
	switch n {
	case NativeContextDeveloper:
		return "developer"
	case NativeContextReminder:
		return "reminder"
	case NativeContextMidSystem:
		return "mid_system"
	default:
		return "none"
	}
}

// RoleOf 返回该形状在**线格式**上使用的角色。
//
// 注意与 [RoleEnvContext] 区分：后者是存储角色，编码时由适配器按自身能力
// 翻译成这里列出的线格式角色。
func (n NativeContext) RoleOf() Role {
	switch n {
	case NativeContextDeveloper:
		return RoleDeveloper
	case NativeContextReminder:
		return RoleLatestReminder
	case NativeContextMidSystem:
		return RoleSystemMid
	default:
		return ""
	}
}

// isContextChannelRole 判断角色是否属于上下文通道（裁剪逻辑用）。
func isContextChannelRole(r Role) bool {
	switch r {
	case RoleDeveloper, RoleLatestReminder, RoleSystemMid, RoleEnvContext:
		return true
	}
	return false
}

// IsContextChannelRole 判断角色是否属于上下文通道。
//
// 这类消息与 assistant/tool 一样是"结构性"消息：裁剪窗口时不能把它的后继
// user 消息裁掉而自己留下（reminder 后面必须紧跟 user；中途 system 必须紧跟
// user 或以 tool_result 结尾的 assistant），否则请求会被 400 拒绝。
func IsContextChannelRole(r Role) bool { return isContextChannelRole(r) }

// knownRole 判断角色是否在已知集合内。
//
// OpenAI 适配器用它做白名单：内部 Role 直接 string() 透传时，任何新角色都会
// 原样发给网关——不支持的网关会以 400 拒绝，而这本可以在本地挡住。
func knownRole(r Role) bool {
	switch r {
	case RoleSystem, RoleUser, RoleAssistant, RoleTool:
		return true
	}
	return isContextChannelRole(r)
}

// IsKnownRole 判断角色是否在已知集合内（适配器白名单用）。
func IsKnownRole(r Role) bool { return knownRole(r) }

// EncodeEnvContext 把 [RoleEnvContext] 消息翻译成给定形状的线格式消息。
//
// 返回 ok=false 表示该形状无法表达环境事实（调用方应回退到 user 文本拼接）。
// 三种形状的差异只有两处：角色名，以及正文的渲染方式（reminder 用单行 CSV，
// developer / 中途 system 用可读的 <environment> 指令块）。
func EncodeEnvContext(n NativeContext, content string) (Message, bool) {
	role := n.RoleOf()
	if role == "" {
		return Message{}, false
	}
	text, ok := RenderEnvContext(n, content)
	if !ok {
		return Message{}, false
	}
	return Message{Role: role, Content: text}, true
}

// RenderEnvContext 把环境事实的存储载荷渲染成该形状的正文。
//
// 存储载荷是 [EnvFacts] 的 JSON；解析失败时按"外部写入的裸文本"原样使用，
// 只对 reminder 做单行折叠（多行会被端点判为格式错误）。
func RenderEnvContext(n NativeContext, content string) (string, bool) {
	if n == NativeContextNone {
		return "", false
	}
	if facts, ok := ParseEnvFacts(content); ok {
		if n == NativeContextReminder {
			return facts.ReminderLine(), true
		}
		return facts.Instruction(), true
	}
	if n == NativeContextReminder {
		return singleLine(content), true
	}
	return strings.TrimSpace(content), true
}

// EnvContextProse 返回兜底形态（前置进 user 文本）的正文。
//
// 兜底内容会混在用户发言里，因此不加 <environment> 标签——那会让模型以为
// 存在结构化边界，而实际上这几行就是 user 消息的一部分。
func EnvContextProse(content string) string {
	if facts, ok := ParseEnvFacts(content); ok {
		return facts.Prose()
	}
	return strings.TrimSpace(content)
}

// singleLine 把多行文本压成单行（换行折成 "; "，去掉首尾空白）。
func singleLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "; ")
}
