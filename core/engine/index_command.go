package engine

// index_command.go — 命令匹配器索引（BandFast）

import "github.com/KomeiDiSanXian/remilia/core/context"

// commandIndex 从 COW state 的 commandIndex 检索命令匹配器，
// 实现 O(1) 命令路由：消息以命令形式开头时直接命中候选，跳过全量遍历。
//
// 语义：以消息的**首个空白分隔 token** 为键做精确查找。
// 这比 [context.OnCommand] 规则（前缀匹配）更严格，是有意为之——
// O(1) 分发的正确性依赖「键 = 完整命令词」，否则 /help 会连带吃掉
// /helper 的流量。需要前缀/关键词语义时请改用 [context.OnPrefix] /
// [context.OnKeyword] 或 Engine.On 注册普通规则（它们走常规索引）。
type commandIndex struct{}

// Band 返回 BandFast。
func (commandIndex) Band() RoutingBand { return BandFast }

// Candidates 消息内容为空或提取不到命令词时返回空候选；
// 命中命令词后返回 specific/generic 两路候选流。
//
// 事件类型为空时只返回一路：此时 specific 与 generic 是同一个桶，
// 叠加会让同一个命令 matcher 在候选流中出现两次。
func (commandIndex) Candidates(env MatcherEnv, ctx *context.Context) MatcherCandidates {
	var c MatcherCandidates
	content := ctx.GetMessageContent()
	if content == "" {
		return c
	}
	cmd := extractCommand(content)
	if cmd == "" {
		return c
	}

	eventType := ctx.GetEventType()
	specific, generic := env.CommandCandidates(cmd, eventType)
	c.Add(specific)
	if eventType != "" {
		c.Add(generic)
	}
	return c
}
