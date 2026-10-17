package engine

// rule_snapshot_publish_test.go — 注册点固定规则快照的不变式测试
//
// 背景：Match 读取规则快照（ruleSnapshot），快照为空时回退读公开字段 Rules。
// 若快照只在"首次链式追加"时才发布，读侧会在快照为空期间读公开字段，与首次
// 追加的字段写入构成竞争（-race 可检出）。因此 matcher 进入运行时索引前
// （registerMatcher / OnTemp / 索引构建）必须固定一份快照。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatcher_PublishInitialRulesAtRegistration(t *testing.T) {
	et := "SNAPSHOT_ONLY"
	eng := newEngineForTest(t)

	rule := func(*context.Context) bool { return true }

	m := eng.On(et, rule)
	require.NotNil(t, m.rules.Load(), "注册后必须已有规则快照（读侧不再回退读公开字段）")
	assert.Len(t, m.ruleSnapshot(), 1)

	// 链式追加：发布新快照，公开字段保持同步（API 视图）
	m.Where(rule)
	assert.Len(t, m.ruleSnapshot(), 2)
	assert.Len(t, m.Rules, 2)

	// 临时 matcher 同样在注册点固定快照
	temp := eng.OnTemp(et, rule)
	require.NotNil(t, temp.rules.Load(), "临时 matcher 也应有规则快照")
}

func TestMatcher_UnregisteredMatcherKeepsFieldFallback(t *testing.T) {
	// 未经引擎注册的 matcher（构造期/测试字面量）没有快照，读侧回退公开字段
	m := &Matcher{EventType: "x", Rules: []context.Rule{func(*context.Context) bool { return true }}}
	assert.Nil(t, m.rules.Load())
	assert.Len(t, m.ruleSnapshot(), 1)
}
