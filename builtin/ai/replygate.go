// replygate.go — 群自主发言接话策略（smart/judge 档的装配侧）。
//
// decision 包（decision/replygate.go）承担纯算法分层；本文件负责三件事：
//   - 解析生效配置：群策略（/ai group set reply，含群级概率）覆盖全局 config；
//   - 输入信号收集：全部复用 messagelog 的出站/入站记录（bot 刚说过话、
//     是否回复了 bot 的消息、是否复读刷屏），无额外状态，零手工埋点；
//   - 在 handler 的自主路径上执行 gate，smart/judge 拒绝时该消息不进入对话。
//
// 档位差异只在"弱信号普通消息"的裁决者：
//   - smart：概率抽签（零 LLM 成本）；
//   - judge：一次低成本的 LLM 插话判定（结合最近群聊上下文），失败回退
//     概率抽签。其余分层（强信号/垃圾过滤/冷静期）两档共享。
//
// 刻意不做全量 LLM 判定：本地规则能确定地拒绝的垃圾消息不值得为它调模型，
// 强信号也不值得问；判定只花在语义模糊的普通闲聊上。
//
// 冷静期数据源说明：出站记录由 messagelog 的 OutboundObserver 落库，发送
// 是异步的，刚回复完的瞬间记录可能还没落库——冷静期窗口以分钟计，落在
// 精度换简单性的区间内，可接受；messagelog 未启用时判定退化为不设冷却。

package ai

import (
	"math/rand"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/decision"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// replyGateState 接话策略的运行时状态（仅概率器需要互斥）。
type replyGateState struct {
	rngMu sync.Mutex
	rng   *rand.Rand
	// judge "judge" 档的 LLM 插话判定单飞闸（见 replyjudge.go）。
	judge *judgeGate
}

// initReplyGate 初始化接话策略状态。
func initReplyGate() *replyGateState {
	return &replyGateState{
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
		judge: &judgeGate{},
	}
}

// maybeRoll 锁内抽签：命中 chance 返回 true。
func (r *replyGateState) maybeRoll(chance float64) bool {
	if chance >= 1 {
		return true
	}
	if chance <= 0 {
		return false
	}
	r.rngMu.Lock()
	defer r.rngMu.Unlock()
	return r.rng.Float64() < chance
}

// groupReplySettings 生效的接话设置。
type groupReplySettings struct {
	mode     string // all / smart / judge
	chance   float64
	cooldown time.Duration
}

// effectiveGroupReply 生效接话设置：群策略（reply 字段，含群级概率）覆盖
// 全局 config。policy 未设置（""，测试直构造 Config 的常见形态）视为 all，
// 与默认值一致，保证 gate 不改变默认行为。
func (p *Plugin) effectiveGroupReply(ctx *eventctx.Context) groupReplySettings {
	policy := p.cfg.GroupReplyPolicy
	if policy == "" {
		policy = config.GroupReplyAll
	}
	chance := p.cfg.GroupReplyChance
	if gp := p.groupPolicyFor(ctx); gp != nil {
		if r := gp.EffectiveReplyPolicy(); r != "" {
			policy = r
		}
		if c, ok := gp.EffectiveReplyChance(); ok {
			chance = c
		}
	}
	return groupReplySettings{
		mode:     policy,
		chance:   chance,
		cooldown: p.cfg.GroupReplyCooldown,
	}
}

// mediumReplyChance 中信号（回复 bot 但不是直接对话 / 正文点名 bot）时的抽签概率：
// 保证一个明显高于基线的接话意愿（基线 +0.2），上限 1。基线=0 时仍有兜底，
// 避免"故意把概率调成 0 的安静模式"突然因弱引用而话多。
func mediumReplyChance(base float64) float64 {
	return min(base*2+0.2, 1)
}

// autonomousReplyDenied 判定群自主路径上是否"本轮不接话"（true=忽略消息）。
//
// 调用点在 handleAI 的自主消息路径（catch-all matcher 命中且消息未被显式
// 点名时）：强信号判定（@/触发词/计划进行中）由本函数自身承担，与调用方
// 的路径分支解耦。
//
// 判定次序刻意把本地拒绝与必须回应的信号放前面：硬拒绝先于强信号；
// 强信号和直接对话绕过冷静期与抽签，其余消息再经冷静期、分层与概率/judge。
func (p *Plugin) autonomousReplyDenied(ctx *eventctx.Context, content string) bool {
	set := p.effectiveGroupReply(ctx)
	if set.mode == config.GroupReplyAll {
		// all 档不过滤（现行行为，向后兼容）。
		return false
	}
	chat := ctx.GetChatInfo()
	if !chat.IsGroup || chat.ID == "" {
		// 接话策略只管群聊自主发言；私聊/频道语义不在范围。
		return false
	}

	// 强信号：会话存在进行中的计划、或本条 @ 了机器人
	//（QQ 群报文不带 mentions 数组，BotMentioned 借触发前缀/全文兜底）——必接话。
	sid := runtime.MakeSessionID(ctx.GetEventPlatform(), chat.ID, ctx.GetSenderInfo().ID)
	strong := runtime.BotMentioned(ctx)
	if sess := p.sm.Peek(sid); sess != nil {
		if plan := sess.PlanSnapshot(); plan != nil && plan.Active {
			strong = true
		}
	}

	in := decision.ReplyInput{
		Query:       content,
		HasContent:  hasBotRelevantMedia(ctx),
		Strong:      strong,
		ReplyToBot:  p.replyTargetsBot(ctx, chat.ID),
		MentionsBot: botMentionedIn(ctx, content),
		Duplicate:   p.recentGroupDuplicate(chat.ID, content),
	}

	// 明确拒绝信号优先于强信号；其余强信号和确实在与 bot 对话的引用直接通过，
	// 不进入冷静期或概率层。
	if decision.IsHardReject(in) {
		return true
	}
	if decision.MustReply(in) {
		return false
	}

	medium := in.ReplyToBot || in.MentionsBot
	// 冷静期：bot 刚发言过即刻闭麦。messagelog 不可用时退化为不设冷却。
	if !medium && p.botSpeechCooldownActive(chat.ID, set.cooldown) {
		return true
	}

	switch decision.ShouldReply(in) {
	case decision.TierReject:
		return true
	case decision.TierReply:
		// 未达到必须回应条件的点名信号按中信号概率抽签，不受冷静期。
		return !p.replyGate.maybeRoll(mediumReplyChance(set.chance))
	default:
		// 弱信号普通消息（模糊地带）：
		//   smart → 概率抽签；
		//   judge → LLM 结合最近群聊上下文终审，失败回退概率抽签。
		// 判定忙（单飞上限已占）：硬静默——刷屏模式下这正是期望行为，
		// 且不占用抽签额度。
		if set.mode == config.GroupReplyJudge {
			reply, ok, busy := p.runReplyJudge(ctx, content)
			if busy {
				return true
			}
			if ok {
				return !reply
			}
		}
		return !p.replyGate.maybeRoll(set.chance)
	}
}

// botSpeechCooldownActive 判定是否仍在 bot 的插话冷静期内。
func (p *Plugin) botSpeechCooldownActive(chatID string, cooldown time.Duration) bool {
	if cooldown <= 0 || p.history == nil {
		return false
	}
	last := p.history.QueryChat(chatID, 1, messagelog.QueryOptions{Direction: messagelog.DirectionOutbound})
	if len(last) == 0 {
		return false
	}
	return time.Since(last[0].Timestamp) < cooldown
}

// replyTargetsBot 判定本条消息是否回复/引用了 bot 的近期消息。
func (p *Plugin) replyTargetsBot(ctx *eventctx.Context, chatID string) bool {
	replyID := platform.GetReplyToID(ctx.GetPlatformEvent())
	if replyID == "" || p.history == nil {
		return false
	}
	for _, e := range p.history.QueryChat(chatID, 8, messagelog.QueryOptions{Direction: messagelog.DirectionOutbound}) {
		if e.PlatformMessageID == replyID {
			return true
		}
	}
	return false
}

// recentGroupDuplicate 判定本条是否与该群最近几条消息重复（复读/刷屏）。
// 只比较入站文本；messagelog 不可用时不判（漏网走到概率层，无害）。
func (p *Plugin) recentGroupDuplicate(chatID, content string) bool {
	if p.history == nil || strings.TrimSpace(content) == "" {
		return false
	}
	recent := p.history.QueryChat(chatID, 4, messagelog.QueryOptions{Direction: messagelog.DirectionInbound})
	for _, e := range recent {
		if e.Content == content {
			return true
		}
	}
	return false
}

// botMentionedIn 判定正文是否直接出现 bot 的显示名（包含匹配）。
// 名称越短误伤越多，因此要求 >=2 rune；ctx 未注入名称时不启用该信号。
func botMentionedIn(ctx *eventctx.Context, content string) bool {
	name := ctx.GetBotName()
	if name == "" || utf8.RuneCountInString(name) < 2 {
		return false
	}
	return strings.Contains(strings.ToLower(content), strings.ToLower(name))
}

// hasBotRelevantMedia 事件附件里是否有 bot 能处理的媒体（图片/语音）。
func hasBotRelevantMedia(ctx *eventctx.Context) bool {
	for _, a := range platform.Attachments(ctx.GetPlatformEvent()) {
		if a.Kind == platform.AttachmentKindImage || a.Kind == platform.AttachmentKindAudio {
			return true
		}
	}
	return false
}
