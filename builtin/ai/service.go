// service.go — 面向其他插件的一次性 LLM 问答服务端口。
//
// 定位：插件凡是想让 AI "总结/翻译/判读/起名/打分"，不必复制本插件的
// base_url / api_key / 超时 / 重试 / 指标 / 换模型逻辑，经 TryService 取到
// 本插件后调用 [Plugin.Complete] 单轮补全即可：
//
//	if aiSvc, ok := ctx.TryService[*ai.Plugin]("ai"); ok {
//	    answer, err := aiSvc.Complete(ctx, system, user,
//	        ai.WithMaxTokens(512), ai.WithModel(cfg.ReplyJudgeModel))
//	}
//
// 边界（刻意收窄的接口面）：
//   - 无工具调用、无会话读写、无审批闸门——那是"对话回合"的事，业务
//     插件要挂能力请走 toolkit.ToolProvider / SkillProvider 两个惯例面；
//   - temperature/max_tokens/model 仅允许在本调用内覆盖；默认值继承
//     插件配置；非法取值静默忽略（安全默认，详见各 With* 文档）；
//   - 调用经过与对话回合同一层的 metricsProvider（p.prov 装配时已包），
//     计入 ai_llm_* 指标；
//   - ctx 无 deadline 时以 cfg.APITimeout 兜底派生，防止调用方忘挂超时；
//     已有 deadline 则完全尊重调用方。

package ai

import (
	"context"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// LLMService 一次性 LLM 问答服务的窄接口（[Plugin] 实现它）。
// 业务插件建议持有这个接口而不是 *Plugin，避免耦合插件内部字段。
type LLMService interface {
	// Complete 发起一次单轮补全：合法的 system 内容（可空）+ user 内容，
	// 返回响应正文。错误原样上抛（调用方可按 errors.As 区分 *protocol.APIError）。
	Complete(ctx context.Context, system, user string, opts ...ChatOption) (string, error)
}

// ChatOption 单次调用级的覆盖项。
type ChatOption func(*chatOptions)

// chatOptions Complete 的生效参数（缺省继承插件配置）。
type chatOptions struct {
	model       string
	temperature *float64
	maxTokens   *int
}

// WithModel 指定本次调用的模型（空串/全空白忽略，沿用默认）。
func WithModel(model string) ChatOption {
	return func(o *chatOptions) {
		if m := strings.TrimSpace(model); m != "" {
			o.model = m
		}
	}
}

// WithTemperature 覆盖温度；0~2 之外忽略（沿用默认）。
func WithTemperature(t float64) ChatOption {
	return func(o *chatOptions) {
		if t >= 0 && t <= 2 {
			v := t
			o.temperature = &v
		}
	}
}

// WithMaxTokens 覆盖输出预算；<=0 忽略（沿用默认）。
func WithMaxTokens(n int) ChatOption {
	return func(o *chatOptions) {
		if n > 0 {
			v := n
			o.maxTokens = &v
		}
	}
}

// Complete 实现 [LLMService]。
func (p *Plugin) Complete(ctx context.Context, system, user string, opts ...ChatOption) (string, error) {
	// WithModel 拒绝空串，因此初值即为最终默认值，无需事后兜底。
	o := chatOptions{model: p.cfg.Model}
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}

	req := &protocol.ChatRequest{
		Model:           o.model,
		Temperature:     p.cfg.Temperature,
		TopP:            p.cfg.TopP,
		MaxTokens:       p.cfg.MaxTokens,
		ReasoningEffort: p.cfg.ReasoningEffort,
	}
	if o.temperature != nil {
		req.Temperature = *o.temperature
	}
	if o.maxTokens != nil {
		req.MaxTokens = *o.maxTokens
	}
	if strings.TrimSpace(system) != "" {
		req.Messages = append(req.Messages, protocol.Message{Role: protocol.RoleSystem, Content: system})
	}
	req.Messages = append(req.Messages, protocol.Message{Role: protocol.RoleUser, Content: user})

	callCtx := ctx
	if ctx == nil {
		callCtx = context.Background()
	}
	if _, hasDeadline := callCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(callCtx, p.effectiveCompleteTimeout())
		defer cancel()
	}

	resp, err := p.prov.Chat(protocol.WithCallKind(callCtx, protocol.CallKindService), req)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// effectiveCompleteTimeout 无 deadline ctx 的兜底超时。
func (p *Plugin) effectiveCompleteTimeout() time.Duration {
	if p.cfg.APITimeout > 0 {
		return p.cfg.APITimeout
	}
	return 60 * time.Second
}
