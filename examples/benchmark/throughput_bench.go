// Package main provides a standalone throughput benchmark for the remilia framework.
//
// Usage:
//
//	cd examples/benchmark && go run throughput_bench.go
//	cd examples/benchmark && go run throughput_bench.go -duration 15s -suite quick
//	cd examples/benchmark && go run throughput_bench.go -suite full -output results.json
//	cd examples/benchmark && go run throughput_bench.go -inject-mode blocking -suite matcher5k
//
// Flags:
//
//	-duration       per-scenario test duration (default 10s)
//	-suite          "quick" | "standard" | "full" | "matcher5k"  (default standard)
//	-inject-mode    "nonblocking" (real-world, may drop) | "blocking" (backpressure) (default nonblocking)
//	-middleware     whether to enable middleware (default true)
//	-disable-latency  disable latency percentile tracking to reduce overhead (default false)
//	-output         path to write JSON results (optional)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
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
	blocking bool // true = blocking send (backpressure), false = non-blocking (real-world)
}

func newPumpAdapter(bufSize int, blocking bool) *pumpAdapter {
	return &pumpAdapter{ch: make(chan platform.Event, bufSize), blocking: blocking}
}

func (a *pumpAdapter) InjectEvent(ev platform.Event) {
	if a.blocking {
		select {
		case a.ch <- ev:
			a.injected.Add(1)
		case <-a.ctx.Done():
		}
		return
	}
	select {
	case a.ch <- ev:
		a.injected.Add(1)
	default:
		a.dropped.Add(1)
	}
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

// ─────────────────────────────────────────────────────────────
// latencyTracker — 百分位延迟采样
// ─────────────────────────────────────────────────────────────
type latencyTracker struct {
	mu         sync.Mutex
	samples    []float64 // ms
	total      int64
	maxSamples int
	rng        *rand.Rand
}

func newLatencyTracker(maxSamples int) *latencyTracker {
	return &latencyTracker{
		samples:    make([]float64, 0, maxSamples),
		maxSamples: maxSamples,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (lt *latencyTracker) Record(ms float64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.total++
	if len(lt.samples) < lt.maxSamples {
		lt.samples = append(lt.samples, ms)
		return
	}
	// reservoir sampling: replace random element with probability maxSamples/total
	if lt.rng.Float64() < float64(lt.maxSamples)/float64(lt.total) {
		lt.samples[lt.rng.Intn(lt.maxSamples)] = ms
	}
}

func (lt *latencyTracker) Percentiles() (p50, p95, p99 float64) {
	lt.mu.Lock()
	sorted := make([]float64, len(lt.samples))
	copy(sorted, lt.samples)
	lt.mu.Unlock()
	if len(sorted) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(sorted)
	n := len(sorted)
	return sorted[n*50/100], sorted[n*95/100], sorted[n*99/100]
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
	latMin    atomic.Int64
	latMax    atomic.Int64
	latency   *latencyTracker
}

func newRunMetrics() *runMetrics {
	return &runMetrics{
		latMin:  atomic.Int64{},
		latMax:  atomic.Int64{},
		latency: newLatencyTracker(100000),
	}
}

func (m *runMetrics) recordLatency(ms float64) {
	m.latency.Record(ms)
	for {
		old := m.latMin.Load()
		ns := int64(ms * 1e6)
		if ns >= old || m.latMin.CompareAndSwap(old, ns) {
			break
		}
	}
	for {
		old := m.latMax.Load()
		ns := int64(ms * 1e6)
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
	DrainTimeSecs   float64 `json:"drain_time_secs"`
	GOMAXPROCS      int     `json:"gomaxprocs"`
	GoVersion       string  `json:"go_version"`
	IsUnlimited     bool    `json:"is_unlimited"`
	ProdConcurrency int     `json:"prod_concurrency"`
	ConsumerWorkers int     `json:"consumer_workers"`
	InjectMode      string  `json:"inject_mode"`

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
	LatencyP50   float64 `json:"latency_p50_ms"`
	LatencyP95   float64 `json:"latency_p95_ms"`
	LatencyP99   float64 `json:"latency_p99_ms"`

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
func runScenario(s Scenario, globalDur time.Duration, injectMode string, disableLatency bool) ScenarioResult {
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
	if disableLatency {
		eng.OnEventKind(platform.EventKindPrivateMessage).Handle(func(ctx *eventctx.Context) error {
			m.processed.Add(1)
			return nil
		})
	} else {
		eng.OnEventKind(platform.EventKindPrivateMessage).Handle(func(ctx *eventctx.Context) error {
			ev := ctx.GetPlatformEvent()
			latencyMs := time.Since(ev.Timestamp()).Seconds() * 1e3
			m.processed.Add(1)
			m.recordLatency(latencyMs)
			return nil
		})
	}

	blocking := injectMode == "blocking"
	consumerWorkers := runtime.NumCPU() * 2
	bufSize := max(s.Workers*max(s.RatePerW, 200)*2, 8192)
	pump := newPumpAdapter(bufSize, blocking)

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

	for w := range s.Workers {
		prodWg.Add(1)
		go func(wid int) {
			defer prodWg.Done()
			var ticker *time.Ticker
			if s.RatePerW > 0 {
				ticker = time.NewTicker(time.Second / time.Duration(s.RatePerW))
				defer ticker.Stop()
			}
			// 静态字段，避免每事件 Sprintf
			sender := platform.UserInfo{ID: "bench"}
			chat := platform.ChatInfo{ID: "bench"}
			for {
				if s.RatePerW > 0 {
					select {
					case <-prodCtx.Done():
						return
					case <-ticker.C:
					}
				} else {
					// Unlimited: 直接死循环注入，不做任何限流
					select {
					case <-prodCtx.Done():
						return
					default:
					}
				}

				ev := acquireBenchEvent()
				ev.platform = "bench"
				ev.kind = platform.EventKindPrivateMessage
				ev.id = ""
				ev.content = "bench"
				ev.sender = sender
				ev.chat = chat
				ev.ts = time.Now()
				pump.InjectEvent(ev)
				m.sent.Add(1)
			}
		}(w)
	}
	prodWg.Wait()
	elapsed := time.Since(start)

	drainStart := time.Now()
	drainDeadline := drainStart.Add(30 * time.Second)
	for time.Now().Before(drainDeadline) {
		inFlight := m.sent.Load() - m.processed.Load() - m.failed.Load() - pump.dropped.Load()
		if inFlight <= 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	drainTime := time.Since(drainStart)

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
	drainSecs := drainTime.Seconds()
	var p50, p95, p99, minLat, maxLat float64
	if !disableLatency {
		p50, p95, p99 = m.latency.Percentiles()
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
		DrainTimeSecs:    drainSecs,
		GOMAXPROCS:       runtime.GOMAXPROCS(0),
		GoVersion:        runtime.Version(),
		IsUnlimited:      isUnlimited,
		ProdConcurrency:  prodCap,
		ConsumerWorkers:  consumerWorkers,
		InjectMode:       injectMode,
		EventsSent:       sent,
		EventsProcessed:  processed,
		EventsFailed:     failed,
		EventsDropped:    dropped,
		SuccessRatePct:   successPct,
		DropRatePct:      dropPct,
		ThroughputActual: actualTP,
		ThroughputTarget: tgtF,
		AchievementPct:   achieve,
		MinLatencyMs:     minLat,
		MaxLatencyMs:     maxLat,
		LatencyP50:       p50,
		LatencyP95:       p95,
		LatencyP99:       p99,
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
		"matcher5k": {
			{Name: "baseline (100 msg/s, 0 matchers)", Workers: 10, RatePerW: 10},
			{Name: "stress   (20K msg/s, 0 matchers)", Workers: 400, RatePerW: 50},
			{Name: "matcher-5K (20K/5K)", Workers: 400, RatePerW: 50, NumMatchers: 5000},
			{Name: "matcher-5K (50K/5K)", Workers: 1000, RatePerW: 50, NumMatchers: 5000},
			{
				Name:        fmt.Sprintf("unlimited 5K matchers  (%d workers)", ncpu*4),
				Workers:     ncpu * 4,
				RatePerW:    0,
				NumMatchers: 5000,
			},
		},
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

func printBanner(suite string, dur time.Duration, withMW bool, gcPct int, injectMode string, disableLat bool) {
	fmt.Println()
	fmt.Println(bar)
	fmt.Printf("  %-26s  Remilia Framework — Throughput Benchmark\n", "")
	fmt.Printf("  Go %-10s  GOMAXPROCS=%d  CPUs=%d  %s\n",
		runtime.Version(), runtime.GOMAXPROCS(0), runtime.NumCPU(),
		time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  Suite: %-12s  Duration/scenario: %-8v  Middleware: %v  GOGC: %d  Inject: %s  Latency: %v\n",
		suite, dur, withMW, gcPct, injectMode, !disableLat)
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
	fmt.Printf("  %-26s %.2f s (drain: %.2f s)\n", "Elapsed:", r.DurationSecs, r.DrainTimeSecs)
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
	fmt.Printf("  %-26s %.4f ms\n", "Latency (min):", r.MinLatencyMs)
	fmt.Printf("  %-26s %.4f ms\n", "Latency (P50):", r.LatencyP50)
	fmt.Printf("  %-26s %.4f ms\n", "Latency (P95):", r.LatencyP95)
	fmt.Printf("  %-26s %.4f ms\n", "Latency (P99):", r.LatencyP99)
	fmt.Printf("  %-26s %.4f ms\n", "Latency (max):", r.MaxLatencyMs)
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
	fmt.Printf("  %-36s  %12s  %12s  %9s  %9s  %9s  %9s  %10s  %8s\n",
		"Scenario", "Target/s", "Actual/s", "Succ%", "P50(ms)", "P99(ms)",
		"Drain(s)", "CpuProc%", "Goro")
	fmt.Println("  " + strings.Repeat("─", 130))
	for _, r := range results {
		tgt := "unlimited"
		if r.TargetRate > 0 {
			tgt = fmt.Sprintf("%d", r.TargetRate)
		}
		fmt.Printf("  %-36s  %12s  %12.0f  %8.1f%%  %9.4f  %9.4f  %9.2f  %10.1f%%  %8d\n",
			r.Name, tgt, r.ThroughputActual, r.SuccessRatePct,
			r.LatencyP50, r.LatencyP99, r.DrainTimeSecs,
			r.CpuProcAvgPct, r.GoroutinesMax)
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
	suiteFlag := flag.String("suite", "standard", `scenario suite: "quick" | "standard" | "full" | "matcher5k"`)
	mwFlag := flag.Bool("middleware", true, "attach Recover middleware to the engine")
	injectModeFlag := flag.String("inject-mode", "nonblocking", `"nonblocking" (real-world, may drop) | "blocking" (backpressure)`)
	disableLatFlag := flag.Bool("disable-latency", false, "disable latency percentile tracking to reduce overhead")
	outputFlag := flag.String("output", "", "write JSON results to this file (optional)")
	gcPctFlag := flag.Int("gcpercent", 100, "GOGC value (100=default, 200=less frequent GC, -1=off)")
	flag.Parse()

	if *gcPctFlag != 100 {
		debug.SetGCPercent(*gcPctFlag)
	}

	injectMode := *injectModeFlag
	if injectMode != "blocking" && injectMode != "nonblocking" {
		fmt.Fprintf(os.Stderr, "invalid inject-mode %q (choose: blocking | nonblocking)\n", injectMode)
		os.Exit(1)
	}

	_ = logger.Init(logger.Config{Level: "error", Console: false})
	suites := buildSuites()
	scenarios, ok := suites[*suiteFlag]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown suite %q  (choose: quick | standard | full | matcher5k)\n", *suiteFlag)
		os.Exit(1)
	}
	for i := range scenarios {
		scenarios[i].WithMW = *mwFlag
	}
	printBanner(*suiteFlag, *durFlag, *mwFlag, *gcPctFlag, injectMode, *disableLatFlag)
	var results []ScenarioResult
	for i, s := range scenarios {
		printScenarioTitle(i, s.Name)
		r := runScenario(s, *durFlag, injectMode, *disableLatFlag)
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
