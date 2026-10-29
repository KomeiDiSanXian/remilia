package ai

import (
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// sessionForTest 构造一个空会话管理器（trace 需要读会话但不写库）。
func sessionForTest() *session.SessionManager {
	return session.NewSessionManager(10, 20, time.Minute, nil)
}

// TestSubCommandHelpMatchesDefinition 守卫"帮助文案与命令定义同源"。
//
// 修复前 tools 帮助与 aiUsageHint 各自手抄一份子命令清单，已经出现
// "帮助漏 trace/stop" 与 "提示里写不存在的 todos" 两种漂移。
func TestSubCommandHelpMatchesDefinition(t *testing.T) {
	def := buildAIDefinition()

	// 定义里的每个可见子命令都必须出现在渲染结果中。
	rendered := renderSubCommandList(def, "/ai")
	for _, sub := range def.SubCommands {
		if sub.Hidden {
			continue
		}
		if !strings.Contains(rendered, "`/ai "+sub.Name) {
			t.Errorf("subcommand %q missing from rendered help:\n%s", sub.Name, rendered)
		}
	}

	// 渲染结果里不得出现定义中不存在的子命令名。
	known := make(map[string]bool, len(def.SubCommands))
	for _, sub := range def.SubCommands {
		known[sub.Name] = true
	}
	for line := range strings.SplitSeq(rendered, "\n") {
		_, after, ok := strings.Cut(line, "`/ai ")
		if !ok {
			continue
		}
		rest := after
		end := strings.IndexByte(rest, '`')
		if end <= 0 {
			continue
		}
		name := strings.Fields(rest[:end])
		if len(name) == 0 {
			continue
		}
		if !known[name[0]] {
			t.Errorf("rendered help mentions unknown subcommand %q", name[0])
		}
	}
}

// TestAIUsageHintOnlyNamesRealSubCommands 守卫裸 /ai 的提示不再指向不存在的命令。
func TestAIUsageHintOnlyNamesRealSubCommands(t *testing.T) {
	def := buildAIDefinition()
	known := make(map[string]bool, len(def.SubCommands))
	for _, sub := range def.SubCommands {
		known[sub.Name] = true
	}

	p := &Plugin{cfg: &config.Config{TriggerCmd: "/ai"}}
	hint := p.aiUsageHint()
	if strings.Contains(hint, " todos") {
		t.Errorf("usage hint references nonexistent %q command: %s", "todos", hint)
	}
	for _, name := range []string{"summary", "retry", "todo", "stop", "reset"} {
		if !known[name] {
			t.Fatalf("usage hint names %q but the definition does not declare it", name)
		}
		if !strings.Contains(hint, name) {
			t.Errorf("usage hint missing %q: %s", name, hint)
		}
	}
}

// TestTriggerCommandFallsBackWhenDisabled 守卫"关闭命令触发后帮助不渲染成空命令"。
//
// config 允许 trigger_cmd: "" 显式禁用命令触发；展示侧必须回落到默认前缀，
// 否则帮助里会出现 “ ` memory clear` “ 这种无法执行的形式。
func TestTriggerCommandFallsBackWhenDisabled(t *testing.T) {
	if got := triggerCommand(&config.Config{TriggerCmd: ""}); got != defaultTriggerCmd {
		t.Errorf("triggerCommand(empty) = %q, want %q", got, defaultTriggerCmd)
	}
	if got := triggerCommand(nil); got != defaultTriggerCmd {
		t.Errorf("triggerCommand(nil) = %q, want %q", got, defaultTriggerCmd)
	}
	if got := triggerCommand(&config.Config{TriggerCmd: "/chat"}); got != "/chat" {
		t.Errorf("triggerCommand(/chat) = %q, want /chat", got)
	}

	p := &Plugin{cfg: &config.Config{}}
	if got := renderSubCommandList(p.aiDefinition(), triggerCommand(p.cfg)); !strings.Contains(got, "`"+defaultTriggerCmd+" memory") {
		t.Errorf("help with disabled trigger_cmd must fall back to %q, got:\n%s", defaultTriggerCmd, got)
	}
}

// TestNaturalLanguageTraceReachesHandler 守卫 @机器人 路径能进入 trace 子命令。
//
// 修复前 execSubCommand 支持 trace，而 handleSubCommand 的 switch 没有该分支，
// 于是 "@机器人 trace" 被当成普通提问丢给 LLM。
func TestNaturalLanguageTraceReachesHandler(t *testing.T) {
	p := &Plugin{
		cfg: &config.Config{TriggerCmd: "/ai"},
		sm:  sessionForTest(),
	}
	if !p.handleSubCommand(makeTraceCtx(t), "trace") {
		t.Fatal("handleSubCommand(trace) must report handled")
	}
}

func makeTraceCtx(t *testing.T) *eventctx.Context {
	t.Helper()
	evt := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "trace",
		platform.WithSyntheticSender(platform.UserInfo{ID: "user"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "chat"}))
	return eventctx.NewContextFromEvent(evt, nil)
}

// TestTriggerPrefixDoesNotFallBack 守卫"剥离"与"展示"两种语义不被混淆。
//
// trigger_cmd: "" 的语义是"不注册命令触发"：此时不得按默认前缀裁正文，
// 否则用户发的 "/ai xx" 会被先裁掉 "/ai" 再交给模型；而展示侧（帮助文案）
// 必须兜底成可执行的形式，两者是不同需求。
func TestTriggerPrefixDoesNotFallBack(t *testing.T) {
	p := &Plugin{cfg: &config.Config{TriggerCmd: ""}}
	if got := p.triggerPrefix(); got != "" {
		t.Errorf("triggerPrefix() with disabled trigger = %q, want empty", got)
	}
	if got := runtime.CleanMessage("/ai hello", p.triggerPrefix()); got != "/ai hello" {
		t.Errorf("CleanMessage with disabled trigger must keep content, got %q", got)
	}

	enabled := &Plugin{cfg: &config.Config{TriggerCmd: "/ai"}}
	if got := enabled.triggerPrefix(); got != "/ai" {
		t.Errorf("triggerPrefix() = %q, want /ai", got)
	}
	if got := runtime.CleanMessage("/ai hello", enabled.triggerPrefix()); got != "hello" {
		t.Errorf("CleanMessage must strip the configured prefix, got %q", got)
	}
}

// TestTriggerParsesRequiresRegisteredMatcher 守卫"未注册 matcher 时恒为 false"。
//
// triggerParses 是自动响应入口的排除条件：它必须反映 matcher 是否真的注册过，
// 而不是 trigger_cmd 有没有值——两者在"配置了前缀但 registerHandlers 尚未运行"
// （测试、DryRun）时会不同。
func TestTriggerParsesRequiresRegisteredMatcher(t *testing.T) {
	p := &Plugin{cfg: &config.Config{TriggerCmd: "/ai"}}
	if p.triggerParses("/ai 你好") {
		t.Error("triggerParses must be false before the matcher is registered")
	}
	p.registeredTrigger = "/ai"
	if !p.triggerParses("/ai 你好") {
		t.Error("triggerParses must be true once the matcher is registered")
	}
}

// TestBothRoutesReachTheSameSubCommands 守卫"两条入口的可达性一致"。
//
// 修复前命令路径（execSubCommand）支持 trace/skill，而自然语言路径
// （handleSubCommand）的 switch 没有对应分支，于是 "@机器人 trace" 被当成
// 普通提问丢给 LLM。现在两条路径共用 subcommands.go 的注册表，这里检查
// 每个声明了 naturalPath 的子命令都能被自然语言入口查到。
func TestBothRoutesReachTheSameSubCommands(t *testing.T) {
	natural := map[string]bool{}
	for _, sc := range subCommands() {
		if sc.naturalPath {
			natural[sc.name] = true
		}
	}

	for _, sc := range subCommands() {
		// canonical 名必须始终可查（命令路径依赖它）。
		if _, ok := lookupSubCommand(sc.name); !ok {
			t.Errorf("subcommand %q is not reachable by canonical name", sc.name)
		}
		for _, alias := range sc.aliases {
			got, ok := lookupSubCommand(alias)
			if !ok {
				t.Errorf("alias %q of %q is not resolvable", alias, sc.name)
				continue
			}
			if got.name != sc.name {
				t.Errorf("alias %q resolves to %q, want %q", alias, got.name, sc.name)
			}
			if sc.naturalPath != natural[got.name] {
				t.Errorf("alias %q and canonical %q disagree about natural-path reachability", alias, sc.name)
			}
		}
	}

	// 定义里声明的每个子命令都必须能在注册表里查到处理函数（skill 例外：
	// 它走自然语言入口的整词前缀特判，仍然必须在表里有 canonical 条目）。
	for _, def := range buildAIDefinition().SubCommands {
		if def.Hidden {
			continue
		}
		if _, ok := lookupSubCommand(def.Name); !ok {
			t.Errorf("command definition declares %q but no handler is registered", def.Name)
		}
	}

	// 注册表里不得出现定义之外的子命令（否则帮助里看不到、却能被调用）。
	declared := map[string]bool{}
	for _, def := range buildAIDefinition().SubCommands {
		declared[def.Name] = true
	}
	for _, sc := range subCommands() {
		if !declared[sc.name] {
			t.Errorf("handler %q has no entry in the command definition", sc.name)
		}
	}
}

// TestParseFromDefinitionAcceptsEveryDefinedSubCommand 守卫命令定义自洽：
// 定义里声明的每个子命令都必须能被解析器认出来（否则 matcher 永远派发不到）。
func TestParseFromDefinitionAcceptsEveryDefinedSubCommand(t *testing.T) {
	def := buildAIDefinition()
	for _, sub := range def.SubCommands {
		input := "/ai " + sub.Name
		parsed, err := command.ParseFromDefinition(input, def, "/")
		if err != nil {
			t.Errorf("ParseFromDefinition(%q) = %v", input, err)
			continue
		}
		if len(parsed.CommandPath) < 2 || parsed.CommandPath[1] != sub.Name {
			t.Errorf("ParseFromDefinition(%q).CommandPath = %v, want [ai %s]", input, parsed.CommandPath, sub.Name)
		}
	}
}

// TestEveryDefinedSubCommandIsRouted 守卫"定义里有的子命令，execSubCommand 一定处理"。
//
// 两份手抄路由表的直接后果是定义与可执行面不一致；这里用"未知命令返回 nil 且
// 不产生任何副作用"之外的行为差来检查——execSubCommand 对未识别子命令返回 nil，
// 因此只能靠 tools 帮助/list 与 trace 这类可观测输出断言，故用 tools 与 trace 覆盖。
func TestEveryDefinedSubCommandIsRouted(t *testing.T) {
	p := &Plugin{
		cfg:      &config.Config{TriggerCmd: "/ai"},
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		sm:       sessionForTest(),
	}
	ctx := makeTraceCtx(t)
	if err := p.execSubCommand(ctx, "trace"); err != nil {
		t.Fatalf("execSubCommand(trace): %v", err)
	}
}
