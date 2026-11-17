// item.go — 上下文条目（Item）与通道（Channel）：上下文的语义模型。
//
// 这一层把"注入什么"与"如何表达"分开：
//
//   - [Item] 描述一条上下文信息本身：属于哪一节、内容是什么、属于哪个通道；
//   - [Channel] 决定这条信息能否随历史回放（见下方注释）；
//   - [Snapshot] 是一次请求最终选中的条目集合（不可变）；
//   - [Render] 把条目拼成今天发出去的文本（唯一实现，逐字节稳定）。
//
// 为什么不给 Item 加 Provider 字段（Role / MessagePosition / 模板）：领域模型
// 一旦携带厂商协议，就会被 OpenAI / Anthropic / DeepSeek 的差异绑架，
// 换一家厂商就要改数据模型。表达方式属于 Renderer，不属于 Item。
//
// 信任边界由类型保证，而不是靠运行时字段检查：
//   - 只有 [EnvironmentFacts.PersistentItems] 能产 [Persistent] 项；
//   - [Source]（群聊窗口 / 长期记忆 / 相关历史）只能产 [Ephemeral] 项。
//
// 因此"把 RAG 命中提升成系统级指令"在类型层面**无法表达**——不需要一个
// Trust 枚举去守，也不会因为某处把字段赋错而悄悄扩大不可信内容的权限。

package promptctx

import (
	"slices"
	"strings"
)

// Channel 上下文条目的投递通道。
//
// 判据只有一条：**回放这条内容，会不会给模型新的指令权限？**
type Channel int

const (
	// Persistent 随会话历史持久化并逐轮原样回传。
	//
	// 只允许框架掌握的事实（时间、时区、星期、平台、会话类型）：回放它们
	// 不改变指令权限，而且能让上一轮的请求成为下一轮请求的字节前缀，
	// 这是提示词前缀缓存唯一能突破"上一轮问答尾部必然失效"的路径。
	Persistent Channel = iota

	// Ephemeral 只属于当前回合，永不写回会话。
	//
	// 群聊窗口、长期记忆、RAG 命中、执行计划、反思/校验指令都归此类：
	// 它们可能来自任意群成员或被模型抽取，回放等于把不可信内容永久钉成
	// 历史指令。
	Ephemeral
)

// String 返回通道名（日志与指标标签用）。
func (c Channel) String() string {
	switch c {
	case Persistent:
		return "persistent"
	case Ephemeral:
		return "ephemeral"
	default:
		return "unknown"
	}
}

// Item 一条上下文信息。
//
// 字段刻意不导出：外部只能通过构造函数拿到 Item，保证 [Persistent] 项的
// 来源被限制在 [EnvironmentFacts] 一处。读取用 Section / Channel / Body。
type Item struct {
	section string
	channel Channel
	body    string
}

// Section 返回节标题（渲染为 "===== <section> ====="）。
func (i Item) Section() string { return i.section }

// Channel 返回条目的投递通道。
func (i Item) Channel() Channel { return i.channel }

// Body 返回条目正文（不含节标题）。
func (i Item) Body() string { return i.body }

// Block 返回最终注入形态：节标题 + 换行 + 正文。
func (i Item) Block() string { return renderBlock(i.section, i.body) }

// renderBlock 渲染一节的注入形态（节标题 + 换行 + 正文）。
func renderBlock(section, body string) string {
	return "===== " + section + " =====\n" + body
}

// PersistentItems 把框架事实渲染成 Persistent 条目。
//
// 这是全仓库唯一能构造 Persistent 项的地方：其余来源（[Source]）只能产
// Ephemeral 项，因此"不可信内容被提升为系统级信息"没有可表达路径。
//
// opts 决定字段白名单（context_fields）；正文为空时返回 nil（不产生空节）。
func (f EnvironmentFacts) PersistentItems(opts ContextOptions) []Item {
	body := RenderEnvProse(opts, f)
	if body == "" {
		return nil
	}
	return []Item{{
		section: RuntimeContextSection,
		channel: Persistent,
		body:    body,
	}}
}

// RuntimeContextSection 运行时上下文的节标题（与改造前一致）。
const RuntimeContextSection = "运行时上下文"

// NewEphemeralItem 构造一条 Ephemeral 条目。
//
// 只供装配侧（builtin/ai）为不经过 [Source] 的临时节使用；诚实的来源应当
// 直接用 [Source]，由 Builder 统一产出条目。
func NewEphemeralItem(section, body string) Item {
	return Item{section: section, channel: Ephemeral, body: body}
}

// Snapshot 一次请求最终选中的上下文集合（不可变）。
//
// 生命周期：回合开始时构建一次，整个回合（含工具轮）复用同一份；回合结束
// 即丢弃。因此同一回合内的各次请求看到的外部上下文完全一致——模型不会在
// 工具轮之间"看到不同的世界"。
type Snapshot struct {
	// items 按注入顺序排列（Persistent 在前，Ephemeral 在后）。
	items []Item
	// Tokens 正文的估算 token 用量（口径同 [EstimateTokens]）。
	tokens int
	// fingerprint 对 items 的内容做稳定摘要，用于判断"本轮上下文与上一轮
	// 是否完全相同"（相同则无需重写历史、可直接复用前缀）。
	fingerprint string
}

// Items 返回按注入顺序排列的条目副本（调用方修改切片不影响快照）。
func (s Snapshot) Items() []Item { return slices.Clone(s.items) }

// Tokens 返回正文的估算 token 用量。
func (s Snapshot) Tokens() int { return s.tokens }

// Fingerprint 返回条目内容的稳定摘要。
func (s Snapshot) Fingerprint() string { return s.fingerprint }

// Empty 表示本轮没有任何上下文。
func (s Snapshot) Empty() bool { return len(s.items) == 0 }

// Persistent 返回 Persistent 条目（这些条目可以写回会话历史）。
func (s Snapshot) Persistent() []Item { return s.byChannel(Persistent) }

// Ephemeral 返回 Ephemeral 条目（这些条目只属于当前回合）。
func (s Snapshot) Ephemeral() []Item { return s.byChannel(Ephemeral) }

func (s Snapshot) byChannel(c Channel) []Item {
	var out []Item
	for _, it := range s.items {
		if it.channel == c {
			out = append(out, it)
		}
	}
	return out
}

// newSnapshot 组装快照并计算 token 用量与指纹（条目顺序即注入顺序）。
func newSnapshot(items []Item) Snapshot {
	if len(items) == 0 {
		return Snapshot{}
	}
	var b strings.Builder
	tokens := 0
	for _, it := range items {
		tokens += EstimateTokens(it.body)
		// 指纹覆盖"通道 + 节 + 正文"，用长度前缀避免边界歧义
		// （否则 ("ab","c") 与 ("a","bc") 会算出同一个摘要）。
		b.WriteString(it.channel.String())
		b.WriteByte(':')
		b.WriteString(itoa(len(it.section)))
		b.WriteByte(':')
		b.WriteString(it.section)
		b.WriteString(itoa(len(it.body)))
		b.WriteByte(':')
		b.WriteString(it.body)
		b.WriteByte('\n')
	}
	return Snapshot{
		items:       slices.Clone(items),
		tokens:      tokens,
		fingerprint: fingerprintHex(b.String()),
	}
}

// Render 把条目拼成注入文本（节之间以空行分隔，空条目跳过）。
//
// 这是动态上下文**唯一**的最终渲染实现：预算路径与非预算路径都经由它，
// 因此"改预算"不会顺手改掉发出去的字节。
func Render(items []Item) string {
	blocks := make([]string, 0, len(items))
	for _, it := range items {
		if it.body == "" {
			continue
		}
		blocks = append(blocks, it.Block())
	}
	return strings.Join(blocks, "\n\n")
}

// Render 把快照渲染成注入文本。
func (s Snapshot) Render() string { return Render(s.items) }

// RenderSuffix 只渲染 Ephemeral 条目——即"挂在本轮请求尾部"的那部分。
//
// Persistent 条目不出现在这里：它们已经作为独立消息写进会话历史（见
// docs/notes/33 的 append-only 形状），再从尾部渲染一遍会让模型看到两份
// 互相矛盾的同一事实，也会让尾部字节白白变长。
func (s Snapshot) RenderSuffix() string { return Render(s.Ephemeral()) }
