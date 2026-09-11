package ai

import (
	"strings"
	"testing"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// TestTriggerRuleAcceptsNaturalLanguageWithApostrophe 回归测试：触发命令后跟
// 自然语言正文时，解析规则必须匹配，否则消息会被静默丢弃（AI 完全不响应）。
//
// 复现的线上报文（2026-09-11，QQ 群 GROUP_AT_MESSAGE_CREATE）：
//
//	content = " /ai Generate a self-contained, valid SVG of a pelican riding
//	           a bicycle. ... the pelican's legs must move in sync with the pedals."
//
// 两个特征都会影响解析：
//   - content 以空格开头（QQ 已把 @机器人 替换为空格）
//   - 正文含单个英文撇号（pelican's），此前被命令分词器判为未闭合引号，
//     导致 OnParseCommand 规则失败 → registerHandlers 注册的 /ai matcher
//     不派发 → AI 无任何响应。
func TestTriggerRuleAcceptsNaturalLanguageWithApostrophe(t *testing.T) {
	content := " /ai Generate a self-contained, valid SVG of a pelican riding a bicycle. " +
		"The bicycle wheels must spin, the pedals must rotate, and the pelican's legs " +
		"must move in sync with the pedals. Do not use any JavaScript or external CSS."

	ctx := makeContext(content)

	// registerHandlers 注册 /ai 时使用的解析规则（见 plugin.RegisterCommandDefWithPrefix）。
	rule := eventctx.OnParseCommand(buildAIDefinition())
	if !rule(ctx) {
		t.Fatalf("触发命令解析规则未匹配，消息会被静默丢弃：%q", content)
	}

	parsed := ctx.GetParsedCommand()
	if parsed == nil {
		t.Fatal("解析结果为空，handler 无法区分命令路径与对话路径")
	}
	if len(parsed.CommandPath) != 1 || parsed.CommandPath[0] != "ai" {
		t.Fatalf("CommandPath = %v, 期望 [ai]（不应误判为子命令）", parsed.CommandPath)
	}

	// 清洗后应得到完整正文（命令前缀与 @ 标记被移除），并交由 handleAIChat 处理。
	p := &Plugin{cfg: &Config{TriggerCmd: "/ai"}, triggerCmd: "/ai"}
	cleaned := p.cleanMessage(content)
	if !strings.HasPrefix(cleaned, "Generate a self-contained, valid SVG") {
		t.Errorf("清洗后正文异常：%q", cleaned)
	}
	if !strings.Contains(cleaned, "pelican's legs") {
		t.Errorf("清洗后正文丢失撇号内容：%q", cleaned)
	}
}

// TestTriggerRuleKeepsSubCommands 确保普通子命令与含撇号的子命令正文都能解析。
func TestTriggerRuleKeepsSubCommands(t *testing.T) {
	rule := eventctx.OnParseCommand(buildAIDefinition())

	for _, tt := range []struct {
		content string
		want    []string
	}{
		{"/ai status", []string{"ai", "status"}},
		{" /ai reset", []string{"ai", "reset"}},
		{"/ai summary it's fine", []string{"ai", "summary"}},
		{"/ai group set prompt don't be rude", []string{"ai", "group", "set", "prompt"}},
	} {
		ctx := makeContext(tt.content)
		if !rule(ctx) {
			t.Errorf("规则未匹配：%q", tt.content)
			continue
		}
		parsed := ctx.GetParsedCommand()
		if parsed == nil {
			t.Errorf("解析结果为空：%q", tt.content)
			continue
		}
		if len(parsed.CommandPath) != len(tt.want) {
			t.Errorf("%q: CommandPath = %v, 期望 %v", tt.content, parsed.CommandPath, tt.want)
			continue
		}
		for i, w := range tt.want {
			if parsed.CommandPath[i] != w {
				t.Errorf("%q: CommandPath[%d] = %q, 期望 %q", tt.content, i, parsed.CommandPath[i], w)
			}
		}
	}
}
