package engine

// benchmark_hotpath_test.go — Benchmark Matrix for hot-path profiling
//
// Usage:
//
//	# CPU profile (focus functions)
//	go test -bench=BenchmarkHotPath -cpuprofile=cpu.pprof -benchtime=3s \
//	  -benchmem ./core/engine/
//	go tool pprof -top -nodecount=30 cpu.pprof
//
//	# Filter specific function
//	go tool pprof -top -focus=Matcher.Match cpu.pprof
//	go tool pprof -top -focus=mergeSortedMatchersSix cpu.pprof
//
//	# Heap profile
//	go test -bench=BenchmarkHotPath -memprofile=mem.pprof -benchtime=3s \
//	  -benchmem ./core/engine/
//	go tool pprof -top -alloc_space -nodecount=30 mem.pprof
//
//	# Run single sub-benchmark
//	go test -bench=BenchmarkHotPath/Heavy -benchtime=5s -benchmem ./core/engine/
//
//	# Interactive profiling
//	go tool pprof -http=:8080 cpu.pprof

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// benchScenario defines one cell in the Benchmark Matrix.
type benchScenario struct {
	name string
	// Matcher counts per category
	numPermSpec int     // sortedCache[eventType] specific permanent
	numPermGen  int     // sortedCache[""] generic permanent
	numCmdSpec  int     // commandIndex[cmd][eventType]
	numCmdGen   int     // commandIndex[cmd][""]
	numTempSpec int     // tempManager.Get(eventType)
	numTempGen  int     // tempManager.Get("")
	passRate    float64 // fraction of matchers where Match() returns true (0.0–1.0)
	messageLen  int     // message content length for extractCommand
	// Behavior controls
	cmdHit      bool // whether the event's message hits an existing command
	useExecPool bool // whether to enable exec pool offload
}

// total returns the total number of candidate matchers after merge.
func (s *benchScenario) total() int {
	return s.numPermSpec + s.numPermGen + s.numCmdSpec + s.numCmdGen + s.numTempSpec + s.numTempGen
}

// Benchmark Matrix — predefined scenarios.
//
// The matrix covers:
//   - Empty (zero matchers) to Extreme (5000+ matchers)
//   - Command hit vs miss
//   - Mixed permanent/command/temp ratios
//   - Match pass rates (most events pass Match, few execute handlers)
//   - Merge pressure (>1024 = exceeds MaxMatcherPoolRetainCapacity)
//   - ExtractCommand pressure (long messages)
func benchmarkMatrix() []benchScenario {
	return []benchScenario{
		// ── Overhead baseline ──────────────────────────────────────────────
		{
			name:       "Empty",
			passRate:   1.0,
			messageLen: 20,
		},
		// ── Light load ─────────────────────────────────────────────────────
		{
			name:        "Light",
			numPermSpec: 10,
			passRate:    0.8,
			messageLen:  20,
		},
		{
			name:        "LightCmdHit",
			numPermSpec: 5,
			numCmdSpec:  5,
			cmdHit:      true,
			passRate:    0.8,
			messageLen:  20,
		},
		// ── Medium load ────────────────────────────────────────────────────
		{
			name:        "Medium",
			numPermSpec: 40,
			numPermGen:  10,
			numCmdSpec:  30,
			numCmdGen:   10,
			numTempSpec: 8,
			numTempGen:  2,
			cmdHit:      false,
			passRate:    0.5,
			messageLen:  50,
		},
		{
			name:        "MediumCmdHit",
			numPermSpec: 40,
			numPermGen:  10,
			numCmdSpec:  30,
			numCmdGen:   10,
			numTempSpec: 8,
			numTempGen:  2,
			cmdHit:      true,
			passRate:    0.5,
			messageLen:  50,
		},
		// ── Heavy load ─────────────────────────────────────────────────────
		{
			name:        "Heavy",
			numPermSpec: 400,
			numPermGen:  100,
			numCmdSpec:  300,
			numCmdGen:   100,
			numTempSpec: 80,
			numTempGen:  20,
			cmdHit:      false,
			passRate:    0.3,
			messageLen:  100,
		},
		{
			name:        "HeavyCmdHit",
			numPermSpec: 400,
			numPermGen:  100,
			numCmdSpec:  300,
			numCmdGen:   100,
			numTempSpec: 80,
			numTempGen:  20,
			cmdHit:      true,
			passRate:    0.3,
			messageLen:  100,
		},
		// ── Merge pressure: total > 1024 (was the pool retain cap) ──
		{
			name:        "MergeBlow",
			numPermSpec: 800,
			numPermGen:  200,
			numCmdSpec:  600,
			numCmdGen:   200,
			numTempSpec: 160,
			numTempGen:  40,
			cmdHit:      false,
			passRate:    0.1,
			messageLen:  100,
		},
		// ── Heavy merge with all 6 lists populated evenly ──────────────────
		{
			name:        "MergeWorst",
			numPermSpec: 500,
			numPermGen:  500,
			numCmdSpec:  500,
			numCmdGen:   500,
			numTempSpec: 500,
			numTempGen:  500,
			cmdHit:      true,
			passRate:    0.01,
			messageLen:  100,
		},
		// ── Extreme load ───────────────────────────────────────────────────
		{
			name:        "Extreme",
			numPermSpec: 2000,
			numPermGen:  500,
			numCmdSpec:  1500,
			numCmdGen:   500,
			numTempSpec: 400,
			numTempGen:  100,
			cmdHit:      false,
			passRate:    0.1,
			messageLen:  200,
		},
		// ── All match pass (compute bound on Match + Handler) ─────────────
		{
			name:        "AllMatch",
			numPermSpec: 1000,
			passRate:    1.0,
			messageLen:  100,
		},
		// ── Long message extractCommand pressure ──────────────────────────
		{
			name:        "LongMsg",
			numPermSpec: 500,
			passRate:    0.5,
			messageLen:  10000,
		},
		// ── ExecPool 池化路径（真实部署形态：慢 handler 常驻池中）───────────
		// handler 被强制保持 promoted，每条事件稳定走 Clone + TrySubmit，
		// 覆盖 ctx.Clone() / copyExtensions 的分配（benchmem + memprofile 验证）。
		{
			name:        "PooledLight",
			numPermSpec: 10,
			useExecPool: true,
			passRate:    0.8,
			messageLen:  20,
		},
		{
			name:        "PooledHeavyCmdHit",
			numPermSpec: 400,
			numPermGen:  100,
			numCmdSpec:  300,
			numCmdGen:   100,
			numTempSpec: 80,
			numTempGen:  20,
			cmdHit:      true,
			useExecPool: true,
			passRate:    0.3,
			messageLen:  100,
		},
		{
			name:        "PooledExtreme",
			numPermSpec: 2000,
			numPermGen:  500,
			numCmdSpec:  1500,
			numCmdGen:   500,
			numTempSpec: 400,
			numTempGen:  100,
			useExecPool: true,
			passRate:    0.1,
			messageLen:  200,
		},
	}
}

// benchFixture holds pre-built state for a benchmark scenario.
type benchFixture struct {
	engine   *Engine
	scenario benchScenario
	ctx      *ctx.Context
}

// buildFixture creates the Engine, registers matchers, and prepares the context
// for the given scenario. All matchers use noop handlers; match behavior is
// controlled via rules.
func buildFixture(s benchScenario, tb testing.TB) *benchFixture {
	tb.Helper()

	opts := []Option{WithNoBackgroundWorkers()}
	if !s.useExecPool {
		opts = append(opts, WithExecPoolDisabled())
	} else {
		// 共享池生命周期归测试所有（newEngineForTest 预置的
		// WithExecPoolDisabled 会使 execPoolCfg=0，此处显式注入池）。
		opts = append(opts, WithSharedExecPool(NewExecPool(DefaultExecPoolConfig())))
	}
	e := newEngineForTest(tb, opts...)

	eventType := string(platform.EventKindPrivateMessage)
	rng := rand.New(rand.NewSource(42))

	// Track which matchers should return true from Match.
	// For command matchers, also track their command string for index lookup.
	passCount := min(int(float64(s.total())*s.passRate), s.total())
	passSet := make(map[int]bool, passCount)
	for i := range passCount {
		passSet[i] = true
	}
	matcherIdx := 0
	nextIdx := func() int {
		idx := matcherIdx
		matcherIdx++
		return idx
	}
	shouldPass := func() bool { return passSet[nextIdx()] }

	// Helper: build a rule that returns true/false based on shouldPass.
	passRule := func() ctx.Rule {
		pass := shouldPass()
		return func(c *ctx.Context) bool { return pass }
	}
	// Helper: build blocking / non-blocking rule for nullipotent cost.
	nullRule := func() ctx.Rule {
		return func(c *ctx.Context) bool { return true }
	}

	// 1. Permanent specific matchers (sortedCache[eventType])
	for range s.numPermSpec {
		m := e.On(eventType, passRule())
		m.Source = "bench-perm-spec"
		if !s.useExecPool && s.passRate > 0 {
			m.execProfile = nil // avoid ExecProfile overhead in benchmark
		}
		if s.useExecPool {
			m.execProfile.promoted.Store(true) // 模拟常驻池的慢 handler
		}
		m.SetPriority(uint64(rng.Intn(10000)))
		m.Handle(func(c *ctx.Context) error { return nil })
	}

	// 2. Permanent generic matchers (sortedCache[""])
	for range s.numPermGen {
		m := e.On("", passRule())
		m.Source = "bench-perm-gen"
		if !s.useExecPool && s.passRate > 0 {
			m.execProfile = nil
		}
		if s.useExecPool {
			m.execProfile.promoted.Store(true)
		}
		m.SetPriority(uint64(rng.Intn(10000)))
		m.Handle(func(c *ctx.Context) error { return nil })
	}

	// 3. Command-specific matchers (commandIndex[cmd][eventType])
	cmdName := "/bench"
	for i := range s.numCmdSpec {
		var extraRules []ctx.Rule
		if !shouldPass() {
			extraRules = append(extraRules, func(c *ctx.Context) bool { return false })
		}
		m := e.OnCommand(eventType, cmdName, extraRules...)
		m.Source = fmt.Sprintf("bench-cmd-spec-%d", i)
		if !s.useExecPool && s.passRate > 0 {
			m.execProfile = nil
		}
		if s.useExecPool {
			m.execProfile.promoted.Store(true)
		}
		m.SetPriority(uint64(rng.Intn(10000)))
		m.Handle(func(c *ctx.Context) error { return nil })
	}

	// 4. Command-generic matchers (commandIndex[cmd][""])
	for i := range s.numCmdGen {
		var extraRules []ctx.Rule
		if !shouldPass() {
			extraRules = append(extraRules, func(c *ctx.Context) bool { return false })
		}
		m := e.OnCommand("", cmdName, extraRules...)
		m.Source = fmt.Sprintf("bench-cmd-gen-%d", i)
		if !s.useExecPool && s.passRate > 0 {
			m.execProfile = nil
		}
		if s.useExecPool {
			m.execProfile.promoted.Store(true)
		}
		m.SetPriority(uint64(rng.Intn(10000)))
		m.Handle(func(c *ctx.Context) error { return nil })
	}

	// 5. Temp-specific matchers (tempManager.Get(eventType))
	for i := range s.numTempSpec {
		m := &Matcher{
			EventType:   eventType,
			Rules:       []ctx.Rule{nullRule()},
			Handler:     func(c *ctx.Context) error { return nil },
			Source:      fmt.Sprintf("bench-temp-spec-%d", i),
			execProfile: nil,
		}
		m.priority.Store(uint64(rng.Intn(10000)))
		m.rt.createdAt = time.Now()
		m.rt.isTemp = 1
		e.internals.tempManager.Add(m)
	}

	// 6. Temp-generic matchers (tempManager.Get(""))
	for i := range s.numTempGen {
		m := &Matcher{
			EventType:   "",
			Rules:       []ctx.Rule{nullRule()},
			Handler:     func(c *ctx.Context) error { return nil },
			Source:      fmt.Sprintf("bench-temp-gen-%d", i),
			execProfile: nil,
		}
		m.priority.Store(uint64(rng.Intn(10000)))
		m.rt.createdAt = time.Now()
		m.rt.isTemp = 1
		e.internals.tempManager.Add(m)
	}

	// Build message content: cmdHit events start with "/bench ", others don't.
	var msgContent string
	bodyPad := s.messageLen
	if s.cmdHit {
		bodyPad -= len("/bench ")
	}
	if bodyPad < 1 {
		bodyPad = 1
	}
	body := strings.Repeat("x", bodyPad)
	if s.cmdHit {
		msgContent = "/bench " + body
	} else {
		msgContent = body
	}

	evt := newTestPlatformEventWithContent(platform.EventKindPrivateMessage, msgContent)
	c := ctx.NewContextFromEvent(evt, nil)

	return &benchFixture{
		engine:   e,
		scenario: s,
		ctx:      c,
	}
}

// ============================================================================
// End-to-End Hot-Path Benchmarks (processEventMatchers)
// ============================================================================

// BenchmarkHotPath runs processEventMatchers in a matrix of scenarios.
//
// This is the primary benchmark for CPU profiling on hot-path functions:
//
//	Matcher.Match, mergeSortedMatchersSix, extractCommand, invokeHandler,
//	isBlocking, tempManager.Get
func BenchmarkHotPath(b *testing.B) {
	for _, s := range benchmarkMatrix() {
		b.Run(s.name, func(b *testing.B) {
			fx := buildFixture(s, b)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				fx.engine.ProcessEvent(fx.ctx)
			}
		})
	}
}

// ============================================================================
// Direct Function Benchmarks
// ============================================================================

// BenchmarkMatcherMatch isolates the Matcher.Match call.
//
// Scenarios:
//   - SingleRule: 1 rule, passes
//   - SingleRuleFail: 1 rule, fails
//   - FiveRules: 5 rules, all pass
//   - FiveRulesFailLast: 5 rules, last one fails
//   - Deleted: matcher is deleted (fast-fail)
//   - CommandIndexed: commandIndexed=true, skips Rules[0]
func BenchmarkMatcherMatch(b *testing.B) {
	makeCtx := func() *ctx.Context {
		evt := newTestPlatformEvent(platform.EventKindPrivateMessage)
		return ctx.NewContextFromEvent(evt, nil)
	}

	b.Run("SingleRule", func(b *testing.B) {
		m := &Matcher{
			Rules:   []ctx.Rule{func(c *ctx.Context) bool { return true }},
			Handler: func(c *ctx.Context) error { return nil },
		}
		c := makeCtx()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Match(c)
		}
	})

	b.Run("SingleRuleFail", func(b *testing.B) {
		m := &Matcher{
			Rules:   []ctx.Rule{func(c *ctx.Context) bool { return false }},
			Handler: func(c *ctx.Context) error { return nil },
		}
		c := makeCtx()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Match(c)
		}
	})

	b.Run("FiveRules", func(b *testing.B) {
		m := &Matcher{
			Rules: []ctx.Rule{
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return true },
			},
			Handler: func(c *ctx.Context) error { return nil },
		}
		c := makeCtx()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Match(c)
		}
	})

	b.Run("FiveRulesFailLast", func(b *testing.B) {
		m := &Matcher{
			Rules: []ctx.Rule{
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return false },
			},
			Handler: func(c *ctx.Context) error { return nil },
		}
		c := makeCtx()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Match(c)
		}
	})

	b.Run("Deleted", func(b *testing.B) {
		m := &Matcher{
			Rules:   []ctx.Rule{func(c *ctx.Context) bool { return true }},
			Handler: func(c *ctx.Context) error { return nil },
		}
		m.rt.deleted.Store(true)
		c := makeCtx()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Match(c)
		}
	})

	b.Run("CommandIndexed", func(b *testing.B) {
		m := &Matcher{
			Rules: []ctx.Rule{
				func(c *ctx.Context) bool { return true }, // OnCommand prefix — skipped
				func(c *ctx.Context) bool { return true },
				func(c *ctx.Context) bool { return true },
			},
			Handler: func(c *ctx.Context) error { return nil },
		}
		m.commandIndexed.Store(true)
		c := makeCtx()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Match(c)
		}
	})
}

// BenchmarkMergeIter isolates the MergeIter Next/Matcher cycle.
//
// Scenarios mirror the old mergeKSortedMatchers benchmarks for direct comparison.
// Key difference: MergeIter produces zero allocs per Next() call.
func BenchmarkMergeIter(b *testing.B) {
	makeMatchers := func(n int, basePriority uint) []*Matcher {
		out := make([]*Matcher, n)
		for i := range n {
			m := &Matcher{Source: "bench"}
			m.priority.Store(uint64(basePriority + uint(i)))
			out[i] = m
		}
		return out
	}

	b.Run("SingleList", func(b *testing.B) {
		l1 := makeMatchers(100, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			it := acquireMergeIter()
			it.add(l1)
			for it.Next() {
				_ = it.Matcher()
			}
			releaseMergeIter(it)
		}
	})

	b.Run("TwoLists", func(b *testing.B) {
		l1 := makeMatchers(50, 0)
		l2 := makeMatchers(50, 25)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			it := acquireMergeIter()
			it.add(l1)
			it.add(l2)
			for it.Next() {
				_ = it.Matcher()
			}
			releaseMergeIter(it)
		}
	})

	b.Run("SixLists", func(b *testing.B) {
		l1 := makeMatchers(50, 0)
		l2 := makeMatchers(50, 100)
		l3 := makeMatchers(50, 200)
		l4 := makeMatchers(50, 300)
		l5 := makeMatchers(50, 400)
		l6 := makeMatchers(50, 500)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			it := acquireMergeIter()
			it.add(l1)
			it.add(l2)
			it.add(l3)
			it.add(l4)
			it.add(l5)
			it.add(l6)
			for it.Next() {
				_ = it.Matcher()
			}
			releaseMergeIter(it)
		}
	})

	b.Run("SixListsInterleaved", func(b *testing.B) {
		l1 := makeMatchers(50, 0)
		l2 := makeMatchers(50, 1)
		l3 := makeMatchers(50, 2)
		l4 := makeMatchers(50, 3)
		l5 := makeMatchers(50, 4)
		l6 := makeMatchers(50, 5)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			it := acquireMergeIter()
			it.add(l1)
			it.add(l2)
			it.add(l3)
			it.add(l4)
			it.add(l5)
			it.add(l6)
			for it.Next() {
				_ = it.Matcher()
			}
			releaseMergeIter(it)
		}
	})
}

// BenchmarkExtractCommand isolates extractCommand.
//
// Scenarios:
//   - Short: "/ping" — short, no space
//   - Normal: "/ping hello world" — typical
//   - Long: 1KB message with leading spaces
//   - NoMatch: plain text with no command prefix
//   - OnlySpaces: whitespace only (edge case)
func BenchmarkExtractCommand(b *testing.B) {
	b.Run("Short", func(b *testing.B) {
		s := "/ping"
		for i := 0; i < b.N; i++ {
			extractCommand(s)
		}
	})

	b.Run("Normal", func(b *testing.B) {
		s := "/help some long args here"
		for i := 0; i < b.N; i++ {
			extractCommand(s)
		}
	})

	b.Run("Long", func(b *testing.B) {
		s := "  /very-long-command-name-with-many-characters " + strings.Repeat("x", 1000)
		for i := 0; i < b.N; i++ {
			extractCommand(s)
		}
	})

	b.Run("NoMatch", func(b *testing.B) {
		s := strings.Repeat("plain text without command ", 20)
		for i := 0; i < b.N; i++ {
			extractCommand(s)
		}
	})

	b.Run("OnlySpaces", func(b *testing.B) {
		s := "     "
		for i := 0; i < b.N; i++ {
			extractCommand(s)
		}
	})
}

// BenchmarkIsBlocking isolates isBlocking.
//
// Scenarios:
//   - NotBlocked: no channel key, isBlock=false
//   - GlobalBlocked: isBlock=true
//   - ChannelBlocked: per-channel blocked
//   - ChannelNotBlocked: channel key set but not blocked (sync.Map miss)
func BenchmarkIsBlocking(b *testing.B) {
	key := ChannelKey("test:chat-001")

	b.Run("NotBlocked", func(b *testing.B) {
		m := &Matcher{}
		for i := 0; i < b.N; i++ {
			m.isBlocking(key)
		}
	})

	b.Run("GlobalBlocked", func(b *testing.B) {
		m := &Matcher{}
		m.isBlock.Store(true)
		for i := 0; i < b.N; i++ {
			m.isBlocking(key)
		}
	})

	b.Run("ChannelBlocked", func(b *testing.B) {
		m := &Matcher{}
		m.channelBlocked.Store(key, true)
		for i := 0; i < b.N; i++ {
			m.isBlocking(key)
		}
	})

	b.Run("ChannelNotBlocked", func(b *testing.B) {
		m := &Matcher{}
		// Store a different key so this key is a miss
		m.channelBlocked.Store("other:chat-999", true)
		for i := 0; i < b.N; i++ {
			m.isBlocking(key)
		}
	})

	b.Run("Nil", func(b *testing.B) {
		var m *Matcher
		for i := 0; i < b.N; i++ {
			m.isBlocking(key)
		}
	})
}

// BenchmarkTempManagerGet isolates tempManager.Get.
//
// Scenarios:
//   - Empty: no temp matchers
//   - Few: 10 matchers spread across shards
//   - Many: 1000 matchers spread across shards
//   - ManySpecific: 1000 matchers, all same eventType
//   - BothTypes: 500 spec + 500 generic
func BenchmarkTempManagerGet(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	eventType := string(platform.EventKindPrivateMessage)

	makeTemp := func(tm *tempMatcherManager, n int, et EventType) {
		for range n {
			m := &Matcher{
				EventType: et,
				Rules:     []ctx.Rule{func(c *ctx.Context) bool { return true }},
				Handler:   func(c *ctx.Context) error { return nil },
				Source:    "bench",
			}
			m.priority.Store(uint64(rng.Intn(10000)))
			m.rt.createdAt = time.Now()
			tm.Add(m)
		}
	}

	b.Run("Empty", func(b *testing.B) {
		tm := newTempMatcherManager()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tm.Get(eventType)
		}
	})

	b.Run("Few", func(b *testing.B) {
		tm := newTempMatcherManager()
		makeTemp(tm, 10, eventType)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tm.Get(eventType)
		}
	})

	b.Run("Many", func(b *testing.B) {
		tm := newTempMatcherManager()
		makeTemp(tm, 1000, eventType)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tm.Get(eventType)
		}
	})

	b.Run("ManyBoth", func(b *testing.B) {
		tm := newTempMatcherManager()
		makeTemp(tm, 500, eventType)
		makeTemp(tm, 500, "")
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tm.Get(eventType)
		}
	})
}

// ============================================================================
// Heap Allocation Focus Benchmarks
// ============================================================================

// BenchmarkMergeIterAlloc isolates MergeIter's allocation behavior.
//
// Unlike the old mergeKSortedMatchers which allocated a full dst slice,
// MergeIter produces zero allocs per Next() call regardless of total size.
func BenchmarkMergeIterAlloc(b *testing.B) {
	makeSorted := func(n int) []*Matcher {
		out := make([]*Matcher, n)
		for i := range n {
			m := &Matcher{Source: "bench"}
			m.priority.Store(uint64(i))
			out[i] = m
		}
		return out
	}

	b.Run("UnderThreshold", func(b *testing.B) {
		l := makeSorted(500)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			it := acquireMergeIter()
			it.add(l)
			for it.Next() {
				_ = it.Matcher()
			}
			releaseMergeIter(it)
		}
	})

	b.Run("OverThreshold", func(b *testing.B) {
		l := makeSorted(2000)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			it := acquireMergeIter()
			it.add(l)
			for it.Next() {
				_ = it.Matcher()
			}
			releaseMergeIter(it)
		}
	})
}

// ============================================================================
// RoutingStrategy.Plan Benchmarks
// ============================================================================

// BenchmarkRoutingPlan isolates RoutingStrategy.Plan + CandidatePlan iteration
// with the three built-in indexes (permanent/command/temp).
//
// 覆盖路由抽象后的完整 Plan 路径：索引检索（含门控）+ K 路归并 + 计划释放。
// 与 BenchmarkHotPath（ProcessEvent 端到端）互补，可单独定位路由层开销。
func BenchmarkRoutingPlan(b *testing.B) {
	eventType := string(platform.EventKindPrivateMessage)
	benchCtx := ctx.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "/bench hello"), nil)

	makeMatcher := func(prio uint64) *Matcher {
		m := &Matcher{Source: "bench"}
		m.priority.Store(prio)
		return m
	}

	b.Run("Empty", func(b *testing.B) {
		router := newMatcherRouter()
		router.addIndex(permanentIndex{}, true)
		router.addIndex(commandIndex{}, true)
		router.addIndex(tempIndex{tm: newTempMatcherManager()}, true)
		st := newEngineState()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			plan := router.Plan(st, benchCtx)
			for plan.Next() {
				_ = plan.Matcher()
			}
			plan.Release()
		}
	})

	b.Run("CommandHit", func(b *testing.B) {
		router := newMatcherRouter()
		router.addIndex(permanentIndex{}, true)
		router.addIndex(commandIndex{}, true)
		tm := newTempMatcherManager()
		router.addIndex(tempIndex{tm: tm}, true)

		permSpec := make([]*Matcher, 50)
		for i := range permSpec {
			permSpec[i] = makeMatcher(uint64(100 + i))
		}
		cmdSpec := make([]*Matcher, 10)
		for i := range cmdSpec {
			cmdSpec[i] = makeMatcher(uint64(10 + i))
		}
		st := newEngineState()
		st.sortedCache[eventType] = permSpec
		st.commandIndex["/bench"] = map[EventType][]*Matcher{eventType: cmdSpec}
		for range 5 {
			tm.Add(makeMatcher(50))
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			plan := router.Plan(st, benchCtx)
			for plan.Next() {
				_ = plan.Matcher()
			}
			plan.Release()
		}
	})
	b.Run("RegexHit", func(b *testing.B) {
		// 慢带路径：正则预匹配（块命中）+ 惰性阶段构建
		regexCtx := ctx.NewContextFromEvent(
			newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "order 42 total"), nil)

		router := newMatcherRouter()
		router.addIndex(permanentIndex{}, true)
		router.addIndex(commandIndex{}, true)
		router.addIndex(tempIndex{tm: newTempMatcherManager()}, true)
		router.addIndex(regexIndex{}, true)

		regexSpec := make([]*Matcher, 5)
		for i := range regexSpec {
			m := makeMatcher(uint64(100 + i))
			m.regexPattern = `\d+`
			m.regexIndexed.Store(true)
			regexSpec[i] = m
		}
		st := newEngineState()
		st.regexIndex[eventType] = regexSpec

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			plan := router.Plan(st, regexCtx)
			for plan.Next() {
				_ = plan.Matcher()
			}
			plan.Release()
		}
	})
}

// ============================================================================
// Synthetic profile helper for reading pprof
// ============================================================================
// To analyze merged profiles across all sub-benchmarks, run:
//
//   go test -bench=BenchmarkHotPath -cpuprofile=cpu.pprof -benchtime=5s ./core/engine/
//   go tool pprof -top -cum cpu.ppprof  | head -30
//
// Then narrow down:
//   go tool pprof -top -focus=Matcher.Match cpu.pprof
//   go tool pprof -top -focus=mergeSortedMatchersSix cpu.pprof
//   go tool pprof -top -focus=extractCommand cpu.pprof
//   go tool pprof -top -focus=invokeHandler cpu.pprof
//   go tool pprof -top -focus=isBlocking cpu.pprof
//   go tool pprof -top -focus=tempManager.Get cpu.pprof
//
// Heap:
//   go test -bench=BenchmarkHotPath -memprofile=mem.pprof -benchtime=5s ./core/engine/
//   go tool pprof -top -alloc_space mem.pprof | head -20
//   go tool pprof -top -alloc_space -focus=mergeKSortedMatchers mem.pprof
