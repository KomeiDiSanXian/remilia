package engine

// index_command.go — 命令匹配器索引（BandFast）

import "github.com/KomeiDiSanXian/remilia/core/context"

// commandIndex 从 COW state 的 commandIndex 检索命令匹配器，
// 实现 O(1) 命令路由：消息以命令形式开头时直接命中候选，跳过全量遍历。
type commandIndex struct{}

// Band 返回 BandFast。
func (commandIndex) Band() RoutingBand { return BandFast }

// Candidates 消息内容为空或提取不到命令词时返回空候选；
// 命中命令词后返回 specific/generic 两路候选流。
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
	specific, generic := env.CommandCandidates(cmd, ctx.GetEventType())
	c.Add(specific)
	c.Add(generic)
	return c
}
