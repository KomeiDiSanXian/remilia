// verify.go — 答案校验器（LLM-as-judge）。
//
// 最终回复发送前的质量校验：非流式调用 LLM 评审"回答是否回答了用户问题、
// 是否捏造信息"，并鲁棒解析评审结果（JSON 或纯文本，容忍中英文）。
//
// 配置（verify_enabled 默认 false，因每次对话多一次 LLM 调用）：
//   - verify_enabled     开启回答校验
//   - verify_max_retries 校验失败后的最大重新生成次数（默认 1）
//
// 校验失败不阻塞对话：校验调用自身出错时返回错误，由调用方（回合编排）
// 决定降级为"原样返回"。
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// VerifyPrompt 回答评审的系统提示词。
const VerifyPrompt = `你是回答质量评审员。判断助手回复是否充分回答了用户的问题。
评审标准：
1. 回答是否直接、完整地回应了用户的问题（答非所问 = 不通过）
2. 是否捏造信息：无工具结果支撑却断言具体事实/数据/来源 = 不通过
3. 是否清晰可读

严格只输出 JSON，不要任何其他内容：
{"verdict": "pass" 或 "fail", "reason": "不通过原因（一句话，中文，通过时留空）"}`

// VerifyResult 校验结果。
type VerifyResult struct {
	// Pass 是否通过校验。
	Pass bool
	// Reason 不通过原因（Pass 时为空）。
	Reason string
}

// Verifier 回答校验器：用独立模型（verify_model，默认空 = 跟随主模型）评审回答。
type Verifier struct {
	// Client 单轮非流式 LLM 调用。
	Client Client
	// Cfg 插件配置（提供校验开关、模型与超时）。
	Cfg *config.Config
}

// Verify 校验一段回答。校验调用自身出错时返回错误（由调用方决定降级）。
func (v Verifier) Verify(ctx context.Context, userContent, answer string) (VerifyResult, error) {
	messages := []protocol.Message{
		{Role: protocol.RoleSystem, Content: VerifyPrompt},
		{Role: protocol.RoleUser, Content: "用户问题：" + userContent + "\n\n助手回答：" + answer},
	}

	verifyCtx, cancel := context.WithTimeout(ctx, v.Cfg.APITimeout)
	defer cancel()

	model := v.Cfg.VerifyModel
	if model == "" {
		model = v.Cfg.Model
	}
	resp, err := v.Client.SingleRound(verifyCtx, model, messages, nil)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("verify llm call: %w", err)
	}
	return ParseVerdict(resp.Text), nil
}

// MaxRetries 返回校验失败后的最大重新生成次数（<=0 用默认 1）。
func (v Verifier) MaxRetries() int {
	if v.Cfg.VerifyMaxRetries <= 0 {
		return 1
	}
	return v.Cfg.VerifyMaxRetries
}

// ParseVerdict 鲁棒解析评审输出。
// 优先解析 JSON（{"verdict": "pass"|"fail", "reason": "..."}）；
// 无 JSON 时按关键词兜底（pass/通过/合格 / fail/不通过/不合格）。
func ParseVerdict(text string) VerifyResult {
	text = strings.TrimSpace(text)
	var v VerifyResult

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

// BuildVerifyRetryMessage 构建校验失败后的修正指令（追加为用户消息）。
func BuildVerifyRetryMessage(reason string) string {
	return fmt.Sprintf(
		"你的上一条回答经质量校验未通过，原因：%s\n"+
			"请针对原因修正回答：确保直接回应问题、不要捏造没有工具结果支撑的事实。重新给出回答。",
		reason)
}
