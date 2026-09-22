// Package textutil 收纳跨 owner 复用的纯文本工具。
//
// 这里是**叶子包**：不依赖任何其他 AI 子包、不持有状态、不读配置。
// 之所以独立成包而不是留在某个 owner 里，是因为它们确实有多个真实消费者：
// 回复上下文与群聊窗口（context）、相关历史检索（context/decision）、
// 工具选择日志（decision）、子命令预览与工具追踪（admin/runtime）。
// 放在任一 owner 里都会迫使其他 owner 反向依赖。
package textutil

import "regexp"

// TruncateRunes 按 rune 截断字符串，避免劈开多字节 UTF-8 字符。
// 超过 max 个字符时以省略号结尾。
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// FirstNonEmpty 返回第一个非空字符串（ID 解析的兜底顺序）。
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// mentionMarkupRegex 匹配各平台的 @ 提及标记：
//   - Discord: <@123> / <@!123> / <@&123>(角色) / <#123>(频道) / @everyone / @here
//   - onebot/QQ: @QQ号 / @all / @全体成员 / @所有人
var mentionMarkupRegex = regexp.MustCompile(`<@!?&?\d+>|<#\d+>|@\d+|@everyone|@here|@all|@全体成员|@所有人`)

// StripMentionMarkup 从消息文本中去除所有平台的 @ 提及标记。
func StripMentionMarkup(content string) string {
	return mentionMarkupRegex.ReplaceAllString(content, "")
}
