package engine

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- 测试用静态索引 ----------------------------------------------------------

// staticIndex 返回固定的候选流列表（测试桩）。
type staticIndex struct {
	band  RoutingBand
	lists [][]*Matcher
}

func (i staticIndex) Band() RoutingBand { return i.band }

func (i staticIndex) Candidates(_ MatcherEnv, _ *context.Context) MatcherCandidates {
	var c MatcherCandidates
	for _, l := range i.lists {
		c.Add(l)
	}
	return c
}

func newTestCtx(content string) *context.Context {
	if content == "" {
		content = "test message"
	}
	return context.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, content), nil)
}

// ---- CandidatePlan 基础行为 --------------------------------------------------

func TestCandidatePlan_Empty(t *testing.T) {
	router := newMatcherRouter()
	plan := router.Plan(newEngineState(), newTestCtx(""))
	defer plan.Release()
	assert.False(t, plan.Next())
}

func TestCandidatePlan_EmptyListsSkipped(t *testing.T) {
	router := newMatcherRouter()
	router.addIndex(staticIndex{lists: [][]*Matcher{nil, {}}}, true)
	router.addIndex(staticIndex{lists: [][]*Matcher{nil}}, true)
	plan := router.Plan(newEngineState(), newTestCtx(""))
	defer plan.Release()
	assert.False(t, plan.Next())
}

func TestCandidatePlan_ReleaseIdempotent(t *testing.T) {
	router := newMatcherRouter()
	router.addIndex(staticIndex{lists: [][]*Matcher{{makeTestMatcher("", 10)}}}, true)
	plan := router.Plan(newEngineState(), newTestCtx(""))
	plan.Release()
	plan.Release() // 幂等：重复释放安全
	assert.False(t, plan.Next())
}

// ---- 优先级归并 --------------------------------------------------------------

func TestMatcherRouter_Plan_PriorityInterleave(t *testing.T) {
	// 两个索引各自按优先级升序，Plan 应按全局优先级交错归并
	a1 := makeTestMatcher("", 10)
	a2 := makeTestMatcher("", 30)
	b1 := makeTestMatcher("", 20)
	b2 := makeTestMatcher("", 40)

	router := newMatcherRouter()
	router.addIndex(staticIndex{lists: [][]*Matcher{{a1, a2}}}, true)
	router.addIndex(staticIndex{lists: [][]*Matcher{{b1, b2}}}, true)

	plan := router.Plan(newEngineState(), newTestCtx(""))
	defer plan.Release()
	var got []*Matcher
	for plan.Next() {
		got = append(got, plan.Matcher())
	}
	require.Equal(t, 4, len(got))
	assert.Same(t, a1, got[0])
	assert.Same(t, b1, got[1])
	assert.Same(t, a2, got[2])
	assert.Same(t, b2, got[3])
}

// ---- 内置索引（真实检索路径） -------------------------------------------------

func TestMatcherRouter_Plan_BuiltinIndexes(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)

	permSpec := makeTestMatcher(et, 40)
	permGen := makeTestMatcher("", 30)
	cmdSpec := makeTestMatcher(et, 10)
	cmdGen := makeTestMatcher("", 20)
	tempM := makeTestMatcher(et, 5)

	st := newEngineState()
	st.sortedCache[et] = []*Matcher{permSpec}
	st.sortedCache[""] = []*Matcher{permGen}
	st.commandIndex["/ping"] = map[EventType][]*Matcher{
		et: {cmdSpec},
		"": {cmdGen},
	}

	tm := newTempMatcherManager()
	tm.Add(tempM)

	router := newMatcherRouter()
	router.addIndex(permanentIndex{}, true)
	router.addIndex(commandIndex{}, true)
	router.addIndex(tempIndex{tm: tm}, true)

	plan := router.Plan(st, newTestCtx("/ping args"))
	defer plan.Release()
	var got []*Matcher
	for plan.Next() {
		got = append(got, plan.Matcher())
	}

	// 按优先级升序：temp(5) cmdSpec(10) cmdGen(20) permGen(30) permSpec(40)
	require.Equal(t, 5, len(got))
	assert.Same(t, tempM, got[0])
	assert.Same(t, cmdSpec, got[1])
	assert.Same(t, cmdGen, got[2])
	assert.Same(t, permGen, got[3])
	assert.Same(t, permSpec, got[4])
}

func TestCommandIndex_Gate_NoContent(t *testing.T) {
	st := newEngineState()
	st.commandIndex["/ping"] = map[EventType][]*Matcher{"": {makeTestMatcher("", 10)}}

	router := newMatcherRouter()
	router.addIndex(commandIndex{}, true)

	// 无消息内容（空事件）→ 门控跳过
	evt := &engineTestEvent{kind: platform.EventKindPrivateMessage, content: "", rawType: "private"}
	c := context.NewContextFromEvent(evt, nil)
	plan := router.Plan(st, c)
	defer plan.Release()
	assert.False(t, plan.Next())

	// 有内容但命令词不存在 → 门控跳过
	plan = router.Plan(st, newTestCtx("/nonexistent"))
	defer plan.Release()
	assert.False(t, plan.Next())
}

// ---- Source Budget -----------------------------------------------------------

func TestMatcherRouter_Budget_InternalPanic(t *testing.T) {
	router := newMatcherRouter()
	for range routingBudgetLimit {
		router.addIndex(staticIndex{}, true)
	}
	assert.Panics(t, func() {
		router.addIndex(staticIndex{}, true) // 框架内部第 17 个 → panic（框架 Bug）
	})
}

func TestMatcherRouter_Budget_ExternalWarnsAndContinues(t *testing.T) {
	router := newMatcherRouter()
	for range routingBudgetLimit {
		router.addIndex(staticIndex{}, false)
	}
	// 第三方第 17 个 → warn + 继续运行（不 panic）
	m := makeTestMatcher("", 10)
	router.addIndex(staticIndex{lists: [][]*Matcher{{m}}}, false)

	plan := router.Plan(newEngineState(), newTestCtx(""))
	defer plan.Release()
	require.True(t, plan.Next())
	assert.Same(t, m, plan.Matcher())
}

// ---- Band（阶段语义） ----------------------------------------------------------

func TestMatcherRouter_BandSlow_SeparatePhase(t *testing.T) {
	// 慢带独立成阶段：即使 slow 优先级数值更小（更先），也排在快带之后执行
	fast := makeTestMatcher("", 20)
	slow := makeTestMatcher("", 10)

	router := newMatcherRouter()
	router.addIndex(staticIndex{band: BandSlow, lists: [][]*Matcher{{slow}}}, false)
	router.addIndex(staticIndex{band: BandFast, lists: [][]*Matcher{{fast}}}, false)

	plan := router.Plan(newEngineState(), newTestCtx(""))
	defer plan.Release()
	var got []*Matcher
	for plan.Next() {
		got = append(got, plan.Matcher())
	}
	require.Equal(t, 2, len(got))
	assert.Same(t, fast, got[0]) // 快带先执行
	assert.Same(t, slow, got[1])
}

// recordingIndex 记录 Candidates 是否被查询（验证阶段惰性）。
type recordingIndex struct {
	band    RoutingBand
	lists   [][]*Matcher
	queried *atomic.Bool
}

func (i recordingIndex) Band() RoutingBand { return i.band }

func (i recordingIndex) Candidates(_ MatcherEnv, _ *context.Context) MatcherCandidates {
	i.queried.Store(true)
	var c MatcherCandidates
	for _, l := range i.lists {
		c.Add(l)
	}
	return c
}

func TestCandidatePlan_BlockingShortCircuitsSlowPhase(t *testing.T) {
	// 快带被 block 短路（执行循环 return，不再调用 Next）→ 慢带索引零查询
	queried := &atomic.Bool{}
	router := newMatcherRouter()
	router.addIndex(staticIndex{band: BandFast, lists: [][]*Matcher{{makeTestMatcher("", 10)}}}, true)
	router.addIndex(recordingIndex{band: BandSlow, queried: queried}, false)

	plan := router.Plan(newEngineState(), newTestCtx(""))
	require.True(t, plan.Next()) // 只消费快带候选
	plan.Release()               // 模拟阻断后的 defer Release
	assert.False(t, queried.Load(), "slow band must not be queried after blocking short-circuit")
}

func TestCandidatePlan_SlowPhaseBuiltOnExhaustion(t *testing.T) {
	// 快带耗尽（无阻断）→ 慢带被惰性构建并执行
	queried := &atomic.Bool{}
	slowM := makeTestMatcher("", 10)
	router := newMatcherRouter()
	router.addIndex(staticIndex{band: BandFast, lists: [][]*Matcher{{makeTestMatcher("", 20)}}}, true)
	router.addIndex(recordingIndex{band: BandSlow, lists: [][]*Matcher{{slowM}}, queried: queried}, false)

	plan := router.Plan(newEngineState(), newTestCtx(""))
	defer plan.Release()
	var got []*Matcher
	for plan.Next() {
		got = append(got, plan.Matcher())
	}
	assert.True(t, queried.Load())
	require.Equal(t, 2, len(got))
	assert.Same(t, slowM, got[1])
}

// ---- 引擎集成 ----------------------------------------------------------------

func TestWithMatcherIndex_CustomIndexRouted(t *testing.T) {
	called := false
	m := &Matcher{Source: "custom"}
	m.Handle(func(ctx *context.Context) error {
		called = true
		return nil
	})
	m.priority.Store(10)

	e := newEngineForTest(t, WithMatcherIndex(staticIndex{lists: [][]*Matcher{{m}}}))

	e.ProcessEvent(newTestCtx("hello"))
	assert.True(t, called)
}

func TestCandidatePlan_BlockingShortCircuit(t *testing.T) {
	// 高优先级 matcher 阻断后，后续候选（含其他索引的）不再执行
	blockedRan := false
	afterRan := false

	blocker := &Matcher{Source: "custom"}
	blocker.Handle(func(ctx *context.Context) error {
		blockedRan = true
		return nil
	})
	blocker.priority.Store(10)
	blocker.SetBlock(true)

	after := &Matcher{Source: "custom"}
	after.Handle(func(ctx *context.Context) error {
		afterRan = true
		return nil
	})
	after.priority.Store(20)

	e := newEngineForTest(t,
		WithMatcherIndex(staticIndex{lists: [][]*Matcher{{blocker}}}),
		WithMatcherIndex(staticIndex{lists: [][]*Matcher{{after}}}),
	)

	e.ProcessEvent(newTestCtx("hello"))
	assert.True(t, blockedRan)
	assert.False(t, afterRan)
}

// ---- 正则索引（BandSlow 首个落地实现） ----------------------------------------

func TestState_RegexIndex_RoutingAndRebuild(t *testing.T) {
	s := newEngineState()
	rm := &Matcher{EventType: "et"}
	rm.Regex(`\d+`)                                              // 置位 regexIndexed + 追加规则
	rm.Handler = func(ctx *context.Context) error { return nil } // 有 handler 才进正则桶
	s.addMatcher(rm)
	pm := makeTestMatcher("et", 50)
	s.addMatcher(pm)

	require.Contains(t, s.regexIndex, "et")
	require.Len(t, s.regexIndex["et"], 1)
	require.Len(t, s.sortedCache["et"], 1, "常规索引只剩永久 matcher")

	s.rebuildIndex()
	require.Len(t, s.regexIndex["et"], 1, "rebuild 必须保持正则路由")
	require.Len(t, s.sortedCache["et"], 1)

	// 无 handler 的正则 matcher 不进入正则桶（慢带预匹配不应为它空转）
	s2 := newEngineState()
	noHandler := &Matcher{EventType: "et"}
	noHandler.Regex(`\d+`)
	s2.addMatcher(noHandler)
	require.NotContains(t, s2.regexIndex, "et", "无 handler 的 regex matcher 必须被过滤")
}

func TestEngine_RegexIndexedMatcher(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)
	called := false

	e := newEngineForTest(t)
	m := e.On(et)
	m.Regex(`\d+`) // 已注册 matcher → 触发索引重建迁移到正则索引（慢带）
	m.Handle(func(ctx *context.Context) error {
		called = true
		return nil
	})

	e.ProcessEvent(newTestCtx("order 42"))
	assert.True(t, called, "regex hit should run handler")

	called = false
	e.ProcessEvent(newTestCtx("no digits"))
	assert.False(t, called, "regex miss should skip handler")
}

func TestEngine_RegexIndexed_SkipsRulesZero(t *testing.T) {
	// regexIndexed：Match 跳过 Rules[0]（正则已由索引预匹配），后续规则照常执行
	et := string(platform.EventKindPrivateMessage)
	secondRan := false

	e := newEngineForTest(t)
	m := e.On(et)
	m.Regex(`\d+`)
	m.Where(func(ctx *context.Context) bool {
		secondRan = true
		return true
	})
	handled := false
	m.Handle(func(ctx *context.Context) error {
		handled = true
		return nil
	})

	e.ProcessEvent(newTestCtx("abc 123"))
	assert.True(t, secondRan, "第二条规则必须在索引预匹配后照常执行")
	assert.True(t, handled)
}

func TestMatcher_Regex_FirstRuleGuard(t *testing.T) {
	// 防御：Regex() 前已有规则时不置位 regexIndexed（否则 Match 会误跳 Rules[0]），
	// 正则规则仍生效（普通路径求值）
	et := string(platform.EventKindPrivateMessage)
	whereRan := false
	regexRan := false

	e := newEngineForTest(t)
	m := e.On(et)
	m.Where(func(ctx *context.Context) bool { // Rules[0] = Where，非 OnRegex
		whereRan = true
		return true
	})
	m.Regex(`\d+`) // 退化为普通规则：warn + 不置位
	assert.False(t, m.regexIndexed.Load(), "已有规则时不得置位 regexIndexed")

	m.Handle(func(ctx *context.Context) error {
		regexRan = true
		return nil
	})

	e.ProcessEvent(newTestCtx("abc 123"))
	assert.True(t, whereRan, "Rules[0] 不得被跳过")
	assert.True(t, regexRan, "正则规则必须在普通路径照常执行")
}

func TestEngine_RegexUnhandled_NotRouted(t *testing.T) {
	// 无 handler 的正则 matcher 不进入正则桶：慢带预匹配不为其空转
	et := string(platform.EventKindPrivateMessage)

	e := newEngineForTest(t)
	m := e.On(et)
	m.Regex(`\d+`) // 不绑定 handler
	_ = m

	// 有 handler 的对照
	handled := false
	m2 := e.On(et)
	m2.Regex(`\d+`)
	m2.Handle(func(ctx *context.Context) error {
		handled = true
		return nil
	})

	e.ProcessEvent(newTestCtx("order 42"))
	assert.True(t, handled)
}

func TestEngine_BlockingSkipsSlowBand(t *testing.T) {
	// 端到端：快带 matcher 阻断 → 慢带索引零查询
	et := string(platform.EventKindPrivateMessage)
	queried := &atomic.Bool{}

	e := newEngineForTest(t, WithMatcherIndex(recordingIndex{band: BandSlow, queried: queried}))

	blocker := e.On(et)
	blocker.priority.Store(10)
	blocker.SetBlock(true)
	blocked := false
	blocker.Handle(func(ctx *context.Context) error {
		blocked = true
		return nil
	})

	e.ProcessEvent(newTestCtx("hello"))
	assert.True(t, blocked)
	assert.False(t, queried.Load(), "阻断的快带 matcher 必须短路慢带索引")
}

// ---- 候选 Meta（正则捕获组随候选传递） ---------------------------------------

func TestMergeIter_MetaAligned(t *testing.T) {
	m1 := makeTestMatcher("", 10)
	m2 := makeTestMatcher("", 20)
	meta1 := "meta-1"
	meta2 := "meta-2"

	it := acquireMergeIter()
	defer releaseMergeIter(it)
	it.addMeta([]*Matcher{m1, m2}, []any{meta1, meta2})
	it.add([]*Matcher{makeTestMatcher("", 30)})

	require.True(t, it.Next())
	assert.Same(t, m1, it.Matcher())
	assert.Equal(t, meta1, it.Meta())

	require.True(t, it.Next())
	assert.Same(t, m2, it.Matcher())
	assert.Equal(t, meta2, it.Meta())

	require.True(t, it.Next())
	assert.NotNil(t, it.Matcher())
	assert.Nil(t, it.Meta(), "无 Meta 的流必须返回 nil")
}

func TestCandidatePlan_MetaForwarding(t *testing.T) {
	rm := context.RegexMatch{Pattern: `\d+`, Groups: []string{"42"}}
	regexM := makeTestMatcher("", 10)
	regexM.regexPattern = `\d+`
	fastM := makeTestMatcher("", 20)

	// 正则索引（BandSlow）返回携带 Meta 的候选；快带索引不携带
	router := newMatcherRouter()
	router.addIndex(staticIndex{band: BandFast, lists: [][]*Matcher{{fastM}}}, true)
	router.addIndex(metaIndex{band: BandSlow, list: []*Matcher{regexM}, metas: []any{rm}}, false)

	plan := router.Plan(newEngineState(), newTestCtx("42"))
	defer plan.Release()

	require.True(t, plan.Next()) // 快带：无 Meta
	assert.Same(t, fastM, plan.Matcher())
	assert.Nil(t, plan.Meta())

	require.True(t, plan.Next()) // 慢带：携带捕获组
	assert.Same(t, regexM, plan.Matcher())
	got, ok := plan.Meta().(context.RegexMatch)
	require.True(t, ok)
	assert.Equal(t, []string{"42"}, got.Groups)
}

// metaIndex 是携带逐条 Meta 的静态索引（测试桩）。
type metaIndex struct {
	band  RoutingBand
	list  []*Matcher
	metas []any
}

func (i metaIndex) Band() RoutingBand { return i.band }

func (i metaIndex) Candidates(_ MatcherEnv, _ *context.Context) MatcherCandidates {
	var c MatcherCandidates
	c.AddMeta(i.list, i.metas)
	return c
}

func TestEngine_RegexCaptureGroups(t *testing.T) {
	// 端到端：regexIndex 预匹配携带捕获组，handler 经 ctx.RegexResult() 读取
	et := string(platform.EventKindPrivateMessage)
	var gotGroups []string

	e := newEngineForTest(t)
	m := e.On(et)
	m.Regex(`hello (\w+)`)
	m.Handle(func(ctx *context.Context) error {
		res, ok := ctx.RegexResult()
		if ok {
			gotGroups = res.Groups
		}
		return nil
	})

	e.ProcessEvent(newTestCtx("hello world"))
	require.NotNil(t, gotGroups, "handler 必须能读取捕获组")
	assert.Equal(t, []string{"hello world", "world"}, gotGroups)

	// 快带 matcher：无 Meta
	var gotMeta bool
	e2 := newEngineForTest(t)
	plain := e2.On(et)
	plain.Handle(func(ctx *context.Context) error {
		gotMeta = ctx.CandidateMeta() != nil
		return nil
	})
	e2.ProcessEvent(newTestCtx("hello"))
	assert.False(t, gotMeta, "普通 matcher 不应携带候选 Meta")
}

// ---- 零分配不变量 ------------------------------------------------------------

func TestRoutingPlan_ZeroAlloc(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)
	st := newEngineState()
	st.sortedCache[et] = []*Matcher{makeTestMatcher(et, 10)}
	st.commandIndex["/ping"] = map[EventType][]*Matcher{et: {makeTestMatcher(et, 5)}}

	tm := newTempMatcherManager()
	tm.Add(makeTestMatcher(et, 20))

	router := newMatcherRouter()
	router.addIndex(permanentIndex{}, true)
	router.addIndex(commandIndex{}, true)
	router.addIndex(tempIndex{tm: tm}, true)

	ctx := newTestCtx("/ping args")

	allocs := testing.AllocsPerRun(100, func() {
		plan := router.Plan(st, ctx)
		for plan.Next() {
			_ = plan.Matcher()
		}
		plan.Release()
	})
	assert.Zero(t, allocs)
}
