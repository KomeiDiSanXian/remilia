// replygate.go — 群自主发言的回复决策（纯算法，装配侧只喂输入）。
//
// 背景：group_autonomous 开启后 matcher 是全量 catch-all，此前群里每条消息
// 都触发一次 LLM 对话——"每条都回"的 bot 是群聊体验大忌。这里的判定为
// 智能档（smart 策略）给出三类结论：
//
//	tierReply   — 内容/姿态过关，可以回复（是否真回由冷却与概率层在装配侧做）
//	tierIdle    — 信号不足以插话（装配侧大概率不回，仅保留概率抽签余地）
//	tierReject  — 明确不该回（纯社交/刷屏/嫌弃/超短无信息），任何情况下不回
//
// 信号强度由装配侧先行分类（本文输入 flag 承载）：
//
//	强信号：被 @、触发前缀、进行中的计划 → 直接 tierReply，不进任何过滤
//	中信号：回复 bot 且在"跟 bot 说话"、直接出现 bot 名字 → 过关但需抽签
//	弱信号：其余一切 → 全部判定依次执行
//
// 判定是纯函数：词表、阈值都在本文件常量区，可独立测试。
package decision

import (
	"strings"
	"unicode"
)

// ReplyTier 返回 tier* 常量之一。
type ReplyTier uint8

const (
	// TierReply 内容与姿态过关（装配侧再决定冷却/概率）。
	TierReply ReplyTier = iota
	// TierIdle 弱信号普通群聊：随概率抽签，大概率不回。
	TierIdle
	// TierReject 明确不回（社交/刷屏/嫌弃/无法判定内容）。
	TierReject
)

// minSubstantiveRunes 普通群消息的最短实质长度（低于此且无强信号不插话）。
// "哦""嗯""6" 这类单字/双字应答不值得动用 LLM；回复 bot 的对话不受此限。
const minSubstantiveRunes = 3

// ReplyInput 一次群消息的判定输入。
type ReplyInput struct {
	// Query 已清洗的消息正文（空串走 Reject）。
	Query string
	// HasContent 消息带可处理的附件（图片/语音已转多模态 part）。
	HasContent bool
	// Strong 强信号（被 @ / 触发词 / 计划进行中）。
	Strong bool
	// ReplyToBot 本条消息回复/引用了 bot 的一条消息。
	ReplyToBot bool
	// MentionsBot 正文直接出现 bot 名字/别名（Contains 命中，无需分词）。
	MentionsBot bool
	// Duplicate 本条与用户上一条消息完全相同（疑似刷屏）。
	Duplicate bool
}

// ShouldReply 返回分层判定。
func ShouldReply(in ReplyInput) ReplyTier {
	switch {
	case in.Strong:
		return TierReply
	case in.Duplicate:
		// 相同消息连发是刷屏或复读机玩梗，任何策略都不该凑热闹。
		return TierReject
	case dislikeBot(in.Query):
		// 对 bot 明确表达负反馈（无论是否回复了 bot）：继续插话只会火上浇油。
		return TierReject
	case isTrivialSocial(in.Query):
		// 纯社交应答/笑声/表情占位，不值得 LLM 插话。
		return TierReject
	case in.ReplyToBot && talksToBot(in.Query):
		// 在跟 bot 说话（称呼/疑问/足够长的内容）。
		return TierReply
	case in.ReplyToBot:
		// 引用 bot 消息但当众聊别的（比如"你们看它又乱说"）：鼓励内容
		// 存在时给抽签余地，正文无信息则拒绝。
		if substantiveLen(in.Query) >= minSubstantiveRunes {
			return TierIdle
		}
		return TierReject
	case in.MentionsBot && substantiveLen(in.Query) > 0:
		// 点名 bot（出现 bot 名/@ + 附件）：任何长度都视为对话请求。
		return TierReply
	case substantiveLen(in.Query) < minSubstantiveRunes && !in.HasContent:
		// 超短且无附件的弱信号消息。
		return TierReject
	default:
		return TierIdle
	}
}

// substantiveLen 去掉媒体占位符与空白/标点后的实质 rune 数。
func substantiveLen(query string) int {
	n := 0
	for _, r := range strings.TrimSpace(stripMediaTokens(query)) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		n++
	}
	return n
}

// talksToBot 判定"回复 bot 的消息"是否在跟 bot 说话：
// 以称呼/请求开头、出现疑问词或问号收尾，或内容足够长（>= 8 rune，
// 长文细节讨论默认按对话处理）。
//
// 称呼词用**前缀**匹配而不是包含匹配：中文口语通常先称呼再说话
// （"你说得对""帮我查下"），包含匹配会把"你们看这广告词"这类
// 对群友说话的消息误判成对话。
func talksToBot(query string) bool {
	norm := normalizeSocial(query)
	if norm == "" {
		return false
	}
	if substantiveLen(query) >= 8 {
		return true
	}
	for w := range addressPrefixes {
		if strings.HasPrefix(norm, w) {
			return true
		}
	}
	for w := range interrogativePhrases {
		if strings.Contains(norm, w) {
			return true
		}
	}
	// 问号（半/全角）出现在原文任何位置都算发问。
	if strings.ContainsAny(query, "?？") {
		return true
	}
	return false
}

// addressPrefixes 开头称呼/请求：以这些前缀开头的回复默认视为对 bot 说话。
// 刻意不用泛化的"你"前缀——"你们看这广告词"类对群友说话的句子会被误判；
// 用动词短语锚定称呼意图（"你说""你看""帮我"），漏网的仍走概率层。
var addressPrefixes = map[string]struct{}{
	"你说": {}, "你看": {}, "你觉": {}, "你发": {}, "你告诉": {},
	"帮我": {}, "拜托": {}, "请问": {}, "问我": {}, "机器人": {}, "ai": {}, "bot": {},
}

// interrogativePhrases 疑问词：引用 bot 消息时出现这些词，几乎必然在
// 向 bot 发问（该判定只在 ReplyToBot 通道使用，不影响普通群聊）。
var interrogativePhrases = map[string]struct{}{
	"什么": {}, "为什么": {}, "怎么": {}, "多少": {}, "哪": {}, "吗": {},
}

// dislikeBot 判定对 bot 的负反馈/嫌弃表达：继续插话只能激化。
// 用片段精确匹配：normalizeSocial 会剥掉标点拼接整串，导致"闭嘴，别刷了"
// 整串匹配失败——先按标点/空白切句，每段单独匹配词表。
func dislikeBot(query string) bool {
	for _, seg := range strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	}) {
		if _, ok := dislikePhrases[normalizeSocial(seg)]; ok {
			return true
		}
		norm := normalizeSocial(seg)
		if norm != "" && strings.HasPrefix(norm, "滚") {
			// "滚"类无法穷举（"滚滚滚"），前缀兜底；单独一个"滚轮"类
			// 专业词组以"滚"开头的误伤由概率层兜住（不会必回）。
			return true
		}
	}
	return false
}

// dislikePhrases 嫌弃/劝退 bot 的常见表达。
// 用整段精确匹配（调用方按标点切段），避免包含匹配把"禁止滚动""滚轮
// 操作"这类无关词误伤；"滚"类前缀兜底见 dislikeBot。漏网的短语仍会落
// 到概率层受冷却约束（不会必回）。
var dislikePhrases = map[string]struct{}{
	"闭嘴": {}, "住嘴": {}, "闭麦": {}, "别刷了": {}, "刷屏": {},
	"烦不烦": {}, "烦人": {}, "好烦": {}, "好吵": {}, "吵死了": {}, "话痨": {},
	"没人问": {}, "谁问你了": {}, "不要你说话": {}, "不需要你": {},
	"没让你说": {}, "谁要你说": {}, "shutup": {},
}

// junkLaughPhrases 更宽的"凑热闹"干扰片段：包含即命中（与 socialPhrases 的
// 全串匹配互补："233333 xxx" 这类混入正文的也能拦下）。
var junkLaughPhrases = []string{"666", "笑死", "绷不住", "太逗了"}

// extraSocialPhrases 群自主发言场景独有的"纯氛围"整串短语（补充
// needaction.go 的 socialPhrases——后者的语义是收缩工具集，词表更窄，
// 这里的语义是"完全不回"，需要兼顾点名问候）。
var extraSocialPhrases = map[string]struct{}{
	"大家好": {}, "大家晚上好": {}, "大家早上好": {}, "大家下午好": {},
	"群里的朋友们好": {}, "哈哈哈哈": {}, "哈哈哈哈哈": {},
}

// isTrivialSocial 判定整条消息是否纯社交凑热闹：
// 复用 NeedAction 的 socialPhrases 全串匹配 + 尾词剥离（"谢谢啦"
// "哈哈哈"），再叠加本处扩展词表与干扰片段包含匹配。
func isTrivialSocial(query string) bool {
	if isSocialOnly(query) {
		return true
	}
	norm := normalizeSocial(query)
	if norm == "" {
		return true
	}
	if _, ok := extraSocialPhrases[norm]; ok {
		return true
	}
	for _, w := range junkLaughPhrases {
		if strings.Contains(norm, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

// mediaPlaceholderTokens 各平台无语义的媒体占位符（与 runtime 的同名常量
// 独立维护：decision 是不依赖 runtime 的纯算法包）。
var mediaPlaceholderTokens = []string{"[图片]", "[表情]", "[动画表情]", "[语音]", "[视频]", "[文件]"}

// stripMediaTokens 去掉媒体占位符。
func stripMediaTokens(s string) string {
	for _, t := range mediaPlaceholderTokens {
		s = strings.ReplaceAll(s, t, "")
	}
	return s
}
