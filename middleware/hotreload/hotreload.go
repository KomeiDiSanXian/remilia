// Package hotreload 提供配置热更新到中间件的传播桥接。
//
// 将 config.Watcher 的变更回调与各中间件的 UpdateConfig 方法连接起来，
// 实现配置文件修改后中间件参数自动生效，无需重启 Bot。
//
// 使用示例:
//
//	bridge := hotreload.NewBridge()
//	bridge.WatchAdaptive(adaptiveLimiter)
//	bridge.WatchRetry(configurableRetry)
//	bridge.WatchCircuitBreaker(circuitBreaker)
//	bridge.WatchDedup(dedupFilter)
//	bridge.WatchDegradation(degradationCtrl)
//	token := config.Subscribe(bridge.OnConfigChange)
//	defer token.Cancel()
package hotreload

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
	"github.com/KomeiDiSanXian/remilia/middleware/dedup"
	"github.com/KomeiDiSanXian/remilia/middleware/degradation"
	"github.com/KomeiDiSanXian/remilia/middleware/ratelimit"
	"github.com/KomeiDiSanXian/remilia/middleware/resilience"
	"github.com/KomeiDiSanXian/remilia/middleware/telemetry"
)

// Bridge 配置热更新桥接器
type Bridge struct {
	mu              sync.RWMutex
	adaptives       []*ratelimit.AdaptiveRateLimiter
	retries         []*resilience.ConfigurableRetry
	circuitBreakers []*resilience.CircuitBreaker
	dedups          []*dedup.DedupFilter
	degradations    []*degradation.AdaptiveDegradation

	middlewareConfig atomic.Value // *config.MiddlewareConfig

	pprofSrv  *remilia.PprofServer
	tracingTP *tracing.Provider

	lastLogCfg          logger.Config // 上次日志输出配置（Format/Console/File/FilePath）
	lastSamplingRate    float64
	lastPprofCfg        remilia.PprofConfig
	lastRetryCfg        config.RetryConfig
	lastDedupMaxSize    int
	lastDedupDefaultTTL string
	lastDegradationCfg  config.DegradationConfig
}

// NewBridge 创建桥接器
func NewBridge() *Bridge {
	return &Bridge{}
}

// SetPprofServer 注册 pprof 服务器以接收热更新
func (b *Bridge) SetPprofServer(srv *remilia.PprofServer) {
	b.pprofSrv = srv
}

// SetTracingProvider 注册追踪提供者以接收采样率热更新
func (b *Bridge) SetTracingProvider(tp *tracing.Provider) {
	b.tracingTP = tp
}

// GetMiddlewareConfig 返回当前中间件配置快照，供 setup.go 运行时检查。
func (b *Bridge) GetMiddlewareConfig() *config.MiddlewareConfig {
	if v := b.middlewareConfig.Load(); v != nil {
		return v.(*config.MiddlewareConfig)
	}
	return &config.MiddlewareConfig{}
}

// WatchAdaptive 注册 AdaptiveRateLimiter 接收热更新
func (b *Bridge) WatchAdaptive(arl *ratelimit.AdaptiveRateLimiter) *Bridge {
	b.mu.Lock()
	b.adaptives = append(b.adaptives, arl)
	b.mu.Unlock()
	return b
}

// WatchRetry 注册 ConfigurableRetry 接收热更新
func (b *Bridge) WatchRetry(cr *resilience.ConfigurableRetry) *Bridge {
	b.mu.Lock()
	b.retries = append(b.retries, cr)
	b.mu.Unlock()
	return b
}

// WatchCircuitBreaker 注册 CircuitBreaker 接收热更新
func (b *Bridge) WatchCircuitBreaker(cb *resilience.CircuitBreaker) *Bridge {
	b.mu.Lock()
	b.circuitBreakers = append(b.circuitBreakers, cb)
	b.mu.Unlock()
	return b
}

// WatchDedup 注册 DedupFilter 接收热更新（MaxSize、DefaultTTL 立即生效）
func (b *Bridge) WatchDedup(df *dedup.DedupFilter) *Bridge {
	b.mu.Lock()
	b.dedups = append(b.dedups, df)
	b.mu.Unlock()
	return b
}

// WatchDegradation 注册 AdaptiveDegradation 接收热更新
// （CPUThreshold、MemoryThreshold、LatencyThreshold、GoroutineThreshold 等下一监控周期生效）
func (b *Bridge) WatchDegradation(ad *degradation.AdaptiveDegradation) *Bridge {
	b.mu.Lock()
	b.degradations = append(b.degradations, ad)
	b.mu.Unlock()
	return b
}

// changed 为两个 int 字段比较辅助，避免 == 对零值的误判。
func changed[T comparable](old, new T) bool { return old != new }

// OnConfigChange 实现 config.ChangeListener，将新配置推送到所有已注册的组件。
// 通过 config.Subscribe(bridge.OnConfigChange) 注册到 config 包。
//
// 每个更新步骤都带有栅栏（仅当对应字段值变化时才执行），
// 避免修改无关字段（如 log.level）触发不必要的组件重初始化。
func (b *Bridge) OnConfigChange(newCfg *config.Config) {
	if newCfg == nil {
		return
	}

	logCfg := newCfg.Log

	// 仅当日志级别/时间格式变化时更新全局变量
	if changed(b.lastLogCfg.Level, logCfg.Level) {
		if logCfg.Level != "" {
			if err := logger.SetLevel(logCfg.Level); err != nil {
				logger.WithError(err).Warn("[HotReload] Failed to update log level")
			}
		}
	}
	if changed(b.lastLogCfg.TimeFormat, logCfg.TimeFormat) {
		logger.SetTimeFormat(logCfg.TimeFormat)
	}

	// 仅当日志输出相关字段变化时才重新初始化 logger（关旧文件、开新文件）
	if changed(b.lastLogCfg.Format, logCfg.Format) ||
		changed(b.lastLogCfg.Console, logCfg.Console) ||
		changed(b.lastLogCfg.File, logCfg.File) ||
		changed(b.lastLogCfg.FilePath, logCfg.FilePath) {
		if err := logger.Init(logCfg); err != nil {
			logger.WithError(err).Warn("[HotReload] Failed to re-init logger, keeping previous config")
		}
	}
	b.lastLogCfg = logCfg

	// 同步 Tracing 运行时开关
	telemetry.SetIncludeEventDetail(newCfg.Tracing.IncludeEventDetail)

	// 同步 Tracing 采样率（仅当值变化时调用，避免固定采样模式下每次都打印警告）
	if changed(b.lastSamplingRate, newCfg.Tracing.SamplingRate) {
		if b.tracingTP != nil && newCfg.Tracing.SamplingRate > 0 {
			b.tracingTP.SetSamplingRate(newCfg.Tracing.SamplingRate)
		}
		b.lastSamplingRate = newCfg.Tracing.SamplingRate
	}

	// 同步 Pprof 参数（仅当相关字段变化时）
	pprof := newCfg.Pprof
	parsedInterval := parseDurationFallback(pprof.ProfileInterval, time.Hour)
	parsedDuration := parseDurationFallback(pprof.ProfileDuration, 30*time.Second)
	if changed(b.lastPprofCfg.AutoProfile, pprof.AutoProfile) ||
		changed(b.lastPprofCfg.ProfileInterval, parsedInterval) ||
		changed(b.lastPprofCfg.ProfileDuration, parsedDuration) ||
		changed(b.lastPprofCfg.EnableMutex, pprof.EnableMutex) ||
		changed(b.lastPprofCfg.EnableBlock, pprof.EnableBlock) {
		if b.pprofSrv != nil {
			b.pprofSrv.UpdateConfig(remilia.PprofConfig{
				AutoProfile:          pprof.AutoProfile,
				ProfileInterval:      parsedInterval,
				ProfileDuration:      parsedDuration,
				EnableMutex:          pprof.EnableMutex,
				EnableBlock:          pprof.EnableBlock,
				MutexProfileFraction: 1,
				BlockProfileRate:     1,
			})
		}
	}
	b.lastPprofCfg = remilia.PprofConfig{
		AutoProfile:     pprof.AutoProfile,
		ProfileInterval: parsedInterval,
		ProfileDuration: parsedDuration,
		EnableMutex:     pprof.EnableMutex,
		EnableBlock:     pprof.EnableBlock,
	}

	// 刷新中间件配置快照（供 setup.go 的运行时开关检查）
	mc := &newCfg.Middleware
	b.middlewareConfig.Store(mc)

	// --- 以下为热更新中间件组件的配置 ---
	b.mu.RLock()
	defer b.mu.RUnlock()

	rc := newCfg.Retry

	// 更新 AdaptiveRateLimiter
	if mc.RateLimit.Enable {
		for _, arl := range b.adaptives {
			arl.UpdateConfig(ratelimit.AdaptiveConfig{
				AdjustStep: mc.RateLimit.Burst,
			})
		}
	}

	// 更新 ConfigurableRetry（仅在字段变化时）
	if rc != b.lastRetryCfg {
		if rc.Enable {
			base := parseDuration(rc.BackoffBase, 200*time.Millisecond)
			duration := parseDuration(rc.BackoffMax, 2*time.Second)
			for _, cr := range b.retries {
				cr.UpdateConfig(resilience.RetryConfig{
					MaxAttempts: rc.MaxAttempts,
					BackoffBase: base,
					BackoffMax:  duration,
				})
			}
		}
		b.lastRetryCfg = rc
	}

	// 更新 DedupFilter（仅在字段变化时）
	if mc.Dedup.Enable &&
		(changed(b.lastDedupMaxSize, mc.Dedup.MaxSize) ||
			changed(b.lastDedupDefaultTTL, mc.Dedup.DefaultTTL)) {
		ttl := parseDuration(mc.Dedup.DefaultTTL, 5*time.Minute)
		for _, df := range b.dedups {
			df.UpdateConfig(dedup.DedupConfig{
				MaxSize:    mc.Dedup.MaxSize,
				DefaultTTL: ttl,
			})
		}
		b.lastDedupMaxSize = mc.Dedup.MaxSize
		b.lastDedupDefaultTTL = mc.Dedup.DefaultTTL
	}

	// 更新 AdaptiveDegradation（仅在字段变化时）
	if mc.Degradation.Enable && mc.Degradation != b.lastDegradationCfg {
		strat := parseDegradationStrategy(mc.Degradation.Strategy)
		for _, ad := range b.degradations {
			ad.UpdateConfig(degradation.DegradationConfig{
				CPUThreshold:       mc.Degradation.CPUThreshold,
				MemoryThreshold:    mc.Degradation.MemoryThreshold,
				LatencyThreshold:   parseDuration(mc.Degradation.LatencyThreshold, 0),
				GoroutineThreshold: mc.Degradation.GoroutineThreshold,
				MonitorInterval:    parseDuration(mc.Degradation.MonitorInterval, 5*time.Second),
				Strategy:           strat,
			})
		}
		b.lastDegradationCfg = mc.Degradation
	}

	logger.Info("[HotReload] All configs updated from config file")
}

// Subscribe 便捷方法：注册到 config 包并返回 token
func (b *Bridge) Subscribe() *config.ListenerToken {
	return config.Subscribe(b.OnConfigChange)
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func parseDurationFallback(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func parseDegradationStrategy(s string) degradation.DegradationStrategy {
	switch s {
	case "drop":
		return degradation.DegradationDrop
	case "delay":
		return degradation.DegradationDelay
	case "simplify":
		return degradation.DegradationSimplify
	default:
		return 0
	}
}
