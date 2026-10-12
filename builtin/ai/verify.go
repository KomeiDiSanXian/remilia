// Package ai verify.go — 答案校验（LLM-as-judge）的装配门面。
//
// 评审提示词、鲁棒解析、重试上限与单次评审调用见 builtin/ai/runtime；
// 本文件只交出插件持有的配置、提供商与会话写入，并保留回合编排：
// 生成失败/中断时的降级、重试计数与"追加修正指令后重新生成"的循环。
package ai

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// verifier 组装回答校验器。
func (p *Plugin) verifier() runtime.Verifier {
	return runtime.Verifier{
		Client:      p.runtimeClient(),
		Model:       p.cfg.Model,
		VerifyModel: p.cfg.VerifyModel,
		APITimeout:  p.cfg.APITimeout,
		MaxRetry:    p.cfg.VerifyMaxRetries,
	}
}

// effectiveVerifyTimeout 返回校验调用的外层预算：单次校验内部还有
// APITimeout 超时，外层给两倍余量避免"超时套超时"把评审误杀。
func (p *Plugin) effectiveVerifyTimeout() time.Duration {
	if p.cfg.APITimeout > 0 {
		return 2 * p.cfg.APITimeout
	}
	return 2 * 60 * time.Second
}

// generateVerified 生成回答并通过校验器校验：
//   - 未开启校验 / 生成失败 / 无文本 → 原样返回
//   - 每次生成后都经校验器评审（含重试后的修正稿）
//   - 校验通过 → 返回；不通过且未达 verify_max_retries → 追加"修正指令"
//     用户消息后重新生成；达到上限仍不通过 → 返回最后一份回答（不无限重试）
//   - 校验器自身报错时不重试、直接返回原回答
func (p *Plugin) generateVerified(ctx *eventctx.Context, session *session.Session) (*chatResult, error) {
	originalQuery := runtime.LastUserMessage(session)
	verifier := p.verifier()

	for attempt := 0; ; attempt++ {
		result, err := p.processWithTools(ctx, session)
		if err != nil {
			return result, err
		}
		// 主动停止/抢占：生成被 /ai stop 或用户新消息打断，直接返回已到手
		// 的部分结果，不再送入校验器（避免额外 LLM 调用与二次生成）。
		if session.Interrupted() {
			return result, nil
		}
		if !p.cfg.VerifyEnabled || result == nil || result.Text == "" {
			return result, nil
		}

		// 校验上下文必须独立于事件 ctx 派生：processWithTools 返回时
		// restoreDeadline 已把 std ctx 恢复为原始事件 ctx，其 30s 全局
		// deadline 在多轮工具循环后早已过期——用它调 Verify 会立刻
		// deadline exceeded，校验静默失效（只记 Debug 日志）。后台路径
		// （continuePlan）的 ctx 恢复后是 Background，校验正常，两条路径
		// 行为不一致即此因。这里从 Background 派生，让校验拥有完整预算。
		vctx, vcancel := context.WithTimeout(context.Background(), p.effectiveVerifyTimeout())
		v, verr := verifier.Verify(vctx, originalQuery, result.Text)
		vcancel()
		if verr != nil {
			logger.Debugf("[AI] Answer verification failed, skipping: %v", verr)
			return result, nil
		}
		if v.Pass {
			return result, nil
		}
		if attempt >= verifier.MaxRetries() {
			logger.Debugf("[AI] Answer verification failed after %d retries, returning as-is: %s",
				attempt, v.Reason)
			return result, nil
		}
		logger.Debugf("[AI] Answer verification failed, regenerating (%d/%d): %s",
			attempt+1, verifier.MaxRetries(), v.Reason)
		// 修正指令与反思/重规划指令同回合处理：NoPersist 追加，
		// 由 processWithTools 的 defer SaveSession 统一落库。每条各写一次库
		// 是写放大，且会让指令永久占据历史窗口。
		p.sm.AppendMessageNoPersist(session, protocol.Message{
			Role:     protocol.RoleUser,
			Internal: true,
			Content:  runtime.BuildVerifyRetryMessage(v.Reason),
		})
	}
}
