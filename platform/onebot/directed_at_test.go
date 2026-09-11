package onebot

// directed_at_test.go — platform.DirectedAtBotEvent 的 OneBot 实现。
//
// 私聊消息（message_type = private）没有 at 段，Mentions() 恒为空，
// 只能由 DirectedAtBotEvent 兜底；群消息仍由 at 段（self_id）覆盖。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectedAtBot_Private(t *testing.T) {
	raw := `{
		"time": 1700000002,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "private",
		"sub_type": "friend",
		"message_id": 3001,
		"user_id": 98765,
		"message": [{"type":"text","data":{"text":"hi"}}],
		"sender": {"nickname": "Charlie"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)

	de, ok := ev.(platform.DirectedAtBotEvent)
	require.True(t, ok, "OneBot 事件应实现 platform.DirectedAtBotEvent")
	assert.True(t, de.DirectedAtBot(), "私聊消息 DirectedAtBot 应为 true")
	assert.True(t, platform.MentionedBot(ev), "私聊消息应被 platform.MentionedBot 判定为指向机器人")
}

func TestDirectedAtBot_GroupFalse(t *testing.T) {
	raw := `{
		"time": 1700000001,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "group",
		"sub_type": "normal",
		"message_id": 2001,
		"group_id": 555,
		"user_id": 98765,
		"message": [{"type":"text","data":{"text":"group hi"}}],
		"sender": {"nickname": "Bob"}
	}`
	ev, err := parseEvent([]byte(raw))
	require.NoError(t, err)

	de, ok := ev.(platform.DirectedAtBotEvent)
	require.True(t, ok, "OneBot 事件应实现 platform.DirectedAtBotEvent")
	assert.False(t, de.DirectedAtBot(), "群消息 DirectedAtBot 应为 false")
	assert.False(t, platform.MentionedBot(ev), "未 @ 机器人的群消息不应被判定为指向机器人")
}
