// facts.go — 框架事实（Environment Facts）的结构化表示。
//
// 这一层把"框架掌握的、回放不会改变指令权限的事实"从事件上下文里抽出来，
// 成为可被多处消费的结构：渲染成一行行 Prose（今天的形状），或按 Provider
// 协议投递成独立的环境消息（见 protocol 的 native context channel）。
//
// 与 Prose 渲染的关系：
//
//   - [FactsFromEvent] 只做取值，不做格式决定；
//   - [RenderEnvProse] 是"今天发出去的那段文本"的唯一实现，逐字节稳定；
//   - 新增事实（星期/时区/语言）默认**不进入** Prose，必须由 context_fields
//     显式开启——这样默认配置的输出与改造前逐字节一致。
//
// 为什么要把这几项单独建模：日期、时区、语言、平台在语义上属于"运行环境"，
// 而不是"用户说的话"。把它们拼进 user 文本等于让模型无法区分二者；一旦将来
// 需要按各家的原生通道投递（OpenAI `developer` / Anthropic 数组内 `system`），
// 同一份事实要能换一种表达形式而不是重新拼字符串。

package promptctx

import (
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ConversationKind 当前会话的类型（环境事实之一）。
//
// 与 platform.ChatInfo 的 IsGroup/IsDM 两个布尔值分开建模：渲染时需要的是
// 一个确定的枚举（群聊/频道私聊/私聊），而判定顺序（IsGroup 先于 IsDM）是
// 语义的一部分，散落在各处会写出不一致的分支。
type ConversationKind int

const (
	// ConversationUnknown 未知类型（未使用：两个标志都为假时归为私聊，
	// 与改造前的渲染分支一致；保留该值供未来平台显式声明"非会话事件"）。
	ConversationUnknown ConversationKind = iota
	// ConversationPrivate 私聊。
	ConversationPrivate
	// ConversationGroup 群聊。
	ConversationGroup
	// ConversationChannelDM 频道私信。
	ConversationChannelDM
)

// String 返回面向模型的中文名称。
func (k ConversationKind) String() string {
	switch k {
	case ConversationPrivate:
		return "私聊"
	case ConversationGroup:
		return "群聊"
	case ConversationChannelDM:
		return "频道私信"
	default:
		return "未知"
	}
}

// EnvironmentFacts 一次事件所对应的框架事实快照。
//
// 所有字段都是取值结果，不含格式：同样的 Facts 可以渲染成 Prose、CSV
// （DeepSeek reminder）或独立消息块（developer / 中途 system）。
//
// 这个结构是本轮"可回放上下文"的唯一来源：只有它能构造 Persistent 通道的
// 内容项（见 [EnvironmentFacts.Items]），因此"RAG 结果被提升成 system 指令"
// 这类隐患在类型层面无法表达。
type EnvironmentFacts struct {
	// Now 事件发生时刻（渲染前即固定，避免同一份事实内的日期与星期错位）。
	Now time.Time
	// Timezone 时区名称（IANA，如 "Asia/Shanghai"）；空表示使用 Now 的位置。
	Timezone string
	// Language 用户语言的自称（如 "中文"）；空表示不注入。
	Language string
	// Location 用户所在地；平台通常拿不到，空表示未知（不猜）。
	Location string

	// Platform 事件平台标识（如 "qq"、"discord"）。
	Platform string
	// BotID / BotName 机器人身份。
	BotID   string
	BotName string
	// Sender 发送者信息。
	Sender platform.UserInfo
	// Chat 会话信息。
	Chat platform.ChatInfo
	// Conversation 会话类型的判定结果。
	Conversation ConversationKind
}

// ConversationKindOf 按"群聊 → 频道私信 → 私聊"的优先级判定会话类型。
//
// 优先级即语义：部分适配器会同时把 IsGroup 与 IsDM 置真（频道场景），
// 群聊优先与改造前的渲染顺序保持一致。两个标志都为假时归为私聊——这与改造
// 前的 `default` 分支一致，渲染出的 "聊天类型" 因此逐字节不变。
func ConversationKindOf(chat platform.ChatInfo) ConversationKind {
	switch {
	case chat.IsGroup:
		return ConversationGroup
	case chat.IsDM:
		return ConversationChannelDM
	default:
		return ConversationPrivate
	}
}

// factsClock 取当前时间（测试可替换，避免用例与真实时钟耦合）。
var factsClock = time.Now

// FactsFromEvent 从事件上下文取值，构造框架事实快照。
//
// opts 只用于取时区与语言（它们是运维配置，不是事件自带的）；其余字段
// 一律来自事件本身。事件为 nil 时返回零值 Facts（渲染为空）。
func FactsFromEvent(opts ContextOptions, ctx *eventctx.Context) EnvironmentFacts {
	if ctx == nil {
		return EnvironmentFacts{}
	}
	now := factsClock()
	d := EnvironmentFacts{
		Now:      now,
		Timezone: opts.Timezone,
		Language: opts.Language,
		Location: opts.Location,
		Platform: ctx.GetEventPlatform(),
		BotID:    ctx.GetBotID(),
		BotName:  ctx.GetBotName(),
		Sender:   ctx.GetSenderInfo(),
		Chat:     ctx.GetChatInfo(),
	}
	d.Conversation = ConversationKindOf(d.Chat)
	if d.Timezone != "" {
		if loc, err := time.LoadLocation(d.Timezone); err == nil {
			d.Now = now.In(loc)
		}
	}
	return d
}

// In 返回事实所处的时区（Timezone 为空时用 Now 自带的位置）。
func (f EnvironmentFacts) In() *time.Location {
	if f.Now.Location() != nil {
		return f.Now.Location()
	}
	return time.Local
}

// Weekday 返回星期几的中文名称（"星期一"…"星期日"）。
//
// 用中文短名而不是 time.Weekday.String()：后者是英文，而 DeepSeek 的
// reminder 协议要求"用 language 对应的语言书写"，中文场景下英文回溯会让
// 模型在中文回复里混进英文星期。
func (f EnvironmentFacts) Weekday() string {
	switch f.Now.Weekday() {
	case time.Monday:
		return "星期一"
	case time.Tuesday:
		return "星期二"
	case time.Wednesday:
		return "星期三"
	case time.Thursday:
		return "星期四"
	case time.Friday:
		return "星期五"
	case time.Saturday:
		return "星期六"
	case time.Sunday:
		return "星期日"
	}
	return "星期一"
}

// Date 返回日期（YYYY-MM-DD）。
func (f EnvironmentFacts) Date() string { return f.Now.Format("2006-01-02") }

// Clock 返回时间（HH:MM:SS）。
func (f EnvironmentFacts) Clock() string { return f.Now.Format("15:04:05") }

// Timestamp 返回"日期 时间"（改造前 Prose 的 time 字段口径，逐字节兼容）。
func (f EnvironmentFacts) Timestamp() string { return f.Now.Format("2006-01-02 15:04:05") }

// Offset 返回 UTC 偏移（如 "+08:00"），供需要显式时区的渲染使用。
func (f EnvironmentFacts) Offset() string { return f.Now.Format("-07:00") }

// optInRuntimeFields 默认不注入的字段：它们是对改造前输出的新增项，
// 只有 context_fields 显式列出时才出现。
//
// 为什么必须默认关闭：运行时上下文每轮都随请求发送，一旦默认多出几行，
// 升级本身就会让所有在线会话的提示词前缀整体失效（缓存按字节比对）。
// 新增事实的价值在"回答时间/地点类问题更准"，不值得用一次全量缓存击穿换取。
var optInRuntimeFields = map[string]bool{
	"weekday":  true,
	"timezone": true,
	"location": true,
	"language": true,
}

// runtimeFieldsOf 把 context_fields 白名单变成判定函数。
//
// 空白名单表示"注入全部字段"，但 [optInRuntimeFields] 里的新字段仍需显式
// 列出——这是"默认输出与改造前逐字节一致"的实现方式。
func runtimeFieldsOf(opts ContextOptions) func(string) bool {
	if len(opts.RuntimeFields) == 0 {
		return func(key string) bool { return !optInRuntimeFields[key] }
	}
	allow := make(map[string]bool, len(opts.RuntimeFields))
	for _, f := range opts.RuntimeFields {
		allow[f] = true
	}
	return func(key string) bool { return allow[key] }
}

// RenderEnvProse 把框架事实渲染成运行时上下文那段文本。
//
// 这是改造前 BuildRuntimeContext 的逐字节等价实现（字段顺序、分隔符、
// 结尾去空行都保持一致）。新增的星期/时区/语言只有被 context_fields
// 显式列出时才会出现，因此默认配置的输出不变。
func RenderEnvProse(opts ContextOptions, f EnvironmentFacts) string {
	in := runtimeFieldsOf(opts)

	var b strings.Builder
	if in("time") {
		fmt.Fprintf(&b, "当前时间: %s\n", f.Timestamp())
	}
	if in("weekday") {
		fmt.Fprintf(&b, "星期: %s\n", f.Weekday())
	}
	if in("timezone") {
		fmt.Fprintf(&b, "时区: %s (UTC%s)\n", f.In().String(), f.Offset())
	}
	if in("location") && f.Location != "" {
		fmt.Fprintf(&b, "所在地: %s\n", f.Location)
	}
	if in("language") && f.Language != "" {
		fmt.Fprintf(&b, "用户语言: %s\n", f.Language)
	}
	if in("platform") {
		fmt.Fprintf(&b, "平台: %s\n", f.Platform)
	}
	if in("bot_id") && f.BotID != "" {
		fmt.Fprintf(&b, "机器人 ID: %s\n", f.BotID)
	}
	if in("bot_name") && f.BotName != "" {
		fmt.Fprintf(&b, "机器人名称: %s\n", f.BotName)
	}
	if in("user_name") {
		fmt.Fprintf(&b, "用户昵称: %s\n", f.Sender.DisplayName)
	}
	if in("user_id") {
		fmt.Fprintf(&b, "用户 ID: %s\n", f.Sender.ID)
	}
	if in("user_is_bot") {
		isBot := "否"
		if f.Sender.IsBot {
			isBot = "是"
		}
		fmt.Fprintf(&b, "发送者是否为机器人: %s\n", isBot)
	}
	if in("chat_type") {
		fmt.Fprintf(&b, "聊天类型: %s\n", f.Conversation)
	}
	if f.Chat.IsGroup {
		if in("chat_id") && f.Chat.ID != "" {
			fmt.Fprintf(&b, "群 ID: %s\n", f.Chat.ID)
		}
		if in("chat_name") && f.Chat.Name != "" {
			fmt.Fprintf(&b, "群名称: %s\n", f.Chat.Name)
		}
		if in("parent_id") && f.Chat.ParentID != "" {
			fmt.Fprintf(&b, "所属服务器 ID: %s\n", f.Chat.ParentID)
		}
		if in("group_role") {
			fmt.Fprintf(&b, "发送者群角色: %s\n", GroupRoleName(f.Sender.GroupRole))
		}
	} else {
		if in("chat_id") && f.Chat.ID != "" {
			fmt.Fprintf(&b, "会话 ID: %s\n", f.Chat.ID)
		}
		if in("chat_name") && f.Chat.Name != "" {
			fmt.Fprintf(&b, "会话名称: %s\n", f.Chat.Name)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// Payload 把框架事实转换成可持久化的载荷（provider 中立）。
//
// 这是 Facts → protocol.EnvFacts 的唯一映射点：渲染方式（指令块 / 散文）
// 由 protocol 侧按形状决定，这里只做取值搬运。
//
// Language 与 Weekday 都保持配置语言的原样取值：两种原生形状（OpenAI
// developer / Anthropic 中途 system）的正文是普通文本，没有字段级校验，
// 因此不需要把它"翻译成某个厂商的原生语言名"。只有当运维显式配置了
// context_fields: [language] 时，语言才会出现在运行时上下文里。
func (f EnvironmentFacts) Payload() protocol.EnvFacts {
	return protocol.EnvFacts{
		Date:     f.Date(),
		Weekday:  f.Weekday(),
		TimeZone: f.In().String(),
		Offset:   f.Offset(),
		Location: f.Location,
		Platform: f.Platform,
		Language: f.Language,
	}
}
