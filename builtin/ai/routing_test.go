package ai

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/mock"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
)

// atBotEvent 模拟 QQ 群 @机器人 消息（GROUP_AT_MESSAGE_CREATE）：
//
//   - 实现 platform.MentionsEvent 但列表为空——真实报文的 mentions 字段仅
//     GROUP_MESSAGE_CREATE 提供，@机器人 事件不带该字段；
//   - 实现 platform.DirectedAtBotEvent——事件类型本身即代表 @ 机器人。
//
// 两个特征缺一不可：只实现 MentionsEvent 会让 OnMentionedBot 走"扫 @ 列表"
// 的严格分支并恒为假（这正是修复前的线上表现）。
type atBotEvent struct {
	*platform.SyntheticEvent
}

func (atBotEvent) Mentions() []platform.UserInfo { return nil }

func (atBotEvent) DirectedAtBot() bool { return true }

// plainGroupEvent 模拟群内普通消息（未被 @、未点名机器人）。
type plainGroupEvent struct {
	*platform.SyntheticEvent
}

func (plainGroupEvent) Mentions() []platform.UserInfo { return nil }

// routeTestEnv 是挂载了真实 registerHandlers 的引擎环境。
type routeTestEnv struct {
	engine *engine.Engine
	calls  *atomic.Int32
	sender *mock.MockSender
}

// newRouteTestEnv 构造最小可运行的 AI 插件实例（LLM 由 mockProvider 承接，
// 不发网络请求），并在真实引擎上执行 registerHandlers。
//
// calls 记录 LLM 调用次数：这是"消息是否被交给 AI、被交给几次"最直接的信号。
func newRouteTestEnv(t *testing.T, cfg *Config) *routeTestEnv {
	t.Helper()
	var calls atomic.Int32

	p := &Plugin{
		cfg: cfg,
		sm:  NewSessionManager(100, 20, time.Hour, nil),
		reg: NewToolRegistry(),
		// skillReg 供 FSM/技能路径使用；本测试只走对话与子命令路径。
		skillReg: NewSkillRegistry(),
		prov: &mockProvider{chatStreamFn: func(context.Context, *ChatRequest) (<-chan StreamEvent, error) {
			calls.Add(1)
			ch := make(chan StreamEvent, 2)
			ch <- StreamEvent{Type: StreamEventText, Content: "ok"}
			ch <- StreamEvent{Type: StreamEventDone}
			close(ch)
			return ch, nil
		}},
	}

	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	t.Cleanup(func() { _ = eng.Shutdown(context.Background()) })

	sctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Engine: eng})
	t.Cleanup(func() { plugintest.StopSetupContext(sctx) })

	p.registerHandlers(sctx)

	return &routeTestEnv{engine: eng, calls: &calls, sender: mock.NewSender()}
}

// dispatch 经真实引擎同步派发一条事件。
//
// 用 ProcessEventSync 而非 ProcessEvent：后者把 handler 卸载到 ExecPool 并发
// 执行，两个 matcher 同时命中时会因会话回合锁与中断信号互相影响（第二个回合
// 可能直接报"对话正在处理中"或中断第一个回合），LLM 调用次数不再稳定；
// 同步执行使"同一条消息被派发两次"稳定表现为两轮完整对话，断言才具确定性。
//
// 回复仍然异步：经 Context.Dispatcher 排队发送（见 core/context/reply.go），
// 故派发后需 WaitForDispatcher 再检查出站内容。
func (e *routeTestEnv) dispatch(evt platform.Event) {
	e.engine.ProcessEventSync(eventctx.NewContextFromEvent(evt, e.sender))
	e.engine.WaitForDispatcher()
}

// groupMsg 构造群内 @机器人 消息。
func (e *routeTestEnv) groupMsg(content string) platform.Event {
	return atBotEvent{platform.NewSyntheticEvent(
		platform.EventKindGroupMessage, content,
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "U"}),
	)}
}

// plainGroupMsg 构造群内普通（未 @）消息。
func (e *routeTestEnv) plainGroupMsg(content string) platform.Event {
	return plainGroupEvent{platform.NewSyntheticEvent(
		platform.EventKindGroupMessage, content,
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "U"}),
	)}
}

// dmMsg 构造私聊消息。
func (e *routeTestEnv) dmMsg(content string) platform.Event {
	return platform.NewSyntheticEvent(
		platform.EventKindPrivateMessage, content,
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "U"}),
	)
}

func testRoutingConfig() *Config {
	return &Config{
		TriggerCmd:  "/ai",
		AtBot:       true,
		PrivateChat: true,
		MaxDepth:    2,
		APITimeout:  5 * time.Second,
		ToolTimeout: 3 * time.Second,
		MaxHistory:  20,
	}
}

// TestRegisterHandlers_SingleLLMCallPerMessage 固定"同一条消息只产生一轮对话"
// 的路由契约。
//
// 修复前的两种偏差都在此覆盖：
//   - "@机器人 + 触发前缀" 同时命中触发命令 matcher 与 @机器人 matcher，
//     同一条消息被派发两次（两轮 LLM、两条回复）；
//   - "@机器人 普通消息" 两条 matcher 都不命中，静默无响应——与文档承诺的
//     "@机器人 后直接发消息"（at_bot: true，默认）相反。
//
// 命令消息（/help）不得被抢答；AI 自身子命令（/ai reset）仍走子命令路径，
// 由回复文案（而非 LLM）确认只被处理一次——重复派发时它会出现两条相同回复。
func TestRegisterHandlers_SingleLLMCallPerMessage(t *testing.T) {
	for _, tt := range []struct {
		name        string
		content     string
		groupAt     bool   // 群内 @机器人
		groupNone   bool   // 群内普通消息
		dm          bool   // 私聊
		wantCalls   int32  // 期望 LLM 调用次数
		wantReplies int    // 期望出站文本回复条数
		wantReply   string // 非空时断言回复文案
	}{
		{name: "@机器人 + 触发前缀", content: " /ai 你好", groupAt: true, wantCalls: 1, wantReplies: 1},
		{name: "@机器人 普通消息", content: "你好啊", groupAt: true, wantCalls: 1, wantReplies: 1},
		{name: "@机器人 + 触发前缀（前导空白）", content: "   /ai 你好", groupAt: true, wantCalls: 1, wantReplies: 1},
		{name: "@机器人 其他插件命令不被抢答", content: "/help", groupAt: true, wantCalls: 0, wantReplies: 0},
		{
			name: "@机器人 AI 子命令只处理一次", content: "/ai reset", groupAt: true,
			wantCalls: 0, wantReplies: 1, wantReply: sessionClearedText,
		},
		{name: "群内未被 @ 的消息不响应", content: "你好啊", groupNone: true, wantCalls: 0, wantReplies: 0},
		// 触发前缀是显式召唤：命令 matcher 注册在 EventType=""（覆盖所有会话
		// 类型）上，群内不 @ 也响应——这是既有语义，非本次改动引入。
		{name: "群内未被 @ 但带触发前缀", content: "/ai 你好", groupNone: true, wantCalls: 1, wantReplies: 1},
		{name: "私聊触发前缀", content: "/ai 你好", dm: true, wantCalls: 1, wantReplies: 1},
		{name: "私聊普通消息", content: "你好啊", dm: true, wantCalls: 1, wantReplies: 1},
		{name: "私聊命令不被抢答", content: "/help", dm: true, wantCalls: 0, wantReplies: 0},
		{
			name: "私聊 AI 子命令只处理一次", content: "/ai reset", dm: true,
			wantCalls: 0, wantReplies: 1, wantReply: sessionClearedText,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newRouteTestEnv(t, testRoutingConfig())

			var evt platform.Event
			switch {
			case tt.groupAt:
				evt = env.groupMsg(tt.content)
			case tt.groupNone:
				evt = env.plainGroupMsg(tt.content)
			default:
				evt = env.dmMsg(tt.content)
			}
			env.dispatch(evt)

			if got := env.calls.Load(); got != tt.wantCalls {
				t.Errorf("LLM 调用 %d 次，期望 %d 次（正文 %q）", got, tt.wantCalls, tt.content)
			}
			replies := env.textReplies()
			if len(replies) != tt.wantReplies {
				t.Errorf("出站文本回复 %d 条，期望 %d 条：%v", len(replies), tt.wantReplies, replies)
			}
			if tt.wantReply != "" && len(replies) == 1 && replies[0] != tt.wantReply {
				t.Errorf("回复文案 = %q，期望 %q", replies[0], tt.wantReply)
			}
		})
	}
}

// TestRegisterHandlers_GroupAutonomousSingleDispatch 群自主发言模式下，
// 触发前缀消息同样不得被派发两次（自主 matcher 与触发命令 matcher 同时成立）。
func TestRegisterHandlers_GroupAutonomousSingleDispatch(t *testing.T) {
	for _, tt := range []struct {
		name      string
		content   string
		wantCalls int32
	}{
		{name: "普通消息", content: "你好啊", wantCalls: 1},
		{name: "触发前缀消息", content: "/ai 你好", wantCalls: 1},
		{name: "其他插件命令不被抢答", content: "/help", wantCalls: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testRoutingConfig()
			cfg.GroupAutonomous = true
			env := newRouteTestEnv(t, cfg)
			env.dispatch(env.plainGroupMsg(tt.content))

			if got := env.calls.Load(); got != tt.wantCalls {
				t.Errorf("LLM 调用 %d 次，期望 %d 次（正文 %q）", got, tt.wantCalls, tt.content)
			}
		})
	}
}

// TestRegisterHandlers_NonSymbolTriggerSingleDispatch 非符号触发词（如 "帮助"）
// 同样不能重复派发：isCommandMessage 只认 "/" 与 "!" 前缀，仅靠它无法排除
// 这类触发命令，必须由 triggerParses 判据兜住。
func TestRegisterHandlers_NonSymbolTriggerSingleDispatch(t *testing.T) {
	for _, tt := range []struct {
		name      string
		content   string
		wantCalls int32
	}{
		{name: "触发词 + 子命令名（命令 matcher 接管）", content: "帮助 reset", wantCalls: 0},
		{name: "触发词开头的普通正文", content: "帮助我写一段代码", wantCalls: 1},
		{name: "普通消息", content: "你好啊", wantCalls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testRoutingConfig()
			cfg.TriggerCmd = "帮助"
			cfg.GroupAutonomous = true
			env := newRouteTestEnv(t, cfg)
			env.dispatch(env.plainGroupMsg(tt.content))

			if got := env.calls.Load(); got != tt.wantCalls {
				t.Errorf("LLM 调用 %d 次，期望 %d 次（正文 %q）", got, tt.wantCalls, tt.content)
			}
		})
	}
}

// TestTriggerParses 固定 triggerParses 与触发命令 matcher 的判据一致：
// 只有"命令词匹配 + 解析成功"的消息才算已被命令 matcher 接管。
func TestTriggerParses(t *testing.T) {
	p := &Plugin{triggerCmd: "/ai", cfg: &Config{TriggerCmd: "/ai"}}
	cases := []struct {
		content string
		want    bool
	}{
		{"/ai 你好", true},
		{"  /ai 你好", true},
		{"/ai reset", true},
		{"/ai", true},
		{"/ai 你好 don't be rude", true}, // 撇号等分词异常已容忍（见 command 包）
		{"/aiother 你好", false},         // 命令词不匹配
		{"/help", false},
		{"你好 /ai 你好", false}, // 触发词不在首位
		{"", false},
	}
	for _, tt := range cases {
		if got := p.triggerParses(tt.content); got != tt.want {
			t.Errorf("triggerParses(%q) = %v，期望 %v", tt.content, got, tt.want)
		}
	}

	// 未配置触发命令时不存在触发命令 matcher，恒为 false。
	noTrigger := &Plugin{cfg: &Config{}}
	if noTrigger.triggerParses("/ai 你好") {
		t.Error("未配置 trigger_cmd 时 triggerParses 应为 false")
	}
}

// TestRegisterHandlers_FallbackCatchAll 固定 fallback 的接法：
// "无命令匹配时由 AI 兜底回复"——群聊（含未 @）与私聊的全部非命令消息都应答，
// 且同一条消息只产生一轮对话。fallback 归一化为 group_autonomous + private_chat
// 后复用同一组 matcher，不得与 at_bot / 私聊入口重复注册导致双重派发。
func TestRegisterHandlers_FallbackCatchAll(t *testing.T) {
	for _, tt := range []struct {
		name      string
		content   string
		groupAt   bool
		groupNone bool
		dm        bool
		wantCalls int32
	}{
		{name: "群内未被 @ 的普通消息", content: "你好啊", groupNone: true, wantCalls: 1},
		{name: "群内 @机器人 的普通消息", content: "你好啊", groupAt: true, wantCalls: 1},
		{name: "私聊普通消息", content: "你好啊", dm: true, wantCalls: 1},
		{name: "群内其他插件命令不被抢答", content: "/help", groupNone: true, wantCalls: 0},
		{name: "私聊其他插件命令不被抢答", content: "/help", dm: true, wantCalls: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testRoutingConfig()
			// 仅开启 fallback：证明它自身即可覆盖群聊与私聊，不依赖
			// group_autonomous / private_chat。
			cfg.Fallback = true
			cfg.GroupAutonomous = false
			cfg.PrivateChat = false
			env := newRouteTestEnv(t, cfg)

			var evt platform.Event
			switch {
			case tt.groupAt:
				evt = env.groupMsg(tt.content)
			case tt.groupNone:
				evt = env.plainGroupMsg(tt.content)
			default:
				evt = env.dmMsg(tt.content)
			}
			env.dispatch(evt)

			if got := env.calls.Load(); got != tt.wantCalls {
				t.Errorf("LLM 调用 %d 次，期望 %d 次（正文 %q）", got, tt.wantCalls, tt.content)
			}
			// 归一化后同一条消息只命中一个对话入口：出站回复条数应等于调用次数。
			if replies := env.textReplies(); len(replies) != int(tt.wantCalls) {
				t.Errorf("出站文本回复 %d 条，期望 %d 条：%v", len(replies), tt.wantCalls, replies)
			}
		})
	}
}

// textReplies 取出经 sender 发出的纯文本回复。
func (e *routeTestEnv) textReplies() []string {
	var out []string
	for _, c := range e.sender.Snapshot() {
		if c.Method != "Send" {
			continue
		}
		if txt := c.Msg.Text; txt != "" {
			out = append(out, txt)
		}
	}
	return out
}
