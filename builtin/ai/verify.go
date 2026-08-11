// Package ai verify.go — 答案校验器（LLM-as-judge）。
//
// 本文件实现最终回复发送前的质量校验：
//   - verifyAnswer：非流式调用 LLM 评审"回答是否回答了用户问题、是否捏造信息"
//   - parseVerdict：鲁棒解析评审结果（JSON 或纯文本，容忍中英文）
//   - generateVerified：生成 → 校验 → 不通过则注入校验反馈重新生成（有重试上限）
//
// 配置（verify_enabled 默认 false，因每次对话多一次 LLM 调用）：
//   - verify_enabled    开启回答校验
//   - verify_max_retries 校验失败后的最大重新生成次数（默认 1）
//
// 校验失败不阻塞对话：校验调用自身出错时直接返回原回答。
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// verifyPrompt 回答评审的系统提示词。
const verifyPrompt = `你是回答质量评审员。判断助手回复是否充分回答了用户的问题。
评审标准：
1. 回答是否直接、完整地回应了用户的问题（答非所问 = 不通过）
2. 是否捏造信息：无工具结果支撑却断言具体事实/数据/来源 = 不通过
3. 是否清晰可读

严格只输出 JSON，不要任何其他内容：
{"verdict": "pass" 或 "fail", "reason": "不通过原因（一句话，中文，通过时留空）"}`

// verifyResult 校验结果。
type verifyResult struct {
	// Pass 是否通过校验。
	Pass bool
	// Reason 不通过原因（Pass 时为空）。
	Reason string
}

// verifyAnswer 校验一段回答。校验调用自身出错时返回错误（由调用方决定降级）。
// 使用独立模型（verify_model，默认空 = 跟随主模型）。
func (p *Plugin) verifyAnswer(ctx context.Context, userContent, answer string) (verifyResult, error) {
	messages := []Message{
		{Role: RoleSystem, Content: verifyPrompt},
		{Role: RoleUser, Content: "用户问题：" + userContent + "\n\n助手回答：" + answer},
	}

	verifyCtx, cancel := context.WithTimeout(ctx, p.cfg.APITimeout)
	defer cancel()

	model := p.cfg.VerifyModel
	if model == "" {
		model = p.cfg.Model
	}
	resp, err := p.runSingleRoundModel(verifyCtx, model, messages, nil)
	if err != nil {
		return verifyResult{}, fmt.Errorf("verify llm call: %w", err)
	}
	return parseVerdict(resp.Text), nil
}

// parseVerdict 鲁棒解析评审输出。
// 优先解析 JSON（{"verdict": "pass"|"fail", "reason": "..."}）；
// 无 JSON 时按关键词兜底（pass/通过/合格 / fail/不通过/不合格）。
func parseVerdict(text string) verifyResult {
	text = strings.TrimSpace(text)
	var v verifyResult

	// JSON 路径：截取第一个 { 到最后一个 }
	if start := strings.IndexByte(text, '{'); start >= 0 {
		if end := strings.LastIndexByte(text, '}'); end > start {
			var parsed struct {
				Verdict string `json:"verdict"`
				Reason  string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(text[start:end+1]), &parsed); err == nil {
				switch strings.ToLower(strings.TrimSpace(parsed.Verdict)) {
				case "pass":
					v.Pass = true
				case "fail":
					v.Pass = false
					v.Reason = strings.TrimSpace(parsed.Reason)
				default:
					// verdict 字段缺失/非法 → 落入关键词兜底
				}
				if v.Pass || v.Reason != "" {
					return v
				}
			}
		}
	}

	// 关键词兜底（注意 fail 关键词需先于 pass 检查："不通过" 含 "通过" 子串）
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, `"fail"`), strings.Contains(lower, "fail\n"),
		strings.Contains(text, "不通过"), strings.Contains(text, "未通过"),
		strings.Contains(text, "不合格"), strings.Contains(text, "未回答"),
		strings.Contains(text, "捏造"), strings.Contains(text, "答非所问"):
		v.Pass = false
		v.Reason = text
	case strings.Contains(lower, `"pass"`), strings.Contains(lower, "pass\n"),
		strings.Contains(text, "通过"), strings.Contains(text, "合格"),
		strings.Contains(text, "已回答"):
		v.Pass = true
	default:
		// 无法判定：默认通过（校验失败不阻塞对话）
		v.Pass = true
	}
	return v
}

// buildVerifyRetryMessage 构建校验失败后的修正指令（追加为用户消息）。
func buildVerifyRetryMessage(reason string) string {
	return fmt.Sprintf(
		"你的上一条回答经质量校验未通过，原因：%s\n"+
			"请针对原因修正回答：确保直接回应问题、不要捏造没有工具结果支撑的事实。重新给出回答。",
		reason)
}

// effectiveVerifyMaxRetries 返回校验失败后的最大重新生成次数（<=0 用默认 1）。
func (p *Plugin) effectiveVerifyMaxRetries() int {
	if p.cfg.VerifyMaxRetries <= 0 {
		return 1
	}
	return p.cfg.VerifyMaxRetries
}

// generateVerified 生成回答并通过校验器校验：
//   - 未开启校验 / 生成失败 / 无文本 → 原样返回
//   - 每次生成后都经校验器评审（含重试后的修正稿）
//   - 校验通过 → 返回；不通过且未达 verify_max_retries → 追加"修正指令"
//     用户消息后重新生成；达到上限仍不通过 → 返回最后一份回答（不无限重试）
//   - 校验器自身报错时不重试、直接返回原回答
func (p *Plugin) generateVerified(ctx *eventctx.Context, session *Session) (*ChatResult, error) {
	originalQuery := getLastUserMessage(session)

	for attempt := 0; ; attempt++ {
		result, err := p.processWithTools(ctx, session)
		if err != nil {
			return result, err
		}
		if !p.cfg.VerifyEnabled || result == nil || result.Text == "" {
			return result, nil
		}

		v, verr := p.verifyAnswer(ctx.Context(), originalQuery, result.Text)
		if verr != nil {
			logger.Debugf("[AI] Answer verification failed, skipping: %v", verr)
			return result, nil
		}
		if v.Pass {
			return result, nil
		}
		if attempt >= p.effectiveVerifyMaxRetries() {
			logger.Debugf("[AI] Answer verification failed after %d retries, returning as-is: %s",
				attempt, v.Reason)
			return result, nil
		}
		logger.Debugf("[AI] Answer verification failed, regenerating (%d/%d): %s",
			attempt+1, p.effectiveVerifyMaxRetries(), v.Reason)
		p.sm.AppendMessage(session, Message{Role: RoleUser, Content: buildVerifyRetryMessage(v.Reason)})
	}
}
