package context

// rules_mentioned_test.go — OnMentionedBot 的跨平台 @ 判定。
//
// 覆盖三类事件：
//
//   - 结构化 @ 列表命中机器人自身（IsSelf）；
//   - 平台实现了 MentionsEvent 但列表为空，由"事件类型即 @机器人"标记兜底
//     （QQ 群 @机器人 报文的真实形态：无 mentions 字段、正文占位符被替换为空格）；
//   - 平台完全无法感知 @ 列表（既有宽松语义）。

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// directedEvent 实现 platform.Event + MentionsEvent + DirectedAtBotEvent，
// 用于模拟"@ 列表为空但事件类型即 @机器人"的平台事件。
type directedEvent struct {
	mentions   []platform.UserInfo
	directedAt bool
}

func (e *directedEvent) Platform() string         { return "test" }
func (e *directedEvent) Kind() platform.EventKind { return platform.EventKindGroupMessage }
func (e *directedEvent) ID() string               { return "evt-directed" }
func (e *directedEvent) Timestamp() time.Time     { return time.Time{} }
func (e *directedEvent) Chat() platform.ChatInfo {
	return platform.ChatInfo{ID: "g1", IsGroup: true}
}
func (e *directedEvent) Sender() platform.UserInfo { return platform.UserInfo{ID: "u1"} }

func (e *directedEvent) Segments() []platform.Segment {
	return []platform.Segment{{Type: platform.SegmentText, Text: "hello"}}
}

func (e *directedEvent) Mentions() []platform.UserInfo { return e.mentions }

func (e *directedEvent) DirectedAtBot() bool { return e.directedAt }

// TestOnMentionedBot_DirectedAtBotFallback 固定"事件类型即 @机器人"的兜底：
// QQ 群 @机器人 事件不带 mentions 数组，平台只能靠 DirectedAtBotEvent 声明。
// 仅扫 @ 列表会让这类消息被判为"未 @ 机器人"——表现为 at_bot 触发失效、
// 群策略要求 @ 时用户 @机器人 的消息被静默丢弃。
func TestOnMentionedBot_DirectedAtBotFallback(t *testing.T) {
	cases := []struct {
		name     string
		event    platform.Event
		expected bool
	}{
		{
			name:     "空 @ 列表 + 指向机器人标记",
			event:    &directedEvent{directedAt: true},
			expected: true,
		},
		{
			name:     "空 @ 列表、无标记",
			event:    &directedEvent{},
			expected: false,
		},
		{
			name: "结构化 @ 命中机器人自身",
			event: &directedEvent{mentions: []platform.UserInfo{
				{ID: "u2"},
				{ID: "bot", IsSelf: true},
			}},
			expected: true,
		},
		{
			name: "结构化 @ 只有他人",
			event: &directedEvent{mentions: []platform.UserInfo{
				{ID: "u2"},
				{ID: "u3"},
			}},
			expected: false,
		},
		{
			name: "平台无法感知 @ 列表 → 放行（既有语义）",
			event: &mockEvent{
				kind: platform.EventKindGroupMessage, content: "hello",
				platform: "terminal", chat: platform.ChatInfo{ID: "c1"},
			},
			expected: true,
		},
	}

	rule := OnMentionedBot()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := NewContextFromEvent(tc.event, nil)
			if got := rule(ctx); got != tc.expected {
				t.Errorf("OnMentionedBot() = %v，期望 %v", got, tc.expected)
			}
			// 实现了 MentionsEvent 的事件上，规则结果必须与 platform.MentionedBot
			// 一致（"无法感知 @ 列表即放行"的宽松语义只属于规则自身）。
			if _, ok := tc.event.(platform.MentionsEvent); ok {
				if got := platform.MentionedBot(tc.event); got != tc.expected {
					t.Errorf("platform.MentionedBot() = %v，期望 %v", got, tc.expected)
				}
			}
		})
	}

	if OnMentionedBot()(NewContextFromEvent(nil, nil)) {
		t.Error("无平台事件时 OnMentionedBot 应为 false")
	}
}
