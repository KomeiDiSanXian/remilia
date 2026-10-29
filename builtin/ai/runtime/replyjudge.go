// replyjudge.go — 群自主发言"值不值得插话"的 LLM 判定结果解析（纯函数）。
//
// 装配侧把最近群聊上下文与当前消息交给一个低成本单轮调用，本文件负责
// 判定 prompt 与输出解析：判为 reply 才进入对话流程，silent 放弃插话。
// 解析失败/不完整输入由调用方按概率抽签兜底，不让判定故障拖垮回复链路。

package runtime

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ReplyJudgeSystemPrompt 插话决策器的系统提示词。
const ReplyJudgeSystemPrompt = `你是一个群里机器人账号的"插话决策器"。根据最近群聊上下文判断：机器人此刻**主动回应**用户的新消息是否得体且有增量价值。

判为 reply（值得回话）：
- 消息在语义上需要机器人帮忙（提问/求算/请求资料/查证等）
- 或与机器人之前的发言直接相关（追问细节、指正、延续话题）
- 或明确点名/称呼了机器人
- 或有一条 bot 能回答而群里没人能回答的信息需求

判为 silent（保持安静，宁可保守）：
- 群友之间互相聊天，无需 bot 加入
- 闲聊寒暄、口嗨、玩笑接龙、情绪表达
- 与机器人无关的日常消息（吐槽/分享/抱怨）
- 机器人插进这些对话会显得突兀、打扰或刷存在感

严格只输出一行 JSON：
{"verdict": "reply" 或 "silent", "reason": "一句话理由（中文）"}`

// BuildReplyJudgeMessage 组装判定的用户内容：最近群聊窗口 + 本条新消息。
func BuildReplyJudgeMessage(window string, inbound string, senderName string) string {
	var b strings.Builder
	b.WriteString("===== 最近群聊 =====\n")
	b.WriteString(strings.TrimSpace(window))
	b.WriteString("\n===== 群聊结束 =====\n\n")
	if senderName != "" {
		fmt.Fprintf(&b, "【新消息】%s：%s", senderName, inbound)
	} else {
		fmt.Fprintf(&b, "【新消息】%s", inbound)
	}
	return b.String()
}

// ReplyVerdict 判定结果。
type ReplyVerdict struct {
	// Reply true = 值得插话。
	Reply bool
	// Reason 判定理由（判定不通过时给日志/调试用）。
	Reason string
}

// ParseReplyVerdict 鲁棒解析判定输出：优先 JSON（verdict 字段），
// 关键词兜底（reply/respond/回复 / silent/不回/静默）。无法判定返回
// (zero, false)，调用方应回退到概率抽签。
func ParseReplyVerdict(text string) (ReplyVerdict, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return ReplyVerdict{}, false
	}
	if start := strings.IndexByte(text, '{'); start >= 0 {
		if end := strings.LastIndexByte(text, '}'); end > start {
			var parsed struct {
				Verdict string `json:"verdict"`
				Reason  string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(text[start:end+1]), &parsed); err == nil {
				switch strings.ToLower(strings.TrimSpace(parsed.Verdict)) {
				case "reply", "respond", "yes", "回复", "回应":
					return ReplyVerdict{Reply: true, Reason: strings.TrimSpace(parsed.Reason)}, true
				case "silent", "skip", "no", "静默", "不回":
					return ReplyVerdict{Reply: false, Reason: strings.TrimSpace(parsed.Reason)}, true
				}
			}
		}
	}
	// 关键词兜底。negative 词先行：中文"不回复"包含"回复"，positive 词
	// 先扫会把否定表达误判为回复。
	lower := strings.ToLower(text)
	switch {
	case containsWord(lower, "silent"), containsWord(lower, "skip"),
		strings.Contains(text, "不回"), strings.Contains(text, "静默"),
		strings.Contains(text, "不必回"), strings.Contains(text, "无需回复"):
		return ReplyVerdict{Reply: false, Reason: text}, true
	case containsWord(lower, "reply"), containsWord(lower, "respond"),
		strings.Contains(text, "回复"), strings.Contains(text, "回应"):
		return ReplyVerdict{Reply: true, Reason: text}, true
	default:
		// 无法判定：按不可用处理，回退概率抽签（不因解析失败乱开口）。
		return ReplyVerdict{}, false
	}
}

// containsWord 词级包含（"replies"/"replying" 与 "reply" 同义；
// 避免 "nonsilent" 这类否定前缀误判）。
func containsWord(haystack, word string) bool {
	idx := strings.Index(haystack, word)
	if idx < 0 {
		return false
	}
	// 要求命中位置邻近无字母邻接："reply" 命中 "replies" 允许（尾部字母
	// 追加），但 "unreply"（前字母邻接）不算。
	prevOK := idx == 0 || !isAlphaByte(haystack[idx-1])
	return prevOK
}

// isAlphaByte 判断字节的 ASCII 字母属性。
func isAlphaByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// BuildReplyJudgeWindow 把最近群聊记录渲染为判定上下文窗口。
// 记录按时间升序输出（调用方传入的顺序即"由新到旧"，这里统一倒序拼接：
// 新消息贴近 prompt 末尾）。单条超长消息按 rune 截断。
func BuildReplyJudgeWindow(entries []ChatWindowEntry, maxRunes int) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for _, e := range slices.Backward(entries) {

		if e.Bot {
			b.WriteString("机器人: ")
		} else {
			b.WriteString(e.SenderName + ": ")
		}
		b.WriteString(e.Content)
		b.WriteString("\n")
	}
	out := strings.TrimRight(b.String(), "\n")
	if maxRunes > 0 {
		runes := []rune(out)
		if len(runes) > maxRunes {
			out = string(runes[:maxRunes]) + "…（早前对话已截断）"
		}
	}
	return out
}

// ChatWindowEntry 判定窗口的单条群消息（装配侧从 messagelog 展平）。
type ChatWindowEntry struct {
	SenderName string
	Content    string
	// Bot true = 本机器人账号的出站消息。
	Bot bool
}
