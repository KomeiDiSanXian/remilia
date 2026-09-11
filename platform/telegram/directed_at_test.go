package telegram

// directed_at_test.go — platform.DirectedAtBotEvent 的 Telegram 实现。
//
// 私聊（chat.type = private）的消息本身即发给机器人：私聊没有 mention /
// text_mention 实体，Mentions() 恒为空，只能由 DirectedAtBotEvent 兜底。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestDirectedAtBot_Private(t *testing.T) {
	ev := newMessageEventWithBot(&Message{
		MessageID: 1,
		Date:      1700000000,
		Chat:      &Chat{ID: 100, Type: ChatTypePrivate},
		From:      &User{ID: 100},
	}, false, "bot1")

	de, ok := ev.(platform.DirectedAtBotEvent)
	if !ok {
		t.Fatal("Telegram 事件应实现 platform.DirectedAtBotEvent")
	}
	if !de.DirectedAtBot() {
		t.Error("私聊消息 DirectedAtBot 应为 true")
	}
	if !platform.MentionedBot(ev) {
		t.Error("私聊消息应被 platform.MentionedBot 判定为指向机器人")
	}
}

func TestDirectedAtBot_GroupFalse(t *testing.T) {
	ev := newMessageEventWithBot(&Message{
		MessageID: 2,
		Date:      1700000000,
		Chat:      &Chat{ID: 200, Type: ChatTypeGroup},
		From:      &User{ID: 200},
	}, false, "bot1")

	de, ok := ev.(platform.DirectedAtBotEvent)
	if !ok {
		t.Fatal("Telegram 事件应实现 platform.DirectedAtBotEvent")
	}
	if de.DirectedAtBot() {
		t.Error("群消息 DirectedAtBot 应为 false")
	}
	if platform.MentionedBot(ev) {
		t.Error("未 @ 机器人的群消息不应被判定为指向机器人")
	}
}
