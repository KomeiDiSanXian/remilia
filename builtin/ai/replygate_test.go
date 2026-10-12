// replygate_test.go — 群自主发言接话策略（装配侧）测试。
package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// newReplyGateContext 构造群聊自主消息上下文，可注入 bot 显示名。
func newReplyGateContext(content, botName string) *eventctx.Context {
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, content,
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)
	if botName != "" {
		ctx.SetBotName(botName)
	}
	return ctx
}

// newReplyGatePlugin smart/judge 策略插件。无 messagelog：冷静期/复读判定
// 退化为不启用（词表层独立可测）；概率固定为 1，便于断言"允许"分支。
func newReplyGatePlugin(policies *groupPolicyManager) *Plugin {
	return &Plugin{
		cfg: &config.Config{
			GroupReplyPolicy:   config.GroupReplySmart,
			GroupReplyChance:   1, // 概率层恒命中
			GroupReplyCooldown: time.Minute,
		},
		sm:            session.NewSessionManager(100, 20, time.Hour, nil),
		groupPolicies: policies,
		replyGate:     initReplyGate(),
	}
}

// TestAutonomousReplyJudgeMode 无 messagelog 时 judge 档的行为契约：
//   - TierReject/TierReply 不产生判定调用（单飞闸的 busy 标志保持空闲）；
//   - 弱信号保留消息：mock provider 输出 silent → 拒绝进入对话。
func TestAutonomousReplyJudgeMode(t *testing.T) {
	p := newReplyGatePlugin(nil)
	p.cfg.GroupReplyPolicy = config.GroupReplyJudge
	called := 0
	p.prov = &mockProvider{
		chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
			called++
			return &protocol.ChatResponse{Content: `{"verdict":"silent","reason":"群友互聊"}`}, nil
		},
	}

	denied := func(content, botName string) bool {
		return p.autonomousReplyDenied(newReplyGateContext(content, botName), content)
	}

	//垃圾消息：本地拒绝，无判定调用。
	if !denied("哈哈哈", "") {
		t.Fatal("junk message should be rejected by local filter")
	}
	if called != 0 {
		t.Errorf("local rejects must not invoke the judge LLM, got %d calls", called)
	}
	// 普通消息：judge 调用 1 次，silent → 不回。
	if !denied("大家来说说各自的想法", "") {
		t.Fatal("judge silent verdict should deny idle message")
	}
	if called != 1 {
		t.Errorf("expected exactly 1 judge call, got %d", called)
	}
	// 忙闸之后：第二判定未释放前普通消息直接安静（不等待）。
	p.prov.(*mockProvider).chatFn = func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
		return &protocol.ChatResponse{Content: `{"verdict":"reply"}`}, nil
	}
	if !p.replyGate.judge.tryAcquire() {
		t.Fatal("judge gate should be idle after the released round")
	}
	if denied := p.autonomousReplyDenied(newReplyGateContext("这条没人接得上，需要帮助", ""), "这条没人接得上，需要帮助"); !denied {
		t.Error("busy judge gate must skip idle messages")
	}
	p.replyGate.judge.release()

	// 判定通过：普通消息进入对话。
	if denied("这条没人接得上，需要帮助", "") {
		t.Error("judge reply verdict should let the message through")
	}
}

// TestAutonomousReplyJudgeFallbackOnFailure 判定不可用（LLM 报错）时，
// 概率抽签兜底（chance=1 → 放行），不因判定故障把链路锁死。
func TestAutonomousReplyJudgeFallbackOnFailure(t *testing.T) {
	p := newReplyGatePlugin(nil)
	p.cfg.GroupReplyPolicy = config.GroupReplyJudge
	p.prov = &mockProvider{
		chatFn: func(ctx context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
			return nil, errors.New("boom")
		},
	}
	if p.autonomousReplyDenied(newReplyGateContext("大家帮帮我这个怎么办", ""), "大家帮帮我这个怎么办") {
		t.Error("judge failure must fall back to chance roll (chance=1 → allow)")
	}
}

func TestAutonomousReplySmartGate(t *testing.T) {
	p := newReplyGatePlugin(nil)

	denied := func(content, botName string) bool {
		return p.autonomousReplyDenied(newReplyGateContext(content, botName), content)
	}

	// 强信号：bot 有显示名且正文点名 → 高概率通道（chance=1 → 必回）。
	if denied("小美今天过得好快", "小美") {
		t.Error("bot name mention should trigger reply at chance=1")
	}
	// 强信息量普通消息：tierIdle → 概率 1 → 回。
	if denied("随便聊个啥天气都不错的", "") {
		t.Error("info-rich message should pass at chance=1")
	}
	// 普通信息量大消息：tierIdle → 概率 1 → 回。
	if denied("这家新开的火锅店排队排到马路上去了", "") {
		t.Error("substantive small-talk should pass at chance=1")
	}
	// 垃圾消息：不回。
	for _, junk := range []string{"哈哈哈", "666666", "大家好", "哦", "闭嘴，都别吵了", "？？？"} {
		if !denied(junk, "") {
			t.Errorf("junk message %q should be denied", junk)
		}
	}
}

func TestAutonomousReplyPerGroupFallback(t *testing.T) {
	// 全局 smart；g2 群显式 all → 退档为每条都回。
	p := newReplyGatePlugin(newGroupPolicyManager(nil, ""))
	all := new("all")
	p.groupPolicies.SetGroup("g2", &groupPolicy{ReplyPolicy: all})

	g1 := newReplyGateContext("哈哈哈", "")
	if !p.autonomousReplyDenied(g1, "哈哈哈") {
		t.Error("no group entry (g1 from ctx) → global smart should deny junk")
	}

	// g2 覆盖为 all：构造 g2 的上下文验证不过滤。
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "哈哈哈",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g2", IsGroup: true}),
	)
	ctxG2 := eventctx.NewContextFromEvent(evt, nil)
	if p.autonomousReplyDenied(ctxG2, "哈哈哈") {
		t.Error("per-group reply_policy=all must bypass the smart gate")
	}
}

func TestAutonomousReplyPerGroupChance(t *testing.T) {
	// 群级概率覆盖：全局 0，g3 配 0.9 → g3 大概率不拒普通消息（抽签命中即放行）。
	p := newReplyGatePlugin(newGroupPolicyManager(nil, ""))
	p.cfg.GroupReplyChance = 0 // g1/g2 博弈：普通消息全拒
	smart, _ := config.GroupReplySmart, true
	chance := 0.9
	p.groupPolicies.SetGroup("g3", &groupPolicy{ReplyPolicy: &smart, ReplyChance: &chance})

	// g1（全局 chance=0）：black debug 先行。
	if denied := p.autonomousReplyDenied(newReplyGateContext("大家都说说各自的想法", ""), "大家都说说各自的想法"); !denied {
		t.Error("base chance 0 must deny idle messages (roll never passes)")
	}
	// g3 chance=0.9：抽签大概率命中（不 refusal）。flaky 风险 0.1^N；单次 0.1 概率失败 → 断言宽松。
	evtG3 := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "大家都说说各自的想法",
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g3", IsGroup: true}),
	)
	ctxG3 := eventctx.NewContextFromEvent(evtG3, nil)
	set := p.effectiveGroupReply(ctxG3)
	if set.chance != 0.9 {
		t.Errorf("group chance override expected 0.9, got %v", set.chance)
	}
}

func TestAutonomousReplyPolicyAllBypassesGate(t *testing.T) {
	p := newReplyGatePlugin(nil)
	p.cfg.GroupReplyPolicy = config.GroupReplyAll

	// all（默认）：任何消息都不过滤（向后兼容）。
	if p.autonomousReplyDenied(newReplyGateContext("哈哈哈", ""), "哈哈哈") {
		t.Error("policy=all must bypass the gate")
	}
	// 私聊/频道不受接话策略影响。
	evt := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "哈哈哈",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "u1"}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)
	if p.autonomousReplyDenied(ctx, "哈哈哈") {
		t.Error("gate must not apply outside groups")
	}
}

func TestMediumReplyChanceBoundaries(t *testing.T) {
	// 基线 0：中信号仍有 0.2 兜底，避免安静模式被引用唤醒。
	if got := mediumReplyChance(0); got <= 0 || got > 1 {
		t.Errorf("medium chance(base=0) = %v", got)
	}
	if got := mediumReplyChance(0.2); got < 0.2 || got > 1 {
		t.Errorf("medium chance(base=0.2) = %v", got)
	}
	if got := mediumReplyChance(1); got != 1 {
		t.Errorf("medium chance(base=1) = %v, want 1", got)
	}
}
