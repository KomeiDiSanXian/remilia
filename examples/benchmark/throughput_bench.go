// Package main provides a standalone throughput benchmark for the remilia framework.
//
// Usage:
//
//	cd examples/benchmark && go run throughput_bench.go
//	cd examples/benchmark && go run throughput_bench.go -duration 15s -suite quick
//	cd examples/benchmark && go run throughput_bench.go -suite full
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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	remilia "github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	mw "github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/tidwall/gjson"
)

// ─────────────────────────────────────────────────────────────
// Mock infrastructure
// ─────────────────────────────────────────────────────────────

// nullAPI satisfies openapi.OpenAPI without any real network I/O.
type nullAPI struct{ calls atomic.Int64 }

func (n *nullAPI) SingleChat(_ string, _ *dto.Message) (gjson.Result, error) {
	n.calls.Add(1)
	return gjson.Result{}, nil
}
func (n *nullAPI) GroupChat(_ string, _ *dto.Message) (gjson.Result, error) {
	n.calls.Add(1)
	return gjson.Result{}, nil
}
func (n *nullAPI) SingleRichMedia(_ string, _ *dto.Media) (gjson.Result, error) {
	n.calls.Add(1)
	return gjson.Result{}, nil
}
func (n *nullAPI) GroupRichMedia(_ string, _ *dto.Media) (gjson.Result, error) {
	n.calls.Add(1)
	return gjson.Result{}, nil
}
func (n *nullAPI) SingleReset(_, _ string) (gjson.Result, error) {
	n.calls.Add(1)
	return gjson.Result{}, nil
}
func (n *nullAPI) GroupReset(_, _ string) (gjson.Result, error) {
	n.calls.Add(1)
	return gjson.Result{}, nil
}

// pumpAdapter is an Adapter that accepts events via InjectEvent.
// Workers = 2 × CPU to forward events to the bot handler concurrently.
type pumpAdapter struct {
	ch      chan *dto.Payload
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started atomic.Bool

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
	nWorkers := runtime.NumCPU() * 2
	for range nWorkers {
		a.wg.Go(func() {
			for {
				select {
				case <-a.ctx.Done():
					return
				case ev := <-a.ch:
					handler(ev)
				}
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

// ─────────────────────────────────────────────────────────────
// Scenario descriptor
// ─────────────────────────────────────────────────────────────

// Scenario defines one benchmark run.
type Scenario struct {
	Name     string
	Workers  int           // concurrent message-producer goroutines
	RatePerW int           // messages/s per worker  (0 = as fast as possible)
	Duration time.Duration // override global duration when non-zero
	WithMW   bool          // attach Recover middleware
}

func (s Scenario) targetRate() int { return s.Workers * s.RatePerW }

// ─────────────────────────────────────────────────────────────
// Per-run metrics (all lock-free)
// ─────────────────────────────────────────────────────────────

type runMetrics struct {
	sent      atomic.Int64
	processed atomic.Int64
	failed    atomic.Int64

	latSum   atomic.Int64
	latCount atomic.Int64
	latMin   atomic.Int64
	latMax   atomic.Int64
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
// ScenarioResult — printable + JSON-serialisable
// ─────────────────────────────────────────────────────────────

type ScenarioResult struct {
	Name          string  `json:"name"`
	Workers       int     `json:"workers"`
	RatePerWorker int     `json:"rate_per_worker"`
	TargetRate    int     `json:"target_rate_per_s"`
	DurationSecs  float64 `json:"duration_secs"`

	EventsSent      int64 `json:"events_sent"`
	EventsProcessed int64 `json:"events_processed"`
	EventsFailed    int64 `json:"events_failed"`
	EventsDropped   int64 `json:"events_dropped"`

	SuccessRatePct float64 `json:"success_rate_pct"`
	DropRatePct    float64 `json:"drop_rate_pct"`

	ThroughputActual float64 `json:"throughput_actual_per_s"`
	ThroughputTarget float64 `json:"throughput_target_per_s"`
	AchievementPct   float64 `json:"achievement_pct"`

	AvgLatencyMs float64 `json:"avg_latency_ms"`
	MinLatencyMs float64 `json:"min_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`

	GOMAXPROCS int    `json:"gomaxprocs"`
	GoVersion  string `json:"go_version"`
}

// ─────────────────────────────────────────────────────────────
// Core: run one scenario
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

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = bot.Stop(stopCtx)

	// ── Aggregate ──
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

	return ScenarioResult{
		Name:             s.Name,
		Workers:          s.Workers,
		RatePerWorker:    s.RatePerW,
		TargetRate:       s.targetRate(),
		DurationSecs:     secs,
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
		GOMAXPROCS:       runtime.GOMAXPROCS(0),
		GoVersion:        runtime.Version(),
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
	fmt.Printf("  Go %-10s  GOMAXPROCS=%d  %s\n",
		runtime.Version(), runtime.GOMAXPROCS(0), time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  Suite: %-12s  Duration/scenario: %-8v  Middleware: %v\n", suite, dur, withMW)
	fmt.Println(bar)
}

func printScenarioTitle(i int, name string) {
	fmt.Printf("\n  ▶  Scenario %d: %s\n", i+1, name)
	fmt.Println("  " + strings.Repeat("─", 58))
}

func printResult(r ScenarioResult) {
	tgtStr := "unlimited (no rate limit)"
	achieveStr := "—"
	if r.TargetRate > 0 {
		tgtStr = fmt.Sprintf("%d msg/s", r.TargetRate)
		achieveStr = fmt.Sprintf("%.1f%%", r.AchievementPct)
	}
	fmt.Printf("  %-24s %s\n", "Target rate:", tgtStr)
	fmt.Printf("  %-24s %.1f msg/s\n", "Actual throughput:", r.ThroughputActual)
	fmt.Printf("  %-24s %s\n", "Achievement:", achieveStr)
	fmt.Printf("  %-24s %.2f s\n", "Elapsed:", r.DurationSecs)
	fmt.Println()
	fmt.Printf("  %-24s %d\n", "Events sent:", r.EventsSent)
	fmt.Printf("  %-24s %d  (%.1f%%)\n", "Events processed:", r.EventsProcessed, r.SuccessRatePct)
	if r.EventsFailed > 0 {
		fmt.Printf("  %-24s %d\n", "Events failed:", r.EventsFailed)
	}
	if r.EventsDropped > 0 {
		fmt.Printf("  %-24s %d  (%.1f%% backpressure)\n", "Events dropped:", r.EventsDropped, r.DropRatePct)
	}
	fmt.Println()
	fmt.Printf("  %-24s %.4f ms\n", "Avg handler latency:", r.AvgLatencyMs)
	fmt.Printf("  %-24s %.4f ms  /  %.4f ms\n", "Min / Max latency:", r.MinLatencyMs, r.MaxLatencyMs)
}

func printSummary(results []ScenarioResult) {
	fmt.Println()
	fmt.Println(bar)
	fmt.Println("  Results Summary")
	fmt.Println(bar)
	header := fmt.Sprintf("  %-38s  %13s  %13s  %9s  %12s", "Scenario", "Target(msg/s)", "Actual(msg/s)", "Success%", "AvgLat(ms)")
	fmt.Println(header)
	fmt.Println("  " + strings.Repeat("─", len(header)-2))
	for _, r := range results {
		tgt := "unlimited"
		if r.TargetRate > 0 {
			tgt = fmt.Sprintf("%d", r.TargetRate)
		}
		fmt.Printf("  %-38s  %13s  %13.0f  %8.1f%%  %12.4f\n",
			r.Name, tgt, r.ThroughputActual, r.SuccessRatePct, r.AvgLatencyMs)
	}
	fmt.Println(bar)

	// Saturation hint: last scenario whose achievement >= 90 %
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
	fmt.Printf("  ℹ  CPU logical cores: %d  ·  GOMAXPROCS: %d\n", runtime.NumCPU(), runtime.GOMAXPROCS(0))
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
			time.Sleep(800 * time.Millisecond) // cool-down between scenarios
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
