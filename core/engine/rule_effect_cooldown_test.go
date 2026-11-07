package engine

// rule_effect_cooldown_test.go — 冷却副作用的延迟提交语义
//
// 语义：OnCooldown 的写入通过 DeferRuleEffect 延迟到「当前 matcher 的全部
// 规则通过」之后才提交——规则链中途失败、或事件最终命中别的 matcher 时，
// 冷却不得被白白消耗。

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

var cdTestSeq atomic.Int64

// TestEngine_CooldownDeferredCommit 验证规则通过但所在 matcher 未命中时
// 不消耗冷却，只有确认命中的 matcher 才提交冷却写入。
func TestEngine_CooldownDeferredCommit(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)
	// 每轮唯一 key：cooldownStore 是全局存储，固定 key 在 -count>1 时会跨轮残留。
	cdKey := fmt.Sprintf("cd_deferred_user_%d", cdTestSeq.Add(1))
	key := func(*context.Context) string { return cdKey }

	// matcher A：OnCooldown 在前，后续规则恒失败 → 永不命中，也不得消耗冷却
	mA := eng.On(et,
		context.OnCooldown(time.Hour, key),
		func(*context.Context) bool { return false },
	)
	mA.Handle(func(ctx *context.Context) error {
		t.Error("matcher A must never fire")
		return nil
	})

	// matcher B：优先级更低（后执行），带同 key 的 OnCooldown
	var fired int
	mB := eng.On(et, context.OnCooldown(time.Hour, key))
	mB.SetPriority(60)
	mB.Handle(func(ctx *context.Context) error {
		fired++
		return nil
	})

	evt := newTestPlatformEvent(platform.EventKindPrivateMessage)

	// 第一个事件：A 的冷却检查通过但被丢弃（规则链失败），B 正常命中并提交冷却
	eng.ProcessEvent(context.NewContextFromEvent(evt, nil))
	if fired != 1 {
		t.Fatalf("matcher B should fire on first event (cooldown not pre-consumed by A), fired=%d", fired)
	}

	// 第二个事件：冷却已由 B 提交 → B 不再命中
	eng.ProcessEvent(context.NewContextFromEvent(evt, nil))
	if fired != 1 {
		t.Fatalf("cooldown should block second event, fired=%d", fired)
	}
}
