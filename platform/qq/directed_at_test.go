package qq_test

// directed_at_test.go — QQ "@机器人"事件的平台级 @ 判定。
//
// 覆盖 GROUP_AT_MESSAGE_CREATE / AT_MESSAGE_CREATE（事件类型即 @机器人，
// payload 无 mentions 数组、正文占位符被替换为空格）与不得误标的两类事件
// （全量群消息、私聊）。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

func TestNewEvent_DirectedAtBot(t *testing.T) {
	cases := []struct {
		name   string
		evType dto.EventType
		detail map[string]any
		want   bool
	}{
		{
			name:   "群 @机器人",
			evType: dto.GroupAtMessageCreate,
			detail: map[string]any{
				"id":           "msg_at001",
				"content":      " /ai 你好", // @ 占位符被服务端替换为空格
				"group_openid": "group001",
				"author":       map[string]any{"member_openid": "member_bob"},
				"timestamp":    "2026-09-11T10:00:00+08:00",
			},
			want: true,
		},
		{
			name:   "频道 @机器人",
			evType: dto.AtMessageCreate,
			detail: map[string]any{
				"id":         "msg_at002",
				"content":    " /ai 你好",
				"channel_id": "chan001",
				"guild_id":   "guild001",
				"author":     map[string]any{"id": "u1", "username": "Bob"},
			},
			want: true,
		},
		{
			name:   "全量群消息（@ 列表由 payload 提供）",
			evType: dto.GroupMessageCreate,
			detail: map[string]any{
				"id":           "msg_g001",
				"content":      "hello everyone",
				"group_openid": "group001",
				"author":       map[string]any{"member_openid": "member_bob"},
			},
			want: false,
		},
		{
			name:   "私聊",
			evType: dto.C2CMessageCreate,
			detail: map[string]any{
				"id":      "msg_c001",
				"content": "你好",
				"author":  map[string]any{"user_openid": "openid_alice"},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := qq.NewEvent(makePayload(tc.evType, tc.detail))

			de, ok := event.(platform.DirectedAtBotEvent)
			if !ok {
				t.Fatal("事件应实现 platform.DirectedAtBotEvent")
			}
			if got := de.DirectedAtBot(); got != tc.want {
				t.Errorf("DirectedAtBot() = %v，期望 %v", got, tc.want)
			}
			// 平台级统一判定必须与之一致——框架与插件都通过它判断 @ 机器人。
			if got := platform.MentionedBot(event); got != tc.want {
				t.Errorf("platform.MentionedBot() = %v，期望 %v", got, tc.want)
			}
		})
	}
}

// TestNewEvent_DirectedAtBotKeepsMentionsEmpty 固定"不伪造 @ 条目"的约定：
// @机器人 事件本就没有 mentions 数组，Mentions() 保持 nil，@ 语义只由
// DirectedAtBotEvent 承载。
//
// 若在此伪造一条自身条目，messagelog 会写入 id 为空的 @ 记录、
// SegmentsMentions 与 Mentions() 也会互相矛盾（段里没有 at 段）。
func TestNewEvent_DirectedAtBotKeepsMentionsEmpty(t *testing.T) {
	for _, evType := range []dto.EventType{dto.GroupAtMessageCreate, dto.AtMessageCreate} {
		detail := map[string]any{
			"id":           "msg_at003",
			"content":      " /ai 你好",
			"group_openid": "group001",
			"channel_id":   "chan001",
			"guild_id":     "guild001",
			"author":       map[string]any{"member_openid": "member_bob", "id": "u1"},
		}
		event := qq.NewEvent(makePayload(evType, detail))
		if mentions := platform.GetMentions(event); len(mentions) != 0 {
			t.Errorf("%s: 期望无 @ 条目，得到 %+v", evType, mentions)
		}
		if !platform.MentionedBot(event) {
			t.Errorf("%s: 仍应判定为 @ 了机器人", evType)
		}
	}
}
