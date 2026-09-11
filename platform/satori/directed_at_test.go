package satori

// directed_at_test.go — platform.DirectedAtBotEvent 的 Satori 实现。
//
// 私聊频道（channel.type = direct）没有 <at> 元素，Mentions() 恒为空，
// 只能由 DirectedAtBotEvent 兜底。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestDirectedAtBot_Direct(t *testing.T) {
	ev := convertEventWithBot(&Event{
		Type:    EventTypeMessageCreated,
		Channel: &Channel{ID: "c1", Type: ChannelTypeDirect},
	}, "satori", "bot1")

	de, ok := platform.Event(ev).(platform.DirectedAtBotEvent)
	if !ok {
		t.Fatal("Satori 事件应实现 platform.DirectedAtBotEvent")
	}
	if !de.DirectedAtBot() {
		t.Error("私聊频道消息 DirectedAtBot 应为 true")
	}
	if !platform.MentionedBot(ev) {
		t.Error("私聊频道消息应被 platform.MentionedBot 判定为指向机器人")
	}
}

func TestDirectedAtBot_TextChannelFalse(t *testing.T) {
	ev := convertEventWithBot(&Event{
		Type:    EventTypeMessageCreated,
		Channel: &Channel{ID: "g1", Type: ChannelTypeText},
	}, "satori", "bot1")

	de, ok := platform.Event(ev).(platform.DirectedAtBotEvent)
	if !ok {
		t.Fatal("Satori 事件应实现 platform.DirectedAtBotEvent")
	}
	if de.DirectedAtBot() {
		t.Error("文本频道消息 DirectedAtBot 应为 false")
	}
	if platform.MentionedBot(ev) {
		t.Error("未 @ 机器人的频道消息不应被判定为指向机器人")
	}
}
