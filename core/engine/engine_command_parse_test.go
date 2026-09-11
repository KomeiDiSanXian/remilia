package engine

// engine_command_parse_test.go — 命令派发链路回归测试。
//
// 覆盖"触发命令 + 自然语言正文"这一真实聊天场景：命令 matcher 的派发不仅
// 依赖 commandIndex 命中（取首个空白词），还依赖 OnParseCommand 规则解析成功。
// 解析失败时 matcher 静默返回 false，消息不会到达 handler——表现为插件
// "完全没有反应"。
//
// 注册方式与插件层一致（plugin.SetupContext.OnCommandDef）：
// OnCommand(trigger, OnParseCommand(def)) + 直接绑定 handler。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// aiLikeDefinition 构造与 builtin/ai 同形的命令定义（根命令 + 子命令）。
func aiLikeDefinition() *command.Definition {
	return &command.Definition{
		Name: "ai",
		SubCommands: []*command.Definition{
			{Name: "reset"},
			{Name: "status"},
		},
	}
}

// registerTrackingHandler 复刻插件层注册：解析规则 + 直接 handler。
// 返回记录到的 CommandPath（handler 未被调用时返回 nil）。
func registerTrackingHandler(t *testing.T, eng *Engine, def *command.Definition) *[]string {
	t.Helper()
	var gotPath []string
	m := eng.OnCommand("", "/"+def.Name, ctx.OnParseCommand(def))
	m.SetDefinition(def)
	m.Handle(func(c *ctx.Context) error {
		if parsed := c.GetParsedCommand(); parsed != nil {
			gotPath = append([]string(nil), parsed.CommandPath...)
		}
		return nil
	})
	return &gotPath
}

// TestEngine_CommandDispatch_NaturalLanguageWithApostrophe 回归测试：
// 正文含英文撇号（以及前导空格）时，命令 handler 仍必须被调用。
//
// 复现的线上报文（2026-09-11，QQ 群 GROUP_AT_MESSAGE_CREATE）：
//
//	content = " /ai Generate a self-contained, valid SVG of a pelican riding
//	           a bicycle. ... the pelican's legs must move in sync with the pedals."
//
// 此前单个英文撇号被判为未闭合引号 → ParseFromDefinition 报错 →
// OnParseCommand 规则不匹配 → handler 未被调用且无任何日志，AI 静默不响应。
func TestEngine_CommandDispatch_NaturalLanguageWithApostrophe(t *testing.T) {
	eng := newEngineForTest(t)
	gotPath := registerTrackingHandler(t, eng, aiLikeDefinition())

	content := " /ai Generate a self-contained, valid SVG of a pelican riding a bicycle. " +
		"The bicycle wheels must spin, the pedals must rotate, and the pelican's legs " +
		"must move in sync with the pedals. Do not use any JavaScript or external CSS."
	evt := newTestPlatformEventWithContent(platform.EventKindGroupMessage, content)

	eng.ProcessEvent(ctx.NewContextFromEvent(evt, nil))

	assert.Equal(t, []string{"ai"}, *gotPath,
		"命令 handler 未被调用（正文中的撇号让解析失败，消息被静默丢弃）")
}

// TestEngine_CommandDispatch_NaturalLanguageWithTrailingBackslash
// 正文以反斜杠结尾（如 Windows 路径）时命令仍必须派发。
func TestEngine_CommandDispatch_NaturalLanguageWithTrailingBackslash(t *testing.T) {
	eng := newEngineForTest(t)
	gotPath := registerTrackingHandler(t, eng, aiLikeDefinition())

	evt := newTestPlatformEventWithContent(platform.EventKindGroupMessage, `/ai open C:\Users\`)
	eng.ProcessEvent(ctx.NewContextFromEvent(evt, nil))

	assert.Equal(t, []string{"ai"}, *gotPath)
}

// TestEngine_CommandDispatch_SubCommandStillRouted 确保容错不影响子命令路由。
func TestEngine_CommandDispatch_SubCommandStillRouted(t *testing.T) {
	eng := newEngineForTest(t)
	gotPath := registerTrackingHandler(t, eng, aiLikeDefinition())

	evt := newTestPlatformEventWithContent(platform.EventKindGroupMessage, " /ai reset don't panic")
	eng.ProcessEvent(ctx.NewContextFromEvent(evt, nil))

	assert.Equal(t, []string{"ai", "reset"}, *gotPath)
}
