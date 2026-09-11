package platform_test

// mentioned_bot_test.go — platform.MentionedBot 的派生顺序。
//
// 该函数是"本条消息是否 @ 了机器人"的跨平台唯一口径：结构化 @ 列表
// （IsSelf）优先，平台级"事件类型即 @机器人"标记兜底。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// mentionedStub 实现 platform.Event（内嵌 *SyntheticEvent，未声明任何
// @ 相关能力）。
type mentionedStub struct {
	*platform.SyntheticEvent
}

// stubWithMentions 额外实现 platform.MentionsEvent。
type stubWithMentions struct {
	mentionedStub
	mentions []platform.UserInfo
}

func (s stubWithMentions) Mentions() []platform.UserInfo { return s.mentions }

// stubDirected 实现 MentionsEvent（列表为空，模拟 QQ @机器人 报文）+
// DirectedAtBotEvent。
type stubDirected struct {
	mentionedStub
}

func (stubDirected) Mentions() []platform.UserInfo { return nil }

func (stubDirected) DirectedAtBot() bool { return true }

func newMentionedEvent() platform.Event {
	return &mentionedStub{SyntheticEvent: platform.NewSyntheticEvent(
		platform.EventKindGroupMessage, "hi",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)}
}

func TestMentionedBot(t *testing.T) {
	cases := []struct {
		name  string
		event platform.Event
		want  bool
	}{
		{
			name:  "nil 事件",
			event: nil,
			want:  false,
		},
		{
			name:  "无 @ 信息的事件",
			event: newMentionedEvent(),
			want:  false,
		},
		{
			name: "结构化 @ 命中自身",
			event: &stubWithMentions{
				mentionedStub: mentionedStub{platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi")},
				mentions:      []platform.UserInfo{{ID: "u2"}, {ID: "bot", IsSelf: true}},
			},
			want: true,
		},
		{
			name: "结构化 @ 未命中自身",
			event: &stubWithMentions{
				mentionedStub: mentionedStub{platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi")},
				mentions:      []platform.UserInfo{{ID: "u2"}},
			},
			want: false,
		},
		{
			name: "空 @ 列表 + 指向机器人标记",
			event: &stubDirected{mentionedStub{platform.NewSyntheticEvent(
				platform.EventKindGroupMessage, "hi")}},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := platform.MentionedBot(tc.event); got != tc.want {
				t.Errorf("MentionedBot() = %v，期望 %v", got, tc.want)
			}
		})
	}
}
