// Package ai context_pipeline_test.go — 动态上下文装配的契约用例。
//
// 锁定节序（运行时 → 群聊 → 记忆 → 历史）、标题渲染，以及两条路径共用的
// 参与条件：空正文章节一律不输出，群聊窗口的开关对两条路径一致。
package ai

import (
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contextTestLogger 构造带群聊历史的 messagelog（热缓存即可）。
func contextTestLogger() *messagelog.Logger {
	l := messagelog.New(20)
	now := time.Now()
	l.Record(messagelog.RecordEntry{ChatID: "g1", UserName: "小明", Content: "服务器方案选型讨论", EventID: "1", Timestamp: now})
	l.Record(messagelog.RecordEntry{ChatID: "g1", UserName: "小红", Content: "服务器方案还在吗", EventID: "2", Timestamp: now.Add(time.Second)})
	l.Record(messagelog.RecordEntry{ChatID: "g1", UserName: "小刚", Content: "咖啡机好像坏了", EventID: "3", Timestamp: now.Add(2 * time.Second)})
	return l
}

func contextTestContext() *eventctx.Context {
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "服务器方案还在吗",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "小明"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	return eventctx.NewContextFromEvent(evt, nil)
}

func contextTestSession() *session.Session {
	s := &session.Session{ID: "s1", UserID: "u1", ChatID: "g1"}
	s.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: "服务器方案还在吗"}}
	return s
}

// sectionOrder 返回各标题在文本中的出现顺序（仅包含出现的标题）。
func sectionOrder(text string, headers ...string) []string {
	var got []string
	for _, h := range headers {
		if strings.Contains(text, "===== "+h+" =====") {
			got = append(got, h)
		}
	}
	return got
}

func TestDynamicContextSectionOrder(t *testing.T) {
	m := newTestMemoryStore(t, 50, time.Minute)
	m.Add(userScope("u1"), "用户关心服务器方案选型")
	m.Add(groupScope("g1"), "本群讨论过服务器方案")

	mkPlugin := func(window int) *Plugin {
		return &Plugin{
			cfg: &config.Config{
				ContextWindow:         window,
				IncludeRuntimeContext: true,
				ContextFields:         []string{"user_is_bot"},
				ContextGroupMessages:  1,
				ContextRAGMessages:    2,
				ContextRAGDays:        7,
				ContextRAGCandidates:  500,
				ContextRAGInjectMax:   3,
				MemoryInjectMax:       2,
			},
			history: contextTestLogger(),
			memory:  m,
		}
	}
	const (
		runtime = "运行时上下文"
		group   = "群聊最近消息"
		memory  = "长期记忆"
		rag     = "相关历史消息"
	)
	want := []string{runtime, group, memory, rag}

	t.Run("配置路径", func(t *testing.T) {
		got := mkPlugin(0).buildDynamicContext(contextTestContext(), contextTestSession())
		assert.Equal(t, want, sectionOrder(got, runtime, group, memory, rag), "节序即优先级：%q", got)
	})

	t.Run("预算路径", func(t *testing.T) {
		got := mkPlugin(100000).buildDynamicContext(contextTestContext(), contextTestSession())
		assert.Equal(t, want, sectionOrder(got, runtime, group, memory, rag), "节序即优先级：%q", got)
	})
}

// TestRuntimeSectionEmptyDroppedOnBothPaths 冻结统一后的语义：运行时字段全部
// 被白名单排除时，配置路径与预算路径都不产生"只有标题的空节"。
func TestRuntimeSectionEmptyDroppedOnBothPaths(t *testing.T) {
	// 运行时字段全部被白名单排除 → 正文为空。
	cfg := config.Config{
		IncludeRuntimeContext: true,
		ContextFields:         []string{"nope"},
	}

	t.Run("配置路径不产生空节", func(t *testing.T) {
		p := &Plugin{cfg: &cfg}
		assert.Empty(t, p.buildDynamicContext(contextTestContext(), nil),
			"正文为空时不得只输出标题")
	})

	t.Run("预算路径不产生空节", func(t *testing.T) {
		budgetCfg := cfg
		budgetCfg.ContextWindow = 100000
		budgetCfg.ContextGroupMessages = 1
		p := &Plugin{cfg: &budgetCfg, history: contextTestLogger()}
		got := p.buildDynamicContext(contextTestContext(), contextTestSession())

		require.Contains(t, got, "群聊最近消息")
		assert.NotContains(t, got, "运行时上下文", "空正文不产生节：%q", got)
	})
}

// TestGroupSectionHonorsSwitchOnBothPaths 冻结统一后的语义：
// context_group_messages 对配置路径与预算路径一致——0 关闭，>0 纳入。
func TestGroupSectionHonorsSwitchOnBothPaths(t *testing.T) {
	t.Run("关闭时两条路径都不纳入", func(t *testing.T) {
		cfg := config.Config{ContextGroupMessages: 0}
		plainCfg := cfg
		budgetCfg := cfg
		budgetCfg.ContextWindow = 100000

		assert.Empty(t, (&Plugin{cfg: &plainCfg, history: contextTestLogger()}).
			buildDynamicContext(contextTestContext(), contextTestSession()))
		assert.Empty(t, (&Plugin{cfg: &budgetCfg, history: contextTestLogger()}).
			buildDynamicContext(contextTestContext(), contextTestSession()))
	})

	t.Run("开启时两条路径都纳入", func(t *testing.T) {
		cfg := config.Config{ContextGroupMessages: 2}
		plainCfg := cfg
		budgetCfg := cfg
		budgetCfg.ContextWindow = 100000

		assert.Contains(t, (&Plugin{cfg: &plainCfg, history: contextTestLogger()}).
			buildDynamicContext(contextTestContext(), contextTestSession()), "===== 群聊最近消息 =====")
		assert.Contains(t, (&Plugin{cfg: &budgetCfg, history: contextTestLogger()}).
			buildDynamicContext(contextTestContext(), contextTestSession()), "===== 群聊最近消息 =====")
	})
}
