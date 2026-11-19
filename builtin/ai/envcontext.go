// envcontext.go — 环境事实通道的装配：决定"本轮把框架事实放在哪里"。
//
// 三种投放形态（由 context_channel 与端点能力共同决定）：
//
//	user_suffix  （默认）事实拼进本轮用户消息，随动态上下文一起发送，
//	             不写回会话历史——与改造前逐字节一致。
//	auto + 原生  事实作为独立消息写入会话历史（RoleEnvContext），逐轮随历史
//	             回放；请求构建时按端点编码成 reminder / developer / 中途 system。
//	auto + 降级  端点拒绝过原生角色（探测记录），编码时折进 user 文本，
//	             与 user_suffix 的表达等价。
//
// 关键不变量：**只有这里能产生 Persistent 上下文。**
// promptctx.Source（群聊窗口 / 长期记忆 / RAG）只能产 Ephemeral 条目，
// 因此不可信内容永远拿不到系统级权限——这是类型层面的保证，不靠运行时检查。
package ai

import (
	"slices"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/promptctx"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// envChannelEnabled 判断本轮是否使用原生上下文通道（把事实写进历史）。
//
// 只有 context_channel=auto、include_runtime_context=true 且端点未被标记为
// 不支持时才启用。默认（user_suffix）永远返回 false，因此升级本身不会改变
// 任何现有会话的请求字节。
//
// include_runtime_context=false 的语义是"完全不注入运行时上下文"（隐私开关），
// 因此这里必须一并遵守：否则关掉开关后，日期/平台/语言仍会以独立消息随历史
// 反复发送，配置失效却没有任何提示。
func (p *Plugin) envChannelEnabled() bool {
	if p.cfg == nil || p.cfg.ContextChannel != config.ContextChannelAuto {
		return false
	}
	if !p.cfg.IncludeRuntimeContext {
		return false
	}
	return p.endpointContextShape() != protocol.NativeContextNone
}

// endpointContextShape 返回端点自身支持的原生形状（只看 provider 声明与探测
// 结果，不看 context_channel 开关）。
func (p *Plugin) endpointContextShape() protocol.NativeContext {
	if p.cfg == nil {
		return protocol.NativeContextNone
	}
	return p.contextProbe.Choose(p.cfg.Provider, p.cfg.BaseURL)
}

// envChannelShape 返回**本轮请求编码**应使用的形状。
//
// 关闭原生通道时必须返回 None，而不是端点首选形状。历史是会跨版本、跨配置
// 长期存在的：一旦某次开过 auto，RoleEnvContext 就留在会话里；此后即使把
// context_channel 改回 user_suffix，若编码时仍按端点形状翻译，这些旧记录仍会
// 以 developer / 中途 system 发出去——配置关掉了却依然被端点拒绝。None 会让
// 旧记录折回 user 文本，与 user_suffix 的表达完全等价。
func (p *Plugin) envChannelShape() protocol.NativeContext {
	if p.cfg == nil || p.cfg.ContextChannel != config.ContextChannelAuto {
		return protocol.NativeContextNone
	}
	return p.endpointContextShape()
}

// buildEnvFacts 构造本轮的环境事实（取值一次，供历史与渲染共用）。
func (p *Plugin) buildEnvFacts(ctx *eventctx.Context) promptctx.EnvironmentFacts {
	return promptctx.FactsFromEvent(contextOptions(p.cfg), ctx)
}

// envContextMessage 把本轮环境事实转成写入会话历史的消息。
//
// 返回 nil 表示本轮不写，原因可能是：
//
//   - 未启用原生通道（默认 user_suffix）；
//   - 事实为空；
//   - 事实与历史里最近一条环境消息**逐字节相同**（同一天内日期/星期/时区/
//     语言都不变）。此时不重复追加：信息已经在历史里，模型随时能读到；
//     重复追加只会按回合线性占用窗口预算，并让每条 user 都被自己的 env
//     切成碎片。
//
// 这条去重是"环境事实不含时钟"的直接推论——EnvFacts 刻意只有日期粒度，
// 正是为了让同一份事实在多轮之间保持字节一致（见 protocol.EnvFacts）。
func (p *Plugin) envContextMessage(ctx *eventctx.Context, sess *session.Session) *protocol.Message {
	if !p.envChannelEnabled() {
		return nil
	}
	payload := p.buildEnvFacts(ctx).Payload()
	if payload.IsZero() || payload.Date == "" {
		return nil
	}
	encoded := protocol.MarshalEnvFacts(payload)
	if encoded == "" {
		return nil
	}
	if last, ok := lastEnvContextContent(sess); ok && last == encoded {
		return nil
	}
	m := runtime.EnvContextMessage(encoded)
	return &m
}

// lastEnvContextContent 返回会话里最近一条环境事实消息的 content。
func lastEnvContextContent(sess *session.Session) (string, bool) {
	if sess == nil {
		return "", false
	}
	sess.Lock()
	defer sess.Unlock()
	for _, m := range slices.Backward(sess.Messages) {
		if m.Role == protocol.RoleEnvContext {
			return m.Content, true
		}
	}
	return "", false
}

// markEnvChannelRejected 记录"端点拒绝原生上下文形状"，本轮起降级。
//
// 触发条件由 protocol.IsChannelRejection 判定（400/422 且错误体提到角色），
// 因此一次鉴权失败或限流不会让环境事实永久回退。
//
// 降级按"端点 + 形状"记忆：OpenAI 兼容端点上 developer 被拒后直接落到
// user 文本；Anthropic 兼容端点上中途 system 被拒同理。这样既不会因为一次
// 形状级拒绝而放弃其它端点，也不需要维护任何厂商/模型名单。
//
// 返回 true 表示确实发生了降级（调用方应据此重建请求重试一次）。
func (p *Plugin) tryDowngradeEnvChannel(err error) bool {
	if p.cfg == nil || !p.envChannelEnabled() || !protocol.IsChannelRejection(err) {
		return false
	}
	shape := p.envChannelShape()
	p.contextProbe.MarkBad(p.cfg.Provider, p.cfg.BaseURL, shape)
	recordContextChannel(shape.String(), "downgraded")
	logger.Warnf("[AI] endpoint rejected native context channel (%s), falling back to user text: %v",
		shape, err)
	return true
}

// rankChannelOutcome 记录一次原生通道使用结果（用于对照命中率评估收益）。
func (p *Plugin) recordChannelUse() {
	shape := p.envChannelShape()
	if shape == protocol.NativeContextNone {
		return
	}
	recordContextChannel(shape.String(), "ok")
}

// tryEnableReasoningEcho 处理"思考型端点要求历史工具轮回传推理"。
//
// 这类端点在请求携带 tools 时，要求历史里每个 assistant(tool_calls) 都带回
// 它当初产出的推理；旧版本落库的轮次没有存（那时还没有这个字段），只能补
// 空串。端点接受空串、拒绝缺字段，因此这是一次纯能力探测：400/422 在推理前
// 拒绝、不消耗 token，记入探测记忆后本进程内不再重复踩坑。
//
// 返回 true 表示应重建同一轮请求重试一次（不占用 max_depth 预算）。
func (p *Plugin) tryEnableReasoningEcho(err error) bool {
	if p.cfg == nil || !protocol.IsReasoningEchoRequired(err) {
		return false
	}
	if p.contextProbe.RequiresReasoningEcho(p.cfg.Provider, p.cfg.BaseURL) {
		return false // 已经打开，重试也不会改变请求字节
	}
	p.contextProbe.MarkReasoningEcho(p.cfg.Provider, p.cfg.BaseURL)
	logger.Warnf("[AI] endpoint requires reasoning_content on tool rounds; "+
		"echoing an empty value for history entries without one: %v", err)
	return true
}
