// Package main provides a standalone throughput benchmark for the remilia framework.
//
// Usage:
//
// cd examples/benchmark && go run throughput_bench.go
// cd examples/benchmark && go run throughput_bench.go -duration 15s -suite quick
// cd examples/benchmark && go run throughput_bench.go -suite full -output results.json
//
// Flags:
//
// -duration   per-scenario test duration (default 10s)
// -suite      "quick" | "standard" | "full"  (default standard)
// -middleware whether to enable middleware (default true)
// -output     path to write JSON results (optional)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	mw "github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// ─────────────────────────────────────────────────────────────
// System metrics sampler
// ─────────────────────────────────────────────────────────────
// sysSampler polls system-level and Go runtime metrics at a fixed interval.
// It runs in a background goroutine for the duration of each scenario.
type sysSampler struct {
	mu      sync.Mutex
	samples []sysSample
	stop    chan struct{}
	done    chan struct{}
}
type sysSample struct {
	ts             time.Time
	cpuSysPct      float64 // system-wide CPU %
	cpuProcPct     float64 // this process CPU %
	memSysUsedMB   float64 // OS-level used RAM (MB)
	memSysUsedPct  float64 // OS-level RAM %
	heapAllocMB    float64 // Go heap in-use (MB)
	heapSysMB      float64 // Go heap obtained from OS (MB)
	stackInUseMB   float64 // Go stacks (MB)
	goroutines     int
	gcRuns         uint32
	gcPauseTotalMs float64 // cumulative GC pause (ms)
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
	// system-wide CPU (non-blocking: 0 interval = since last call)
	if pcts, err := cpu.Percent(0, false); err == nil && len(pcts) > 0 {
		sample.cpuSysPct = pcts[0]
	}
	// this-process CPU
	if proc != nil {
		if pct, err := proc.CPUPercent(); err == nil {
			sample.cpuProcPct = pct
		}
	}
	// system RAM
	if vmStat, err := mem.VirtualMemory(); err == nil {
		sample.memSysUsedMB = float64(vmStat.Used) / 1e6
		sample.memSysUsedPct = vmStat.UsedPercent
	}
	s.mu.Lock()
	s.samples = append(s.samples, sample)
	s.mu.Unlock()
}

// sysStats aggregates all collected samples into summary statistics.
type sysStats struct {
	// CPU
	CpuSysAvgPct  float64
	CpuSysMaxPct  float64
	CpuProcAvgPct float64
	CpuProcMaxPct float64
	// OS RAM
	MemSysUsedAvgMB  float64
	MemSysUsedMaxMB  float64
	MemSysUsedAvgPct float64
	MemSysUsedMaxPct float64
	// Go heap
	HeapAllocAvgMB float64
	HeapAllocMaxMB float64
	HeapSysMaxMB   float64
	// Go stack
	StackInUseMaxMB float64
	// Goroutines
	GoroutinesAvg float64
	GoroutinesMax int
	// GC
	GCRuns         uint32
	GCPauseDeltaMs float64 // GC pause added during this scenario
	GCPauseAvgMs   float64 // avg per GC run during scenario
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
	// GC delta: compare first and last sample
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
// pumpAdapter
// ─────────────────────────────────────────────────────────────
type pumpAdapter struct {
	ch       chan *dto.Payload
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	started  atomic.Bool
	injected atomic.Int64
	dropped  atomic.Int64
}

func newPumpAdapter(bufSize int) *pumpAdapter {
	return &pumpAdapter{ch: make(chan *dto.Payload, bufSize)}
}
func (a *pumpAdapter) Start(ctx context.Context, handler func(*dto.Payload)) error {
	if !a.started.CompareAndSwap(false, true) {
		return fmt.Errorf("pumpAdapter already started")
	}
	a.ctx, a.cancel = context.WithCancel(ctx)
	for range runtime.NumCPU() * 2 {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			for {
				select {
				case <-a.ctx.Done():
					return
				case ev := <-a.ch:
					handler(ev)
				}
			}
		}()
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
func (a *pumpAdapter) InjectEvent(p *dto.Payload) {
	select {
	case a.ch <- p:
		a.injected.Add(1)
	default:
		a.dropped.Add(1)
	}
}

// ─────────────────────────────────────────────────────────────
// Scenario descriptor
// ─────────────────────────────────────────────────────────────

type Scenario struct {
	Name     string
	Workers  int
	RatePerW int           // msg/s per worker; 0 = unlimited
	Duration time.Duration // 0 = use global flag
	WithMW   bool
}

func (s Scenario) targetRate() int { return s.Workers * s.RatePerW }

// ─────────────────────────────────────────────────────────────
// Per-run event metrics (lock-free)
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
// ScenarioResult — JSON-serialisable output
// ─────────────────────────────────────────────────────────────

type ScenarioResult struct {
	// ── Identity ──
	Name          string  `json:"name"`
	Workers       int     `json:"workers"`
	RatePerWorker int     `json:"rate_per_worker"`
	TargetRate    int     `json:"target_rate_per_s"`
	DurationSecs  float64 `json:"duration_secs"`
	GOMAXPROCS    int     `json:"gomaxprocs"`
	GoVersion     string  `json:"go_version"`
	// ── Throughput ──
	EventsSent       int64   `json:"events_sent"`
	EventsProcessed  int64   `json:"events_processed"`
	EventsFailed     int64   `json:"events_failed"`
	EventsDropped    int64   `json:"events_dropped"`
	SuccessRatePct   float64 `json:"success_rate_pct"`
	DropRatePct      float64 `json:"drop_rate_pct"`
	ThroughputActual float64 `json:"throughput_actual_per_s"`
	ThroughputTarget float64 `json:"throughput_target_per_s"`
	AchievementPct   float64 `json:"achievement_pct"`
	// ── Handler latency ──
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	MinLatencyMs float64 `json:"min_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`
	// ── CPU ──
	CpuSysAvgPct  float64 `json:"cpu_sys_avg_pct"`  // system-wide CPU, average
	CpuSysMaxPct  float64 `json:"cpu_sys_max_pct"`  // system-wide CPU, peak
	CpuProcAvgPct float64 `json:"cpu_proc_avg_pct"` // this process CPU, average
	CpuProcMaxPct float64 `json:"cpu_proc_max_pct"` // this process CPU, peak
	// ── OS memory ──
	MemSysUsedAvgMB  float64 `json:"mem_sys_used_avg_mb"`
	MemSysUsedMaxMB  float64 `json:"mem_sys_used_max_mb"`
	MemSysUsedAvgPct float64 `json:"mem_sys_used_avg_pct"`
	MemSysUsedMaxPct float64 `json:"mem_sys_used_max_pct"`
	// ── Go heap ──
	HeapAllocAvgMB float64 `json:"heap_alloc_avg_mb"`
	HeapAllocMaxMB float64 `json:"heap_alloc_max_mb"`
	HeapSysMaxMB   float64 `json:"heap_sys_max_mb"`
	// ── Go stack ──
	StackInUseMaxMB float64 `json:"stack_in_use_max_mb"`
	// ── Goroutines ──
	GoroutinesAvg float64 `json:"goroutines_avg"`
	GoroutinesMax int     `json:"goroutines_max"`
	// ── GC ──
	GCRuns         uint32  `json:"gc_runs"`
	GCPauseDeltaMs float64 `json:"gc_pause_delta_ms"`   // total GC pause added during test
	GCPauseAvgMs   float64 `json:"gc_pause_avg_per_gc"` // per-run average
	// ── Engine ──
	EngineMatchers int `json:"engine_matchers"`
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
	// ── Engine + handler ──
	eng := engine.NewEngine()
	if s.WithMW {
		eng.Use(mw.Recover())
	}
	eng.On(dto.C2CMessageCreate).Handle(func(ctx *eventctx.Context) error {
		t0 := time.Now()
		var ev dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&ev); err != nil {
			m.failed.Add(1)
			return err
		}
		m.recordLatency(time.Since(t0).Nanoseconds())
		m.processed.Add(1)
		return nil
	})
	// ── Adapter + Bot ──
	bufSize := max(s.Workers*max(s.RatePerW, 200)*2, 8192)
	pump := newPumpAdapter(bufSize)
	bot := remilia.NewBot(pump, eng)
	if err := bot.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "bot.Start failed for %q: %v\n", s.Name, err)
		os.Exit(1)
	}
	time.Sleep(150 * time.Millisecond) // warm-up
	// ── Start system sampler ──
	// First cpu.Percent call initializes the per-process baseline; discard.
	_, _ = cpu.Percent(0, false)
	time.Sleep(50 * time.Millisecond)
	sampler := newSysSampler()
	sampler.Start(250 * time.Millisecond) // sample every 250 ms
	// ── Produce events ──
	prodCtx, prodCancel := context.WithTimeout(context.Background(), dur)
	defer prodCancel()
	start := time.Now()
	var prodWg sync.WaitGroup
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
						runtime.Gosched()
					}
				}
				seq++
				eid := dto.EventID(fmt.Sprintf("w%d-s%d", wid, seq))
				pump.InjectEvent(&dto.Payload{
					ID:        eid,
					Type:      dto.C2CMessageCreate,
					Operation: dto.Dispatch,
					Detail:    fmt.Appendf(nil, `{"id":%q,"content":"bench","author":{"user_openid":"u%d"}}`, eid, wid),
				})
				m.sent.Add(1)
			}
		}(w)
	}
	prodWg.Wait()
	elapsed := time.Since(start)
	// ── Drain in-flight events (up to 3 s) ──
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		inFlight := m.sent.Load() - m.processed.Load() - m.failed.Load() - pump.dropped.Load()
		if inFlight <= 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// ── Stop sampler & bot ──
	sampler.Stop()
	sys := sampler.Aggregate()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = bot.Stop(stopCtx)
	// ── Aggregate event metrics ──
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
		// system
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
	return map[string][]Scenario{
		"quick": {
			{Name: "low    (100 msg/s)", Workers: 10, RatePerW: 10},
			{Name: "mid   (5000 msg/s)", Workers: 100, RatePerW: 50},
			{Name: "high (20000 msg/s)", Workers: 400, RatePerW: 50},
		},
		"standard": {
			{Name: "smoke      (100 msg/s)", Workers: 10, RatePerW: 10},
			{Name: "medium    (1000 msg/s)", Workers: 50, RatePerW: 20},
			{Name: "high      (5000 msg/s)", Workers: 100, RatePerW: 50},
			{Name: "stress   (20000 msg/s)", Workers: 400, RatePerW: 50},
			{Name: "extreme  (50000 msg/s)", Workers: 1000, RatePerW: 50},
			{Name: fmt.Sprintf("unlimited  (%d workers, no rate limit)", ncpu*4), Workers: ncpu * 4, RatePerW: 0},
		},
		"full": {
			{Name: "smoke       (100 msg/s)", Workers: 10, RatePerW: 10},
			{Name: "light       (500 msg/s)", Workers: 25, RatePerW: 20},
			{Name: "medium     (1000 msg/s)", Workers: 50, RatePerW: 20},
			{Name: "moderate   (2000 msg/s)", Workers: 100, RatePerW: 20},
			{Name: "high       (5000 msg/s)", Workers: 100, RatePerW: 50},
			{Name: "stress    (10000 msg/s)", Workers: 200, RatePerW: 50},
			{Name: "heavy     (20000 msg/s)", Workers: 400, RatePerW: 50},
			{Name: "extreme   (50000 msg/s)", Workers: 1000, RatePerW: 50},
			{Name: "max      (100000 msg/s)", Workers: 2000, RatePerW: 50},
			{Name: fmt.Sprintf("unlimited  (%d workers, no rate limit)", ncpu*4), Workers: ncpu * 4, RatePerW: 0},
		},
	}
}

// ─────────────────────────────────────────────────────────────
// Printing helpers
// ─────────────────────────────────────────────────────────────
const bar = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

func printBanner(suite string, dur time.Duration, withMW bool) {
	fmt.Println()
	fmt.Println(bar)
	fmt.Printf("  %-26s  Remilia Framework — Throughput Benchmark\n", "")
	fmt.Printf("  Go %-10s  GOMAXPROCS=%d  CPUs=%d  %s\n",
		runtime.Version(), runtime.GOMAXPROCS(0), runtime.NumCPU(),
		time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  Suite: %-12s  Duration/scenario: %-8v  Middleware: %v\n", suite, dur, withMW)
	fmt.Println(bar)
}
func printScenarioTitle(i int, name string) {
	fmt.Printf("\n  ▶  Scenario %d: %s\n", i+1, name)
	fmt.Println("  " + strings.Repeat("─", 60))
}
func printResult(r ScenarioResult) {
	tgtStr := "unlimited (no rate limit)"
	achieveStr := "—"
	if r.TargetRate > 0 {
		tgtStr = fmt.Sprintf("%d msg/s", r.TargetRate)
		achieveStr = fmt.Sprintf("%.1f%%", r.AchievementPct)
	}
	// ── Throughput ──
	fmt.Printf("  %-26s %s\n", "Target rate:", tgtStr)
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
	// ── Handler latency ──
	fmt.Println()
	fmt.Printf("  %-26s %.4f ms\n", "Handler latency (avg):", r.AvgLatencyMs)
	fmt.Printf("  %-26s %.4f ms  /  %.4f ms\n", "Handler latency (min/max):", r.MinLatencyMs, r.MaxLatencyMs)
	// ── CPU ──
	fmt.Println()
	fmt.Printf("  %-26s %.1f%%  (peak %.1f%%)\n", "CPU system (avg/peak):", r.CpuSysAvgPct, r.CpuSysMaxPct)
	fmt.Printf("  %-26s %.1f%%  (peak %.1f%%)\n", "CPU process (avg/peak):", r.CpuProcAvgPct, r.CpuProcMaxPct)
	// ── Memory ──
	fmt.Println()
	fmt.Printf("  %-26s %.1f MB  (peak %.1f MB  /  %.1f%%)\n",
		"OS RAM used (avg):", r.MemSysUsedAvgMB, r.MemSysUsedMaxMB, r.MemSysUsedMaxPct)
	fmt.Printf("  %-26s avg %.1f MB  /  peak %.1f MB  (sys %.1f MB)\n",
		"Go heap alloc:", r.HeapAllocAvgMB, r.HeapAllocMaxMB, r.HeapSysMaxMB)
	fmt.Printf("  %-26s peak %.2f MB\n", "Go stack in-use:", r.StackInUseMaxMB)
	// ── Goroutines & GC ──
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
	// Throughput table
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
	// Saturation hint
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
	flag.Parse()
	// Silence framework logs so they don't skew timing measurements.
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
	printBanner(*suiteFlag, *durFlag, *mwFlag)
	var results []ScenarioResult
	for i, s := range scenarios {
		printScenarioTitle(i, s.Name)
		r := runScenario(s, *durFlag)
		printResult(r)
		results = append(results, r)
		if i < len(scenarios)-1 {
			// Cool-down: let GC finish, OS reclaim RSS, etc.
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
