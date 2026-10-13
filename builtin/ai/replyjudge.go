// Package ai replyjudge.go — "judge" 档的 LLM 插话判定装配。
//
// decision.ShouldReply 留下三类结论：确定性拒绝（垃圾/复读/嫌弃）、
// 强中信号直通，其余"值得商榷的普通群聊"由本文件换成一次低成本的
// LLM 判定：结合最近群聊上下文，判断"此刻主动插话是否得体且有增量价值"。
//
// 成本护栏：
//   - 拒绝类/强信号不产生判定调用；
//   - 单飞：同一时刻至多一个判定在跑，忙时消息直接保持安静（刷屏模式下
//     这恰好是期望行为）；
//   - 判定失败/超时回退到概率抽签，判定故障不能拖垮回复链路。
package ai

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// replyJudgeWindowEntries 判定窗口大小（最近群聊条数）。
const replyJudgeWindowEntries = 8

// replyJudgeWindowRunes 判定窗口文本的总长度上限（rune）。
const replyJudgeWindowRunes = 1200

// replyJudgeTimeout 单次判定的超时：判定落在用户可感知的时延内
// （不回时静默，回时希望尽快跟上节奏）。
const replyJudgeTimeout = 6 * time.Second

// runReplyJudge 执行一次 LLM 插话判定。
// 返回 (值得回话, 判定成功, 判定槽忙碌)；出错或输出不可解析时调用方回退概率抽签，
// 判定槽忙碌时则保持静默。
func (p *Plugin) runReplyJudge(ctx *eventctx.Context, content string) (reply, ok, busy bool) {
	if !p.replyGate.judge.tryAcquire() {
		// 忙：视为"不回"。刷屏模式下不让判定排队正是期望行为。
		return false, false, true
	}
	defer p.replyGate.judge.release()

	chatID := ctx.GetChatInfo().ID
	var window string
	if p.history != nil {
		recent := p.history.QueryChat(chatID, replyJudgeWindowEntries, messagelog.QueryOptions{})
		entries := make([]runtime.ChatWindowEntry, 0, len(recent))
		for _, e := range recent {
			entries = append(entries, runtime.ChatWindowEntry{
				SenderName: e.UserName,
				Content:    e.Content,
				Bot:        e.IsOutbound,
			})
		}
		window = runtime.BuildReplyJudgeWindow(entries, replyJudgeWindowRunes)
	}
	msg := runtime.BuildReplyJudgeMessage(window, content, ctx.GetSenderInfo().DisplayName)

	// 判定用便宜的小模型：优先 verify/extract 专用模型（同属"小额任务"），
	// 都没配则跟随主模型。输出极短，不与其余预算共享。
	model := p.cfg.VerifyModel
	if model == "" {
		model = p.cfg.ExtractModel
	}
	if model == "" {
		model = p.cfg.Model
	}

	base := p.lifecycleCtx
	if base == nil {
		base = context.Background()
	}
	judgeCtx, cancel := context.WithTimeout(base, replyJudgeTimeout)
	defer cancel()

	req := &protocol.ChatRequest{
		Model: model,
		Messages: []protocol.Message{
			{Role: protocol.RoleSystem, Content: runtime.ReplyJudgeSystemPrompt},
			{Role: protocol.RoleUser, Content: msg},
		},
		MaxTokens:   64,
		Temperature: 0,
		// 思考程度沿用主配置；空值仍不发送，兼容不支持该字段的网关。
		ReasoningEffort: p.cfg.ReasoningEffort,
	}
	resp, err := p.prov.Chat(judgeCtx, req)
	if err != nil {
		if p.lifecycleCtx == nil || p.lifecycleCtx.Err() == nil {
			logger.Debugf("[AI] reply judge call failed: %v", err)
		}
		return false, false, false
	}
	verdict, ok := runtime.ParseReplyVerdict(resp.Content)
	if !ok {
		logger.Debugf("[AI] reply judge unparsable output: %q", resp.Content)
		return false, false, false
	}
	logger.Debugf("[AI] reply judge verdict=%t reason=%q", verdict.Reply, verdict.Reason)
	return verdict.Reply, true, false
}

// judgeGate 判定单飞闸（进程级：判定无状态，不需要 per-chat）。
type judgeGate struct{ busy atomic.Bool }

// tryAcquire 尝试占用判定槽；成功返回 true（调用方负责 release）。
func (g *judgeGate) tryAcquire() bool { return g.busy.CompareAndSwap(false, true) }

// release 释放判定槽。
func (g *judgeGate) release() { g.busy.Store(false) }
