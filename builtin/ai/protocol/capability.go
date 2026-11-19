// capability.go — 原生上下文能力的"按线格式选择 + 失败一次即降级"进程级记忆。
//
// 为什么不做端点/模型名单：
//
//	框架只声明兼容两种线格式（OpenAI 兼容 / Anthropic 兼容），用户部署的
//	模型是未知的。任何"按 base_url 片段识别厂商"或"按模型名判断能力"的表
//	都必然腐化：新网关一上线就要改代码，改错就是整轮对话被 400 拒绝。因此
//	形状只由 provider（用户声明的线格式）决定，能力由探测确认。
//
// 探测策略：
//
//  1. 按 provider 取首选形状（[NativeContextCandidates]）；
//  2. 收到 400/422 且错误体指向该形状时，把端点记进降级集合，本进程内不再
//     尝试该形状，自动落到下一个候选（最终是"拼进 user 文本"）。
//
// 400/422 在推理前被拒绝、不消耗 token，因此"失败一次"的成本可以接受。

package protocol

import (
	"errors"
	"strings"
	"sync"
)

// ProviderNameOpenAI / ProviderNameAnthropic 是线格式名（配置里的 provider）。
//
// 定义在这里而不是 config 包：能力判定属于协议层，且避免 capability 反向
// 依赖插件配置。
const (
	ProviderNameOpenAI    = "openai"
	ProviderNameAnthropic = "anthropic"
)

// ProbeMemory 记录端点层面的能力探测结果，用于进程内一次性降级/修复。
//
// 零值可用。并发安全（会话并行回合会同时查询/写入）。键取 provider + base_url：
// 同一个进程可能配置多个端点（主模型 + 校验/抽取模型），而同一端点上的不同
// 形状需要分别记忆（developer 被拒不代表中途 system 也被拒，反之亦然）。
type ProbeMemory struct {
	mu  sync.RWMutex
	bad map[string]bool
	// echoReasoning 需要为历史工具轮补发空 reasoning_content 的端点。
	echoReasoning map[string]bool
}

// endpointKey 生成降级集合的键（不含形状，供诊断使用）。
func endpointKey(provider, baseURL string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "|" +
		strings.ToLower(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
}

// shapeKey 生成"端点 + 形状"的降级键。
func shapeKey(provider, baseURL string, shape NativeContext) string {
	return endpointKey(provider, baseURL) + "|" + shape.String()
}

// Choose 返回该端点本次应使用的形状：按 provider 的候选顺序取第一个未被
// 标记为不可用的形状；全被标记时返回 [NativeContextNone]（走兜底）。
func (m *ProbeMemory) Choose(provider, baseURL string) NativeContext {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, shape := range NativeContextCandidates(provider) {
		if shape == NativeContextNone {
			return NativeContextNone
		}
		if !m.bad[shapeKey(provider, baseURL, shape)] {
			return shape
		}
	}
	return NativeContextNone
}

// MarkBad 把"该端点上的该形状"记入降级集合（幂等）。
func (m *ProbeMemory) MarkBad(provider, baseURL string, shape NativeContext) {
	if shape == NativeContextNone {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bad == nil {
		m.bad = make(map[string]bool, 2)
	}
	m.bad[shapeKey(provider, baseURL, shape)] = true
}

// IsBad 判断该端点上的该形状是否已被标记为不支持（测试与诊断用）。
func (m *ProbeMemory) IsBad(provider, baseURL string, shape NativeContext) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bad[shapeKey(provider, baseURL, shape)]
}

// Reset 清空降级集合（测试与配置热更新时使用）。
func (m *ProbeMemory) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bad = nil
	m.echoReasoning = nil
}

// RequiresReasoningEcho 判断该端点是否要求为历史工具轮补发空 reasoning_content。
func (m *ProbeMemory) RequiresReasoningEcho(provider, baseURL string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.echoReasoning[endpointKey(provider, baseURL)]
}

// MarkReasoningEcho 记录"该端点要求历史工具轮回传推理"（幂等）。
func (m *ProbeMemory) MarkReasoningEcho(provider, baseURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.echoReasoning == nil {
		m.echoReasoning = make(map[string]bool, 2)
	}
	m.echoReasoning[endpointKey(provider, baseURL)] = true
}

// IsChannelRejection 判断错误是否是"端点不接受该上下文形状"。
//
// 只认 400/422：这两个状态码代表请求在推理前被拒（不消耗 token），因此
// 值得记入降级。其余状态码（401/404/429/5xx）与形状无关，重试或改配置才有
// 意义，不应触发降级——否则一次鉴权失败会让环境事实永久回退。
//
// 错误体必须提到角色/渠道相关的词，避免把"参数缺失/上下文超长"一类的 400
// 误判成本地降级（那会让真正的原因被掩盖）。
func IsChannelRejection(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.StatusCode {
	case 400, 422:
	default:
		return false
	}
	body := strings.ToLower(ae.Body)
	for _, marker := range []string{
		"role", "developer", "system",
		// 部分兼容网关把应用级指令角色当成未知字段/不支持的内容块报错。
		"unknown variant", "unsupported role", "invalid role",
	} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// IsReasoningEchoRequired 判断错误是否是"思考型端点要求回传历史推理"。
//
// 触发条件与 [IsChannelRejection] 相同（400/422，推理前拒绝、不消耗 token），
// 但含义完全不同：这不是形状不可用，而是"这类端点在请求携带 tools 时要求
// 历史轮次的推理原样带回"。一旦确认，后续请求给带工具调用、但没有推理的
// 历史轮次补一个空的 reasoning_content 即可（端点接受空串，却拒绝缺字段）。
//
// 判据是错误体本身，不看模型名/域名：从不产出推理的端点永远不会返回这条
// 错误，因此不会被动收到这个额外字段。
func IsReasoningEchoRequired(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.StatusCode {
	case 400, 422:
	default:
		return false
	}
	body := strings.ToLower(ae.Body)
	return strings.Contains(body, "reasoning_content") ||
		strings.Contains(body, "reasoning content") ||
		strings.Contains(body, "thinking mode must be passed back")
}
