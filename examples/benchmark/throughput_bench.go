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
	qqplatform "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
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
func (a *pumpAdapter) Platform() string        { return "qq" }
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
			batch := make([]*dto.Payload, 0, 64)
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
					handler(qqplatform.NewEvent(ev))
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
func (a *pumpAdapter) InjectEvent(p *dto.Payload) {
	select {
	case a.ch <- p:
		a.injected.Add(1)
	default:
		a.dropped.Add(1)
	}
}

// acquirePayload obtains a *dto.Payload from dto's own pool and fills in the
// benchmark fields. Ownership is transferred to bot.handleEvent which will
// call dto.ReleasePayload after processing — do NOT call dto.ReleasePayload
// (or any custom release) inside the event handler.
func acquirePayload(id dto.EventID, wid int) *dto.Payload {
	p := dto.AcquirePayload()
	p.ID = id
	p.Type = dto.C2CMessageCreate
	p.Operation = dto.Dispatch
	p.Detail = fmt.Appendf(p.Detail, `{"id":%q,"content":"bench","author":{"user_openid":"u%d"}}`, id, wid)
	return p
}

// detailPool recycles the []byte slices used for Payload.Detail in the
// benchmark producer goroutines.  The pool is sized to 256 bytes which is
// sufficient for the bench JSON payload; larger allocations fall back to the
// heap as usual.
var detailPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 256)
		return &b
	},
}

func acquireDetail() []byte {
	return *detailPool.Get().(*[]byte)
}

func releaseDetail(b []byte) {
	b = b[:0]
	detailPool.Put(&b)
}

type Scenario struct {
	Name     string
	Workers  int
	RatePerW int           // msg/s per worker; 0 = unlimited
	Duration time.Duration // 0 = use global flag
	WithMW   bool
	// ProdConcurrency caps how many producer goroutines may be
	// simultaneously active in the unlimited (RatePerW==0) path.
	// 0 means "use default": GOMAXPROCS/2, minimum 1.
	// This prevents producers from starving the adapter workers
	// (consumers) for OS threads when all goroutines compete for
	// the same P slots.
	ProdConcurrency int
}

func (s Scenario) targetRate() int { return s.Workers * s.RatePerW }

// prodConcurrency returns the effective producer concurrency cap for
// the unlimited path, resolving the zero-value default.
func (s Scenario) prodConcurrency() int {
	if s.RatePerW > 0 {
		// rate-limited path does not need a cap
		return s.Workers
	}
	if s.ProdConcurrency > 0 {
		return s.ProdConcurrency
	}
	// Default: leave half the Ps for the adapter/engine consumer workers.
	n := max(runtime.GOMAXPROCS(0)/2, 1)
	return n
}

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
	IsUnlimited   bool    `json:"is_unlimited"`
	// ProdConcurrency is the semaphore width used in the unlimited path
	// (how many producer goroutines may hold the CPU simultaneously).
	ProdConcurrency int `json:"prod_concurrency"`
	// ConsumerWorkers is the number of adapter dispatch goroutines.
	ConsumerWorkers int `json:"consumer_workers"`
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
	CpuSysAvgPct  float64 `json:"cpu_sys_avg_pct"`
	CpuSysMaxPct  float64 `json:"cpu_sys_max_pct"`
	CpuProcAvgPct float64 `json:"cpu_proc_avg_pct"`
	CpuProcMaxPct float64 `json:"cpu_proc_max_pct"`
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
	GCPauseDeltaMs float64 `json:"gc_pause_delta_ms"`
	GCPauseAvgMs   float64 `json:"gc_pause_avg_per_gc"`
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
		m.recordLatency(time.Since(t0).Nanoseconds())
		m.processed.Add(1)
		// NOTE: Do NOT release the payload here. bot.handleEvent() owns the
		// payload lifetime and calls dto.ReleasePayload after ProcessEvent
		// returns. Releasing it inside the handler causes a use-after-free:
		// the payload is recycled by a producer while ctx still holds a
		// reference to it, corrupting the event.Type string and causing a
		// nil-pointer panic in the map lookup inside ProcessEvent.
		return nil
	})
	// ── Adapter + Bot ──
	consumerWorkers := runtime.NumCPU() * 2
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

	isUnlimited := s.RatePerW == 0
	prodCap := s.prodConcurrency()

	// semaphore: only used in the unlimited path to cap how many
	// producers are simultaneously running (not just goroutine-alive).
	// Sized to prodCap; producers acquire before injecting, release after.
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
					// ── rate-limited path (unchanged) ──
					select {
					case <-prodCtx.Done():
						return
					case <-ticker.C:
					}
				} else {
					// ── unlimited path: semaphore instead of Gosched spin ──
					// First check context cheaply.
					select {
					case <-prodCtx.Done():
						return
					default:
					}
					// Block until a slot is free; this yields the P to other
					// goroutines (consumers) while we wait, instead of spinning.
					select {
					case <-prodCtx.Done():
						return
					case sema <- struct{}{}:
					}
				}

				seq++
				// Build the EventID string in a pool buffer to avoid fmt.Sprintf alloc.
				idBuf := acquireDetail()
				idBuf = fmt.Appendf(idBuf, "w%d-s%d", wid, seq)
				eid := dto.EventID(idBuf)
				releaseDetail(idBuf)

				payload := acquirePayload(eid, wid)
				pump.InjectEvent(payload)
				m.sent.Add(1)

				// Release semaphore slot after inject (not after handler finishes —
				// we measure the channel-send cost, not the downstream processing).
				if isUnlimited {
					<-sema
				}
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
		Name:            s.Name,
		Workers:         s.Workers,
		RatePerWorker:   s.RatePerW,
		TargetRate:      s.targetRate(),
		DurationSecs:    secs,
		GOMAXPROCS:      runtime.GOMAXPROCS(0),
		GoVersion:       runtime.Version(),
		IsUnlimited:     isUnlimited,
		ProdConcurrency: prodCap,
		ConsumerWorkers: consumerWorkers,
		// throughput
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
	// Default unlimited prod-concurrency: half of GOMAXPROCS, at least 1.
	unlimProd := max(ncpu/2, 1)
	return map[string][]Scenario{
		"quick": {
			{Name: "low    (100 msg/s)", Workers: 10, RatePerW: 10},
			{Name: "mid   (5000 msg/s)", Workers: 100, RatePerW: 50},
			{Name: "high (20000 msg/s)", Workers: 400, RatePerW: 50},
			{
				Name:            fmt.Sprintf("unlimited  (%d workers, sema=%d)", ncpu*4, unlimProd),
				Workers:         ncpu * 4,
				RatePerW:        0,
				ProdConcurrency: unlimProd,
			},
		},
		"standard": {
			{Name: "smoke      (100 msg/s)", Workers: 10, RatePerW: 10},
			{Name: "medium    (1000 msg/s)", Workers: 50, RatePerW: 20},
			{Name: "high      (5000 msg/s)", Workers: 100, RatePerW: 50},
			{Name: "stress   (20000 msg/s)", Workers: 400, RatePerW: 50},
			{Name: "extreme  (50000 msg/s)", Workers: 1000, RatePerW: 50},
			{
				Name:            fmt.Sprintf("unlimited  (%d workers, sema=%d)", ncpu*4, unlimProd),
				Workers:         ncpu * 4,
				RatePerW:        0,
				ProdConcurrency: unlimProd,
			},
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
// Printing helpers
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
	// ── Throughput ──
	fmt.Printf("  %-26s %s\n", "Target rate:", tgtStr)
	if r.IsUnlimited {
		// Show how CPU slots are divided between producers and consumers
		// so the reader understands the fairness budget.
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
	gcPctFlag := flag.Int("gcpercent", 100, "GOGC value (100=default, 200=less frequent GC, -1=off)")
	flag.Parse()

	// Apply GC tuning before any allocation.
	if *gcPctFlag != 100 {
		debug.SetGCPercent(*gcPctFlag)
	}

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
	printBanner(*suiteFlag, *durFlag, *mwFlag, *gcPctFlag)
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
