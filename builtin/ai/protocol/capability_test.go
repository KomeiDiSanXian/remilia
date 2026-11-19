// capability_test.go — 原生上下文能力判定与降级记忆的契约。
//
// 这里冻结三件事：
//  1. 形状只由 **provider（用户声明的线格式）** 决定，不看模型名或域名；
//  2. "失败一次即降级"按"端点 + 形状"记忆，且只认 400/422；
//  3. 任何新端点都有一条恒可用的兜底（折进 user 文本）。
package protocol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNativeContextCandidates 冻结候选项：per-provider、恒以 None 结尾。
//
// 关键不变量是"不看 base_url / 模型名"：部署方用的模型是未知的，任何按域名
// 识别厂商的表都会腐化。DeepSeek 的 OpenAI 兼容端点因此也走 developer。
func TestNativeContextCandidates(t *testing.T) {
	cases := []struct {
		provider string
		want     []NativeContext
	}{
		{ProviderNameOpenAI, []NativeContext{NativeContextDeveloper, NativeContextMidSystem, NativeContextNone}},
		{ProviderNameAnthropic, []NativeContext{NativeContextMidSystem, NativeContextNone}},
		// 大小写与空白容错（配置层已规范化，这里只是防御）。
		{"Anthropic", []NativeContext{NativeContextMidSystem, NativeContextNone}},
		{"  openai  ", []NativeContext{NativeContextDeveloper, NativeContextMidSystem, NativeContextNone}},
		// 未知 provider（装配侧会拒绝，但协议层不应 panic 或猜形状）。
		{"", []NativeContext{NativeContextDeveloper, NativeContextMidSystem, NativeContextNone}},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, NativeContextCandidates(c.provider), "provider %q", c.provider)
	}
}

// TestDetectIgnoresBaseURL 冻结"base_url 不参与形状选择"。
//
// DeepSeek、Ollama、自建网关在 OpenAI 兼容线上完全一样；把某个域名特判成
// 私有角色，等于内置一张会腐化的厂商名单——生产事故就是这么来的。
func TestDetectIgnoresBaseURL(t *testing.T) {
	for _, baseURL := range []string{
		"https://api.deepseek.com",
		"https://api.openai.com/v1",
		"http://localhost:11434/v1",
		"http://10.0.0.5:8080",
		"",
	} {
		var m ProbeMemory
		assert.Equalf(t, NativeContextDeveloper, m.Choose(ProviderNameOpenAI, baseURL),
			"openai 线格式在 %q 上必须先用 developer", baseURL)
		assert.Equalf(t, NativeContextMidSystem, m.Choose(ProviderNameAnthropic, baseURL),
			"anthropic 线格式在 %q 上必须先用中途 system", baseURL)
	}
}

// TestProbeMemoryDowngradesOnce 冻结"失败一次即降级"。
func TestProbeMemoryDowngradesOnce(t *testing.T) {
	var m ProbeMemory

	const provider, baseURL = ProviderNameOpenAI, "https://api.openai.com/v1"
	assert.Equal(t, NativeContextDeveloper, m.Choose(provider, baseURL), "初始应尝试首选形状")
	assert.False(t, m.IsBad(provider, baseURL, NativeContextDeveloper))

	m.MarkBad(provider, baseURL, NativeContextDeveloper)
	assert.True(t, m.IsBad(provider, baseURL, NativeContextDeveloper))
	assert.Equal(t, NativeContextMidSystem, m.Choose(provider, baseURL),
		"首选被拒后应退到同线格式的第二个候选")

	m.MarkBad(provider, baseURL, NativeContextMidSystem)
	assert.Equal(t, NativeContextNone, m.Choose(provider, baseURL), "全被拒后必须回退到 user 文本")

	// 幂等：重复标记不改变结果。
	m.MarkBad(provider, baseURL, NativeContextDeveloper)
	assert.Equal(t, NativeContextNone, m.Choose(provider, baseURL))

	// 端点之间互相隔离：另一个端点不受影响。
	assert.Equal(t, NativeContextDeveloper, m.Choose(provider, "https://example.com/v1"))

	m.Reset()
	assert.Equal(t, NativeContextDeveloper, m.Choose(provider, baseURL), "Reset 后恢复尝试")
}

// TestProbeMemoryIsolatesShapes 冻结"同一端点上不同形状分别记忆"：
// 用户把 provider 从 openai 切成 anthropic 时，旧形状的失败不该连带新形状。
func TestProbeMemoryIsolatesShapes(t *testing.T) {
	var m ProbeMemory
	const baseURL = "https://gateway.example.com"

	m.MarkBad(ProviderNameOpenAI, baseURL, NativeContextDeveloper)

	assert.Equal(t, NativeContextMidSystem, m.Choose(ProviderNameOpenAI, baseURL),
		"developer 被拒后应退到中途 system 候选")
	assert.Equal(t, NativeContextMidSystem, m.Choose(ProviderNameAnthropic, baseURL),
		"同一端点上的中途 system 是独立判定")
	assert.False(t, m.IsBad(ProviderNameAnthropic, baseURL, NativeContextMidSystem))

	// 标记 None 是 no-op（它是兜底，不可"更不可用"）。
	m.MarkBad(ProviderNameOpenAI, baseURL, NativeContextNone)
	assert.Equal(t, NativeContextMidSystem, m.Choose(ProviderNameOpenAI, baseURL))
}

// TestProbeMemoryNormalizesKey 冻结键归一化：大小写与尾斜杠不影响判定。
func TestProbeMemoryNormalizesKey(t *testing.T) {
	var m ProbeMemory
	m.MarkBad("OpenAI", "https://API.OpenAI.com/v1/", NativeContextDeveloper)
	assert.True(t, m.IsBad("openai", "https://api.openai.com/v1", NativeContextDeveloper))
	assert.Equal(t, NativeContextMidSystem, m.Choose("openai", "https://api.openai.com/v1"))
}

// TestIsChannelRejection 冻结降级触发条件：只有 400/422 且错误体提到形状。
func TestIsChannelRejection(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"400 提到 role", &APIError{StatusCode: 400, Body: `{"error":{"message":"invalid role: developer"}}`}, true},
		{"422 content 类型", &APIError{StatusCode: 422, Body: `invalid content for role`}, true},
		{"400 提到 system", &APIError{StatusCode: 400, Body: `role: system is not allowed here`}, true},
		// 生产事故（v1.81.0）：未知角色变体。
		{"422 未知角色变体", &APIError{StatusCode: 422, Body: `messages[1].role: unknown variant 'env_context'`}, true},
		// 兼容网关常见的两种说法。
		{"400 不支持的 role", &APIError{StatusCode: 400, Body: `unsupported role: developer`}, true},
		{"400 但不含形状词", &APIError{StatusCode: 400, Body: `context length exceeded`}, false},
		{"401 鉴权失败", &APIError{StatusCode: 401, Body: `invalid api key`}, false},
		{"404 模型不存在", &APIError{StatusCode: 404, Body: `model not found`}, false},
		{"429 限流", &APIError{StatusCode: 429, Body: `rate limited`}, false},
		{"500 服务端", &APIError{StatusCode: 500, Body: `role internal error`}, false},
		{"非 API 错误", errors.New("dial tcp: connection refused"), false},
		{"包装后的 API 错误", errors.Join(errors.New("chat stream"), &APIError{StatusCode: 400, Body: `bad role`}), true},
	}
	for _, c := range cases {
		if got := IsChannelRejection(c.err); got != c.want {
			t.Errorf("%s: IsChannelRejection = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestNativeContextRoleOf 冻结形状到线格式角色的映射。
func TestNativeContextRoleOf(t *testing.T) {
	assert.Equal(t, RoleDeveloper, NativeContextDeveloper.RoleOf())
	assert.Equal(t, RoleSystemMid, NativeContextMidSystem.RoleOf())
	assert.Equal(t, Role(""), NativeContextNone.RoleOf())

	// 存储角色不是任何形状的线格式角色（它必须被编码后才发出）。
	for _, n := range []NativeContext{
		NativeContextDeveloper, NativeContextMidSystem, NativeContextNone,
	} {
		assert.NotEqual(t, RoleEnvContext, n.RoleOf(), "存储角色不得作为线格式角色")
	}
}

// TestIsReasoningEchoRequired 冻结"要求历史工具轮回传推理"的判据：
// 只认 400/422，且错误体必须提到推理字段/思考模式。
func TestIsReasoningEchoRequired(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"400 reasoning_content 缺失",
			&APIError{StatusCode: 400, Body: `The reasoning_content in the thinking mode must be passed back to the API.`}, true},
		{"422 reasoning content",
			&APIError{StatusCode: 422, Body: `missing "reasoning content"`}, true},
		{"400 thinking mode must be passed back",
			&APIError{StatusCode: 400, Body: `thinking mode must be passed back to the API`}, true},
		{"400 上下文超长", &APIError{StatusCode: 400, Body: `context length exceeded`}, false},
		{"401 鉴权失败", &APIError{StatusCode: 401, Body: `reasoning_content required`}, false},
		{"500 服务端", &APIError{StatusCode: 500, Body: `reasoning_content boom`}, false},
		{"非 API 错误", errors.New("dial tcp: connection refused"), false},
	}
	for _, c := range cases {
		if got := IsReasoningEchoRequired(c.err); got != c.want {
			t.Errorf("%s: IsReasoningEchoRequired = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestProbeMemoryReasoningEcho 冻结"推理回传"的进程级记忆（端点级，幂等）。
func TestProbeMemoryReasoningEcho(t *testing.T) {
	var m ProbeMemory
	const provider, baseURL = ProviderNameOpenAI, "https://api.openai.com/v1"

	assert.False(t, m.RequiresReasoningEcho(provider, baseURL))
	m.MarkReasoningEcho(provider, baseURL)
	assert.True(t, m.RequiresReasoningEcho(provider, baseURL))
	assert.True(t, m.RequiresReasoningEcho("OpenAI", "https://API.OpenAI.com/v1/"),
		"键归一化必须大小写/尾斜杠不敏感")
	assert.False(t, m.RequiresReasoningEcho(provider, "https://other.example.com"),
		"端点之间互相隔离")

	m.Reset()
	assert.False(t, m.RequiresReasoningEcho(provider, baseURL))
}

// TestRolesAreKnown 验证白名单覆盖全部已知角色，且存储角色也在其中
// （它需要被 session/trim 逻辑识别，但不能被适配器裸透传）。
func TestRolesAreKnown(t *testing.T) {
	for _, r := range []Role{
		RoleSystem, RoleUser, RoleAssistant, RoleTool,
		RoleDeveloper, RoleSystemMid, RoleEnvContext,
	} {
		assert.True(t, IsKnownRole(r), "%q 必须是已知角色", r)
		assert.True(t, IsContextChannelRole(r) || r == RoleSystem || r == RoleUser ||
			r == RoleAssistant || r == RoleTool, "%q 分类错误", r)
	}
	assert.False(t, IsKnownRole(Role("bogus")))
	assert.False(t, IsContextChannelRole(Role("bogus")))
}

// TestOpenAIRoleWhitelistDropsEnvContext 冻结 OpenAI 适配器的白名单：
// 未编码的存储角色绝不能原样发给网关（会 400）。
func TestOpenAIRoleWhitelistDropsEnvContext(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleEnvContext, Content: `{"date":"2026-11-17"}`},
		{Role: RoleUser, Content: "问题"},
	}
	out := toOpenAIMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("RoleEnvContext must be dropped locally, got %d messages: %+v", len(out), out)
	}
	for _, m := range out {
		if m.Role == string(RoleEnvContext) {
			t.Errorf("storage role leaked to the wire: %+v", m)
		}
	}

	// 已知的通道角色必须原样保留（它们由装配侧编码后才会出现）。
	withChannel := toOpenAIMessages([]Message{{Role: RoleDeveloper, Content: "env"}})
	if len(withChannel) != 1 || withChannel[0].Role != string(RoleDeveloper) {
		t.Fatalf("developer role must reach the wire, got %+v", withChannel)
	}
}
