// capability.go — 端点原生上下文能力的判定与"失败一次即降级"的进程级记忆。
//
// 为什么不做模型白名单表：那是一张必然腐化、且新模型上线必须改代码的表
// （Anthropic 的中途 system 就明确只支持部分模型，Sonnet 5 不支持）。改用
// 探测：
//
//  1. 按**端点身份**（base_url + provider）决定首选形状；
//  2. 收到 400/422 且错误体指向该角色时，把端点记进降级集合，本进程内不再
//     尝试，直接回退到"拼进 user 文本"的兜底形态。
//
// 400/422 在推理前被拒绝、不消耗 token，因此"失败一次"的成本可以接受，
// 换来的是新模型零维护。
//
// 注意端点身份必须看 base_url 而不只看 provider：`provider: anthropic` +
// `base_url: https://api.deepseek.com/anthropic` 是合法组合，而该端点认
// latest_reminder，不认 Anthropic 的中途 system。

package protocol

import (
	"errors"
	"strings"
	"sync"
)

// knownEndpoints 按 base_url 片段识别的端点（小写子串匹配，顺序即优先级）。
//
// 只放"必须特殊处理"的端点：DeepSeek 的 latest_reminder 是它独有的协议，
// 而所有 OpenAI 兼容网关都可以尝试 developer（不认就降级）。
var knownEndpoints = []struct {
	// fragment base_url 里的小写子串。
	fragment string
	// shape 命中的首选形状。
	shape NativeContext
}{
	{"deepseek.com", NativeContextReminder},
}

// anthropicHost 判断端点是否是 Anthropic 官方（或其区域域名）。
//
// 只有官方 Messages API 支持数组内的 role=system；DeepSeek 的 Anthropic
// 兼容端点虽然路径相同，但语义不同（认 latest_reminder），因此先按 base_url
// 精确识别官方域名。
func anthropicHost(baseURL string) bool {
	u := strings.ToLower(baseURL)
	return strings.Contains(u, "api.anthropic.com") ||
		strings.Contains(u, "anthropic.com")
}

// DetectNativeContext 按端点身份返回首选的原生上下文形状。
//
// 判定顺序：
//  1. base_url 命中已知端点（DeepSeek）→ reminder；
//  2. provider=anthropic 且是 Anthropic 官方域名 → mid_system；
//  3. provider=anthropic（自建/第三方兼容端点）→ none（不猜形状，
//     直接走 user 文本拼接，避免把官方语义套到未知网关上）；
//  4. 其余（OpenAI 兼容）→ developer（不认则探测降级）。
func DetectNativeContext(provider, baseURL string) NativeContext {
	for _, ep := range knownEndpoints {
		if strings.Contains(strings.ToLower(baseURL), ep.fragment) {
			return ep.shape
		}
	}
	if provider == "anthropic" {
		if anthropicHost(baseURL) {
			return NativeContextMidSystem
		}
		return NativeContextNone
	}
	return NativeContextDeveloper
}

// ProbeMemory 记录"哪些端点在原生通道上失败过"，用于进程内一次性降级。
//
// 零值可用。并发安全（会话并行回合会同时查询/写入）。键取 provider+base_url，
// 因为同一个进程可能配置多个端点（主模型 + 校验/抽取模型）。
type ProbeMemory struct {
	mu  sync.RWMutex
	bad map[string]bool
}

// endpointKey 生成降级集合的键。
func endpointKey(provider, baseURL string) string {
	return strings.ToLower(provider) + "|" + strings.ToLower(strings.TrimRight(baseURL, "/"))
}

// Choose 返回该端点本次应使用的形状：探测失败过则返回 None（走兜底）。
func (m *ProbeMemory) Choose(provider, baseURL string) NativeContext {
	shape := DetectNativeContext(provider, baseURL)
	if shape == NativeContextNone {
		return NativeContextNone
	}
	m.mu.RLock()
	bad := m.bad[endpointKey(provider, baseURL)]
	m.mu.RUnlock()
	if bad {
		return NativeContextNone
	}
	return shape
}

// MarkBad 把端点记入降级集合（幂等）。
func (m *ProbeMemory) MarkBad(provider, baseURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bad == nil {
		m.bad = make(map[string]bool, 2)
	}
	m.bad[endpointKey(provider, baseURL)] = true
}

// IsBad 判断端点是否已被标记为不支持原生通道（测试与诊断用）。
func (m *ProbeMemory) IsBad(provider, baseURL string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bad[endpointKey(provider, baseURL)]
}

// Reset 清空降级集合（测试与配置热更新时使用）。
func (m *ProbeMemory) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bad = nil
}

// IsChannelRejection 判断错误是否是"端点不接受该上下文角色"。
//
// 只认 400/422：这两个状态码代表请求在推理前被拒（不消耗 token），因此
// 值得记入降级。其余状态码（401/404/429/5xx）与角色无关，重试或改配置才有
// 意义，不应触发降级——否则一次鉴权失败会让环境事实永久回退。
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
	// 错误体里必须提到角色相关词，避免把"参数缺失/上下文超长"一类的 400
	// 误判成角色不支持（那会让环境事实永久降级，且真正的原因被掩盖）。
	body := strings.ToLower(ae.Body)
	for _, marker := range []string{"role", "latest_reminder", "developer", "system"} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}
