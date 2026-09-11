package ai

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// newGuardTestPlugin 构造一个最小可运行的 AI 插件实例：LLM 由 mockProvider
// 承接（不发网络请求），仅用于观测"消息是否真的交给了 AI"。
//
// calls 记录 LLM 被调用的次数——这是判断"消息被静默丢弃"最直接的信号。
func newGuardTestPlugin(t *testing.T, trigger string) (*Plugin, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	p := &Plugin{
		cfg: &Config{
			TriggerCmd: trigger, MaxDepth: 2, APITimeout: 5 * time.Second,
			ToolTimeout: 3 * time.Second, MaxHistory: 20,
		},
		sm:         NewSessionManager(100, 20, time.Hour, nil),
		reg:        NewToolRegistry(),
		skillReg:   NewSkillRegistry(),
		triggerCmd: trigger,
		prov: &mockProvider{chatStreamFn: func(context.Context, *ChatRequest) (<-chan StreamEvent, error) {
			calls.Add(1)
			ch := make(chan StreamEvent, 2)
			ch <- StreamEvent{Type: StreamEventText, Content: "ok"}
			ch <- StreamEvent{Type: StreamEventDone}
			close(ch)
			return ch, nil
		}},
	}
	return p, &calls
}

// dispatch 模拟引擎的派发结果后调用 handleAI。
//
// parse 为 true 时先跑 registerHandlers 注册的真实解析规则（/ai 命令路径，
// GetParsedCommand 非空）；为 false 时模拟 @机器人+触发前缀 / 私聊兜底路径
// （规则只做前缀过滤，不产生 Parsed）。
func dispatch(t *testing.T, p *Plugin, content string, parse bool) error {
	t.Helper()
	evt := platform.NewSyntheticEvent("c2c", content,
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "U"}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	if parse {
		parsed, err := command.ParseFromDefinition(content, buildAIDefinition(), "/")
		if err != nil {
			t.Fatalf("解析失败：%q: %v", content, err)
		}
		ctx.SetParsedCommand(parsed)
	}
	return p.handleAI(ctx)
}

// TestHandleAI_ExplicitTriggerWithCommandLikeBody 回归测试：显式点名 AI 时，
// 正文以命令样式开头（/tmp、#tag、!help）不应被当成“其他插件的命令”丢弃。
//
// 旧行为：handleAI 在 parsed == nil 路径上无条件执行
// isCommandMessage(content) → return nil。QQ 群 @机器人 报文恰好只走该路径
// （GROUP_AT_MESSAGE_CREATE 既无 mentions 数组、也无 Parsed），于是
// “@机器人 /ai /tmp 是什么” 清洗后得到的 “/tmp 是什么” 被当作外部命令丢弃：
// 既无 AI 回复，也无任何提示，用户侧只看到“发了消息没反应”。
//
// 两条路径分别对应生产环境：
//   - viaCommand=true：/ai 命令 matcher（OnParseCommand 产生 Parsed）。
//     该路径旧代码已直接进入对话，此处是“不得退化”的保护性断言。
//   - viaCommand=false：QQ 群 @机器人 + 触发前缀 matcher（仅前缀过滤）。
//     这是上述线上缺陷的真实路径。
func TestHandleAI_ExplicitTriggerWithCommandLikeBody(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
	}{
		{"/ai 后接绝对路径", "/ai /tmp 目录是干什么的"},
		{"/ai 后接井号标签", "/ai #tag 说明一下这个标记"},
		{"/ai 后接感叹号命令", "/ai !help 的输出是什么意思"},
	} {
		for _, viaCommand := range []bool{true, false} {
			t.Run(tt.name, func(t *testing.T) {
				p, calls := newGuardTestPlugin(t, "/ai")
				if err := dispatch(t, p, tt.content, viaCommand); err != nil {
					t.Fatalf("handleAI 返回错误：%v", err)
				}
				if calls.Load() == 0 {
					t.Errorf("正文 %q（命令路径=%v）未交给 AI，被静默丢弃", tt.content, viaCommand)
				}
			})
		}
	}
}

// TestHandleAI_NonExplicitCommandLikeBodyStillSkipped 确认修复没有放宽
// 兜底路径的门槛：自主发言/私聊自动响应下，其他插件的命令（不带 AI 触发
// 前缀）仍然不得被 AI 抢答。
func TestHandleAI_NonExplicitCommandLikeBodyStillSkipped(t *testing.T) {
	for _, content := range []string{"/help", "/ping now", "!!admin status", "/aiother"} {
		t.Run(content, func(t *testing.T) {
			// 无触发前缀：模拟私聊/自主发言兜底路径。
			p, calls := newGuardTestPlugin(t, "")
			if err := dispatch(t, p, content, false); err != nil {
				t.Fatalf("handleAI 返回错误：%v", err)
			}
			if calls.Load() != 0 {
				t.Errorf("其他插件的命令 %q 被 AI 抢答（LLM 调用 %d 次）", content, calls.Load())
			}
		})
	}
}

// TestHandleAI_SubCommandStillWinsOverPassthrough 确认放行正文不会让
// 子命令识别退化：/ai reset 等仍走子命令，不进入对话。
func TestHandleAI_SubCommandStillWinsOverPassthrough(t *testing.T) {
	p, calls := newGuardTestPlugin(t, "/ai")
	if err := dispatch(t, p, "/ai reset", true); err != nil {
		t.Fatalf("handleAI 返回错误：%v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("子命令 /ai reset 不应调用 LLM（调用了 %d 次）", calls.Load())
	}
}

// TestHasTriggerPrefix 覆盖触发前缀判定：前缀可带符号或为普通词，
// 前导空白不影响判定，与 cleanMessage 的剥离语义保持一致。
func TestHasTriggerPrefix(t *testing.T) {
	for _, tt := range []struct {
		trigger string
		content string
		want    bool
	}{
		{"/ai", "/ai /tmp", true},
		{"/ai", "  /ai 正文", true},
		{"/ai", " /ai", true},
		{"/ai", "/aiother", true}, // 与 OnCommand 的 HasPrefix 语义一致
		{"/ai", "你好 /ai", false},
		{"/ai", "/help", false},
		{"", "/ai 正文", false},
		{"帮助", "帮助 我一下", true},
		{"帮助", "需要帮助 吗", false},
	} {
		p := &Plugin{triggerCmd: tt.trigger}
		if got := p.hasTriggerPrefix(tt.content); got != tt.want {
			t.Errorf("hasTriggerPrefix(%q) 触发词=%q = %v, 期望 %v",
				tt.content, tt.trigger, got, tt.want)
		}
	}
}
