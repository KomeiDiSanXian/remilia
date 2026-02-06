# Code Review Report (2026-02-06)

Scope (sampled)
- adapter.go
- bot.go
- config/config.go
- config/watcher.go
- command/parser.go
- middleware/dedup.go
- middleware/retry.go
- middleware/circuitbreaker.go
- infra/metrics/metrics.go
- core/engine/engine.go
- core/engine/process.go

Potential bugs / risks (ordered roughly by severity)
1) config/config.go: globalConfig is written from Load() and watcher reloads without synchronization. Get() returns a shared pointer without atomic protection. With concurrent reads/writes (config hot reload + runtime reads), this can trigger data races under -race and undefined behavior.
2) config/config.go: Get() can return nil if Load() has not been called. Callers that assume non-nil will panic. Consider guard rails or a safer default path.
3) middleware/dedup.go: DedupFilter.Stop() closes cleanupDone without guard. A second Stop() will panic (close of closed channel). This shows up in tests or repeated lifecycle stop calls.
4) command/parser.go: flag value parsing treats any next token starting with '-' as another flag. This breaks valid values like negative numbers ("--days -1") or strings that start with '-' ("--name -foo") and silently flips to "true".
5) infra/metrics/metrics.go: NewMetricsCollector uses promauto (global default registry). Creating multiple collectors with the same namespace or metric names can panic due to duplicate registration, which is likely in tests or multi-engine scenarios.
6) bot.go: NewBot registers lifecycle components that call b.adapter.Start/Stop and b.engine.Shutdown without nil checks. Passing a nil adapter or engine will panic at runtime; this is easy to do in tests or partial setups.
7) middleware/dedup.go: TTL is stored in seconds (Unix). Durations below 1s effectively become 0, causing immediate expiration. If small TTL values are used, dedup behavior can silently degrade.

High-impact improvement opportunities
- Make config access race-free: replace globalConfig with an atomic.Value or RWMutex-protected accessor; return a copy of config for safety when applying reloads.
- Make config.Get() safer: return (*Config, bool) or panic with a clear message if not initialized; update callers to handle "not loaded" explicitly.
- Harden DedupFilter lifecycle: add a once.Do or atomic guard in Stop(), and optionally expose a context-based shutdown.
- Improve command parsing: allow flag values that start with '-' by checking "--" only for long flags and a dedicated "--" end-of-flags delimiter, or allow "--key=-1" and "--" behavior.
- Metrics registration: allow passing a custom prometheus.Registry (or use promauto.With(registry)) to avoid global collisions; document singletons if that is intended.
- Bot constructor validation: return error from NewBot if adapter/engine are nil, or change lifecycle registration to be conditional.

Test gaps / checks to add
- config: data race tests for hot reload + concurrent Get() (run with -race).
- dedup: Stop() called twice should not panic.
- parser: cover flags with negative values and values that start with '-'.
- metrics: multiple collectors in tests should not panic (if multi-instance is supported).
- bot: nil adapter/engine behavior should be tested and documented.

Notes
- Findings are based on a targeted review of the files listed in scope, not a full repo audit.

