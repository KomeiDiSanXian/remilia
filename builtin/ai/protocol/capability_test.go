// capability_test.go — 端点原生上下文能力判定与降级记忆的契约。
//
// 这里冻结的是"按端点身份定形状"的判定顺序，以及"失败一次即降级"的边界：
// 只有 400/422 且错误体提到角色才降级，避免一次鉴权失败让环境事实永久回退。
package protocol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDetectNativeContext 冻结端点识别顺序。
func TestDetectNativeContext(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		baseURL  string
		want     NativeContext
	}{
		{"DeepSeek OpenAI 兼容", "openai", "https://api.deepseek.com", NativeContextReminder},
		{"DeepSeek Anthropic 兼容", "anthropic", "https://api.deepseek.com/anthropic", NativeContextReminder},
		{"DeepSeek 带斜杠", "openai", "https://api.deepseek.com/v1/", NativeContextReminder},
		{"Anthropic 官方", "anthropic", "https://api.anthropic.com", NativeContextMidSystem},
		{"Anthropic 官方带路径", "anthropic", "https://api.anthropic.com/v1", NativeContextMidSystem},
		{"Anthropic 自建兼容端点", "anthropic", "http://10.0.0.5:8080", NativeContextNone},
		{"OpenAI 官方", "openai", "https://api.openai.com/v1", NativeContextDeveloper},
		{"Ollama 本地", "openai", "http://localhost:11434/v1", NativeContextDeveloper},
		{"空地址", "openai", "", NativeContextDeveloper},
	}
	for _, c := range cases {
		if got := DetectNativeContext(c.provider, c.baseURL); got != c.want {
			t.Errorf("%s: DetectNativeContext(%q, %q) = %v, want %v",
				c.name, c.provider, c.baseURL, got, c.want)
		}
	}
}

// TestDetectNativeContextDeepSeekBeatsProvider 冻结"端点身份优先于 provider 字段"：
// DeepSeek 的 Anthropic 兼容端点认 latest_reminder，不认官方中途 system。
func TestDetectNativeContextDeepSeekBeatsProvider(t *testing.T) {
	got := DetectNativeContext("anthropic", "https://api.deepseek.com/anthropic")
	assert.Equal(t, NativeContextReminder, got,
		"DeepSeek 的 Anthropic 兼容端点必须用 reminder，而不是中途 system")
}

// TestProbeMemoryDowngradesOnce 冻结"失败一次即降级"。
func TestProbeMemoryDowngradesOnce(t *testing.T) {
	var m ProbeMemory

	const provider, baseURL = "openai", "https://api.openai.com/v1"
	assert.Equal(t, NativeContextDeveloper, m.Choose(provider, baseURL), "初始应尝试原生通道")
	assert.False(t, m.IsBad(provider, baseURL))

	m.MarkBad(provider, baseURL)
	assert.True(t, m.IsBad(provider, baseURL))
	assert.Equal(t, NativeContextNone, m.Choose(provider, baseURL), "标记后必须回退")

	// 幂等：重复标记不改变结果。
	m.MarkBad(provider, baseURL)
	assert.Equal(t, NativeContextNone, m.Choose(provider, baseURL))

	// 端点之间互相隔离：另一个端点不受影响。
	const otherBase = "https://api.deepseek.com"
	assert.Equal(t, NativeContextReminder, m.Choose("openai", otherBase))

	m.Reset()
	assert.Equal(t, NativeContextDeveloper, m.Choose(provider, baseURL), "Reset 后恢复尝试")
}

// TestProbeMemoryNormalizesKey 冻结键归一化：大小写与尾斜杠不影响判定。
func TestProbeMemoryNormalizesKey(t *testing.T) {
	var m ProbeMemory
	m.MarkBad("OpenAI", "https://API.OpenAI.com/v1/")
	assert.True(t, m.IsBad("openai", "https://api.openai.com/v1"))
	assert.Equal(t, NativeContextNone, m.Choose("openai", "https://api.openai.com/v1"))
}

// TestIsChannelRejection 冻结降级触发条件：只有 400/422 且错误体提到角色。
func TestIsChannelRejection(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"400 提到 role", &APIError{StatusCode: 400, Body: `{"error":{"message":"invalid role: latest_reminder"}}`}, true},
		{"422 content 类型", &APIError{StatusCode: 422, Body: `invalid content for role`}, true},
		{"400 提到 system", &APIError{StatusCode: 400, Body: `role: system is not allowed here`}, true},
		{"400 但不含角色词", &APIError{StatusCode: 400, Body: `context length exceeded`}, false},
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
	assert.Equal(t, RoleLatestReminder, NativeContextReminder.RoleOf())
	assert.Equal(t, RoleSystemMid, NativeContextMidSystem.RoleOf())
	assert.Equal(t, Role(""), NativeContextNone.RoleOf())

	// 存储角色不是任何形状的线格式角色（它必须被编码后才发出）。
	for _, n := range []NativeContext{
		NativeContextDeveloper, NativeContextReminder, NativeContextMidSystem, NativeContextNone,
	} {
		assert.NotEqual(t, RoleEnvContext, n.RoleOf(), "存储角色不得作为线格式角色")
	}
}

// TestRolesAreKnown 验证白名单覆盖全部已知角色，且存储角色也在其中
// （它需要被 session/trim 逻辑识别，但不能被适配器裸透传）。
func TestRolesAreKnown(t *testing.T) {
	for _, r := range []Role{
		RoleSystem, RoleUser, RoleAssistant, RoleTool,
		RoleDeveloper, RoleLatestReminder, RoleSystemMid, RoleEnvContext,
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
