// Package ai verify.go — 答案校验（LLM-as-judge）的装配门面。
//
// 评审提示词、鲁棒解析、重试上限与单次评审调用见 builtin/ai/runtime；
// 本文件只交出插件持有的配置、提供商与会话写入，并保留回合编排：
// 生成失败/中断时的降级、重试计数与"追加修正指令后重新生成"的循环。
package ai

import (
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// verifier 组装回答校验器。
func (p *Plugin) verifier() runtime.Verifier {
	return runtime.Verifier{Client: p.runtimeClient(), Cfg: p.cfg}
}

// generateVerified 生成回答并通过校验器校验：
//   - 未开启校验 / 生成失败 / 无文本 → 原样返回
//   - 每次生成后都经校验器评审（含重试后的修正稿）
//   - 校验通过 → 返回；不通过且未达 verify_max_retries → 追加"修正指令"
//     用户消息后重新生成；达到上限仍不通过 → 返回最后一份回答（不无限重试）
//   - 校验器自身报错时不重试、直接返回原回答
func (p *Plugin) generateVerified(ctx *eventctx.Context, session *session.Session) (*ChatResult, error) {
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

		v, verr := verifier.Verify(ctx.Context(), originalQuery, result.Text)
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
		p.sm.AppendMessage(session, protocol.Message{Role: protocol.RoleUser, Content: runtime.BuildVerifyRetryMessage(v.Reason)})
	}
}
