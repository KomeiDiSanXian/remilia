package discord_test

// directed_at_test.go — platform.DirectedAtBotEvent 的 Discord 实现。
//
// DM 是"消息本身即指向机器人"的典型形态：私聊没有 @ 概念，mentions 数组
// 恒为空，GetMentions 无法表达"这条消息指向机器人"，只能由 DirectedAtBotEvent
// 兜底（与 QQ 群 @机器人 报文不带 mentions 数组同类）。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/discord"
	"github.com/bwmarrin/discordgo"
)

func TestDirectedAtBot_DM(t *testing.T) {
	dm := discord.NewMessageCreateEventWithBot(&discordgo.MessageCreate{
		Message: &discordgo.Message{ID: "1001", ChannelID: "ch1", Content: "hi"},
	}, "bot1")

	de, ok := dm.(platform.DirectedAtBotEvent)
	if !ok {
		t.Fatal("Discord 私聊事件应实现 platform.DirectedAtBotEvent")
	}
	if !de.DirectedAtBot() {
		t.Error("私聊 DM 事件 DirectedAtBot 应为 true")
	}
	if !platform.MentionedBot(dm) {
		t.Error("私聊 DM 消息应被 platform.MentionedBot 判定为指向机器人")
	}
}

func TestDirectedAtBot_GuildFalse(t *testing.T) {
	guild := discord.NewMessageCreateEventWithBot(&discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID: "1002", ChannelID: "ch2", GuildID: "g1", Content: "hi",
		},
	}, "bot1")

	de, ok := guild.(platform.DirectedAtBotEvent)
	if !ok {
		t.Fatal("Discord 事件应实现 platform.DirectedAtBotEvent")
	}
	if de.DirectedAtBot() {
		t.Error("群/服务器消息 DirectedAtBot 应为 false")
	}
	if platform.MentionedBot(guild) {
		t.Error("未 @ 机器人的群消息不应被判定为指向机器人")
	}
}
