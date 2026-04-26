// Package main provides a standalone throughput benchmark for the remilia framework.
//
// Usage:
//
//	cd examples/benchmark && go run throughput_bench.go
//	cd examples/benchmark && go run throughput_bench.go -duration 15s -suite quick
//	cd examples/benchmark && go run throughput_bench.go -suite full -output results.json
//
// Flags:
//
//	-duration   per-scenario test duration (default 10s)
//	-suite      "quick" | "standard" | "full"  (default standard)
//	-middleware whether to enable middleware (default true)
//	-output     path to write JSON results (optional)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	mw "github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// ─────────────────────────────────────────────────────────────
// benchEvent — pooled, platform-agnostic Event implementation
// ─────────────────────────────────────────────────────────────

type benchEvent struct {
	platform string
	kind     platform.EventKind
	id       string
	content  string
	sender   platform.UserInfo
	chat     platform.ChatInfo
	ts       time.Time
}

func acquireBenchEvent() *benchEvent {
	return benchEventPool.Get().(*benchEvent)
}

func releaseBenchEvent(e *benchEvent) {
	*e = benchEvent{}
	benchEventPool.Put(e)
}

var benchEventPool = sync.Pool{
	New: func() any { return &benchEvent{} },
}

func (e *benchEvent) Platform() string                          { return e.platform }
func (e *benchEvent) Kind() platform.EventKind                  { return e.kind }
func (e *benchEvent) ID() string                                { return e.id }
func (e *benchEvent) Content() string                           { return e.content }
func (e *benchEvent) Sender() platform.UserInfo                 { return e.sender }
func (e *benchEvent) Chat() platform.ChatInfo                   { return e.chat }
func (e *benchEvent) Timestamp() time.Time                      { return e.ts }
func (e *benchEvent) Attachments() []platform.InboundAttachment { return nil }

// ─────────────────────────────────────────────────────────────
// System metrics sampler
// ─────────────────────────────────────────────────────────────
type sysSampler struct {
	mu      sync.Mutex
	samples []sysSample
	stop    chan struct{}
	done    chan struct{}
}

type sysSample struct {
	ts             time.Time
	cpuSysPct      float64
	cpuProcPct     float64
	memSysUsedMB   float64
	memSysUsedPct  float64
	heapAllocMB    float64
	heapSysMB      float64
	stackInUseMB   float64
	goroutines     int
	gcRuns         uint32
	gcPauseTotalMs float64
}

func newSysSampler() *sysSampler {
	return &sysSampler{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

func (s *sysSampler) Start(interval time.Duration) {
	proc, _ := process.NewProcess(int32(os.Getpid()))
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case t := <-ticker.C:
				s.collect(t, proc)
			}
		}
	}()
}

func (s *sysSampler) Stop() {
	close(s.stop)
	<-s.done
}

func (s *sysSampler) collect(t time.Time, proc *process.Process) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	sample := sysSample{
		ts:             t,
		heapAllocMB:    float64(ms.HeapAlloc) / 1e6,
		heapSysMB:      float64(ms.HeapSys) / 1e6,
		stackInUseMB:   float64(ms.StackInuse) / 1e6,
		goroutines:     runtime.NumGoroutine(),
		gcRuns:         ms.NumGC,
		gcPauseTotalMs: float64(ms.PauseTotalNs) / 1e6,
	}
	if pcts, err := cpu.Percent(0, false); err == nil && len(pcts) > 0 {
		sample.cpuSysPct = pcts[0]
	}
	if proc != nil {
		if pct, err := proc.CPUPercent(); err == nil {
			sample.cpuProcPct = pct
		}
	}
	if vmStat, err := mem.VirtualMemory(); err == nil {
		sample.memSysUsedMB = float64(vmStat.Used) / 1e6
		sample.memSysUsedPct = vmStat.UsedPercent
	}
	s.mu.Lock()
	s.samples = append(s.samples, sample)
	s.mu.Unlock()
}

type sysStats struct {
	CpuSysAvgPct     float64
	CpuSysMaxPct     float64
	CpuProcAvgPct    float64
	CpuProcMaxPct    float64
	MemSysUsedAvgMB  float64
	MemSysUsedMaxMB  float64
	MemSysUsedAvgPct float64
	MemSysUsedMaxPct float64
	HeapAllocAvgMB   float64
	HeapAllocMaxMB   float64
	HeapSysMaxMB     float64
	StackInUseMaxMB  float64
	GoroutinesAvg    float64
	GoroutinesMax    int
	GCRuns           uint32
	GCPauseDeltaMs   float64
	GCPauseAvgMs     float64
}

func (s *sysSampler) Aggregate() sysStats {
	s.mu.Lock()
	samples := make([]sysSample, len(s.samples))
	copy(samples, s.samples)
	s.mu.Unlock()
	if len(samples) == 0 {
		return sysStats{}
	}
	var st sysStats
	st.GCRuns = samples[len(samples)-1].gcRuns - samples[0].gcRuns
	gcPauseDelta := samples[len(samples)-1].gcPauseTotalMs - samples[0].gcPauseTotalMs
	st.GCPauseDeltaMs = gcPauseDelta
	if st.GCRuns > 0 {
		st.GCPauseAvgMs = gcPauseDelta / float64(st.GCRuns)
	}
	var (
		sumCpuSys, sumCpuProc float64
		sumMemMB, sumMemPct   float64
		sumHeap               float64
		sumGoro               float64
	)
	for _, sp := range samples {
		sumCpuSys += sp.cpuSysPct
		if sp.cpuSysPct > st.CpuSysMaxPct {
			st.CpuSysMaxPct = sp.cpuSysPct
		}
		sumCpuProc += sp.cpuProcPct
		if sp.cpuProcPct > st.CpuProcMaxPct {
			st.CpuProcMaxPct = sp.cpuProcPct
		}
		sumMemMB += sp.memSysUsedMB
		if sp.memSysUsedMB > st.MemSysUsedMaxMB {
			st.MemSysUsedMaxMB = sp.memSysUsedMB
		}
		sumMemPct += sp.memSysUsedPct
		if sp.memSysUsedPct > st.MemSysUsedMaxPct {
			st.MemSysUsedMaxPct = sp.memSysUsedPct
		}
		sumHeap += sp.heapAllocMB
		if sp.heapAllocMB > st.HeapAllocMaxMB {
			st.HeapAllocMaxMB = sp.heapAllocMB
		}
		if sp.heapSysMB > st.HeapSysMaxMB {
			st.HeapSysMaxMB = sp.heapSysMB
		}
		if sp.stackInUseMB > st.StackInUseMaxMB {
			st.StackInUseMaxMB = sp.stackInUseMB
		}
		sumGoro += float64(sp.goroutines)
		if sp.goroutines > st.GoroutinesMax {
			st.GoroutinesMax = sp.goroutines
		}
	}
	n := float64(len(samples))
	st.CpuSysAvgPct = sumCpuSys / n
	st.CpuProcAvgPct = sumCpuProc / n
	st.MemSysUsedAvgMB = sumMemMB / n
	st.MemSysUsedAvgPct = sumMemPct / n
	st.HeapAllocAvgMB = sumHeap / n
	st.GoroutinesAvg = sumGoro / n
	return st
}

// ─────────────────────────────────────────────────────────────
// pumpAdapter — pumps platform.Events into the bot
// ─────────────────────────────────────────────────────────────
type pumpAdapter struct {
	ch       chan platform.Event
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	started  atomic.Bool
	injected atomic.Int64
	dropped  atomic.Int64
}

func newPumpAdapter(bufSize int) *pumpAdapter {
	return &pumpAdapter{ch: make(chan platform.Event, bufSize)}
}

func (a *pumpAdapter) Platform() string        { return "bench" }
func (a *pumpAdapter) Sender() platform.Sender { return &platform.NoopSender{} }
func (a *pumpAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}

func (a *pumpAdapter) Start(ctx context.Context, handler func(platform.Event)) error {
	if !a.started.CompareAndSwap(false, true) {
		return fmt.Errorf("pumpAdapter already started")
	}
	a.ctx, a.cancel = context.WithCancel(ctx)
	workers := runtime.NumCPU() * 2
	for range workers {
		a.wg.Go(func() {
			batch := make([]platform.Event, 0, 64)
			for {
				select {
				case <-a.ctx.Done():
					return
				case ev := <-a.ch:
					batch = append(batch, ev)
				}
			drain:
				for len(batch) < cap(batch) {
					select {
					case ev := <-a.ch:
						batch = append(batch, ev)
					default:
						break drain
					}
				}
				for _, ev := range batch {
					handler(ev)
					if be, ok := ev.(*benchEvent); ok {
						releaseBenchEvent(be)
					}
				}
				batch = batch[:0]
			}
		})
	}
	return nil
}

func (a *pumpAdapter) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *pumpAdapter) IsRunning() bool { return a.cancel != nil }

func (a *pumpAdapter) InjectEvent(ev platform.Event) {
	select {
	case a.ch <- ev:
		a.injected.Add(1)
	default:
		a.dropped.Add(1)
	}
}

// ─────────────────────────────────────────────────────────────
// Scenario
// ─────────────────────────────────────────────────────────────
type Scenario struct {
	Name            string
	Workers         int
	RatePerW        int
	Duration        time.Duration
	WithMW          bool
	ProdConcurrency int
	NumMatchers     int // extra matchers to stress-test merge+match pipeline
}

func (s Scenario) targetRate() int { return s.Workers * s.RatePerW }

func (s Scenario) prodConcurrency() int {
	if s.RatePerW > 0 {
		return s.Workers
	}
	if s.ProdConcurrency > 0 {
		return s.ProdConcurrency
	}
	n := max(runtime.GOMAXPROCS(0)/2, 1)
	return n
}

// ─────────────────────────────────────────────────────────────
// runMetrics — lock-free per-run counters
// ─────────────────────────────────────────────────────────────
type runMetrics struct {
	sent      atomic.Int64
	processed atomic.Int64
	failed    atomic.Int64
	latSum    atomic.Int64
	latCount  atomic.Int64
	latMin    atomic.Int64
	latMax    atomic.Int64
}

func newRunMetrics() *runMetrics {
	m := &runMetrics{}
	m.latMin.Store(math.MaxInt64)
	return m
}

func (m *runMetrics) recordLatency(ns int64) {
	m.latSum.Add(ns)
	m.latCount.Add(1)
	for {
		old := m.latMin.Load()
		if ns >= old || m.latMin.CompareAndSwap(old, ns) {
			break
		}
	}
	for {
		old := m.latMax.Load()
		if ns <= old || m.latMax.CompareAndSwap(old, ns) {
			break
		}
	}
}

// ─────────────────────────────────────────────────────────────
// ScenarioResult
// ─────────────────────────────────────────────────────────────
type ScenarioResult struct {
	Name            string  `json:"name"`
	Workers         int     `json:"workers"`
	RatePerWorker   int     `json:"rate_per_worker"`
	TargetRate      int     `json:"target_rate_per_s"`
	DurationSecs    float64 `json:"duration_secs"`
	GOMAXPROCS      int     `json:"gomaxprocs"`
	GoVersion       string  `json:"go_version"`
	IsUnlimited     bool    `json:"is_unlimited"`
	ProdConcurrency int     `json:"prod_concurrency"`
	ConsumerWorkers int     `json:"consumer_workers"`

	EventsSent       int64   `json:"events_sent"`
	EventsProcessed  int64   `json:"events_processed"`
	EventsFailed     int64   `json:"events_failed"`
	EventsDropped    int64   `json:"events_dropped"`
	SuccessRatePct   float64 `json:"success_rate_pct"`
	DropRatePct      float64 `json:"drop_rate_pct"`
	ThroughputActual float64 `json:"throughput_actual_per_s"`
	ThroughputTarget float64 `json:"throughput_target_per_s"`
	AchievementPct   float64 `json:"achievement_pct"`

	AvgLatencyMs float64 `json:"avg_latency_ms"`
	MinLatencyMs float64 `json:"min_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`

	CpuSysAvgPct     float64 `json:"cpu_sys_avg_pct"`
	CpuSysMaxPct     float64 `json:"cpu_sys_max_pct"`
	CpuProcAvgPct    float64 `json:"cpu_proc_avg_pct"`
	CpuProcMaxPct    float64 `json:"cpu_proc_max_pct"`
	MemSysUsedAvgMB  float64 `json:"mem_sys_used_avg_mb"`
	MemSysUsedMaxMB  float64 `json:"mem_sys_used_max_mb"`
	MemSysUsedAvgPct float64 `json:"mem_sys_used_avg_pct"`
	MemSysUsedMaxPct float64 `json:"mem_sys_used_max_pct"`
	HeapAllocAvgMB   float64 `json:"heap_alloc_avg_mb"`
	HeapAllocMaxMB   float64 `json:"heap_alloc_max_mb"`
	HeapSysMaxMB     float64 `json:"heap_sys_max_mb"`
	StackInUseMaxMB  float64 `json:"stack_in_use_max_mb"`
	GoroutinesAvg    float64 `json:"goroutines_avg"`
	GoroutinesMax    int     `json:"goroutines_max"`
	GCRuns           uint32  `json:"gc_runs"`
	GCPauseDeltaMs   float64 `json:"gc_pause_delta_ms"`
	GCPauseAvgMs     float64 `json:"gc_pause_avg_per_gc"`
	EngineMatchers   int     `json:"engine_matchers"`
}

// ─────────────────────────────────────────────────────────────
// runScenario
// ─────────────────────────────────────────────────────────────
func runScenario(s Scenario, globalDur time.Duration) ScenarioResult {
	dur := globalDur
	if s.Duration > 0 {
		dur = s.Duration
	}
	m := newRunMetrics()

	eng := engine.NewEngine()
	if s.WithMW {
		eng.Use(mw.Recover())
	}
	// Register extra matchers to stress the merge+match pipeline.
	for i := range s.NumMatchers {
		if i%2 == 0 {
			eng.OnEventKind(platform.EventKindPrivateMessage) // matches; incurs Match() call
		} else {
			eng.OnEventKind(platform.EventKindGroupMessage) // does not match; adds cache + merge cost
		}
	}
	eng.OnEventKind(platform.EventKindPrivateMessage).Handle(func(ctx *eventctx.Context) error {
		t0 := time.Now()
		m.processed.Add(1)
		m.recordLatency(time.Since(t0).Nanoseconds())
		return nil
	})

	consumerWorkers := runtime.NumCPU() * 2
	bufSize := max(s.Workers*max(s.RatePerW, 200)*2, 8192)
	pump := newPumpAdapter(bufSize)

	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(pump).
		WithEngine(eng).
		Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bot creation failed for %q: %v\n", s.Name, err)
		os.Exit(1)
	}
	if err := bot.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "bot.Start failed for %q: %v\n", s.Name, err)
		os.Exit(1)
	}
	time.Sleep(150 * time.Millisecond)

	_, _ = cpu.Percent(0, false)
	time.Sleep(50 * time.Millisecond)
	sampler := newSysSampler()
	sampler.Start(250 * time.Millisecond)

	prodCtx, prodCancel := context.WithTimeout(context.Background(), dur)
	defer prodCancel()
	start := time.Now()
	var prodWg sync.WaitGroup

	isUnlimited := s.RatePerW == 0
	prodCap := s.prodConcurrency()

	var sema chan struct{}
	if isUnlimited {
		sema = make(chan struct{}, prodCap)
	}

	for w := range s.Workers {
		prodWg.Add(1)
		go func(wid int) {
			defer prodWg.Done()
			var ticker *time.Ticker
			if s.RatePerW > 0 {
				ticker = time.NewTicker(time.Second / time.Duration(s.RatePerW))
				defer ticker.Stop()
			}
			var seq int64
			for {
				if s.RatePerW > 0 {
					select {
					case <-prodCtx.Done():
						return
					case <-ticker.C:
					}
				} else {
					select {
					case <-prodCtx.Done():
						return
					default:
					}
					select {
					case <-prodCtx.Done():
						return
					case sema <- struct{}{}:
					}
				}

				seq++
				ev := acquireBenchEvent()
				ev.platform = "bench"
				ev.kind = platform.EventKindPrivateMessage
				ev.id = fmt.Sprintf("w%d-s%d", wid, seq)
				ev.content = "bench"
				ev.sender = platform.UserInfo{ID: fmt.Sprintf("u%d", wid)}
				ev.chat = platform.ChatInfo{ID: fmt.Sprintf("c%d", wid)}
				ev.ts = time.Now()
				pump.InjectEvent(ev)
				m.sent.Add(1)

				if isUnlimited {
					<-sema
				}
			}
		}(w)
	}
	prodWg.Wait()
	elapsed := time.Since(start)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		inFlight := m.sent.Load() - m.processed.Load() - m.failed.Load() - pump.dropped.Load()
		if inFlight <= 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	sampler.Stop()
	sys := sampler.Aggregate()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = bot.Stop(stopCtx)

	sent := m.sent.Load()
	processed := m.processed.Load()
	failed := m.failed.Load()
	dropped := pump.dropped.Load()
	secs := elapsed.Seconds()
	var avgLat, minLat, maxLat float64
	if cnt := m.latCount.Load(); cnt > 0 {
		avgLat = float64(m.latSum.Load()) / float64(cnt) / 1e6
		minLat = float64(m.latMin.Load()) / 1e6
		maxLat = float64(m.latMax.Load()) / 1e6
	}
	tgtF := float64(s.targetRate())
	actualTP := float64(processed) / secs
	var achieve float64
	if tgtF > 0 {
		achieve = actualTP / tgtF * 100
	}
	var successPct, dropPct float64
	if sent > 0 {
		successPct = float64(processed) / float64(sent) * 100
		dropPct = float64(dropped) / float64(sent) * 100
	}
	matcherStats := eng.GetMatcherStats()
	return ScenarioResult{
		Name:             s.Name,
		Workers:          s.Workers,
		RatePerWorker:    s.RatePerW,
		TargetRate:       s.targetRate(),
		DurationSecs:     secs,
		GOMAXPROCS:       runtime.GOMAXPROCS(0),
		GoVersion:        runtime.Version(),
		IsUnlimited:      isUnlimited,
		ProdConcurrency:  prodCap,
		ConsumerWorkers:  consumerWorkers,
		EventsSent:       sent,
		EventsProcessed:  processed,
		EventsFailed:     failed,
		EventsDropped:    dropped,
		SuccessRatePct:   successPct,
		DropRatePct:      dropPct,
		ThroughputActual: actualTP,
		ThroughputTarget: tgtF,
		AchievementPct:   achieve,
		AvgLatencyMs:     avgLat,
		MinLatencyMs:     minLat,
		MaxLatencyMs:     maxLat,
		CpuSysAvgPct:     sys.CpuSysAvgPct,
		CpuSysMaxPct:     sys.CpuSysMaxPct,
		CpuProcAvgPct:    sys.CpuProcAvgPct,
		CpuProcMaxPct:    sys.CpuProcMaxPct,
		MemSysUsedAvgMB:  sys.MemSysUsedAvgMB,
		MemSysUsedMaxMB:  sys.MemSysUsedMaxMB,
		MemSysUsedAvgPct: sys.MemSysUsedAvgPct,
		MemSysUsedMaxPct: sys.MemSysUsedMaxPct,
		HeapAllocAvgMB:   sys.HeapAllocAvgMB,
		HeapAllocMaxMB:   sys.HeapAllocMaxMB,
		HeapSysMaxMB:     sys.HeapSysMaxMB,
		StackInUseMaxMB:  sys.StackInUseMaxMB,
		GoroutinesAvg:    sys.GoroutinesAvg,
		GoroutinesMax:    sys.GoroutinesMax,
		GCRuns:           sys.GCRuns,
		GCPauseDeltaMs:   sys.GCPauseDeltaMs,
		GCPauseAvgMs:     sys.GCPauseAvgMs,
		EngineMatchers:   matcherStats.Total,
	}
}

// ─────────────────────────────────────────────────────────────
// Suites
// ─────────────────────────────────────────────────────────────
func buildSuites() map[string][]Scenario {
	ncpu := runtime.NumCPU()
	unlimProd := max(ncpu/2, 1)
	return map[string][]Scenario{
		"quick": {
			{Name: "low         (100 msg/s, 0 matchers)", Workers: 10, RatePerW: 10},
			{Name: "mid        (5000 msg/s, 0 matchers)", Workers: 100, RatePerW: 50},
			{Name: "high     (20000 msg/s, 0 matchers)", Workers: 400, RatePerW: 50},
			{Name: "matcher  (20000 msg/s, 1K)", Workers: 400, RatePerW: 50, NumMatchers: 1000},
			{
				Name:            fmt.Sprintf("unlimited  (%d workers, sema=%d)", ncpu*4, unlimProd),
				Workers:         ncpu * 4,
				RatePerW:        0,
				ProdConcurrency: unlimProd,
			},
		},
		"standard": {
			{Name: "smoke      (100 msg/s, 0 matchers)", Workers: 10, RatePerW: 10},
			{Name: "medium    (1000 msg/s, 0 matchers)", Workers: 50, RatePerW: 20},
			{Name: "high      (5000 msg/s, 0 matchers)", Workers: 100, RatePerW: 50},
			{Name: "stress   (20000 msg/s, 0 matchers)", Workers: 400, RatePerW: 50},
			{Name: "extreme  (50000 msg/s, 0 matchers)", Workers: 1000, RatePerW: 50},
			// matcher scaling at 20K msg/s
			{Name: "matcher-100   (20K/100)", Workers: 400, RatePerW: 50, NumMatchers: 100},
			{Name: "matcher-1K    (20K/1K)", Workers: 400, RatePerW: 50, NumMatchers: 1000},
			{Name: "matcher-5K    (20K/5K)", Workers: 400, RatePerW: 50, NumMatchers: 5000},
			// matcher scaling at 50K msg/s
			{Name: "matcher-100   (50K/100)", Workers: 1000, RatePerW: 50, NumMatchers: 100},
			{Name: "matcher-1K    (50K/1K)", Workers: 1000, RatePerW: 50, NumMatchers: 1000},
			{
				Name:            fmt.Sprintf("unlimited  (%d workers, sema=%d)", ncpu*4, unlimProd),
				Workers:         ncpu * 4,
				RatePerW:        0,
				ProdConcurrency: unlimProd,
			},
		},
		"full": {
			{Name: "smoke       (100 msg/s, 0 matchers)", Workers: 10, RatePerW: 10},
			{Name: "light       (500 msg/s, 0 matchers)", Workers: 25, RatePerW: 20},
			{Name: "medium     (1000 msg/s, 0 matchers)", Workers: 50, RatePerW: 20},
			{Name: "moderate   (2000 msg/s, 0 matchers)", Workers: 100, RatePerW: 20},
			{Name: "high       (5000 msg/s, 0 matchers)", Workers: 100, RatePerW: 50},
			{Name: "stress    (10000 msg/s, 0 matchers)", Workers: 200, RatePerW: 50},
			{Name: "heavy     (20000 msg/s, 0 matchers)", Workers: 400, RatePerW: 50},
			{Name: "extreme   (50000 msg/s, 0 matchers)", Workers: 1000, RatePerW: 50},
			{Name: "max      (100000 msg/s, 0 matchers)", Workers: 2000, RatePerW: 50},
			// matcher scaling through throughput range
			{Name: "m100     (1000 msg/s, 100 matchers)", Workers: 50, RatePerW: 20, NumMatchers: 100},
			{Name: "m100    (5000 msg/s, 100 matchers)", Workers: 100, RatePerW: 50, NumMatchers: 100},
			{Name: "m1K    (20000 msg/s, 1K matchers)", Workers: 400, RatePerW: 50, NumMatchers: 1000},
			{Name: "m5K    (20000 msg/s, 5K matchers)", Workers: 400, RatePerW: 50, NumMatchers: 5000},
			{Name: "m10K   (20000 msg/s, 10K matchers)", Workers: 400, RatePerW: 50, NumMatchers: 10000},
			{
				Name:            fmt.Sprintf("unlimited  (%d workers, sema=%d)", ncpu*4, unlimProd),
				Workers:         ncpu * 4,
				RatePerW:        0,
				ProdConcurrency: unlimProd,
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────
// Printing
// ─────────────────────────────────────────────────────────────
const bar = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

func printBanner(suite string, dur time.Duration, withMW bool, gcPct int) {
	fmt.Println()
	fmt.Println(bar)
	fmt.Printf("  %-26s  Remilia Framework — Throughput Benchmark\n", "")
	fmt.Printf("  Go %-10s  GOMAXPROCS=%d  CPUs=%d  %s\n",
		runtime.Version(), runtime.GOMAXPROCS(0), runtime.NumCPU(),
		time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  Suite: %-12s  Duration/scenario: %-8v  Middleware: %v  GOGC: %d\n", suite, dur, withMW, gcPct)
	fmt.Println(bar)
}

func printScenarioTitle(i int, name string) {
	fmt.Printf("\n  ▶  Scenario %d: %s\n", i+1, name)
	fmt.Println("  " + strings.Repeat("─", 60))
}

func printResult(r ScenarioResult) {
	tgtStr := "unlimited (no rate limit)"
	achieveStr := "-"
	if r.TargetRate > 0 {
		tgtStr = fmt.Sprintf("%d msg/s", r.TargetRate)
		achieveStr = fmt.Sprintf("%.1f%%", r.AchievementPct)
	}
	fmt.Printf("  %-26s %s\n", "Target rate:", tgtStr)
	if r.IsUnlimited {
		fmt.Printf("  %-26s prod sema=%d  consumers=%d  (GOMAXPROCS=%d)\n",
			"Concurrency split:", r.ProdConcurrency, r.ConsumerWorkers, r.GOMAXPROCS)
	}
	fmt.Printf("  %-26s %.1f msg/s\n", "Actual throughput:", r.ThroughputActual)
	fmt.Printf("  %-26s %s\n", "Achievement:", achieveStr)
	fmt.Printf("  %-26s %.2f s\n", "Elapsed:", r.DurationSecs)
	fmt.Println()
	fmt.Printf("  %-26s %d\n", "Events sent:", r.EventsSent)
	fmt.Printf("  %-26s %d  (%.1f%%)\n", "Events processed:", r.EventsProcessed, r.SuccessRatePct)
	if r.EventsFailed > 0 {
		fmt.Printf("  %-26s %d\n", "Events failed:", r.EventsFailed)
	}
	if r.EventsDropped > 0 {
		fmt.Printf("  %-26s %d  (%.1f%% backpressure)\n", "Events dropped:", r.EventsDropped, r.DropRatePct)
	}
	fmt.Println()
	fmt.Printf("  %-26s %.4f ms\n", "Handler latency (avg):", r.AvgLatencyMs)
	fmt.Printf("  %-26s %.4f ms  /  %.4f ms\n", "Handler latency (min/max):", r.MinLatencyMs, r.MaxLatencyMs)
	fmt.Println()
	fmt.Printf("  %-26s %.1f%%  (peak %.1f%%)\n", "CPU system (avg/peak):", r.CpuSysAvgPct, r.CpuSysMaxPct)
	fmt.Printf("  %-26s %.1f%%  (peak %.1f%%)\n", "CPU process (avg/peak):", r.CpuProcAvgPct, r.CpuProcMaxPct)
	fmt.Println()
	fmt.Printf("  %-26s %.1f MB  (peak %.1f MB  /  %.1f%%)\n",
		"OS RAM used (avg):", r.MemSysUsedAvgMB, r.MemSysUsedMaxMB, r.MemSysUsedMaxPct)
	fmt.Printf("  %-26s avg %.1f MB  /  peak %.1f MB  (sys %.1f MB)\n",
		"Go heap alloc:", r.HeapAllocAvgMB, r.HeapAllocMaxMB, r.HeapSysMaxMB)
	fmt.Printf("  %-26s peak %.2f MB\n", "Go stack in-use:", r.StackInUseMaxMB)
	fmt.Println()
	fmt.Printf("  %-26s avg %.0f  /  peak %d\n", "Goroutines:", r.GoroutinesAvg, r.GoroutinesMax)
	if r.GCRuns > 0 {
		fmt.Printf("  %-26s %d runs  /  total pause %.2f ms  (avg %.3f ms/run)\n",
			"GC:", r.GCRuns, r.GCPauseDeltaMs, r.GCPauseAvgMs)
	} else {
		fmt.Printf("  %-26s 0 runs\n", "GC:")
	}
	fmt.Printf("  %-26s %d\n", "Engine matchers:", r.EngineMatchers)
}

func printSummary(results []ScenarioResult) {
	fmt.Println()
	fmt.Println(bar)
	fmt.Println("  Results Summary")
	fmt.Println(bar)
	fmt.Printf("  %-36s  %12s  %12s  %9s  %11s  %10s  %11s\n",
		"Scenario", "Target(msg/s)", "Actual(msg/s)", "Success%", "AvgLat(ms)",
		"CpuProc%", "HeapAlloc(MB)")
	fmt.Println("  " + strings.Repeat("─", 105))
	for _, r := range results {
		tgt := "unlimited"
		if r.TargetRate > 0 {
			tgt = fmt.Sprintf("%d", r.TargetRate)
		}
		fmt.Printf("  %-36s  %12s  %12.0f  %8.1f%%  %11.4f  %9.1f%%  %11.1f\n",
			r.Name, tgt, r.ThroughputActual, r.SuccessRatePct,
			r.AvgLatencyMs, r.CpuProcAvgPct, r.HeapAllocAvgMB)
	}
	fmt.Println(bar)
	satIdx := -1
	for i, r := range results {
		if r.TargetRate > 0 && r.AchievementPct >= 90 {
			satIdx = i
		}
	}
	if satIdx >= 0 && satIdx < len(results)-1 {
		sat := results[satIdx]
		fmt.Printf("\n  ℹ  Estimated saturation: ~%.0f msg/s  (scenario %q, %.1f%% achievement)\n",
			sat.ThroughputActual, sat.Name, sat.AchievementPct)
	}
	fmt.Printf("  ℹ  CPU logical cores: %d  ·  GOMAXPROCS: %d  ·  Go: %s\n",
		runtime.NumCPU(), runtime.GOMAXPROCS(0), runtime.Version())
	fmt.Println(bar)
}

// ─────────────────────────────────────────────────────────────
// main
// ─────────────────────────────────────────────────────────────
func main() {
	durFlag := flag.Duration("duration", 10*time.Second, "per-scenario test duration")
	suiteFlag := flag.String("suite", "standard", `scenario suite: "quick" | "standard" | "full"`)
	mwFlag := flag.Bool("middleware", true, "attach Recover middleware to the engine")
	outputFlag := flag.String("output", "", "write JSON results to this file (optional)")
	gcPctFlag := flag.Int("gcpercent", 100, "GOGC value (100=default, 200=less frequent GC, -1=off)")
	flag.Parse()

	if *gcPctFlag != 100 {
		debug.SetGCPercent(*gcPctFlag)
	}

	_ = logger.Init(logger.Config{Level: "error", Console: false})
	suites := buildSuites()
	scenarios, ok := suites[*suiteFlag]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown suite %q  (choose: quick | standard | full)\n", *suiteFlag)
		os.Exit(1)
	}
	for i := range scenarios {
		scenarios[i].WithMW = *mwFlag
	}
	printBanner(*suiteFlag, *durFlag, *mwFlag, *gcPctFlag)
	var results []ScenarioResult
	for i, s := range scenarios {
		printScenarioTitle(i, s.Name)
		r := runScenario(s, *durFlag)
		printResult(r)
		results = append(results, r)
		if i < len(scenarios)-1 {
			runtime.GC()
			time.Sleep(1 * time.Second)
		}
	}
	printSummary(results)
	if *outputFlag != "" {
		data, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(*outputFlag, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write JSON: %v\n", err)
		} else {
			fmt.Printf("\n  Results written to %s\n", *outputFlag)
		}
	}
}
