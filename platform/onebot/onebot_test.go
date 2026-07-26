package onebot

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("ws://127.0.0.1:6700")
	assert.Equal(t, ModeForwardWS, cfg.Mode)
	assert.Equal(t, "ws://127.0.0.1:6700", cfg.URL)
	assert.Equal(t, 1*time.Second, cfg.ReconnectDelay)
	assert.Equal(t, 60*time.Second, cfg.ReconnectMaxDelay)
	assert.Equal(t, 10*time.Second, cfg.APITimeout)
	assert.Equal(t, 100, cfg.EventBufferSize)
}

func TestDefaultReverseConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultReverseConfig(":8080")
	assert.Equal(t, ModeReverseWS, cfg.Mode)
	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Equal(t, 10*time.Second, cfg.APITimeout)
	assert.Equal(t, 100, cfg.EventBufferSize)
}

func TestDefaultHTTPPostConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultHTTPPostConfig(":8080", "http://127.0.0.1:5700")
	assert.Equal(t, ModeHTTPPost, cfg.Mode)
	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Equal(t, "http://127.0.0.1:5700", cfg.URL)
	assert.Equal(t, 10*time.Second, cfg.APITimeout)
	assert.Equal(t, 100, cfg.EventBufferSize)
}

func TestConfigSetDefaults(t *testing.T) {
	t.Parallel()
	t.Run("fills zero values", func(t *testing.T) {
		var cfg Config
		cfg.setDefaults()
		assert.Equal(t, time.Second, cfg.ReconnectDelay)
		assert.Equal(t, 60*time.Second, cfg.ReconnectMaxDelay)
		assert.Equal(t, 10*time.Second, cfg.APITimeout)
		assert.Equal(t, 100, cfg.EventBufferSize)
	})

	t.Run("preserves configured values", func(t *testing.T) {
		cfg := Config{
			ReconnectDelay:    5 * time.Second,
			ReconnectMaxDelay: 30 * time.Second,
			APITimeout:        15 * time.Second,
			EventBufferSize:   200,
		}
		cfg.setDefaults()
		assert.Equal(t, 5*time.Second, cfg.ReconnectDelay)
		assert.Equal(t, 30*time.Second, cfg.ReconnectMaxDelay)
		assert.Equal(t, 15*time.Second, cfg.APITimeout)
		assert.Equal(t, 200, cfg.EventBufferSize)
	})

	t.Run("handles negative and zero values", func(t *testing.T) {
		cfg := Config{
			ReconnectDelay:    -1,
			ReconnectMaxDelay: -1,
			APITimeout:        -1,
			EventBufferSize:   -1,
		}
		cfg.setDefaults()
		assert.Equal(t, time.Second, cfg.ReconnectDelay)
		assert.Equal(t, 60*time.Second, cfg.ReconnectMaxDelay)
		assert.Equal(t, 10*time.Second, cfg.APITimeout)
		assert.Equal(t, 100, cfg.EventBufferSize)
	})
}

func TestNewForwardWSAdapter(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("ws://127.0.0.1:6700")
	adapter := NewForwardWSAdapter(cfg)
	require.NotNil(t, adapter)

	assert.Equal(t, ModeForwardWS, adapter.config.Mode)
	assert.Equal(t, "ws://127.0.0.1:6700", adapter.config.URL)
}

func TestNewAdapter(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	require.NotNil(t, adapter)
	assert.Equal(t, ModeForwardWS, adapter.config.Mode)
	assert.Equal(t, "ws://127.0.0.1:6700", adapter.config.URL)
}

func TestForwardWSAdapter_Platform(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	assert.Equal(t, "onebot", adapter.Platform())
}

func TestForwardWSAdapter_IsRunningBeforeStart(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	assert.False(t, adapter.IsRunning())
}

func TestForwardWSAdapter_Capabilities(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	caps := adapter.Capabilities()
	assert.True(t, caps.MessageDelete)
	assert.True(t, caps.ThreadReply)
	assert.True(t, caps.MentionAll)
	assert.False(t, caps.Reactions)
	assert.False(t, caps.MessageEdit)
	assert.False(t, caps.MultiAttachment)
	assert.False(t, caps.FileUpload)
}

func TestForwardWSAdapter_BotIdentityBeforeStart(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("ws://127.0.0.1:6700")
	assert.Empty(t, adapter.BotID())
	assert.Empty(t, adapter.BotName())
}

// TestUnescapeText_AmpersandDecodedLast 固定 CQ 反转义的顺序要求。
//
// OneBot V11 规定 &amp; 必须最后解码。若先解码 &amp;，
// "&amp;#91;"（字面文本 "&#91;" 的正确编码）会先变成 "&#91;"，
// 再被二次解码成 "["，凭空造出 CQ 码分隔符——把用户输入的普通文本
// 变成了看起来像 CQ 码的内容，转发/桥接场景下即为 CQ 码注入。
func TestUnescapeText_AmpersandDecodedLast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"字面量 &#91; 不应被二次解码", "&amp;#91;", "&#91;"},
		{"字面量 &#93; 不应被二次解码", "&amp;#93;", "&#93;"},
		{"真实的转义方括号正常还原", "&#91;CQ:at&#93;", "[CQ:at]"},
		{"裸 & 正常还原", "a&amp;b", "a&b"},
		{"混合场景", "&amp;#91;x&#93;", "&#91;x]"},
		{"无转义内容原样返回", "hello world", "hello world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, unescapeText(tt.in))
		})
	}
}

// TestUnescapeText_MatchesCQValueOrdering 两个反转义函数对共有转义序列的
// 处理必须一致，否则同一段文本在正文与 CQ 参数中会得到不同结果。
func TestUnescapeText_MatchesCQValueOrdering(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"&amp;#91;", "&amp;#93;", "a&amp;b", "&#91;&#93;"} {
		assert.Equal(t, unescapeCQValue(in), unescapeText(in), "输入 %q 两者结果应一致", in)
	}
}

// TestSegmentData_TolerantDecode 固定"单个字段类型不符不得丢弃整条事件"。
//
// 各 OneBot 实现在 data 里混用类型非常普遍：{"qq":123}（数字）、
// {"flash":true}（布尔）、node 段的 {"content":[...]}（数组）。
// data 若声明为 map[string]string，这些值会让 json.Unmarshal 直接报错，
// 错误一路冒泡到 parseEvent，receiveLoop 打一行日志便丢弃**整条消息**。
func TestSegmentData_TolerantDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		key     string
		want    string
	}{
		{"字符串值", `{"type":"at","data":{"qq":"123"}}`, "qq", "123"},
		{"数字值降级为字符串", `{"type":"at","data":{"qq":123}}`, "qq", "123"},
		{"布尔值降级为字符串", `{"type":"image","data":{"flash":true}}`, "flash", "true"},
		{"浮点值降级为字符串", `{"type":"x","data":{"n":1.5}}`, "n", "1.5"},
		{"null 值降级为空串", `{"type":"x","data":{"n":null}}`, "n", ""},
		{"数组值保留原始 JSON", `{"type":"node","data":{"content":[{"type":"text"}]}}`, "content", `[{"type":"text"}]`},
		{"对象值保留原始 JSON", `{"type":"x","data":{"o":{"a":1}}}`, "o", `{"a":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seg MessageSegment
			require.NoError(t, json.Unmarshal([]byte(tt.payload), &seg))
			assert.Equal(t, tt.want, seg.Data[tt.key])
		})
	}
}

// TestMessageChain_NullMessage 固定 "message": null 不丢事件。
//
// encoding/json 明确规定字面量 null 也会调用 UnmarshalJSON，
// 实现应将其视为空操作。
func TestMessageChain_NullMessage(t *testing.T) {
	t.Parallel()

	var payload struct {
		Message MessageChain `json:"message"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"message":null}`), &payload))
	assert.Empty(t, payload.Message)
}

// TestMessageChain_MixedTypesSurviveDecode 整条消息链里混有异常类型时，
// 其余消息段仍应正常可用。
func TestMessageChain_MixedTypesSurviveDecode(t *testing.T) {
	t.Parallel()

	raw := `[{"type":"text","data":{"text":"hi"}},{"type":"at","data":{"qq":10001}}]`
	var mc MessageChain
	require.NoError(t, json.Unmarshal([]byte(raw), &mc))
	require.Len(t, mc, 2)
	assert.Equal(t, "hi", mc.Text())
	assert.Equal(t, "10001", mc[1].AtQQ())
}

// TestNewNodeSegment 固定 node 段可被构造并序列化为嵌套结构。
//
// node 的 content 是消息类型（字符串或消息段数组），map[string]string
// 无法表达，此前 SegTypeNode 完全不可构造，合并转发发不出去。
func TestNewNodeSegment(t *testing.T) {
	t.Parallel()

	node, err := NewNodeSegment("10001", "小明", MessageChain{textSegment("hello")})
	require.NoError(t, err)

	out, err := json.Marshal(node)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"type":"node","data":{"user_id":"10001","nickname":"小明","content":[{"type":"text","data":{"text":"hello"}}]}}`,
		string(out))
}
