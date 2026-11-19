// contextchannel.go — 原生上下文通道：把"框架事实"作为独立消息投递给模型。
//
// 为什么需要这一层：日期、时区、星期、平台等是框架掌握的事实，不是用户说的
// 话。把它们拼进 user 文本（改造前的做法）有两个代价——模型无法区分二者，
// 而且每一轮的 user 消息字节都不同，导致上一轮问答的尾部永久不可缓存。
//
// # 只用两家公开协议里存在的形状
//
// 本框架只声明兼容两种线格式（见 config.provider）：
//
//	OpenAI 兼容    messages 数组内的 role: "developer"（o1 之后的应用级指令角色）
//	Anthropic 兼容 messages 数组内的 role: "system"（mid-conversation system）
//
// 刻意**不使用**任何厂商私有角色（例如某些端点的 latest_reminder）：这类角色
// 只有该厂商接受，字段校验也由该厂商单方面定义，而部署方用的模型是未知的。
// 一旦把它写成"按 base_url 识别端点就发私有角色"，就等于在框架里内置了一张
// 必然腐化的厂商名单——新增一个兼容网关就要改代码，改错就是整轮对话 400。
//
// 于是形状选择只看 provider（用户配置里声明的线格式），不看模型名或域名；
// 端点不认该角色时由 [ProbeMemory] 逐级降级，最终落到"折进 user 文本"。
//
// 硬约束（官方文档，违规即 4xx）：
//   - Anthropic 中途 system 不能是首条、必须紧跟 user（或以 server tool result
//     结尾的 assistant）、不得夹在 tool_use 与 tool_result 之间，且只有部分
//     模型支持（Sonnet 5 不支持）。
//   - OpenAI 的 developer 消息可出现在任意位置；部分兼容网关不认该角色。
//
// 因此本层只提供"放到哪里"的语法，策略（何时投递、失败如何降级）在装配侧。
//
// 存储与编码分离：会话历史里保存 [RoleEnvContext]（provider 中立），适配器
// 在编码时把它翻译成自家形状。这样换端点不需要改写已落库的历史。

package protocol

import "strings"

// 原生上下文角色的内部取值（只在原生通道启用时出现）。
const (
	// RoleDeveloper OpenAI 的应用级指令角色（o1 之后取代 system）。
	// 部分兼容网关不认，由探测降级覆盖。
	RoleDeveloper Role = "developer"

	// RoleSystemMid Anthropic 的对话中途系统消息（内部名）。
	// Anthropic 适配器把它映射成数组内的 "system"；其余适配器不得原样透传。
	RoleSystemMid Role = "system_mid"

	// RoleEnvContext 框架事实的**存储角色**（provider 中立）。
	//
	// 会话历史里存这个角色、而不是某家的线格式角色：历史一旦落库就会长期
	// 存在，而换模型/换端点只需要在编码时重新映射。适配器负责把它翻译成
	// 自己的原生形状（OpenAI 兼容 → developer；Anthropic 兼容 → 数组内 system）。
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
	// NativeContextMidSystem Anthropic 的对话中途 system 消息。
	NativeContextMidSystem
)

// String 返回形状名（日志与指标标签用）。
func (n NativeContext) String() string {
	switch n {
	case NativeContextDeveloper:
		return "developer"
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
	case NativeContextMidSystem:
		return RoleSystemMid
	default:
		return ""
	}
}

// isContextChannelRole 判断角色是否属于上下文通道（裁剪逻辑用）。
func isContextChannelRole(r Role) bool {
	switch r {
	case RoleDeveloper, RoleSystemMid, RoleEnvContext:
		return true
	}
	return false
}

// IsContextChannelRole 判断角色是否属于上下文通道。
//
// 这类消息与 assistant/tool 一样是"结构性"消息：裁剪窗口时不能把它的后继
// user 消息裁掉而自己留下（中途 system 必须紧跟 user 或以 tool_result 结尾的
// assistant），否则请求会被 400 拒绝。
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

// NativeContextCandidates 返回该线格式下可尝试的原生形状（按优先级）。
//
// 只看 provider（= 用户声明的线格式 / 适配器），不看模型名、域名或厂商：
// 部署方用的模型是未知的，任何"按 base_url 片段识别厂商"的表都会腐化。
//
//	anthropic → mid_system（数组内 system）→ none
//	openai    → developer → mid_system（退化为普通 system）→ none
//
// OpenAI 那条多一个候选，因为"不认 developer、但认 system"是兼容网关最常见的
// 差异，而 system 放在 user 之后对 OpenAI 形状没有任何位置约束。Anthropic 没有
// 这个退路：官方要求中途 system 必须紧跟 user，developer 角色它根本不认。
//
// 返回的最后一个元素恒为 [NativeContextNone]：任何未知端点的兜底形态。
// 逐级失败会被 [ProbeMemory] 记住，第二次起直接跳到下一个候选。
func NativeContextCandidates(provider string) []NativeContext {
	if strings.EqualFold(strings.TrimSpace(provider), "anthropic") {
		return []NativeContext{NativeContextMidSystem, NativeContextNone}
	}
	return []NativeContext{NativeContextDeveloper, NativeContextMidSystem, NativeContextNone}
}

// EncodeEnvContext 把 [RoleEnvContext] 消息翻译成给定形状的线格式消息。
//
// 返回 ok=false 表示该形状无法表达环境事实（调用方应回退到 user 文本拼接）。
// 两种形状的差异只有两处：角色名，以及正文的渲染方式（都是可读的
// <environment> 指令块，Anthropic 与 OpenAI 对应用级指令的理解一致）。
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
// 存储载荷是 [EnvFacts] 的 JSON；解析失败时按"外部写入的裸文本"原样使用。
// 两种形状都不需要厂商特有的字段校验，因此这里几乎不会失败——这是刻意
// 设计的一部分：正文越"通用"，就不会因为某个端点的私有格式而整轮 400。
func RenderEnvContext(n NativeContext, content string) (string, bool) {
	if n == NativeContextNone {
		return "", false
	}
	if facts, ok := ParseEnvFacts(content); ok {
		return facts.Instruction(), true
	}
	text := strings.TrimSpace(content)
	if text == "" {
		return "", false
	}
	return text, true
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
