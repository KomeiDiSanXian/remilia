package milky

// directed_at_test.go — platform.DirectedAtBotEvent 的 Milky 实现。
//
// 好友/临时会话没有 mention 段，Mentions() 恒为空，只能由
// DirectedAtBotEvent 兜底；群消息仍由 mention 段覆盖。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectedAtBot_Friend(t *testing.T) {
	raw := `{
		"event_type": "message_receive",
		"time": 1,
		"self_id": 123456,
		"data": {
			"message_scene": "friend",
			"peer_id": 100,
			"message_seq": 1,
			"sender_id": 100,
			"segments": [{"type":"text","data":{"text":"hi"}}]
		}
	}`
	ev, err := parseRawEventWithBot([]byte(raw), "123456")
	require.NoError(t, err)

	de, ok := ev.(platform.DirectedAtBotEvent)
	require.True(t, ok, "Milky 事件应实现 platform.DirectedAtBotEvent")
	assert.True(t, de.DirectedAtBot(), "好友会话消息 DirectedAtBot 应为 true")
	assert.True(t, platform.MentionedBot(ev), "好友会话消息应被 platform.MentionedBot 判定为指向机器人")
}

func TestDirectedAtBot_Temp(t *testing.T) {
	raw := `{
		"event_type": "message_receive",
		"time": 1,
		"self_id": 123456,
		"data": {
			"message_scene": "temp",
			"peer_id": 100,
			"message_seq": 2,
			"sender_id": 100,
			"segments": [{"type":"text","data":{"text":"hi"}}]
		}
	}`
	ev, err := parseRawEventWithBot([]byte(raw), "123456")
	require.NoError(t, err)

	de, ok := ev.(platform.DirectedAtBotEvent)
	require.True(t, ok, "Milky 事件应实现 platform.DirectedAtBotEvent")
	assert.True(t, de.DirectedAtBot(), "临时会话消息 DirectedAtBot 应为 true")
	assert.True(t, platform.MentionedBot(ev), "临时会话消息应被 platform.MentionedBot 判定为指向机器人")
}

func TestDirectedAtBot_GroupFalse(t *testing.T) {
	raw := `{
		"event_type": "message_receive",
		"time": 1,
		"self_id": 123456,
		"data": {
			"message_scene": "group",
			"peer_id": 200,
			"message_seq": 3,
			"sender_id": 100,
			"segments": [{"type":"text","data":{"text":"hi"}}]
		}
	}`
	ev, err := parseRawEventWithBot([]byte(raw), "123456")
	require.NoError(t, err)

	de, ok := ev.(platform.DirectedAtBotEvent)
	require.True(t, ok, "Milky 事件应实现 platform.DirectedAtBotEvent")
	assert.False(t, de.DirectedAtBot(), "群消息 DirectedAtBot 应为 false")
	assert.False(t, platform.MentionedBot(ev), "未 @ 机器人的群消息不应被判定为指向机器人")
}
