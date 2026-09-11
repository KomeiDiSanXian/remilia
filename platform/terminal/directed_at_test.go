package terminal

// directed_at_test.go — platform.DirectedAtBotEvent 的终端实现。
//
// 终端私聊事件（NewEvent）的输入本身即发给机器人；群组终端事件
// （NewGroupEvent）仍需显式 SetMentions 才算 @。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

func TestDirectedAtBot_Private(t *testing.T) {
	ev := NewEvent("hi")

	de, ok := platform.Event(ev).(platform.DirectedAtBotEvent)
	if !ok {
		t.Fatal("终端事件应实现 platform.DirectedAtBotEvent")
	}
	if !de.DirectedAtBot() {
		t.Error("私聊终端事件 DirectedAtBot 应为 true")
	}
	if !platform.MentionedBot(ev) {
		t.Error("私聊终端事件应被 platform.MentionedBot 判定为指向机器人")
	}
}

func TestDirectedAtBot_GroupFalse(t *testing.T) {
	ev := NewGroupEvent("hi", "g1")

	de, ok := platform.Event(ev).(platform.DirectedAtBotEvent)
	if !ok {
		t.Fatal("终端事件应实现 platform.DirectedAtBotEvent")
	}
	if de.DirectedAtBot() {
		t.Error("群组终端事件 DirectedAtBot 应为 false")
	}
	if platform.MentionedBot(ev) {
		t.Error("未 @ 机器人的群组终端事件不应被判定为指向机器人")
	}
}
