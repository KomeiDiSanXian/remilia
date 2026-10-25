package main

import (
	"slices"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/middleware/admission"
	"github.com/KomeiDiSanXian/remilia/middleware/dedup"
	"github.com/KomeiDiSanXian/remilia/middleware/degradation"
	"github.com/KomeiDiSanXian/remilia/middleware/hotreload"
	"github.com/KomeiDiSanXian/remilia/middleware/ratelimit"
	"github.com/KomeiDiSanXian/remilia/middleware/resilience"
	"github.com/KomeiDiSanXian/remilia/middleware/sender"
	"github.com/KomeiDiSanXian/remilia/middleware/telemetry"
)

// setupMiddleware 创建中间件链（不含自适应限流器），结果写入 a.bridge。
// 自适应限流器需要绑 bot.Context()，由 setupAdaptiveLimiter 在 bot.Start() 之后完成。
func (a *app) setupMiddleware() {
	cfg := a.cfg
	traceCfg := &cfg.Tracing
	eng := a.eng
	mc := cfg.Middleware
	bridge := hotreload.NewBridge()

	mws := make([]eventctx.Middleware, 0)

	// 始终注册的基础中间件
	mws = append(mws, middleware.RequestID())
	mws = append(mws, middleware.Timeout(30*time.Second))

	// 准入控制（按 handler 成本类别）— 最外层之一：被拒绝的事件不再消耗后续中间件的预算。
	// 与 backpressure 互斥：两者都是并发限制，叠加会让有效额度难以推算。
	if mc.Admission.Enable {
		if adm := a.buildAdmission(mc.Admission); adm != nil {
			a.admission = adm
			mws = append(mws, adm.Middleware())
			logger.Info("[remilia] Admission control enabled (per-cost-class budgets)")
		}
	} else if mc.Backpressure.Limit > 0 {
		policy := parseBackpressurePolicy(mc.Backpressure.Policy)
		wTimeout := parseDuration(mc.Backpressure.WaitTimeout, 200*time.Millisecond)
		innerBP := middleware.Backpressure(mc.Backpressure.Limit, policy, wTimeout)
		mws = append(mws, wrapEnabled(func() bool {
			return bridge.GetMiddlewareConfig().Backpressure.Limit > 0
		}, innerBP))
	}

	// Recover — 运行时检查开关
	mws = append(mws, wrapEnabled(func() bool { return bridge.GetMiddlewareConfig().Recover }, middleware.Recover()))

	// Logging — 运行时检查开关
	mws = append(mws, wrapEnabled(func() bool { return bridge.GetMiddlewareConfig().Logging }, middleware.Logging()))

	// Auth（白名单）— 运行时读取 whitelist
	mws = append(mws, middleware.Auth(func(ctx *eventctx.Context) bool {
		ac := bridge.GetMiddlewareConfig().Auth
		if !ac.Enable || len(ac.Whitelist) == 0 {
			return true // 未启用时放行
		}
		userID := ctx.GetChatInfo().ID
		return slices.Contains(ac.Whitelist, userID)
	}))

	// Dedup — 配置开关 + 参数，支持热重载
	if mc.Dedup.Enable {
		ttl := parseDuration(mc.Dedup.DefaultTTL, 5*time.Minute)
		df := dedup.NewDedupFilter(dedup.DedupConfig{
			MaxSize:    mc.Dedup.MaxSize,
			DefaultTTL: ttl,
		})
		bridge.WatchDedup(df)
		mws = append(mws, dedup.Dedup(df))
	}

	// Circuit breaker — 支持热重载
	cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		MaxFailures:         5,
		ResetTimeout:        30 * time.Second,
		HalfOpenMaxRequests: 1,
		SuccessThreshold:    1,
		HalfOpenTimeout:     10 * time.Second,
	})
	bridge.WatchCircuitBreaker(cb)
	mws = append(mws, resilience.CircuitBreakerMiddleware(cb))

	eng.Use(mws...)

	// Metrics — 运行时检查开关
	eng.Use(wrapEnabled(func() bool { return bridge.GetMiddlewareConfig().Metrics }, telemetry.PrometheusMetrics("remilia")))

	// 出站发送指标 — 经 ctx.Reply 观察者记录（不包装 Sender，保留平台能力接口）
	eng.Use(outboundMetricsMiddleware())

	eng.Use(telemetry.Tracing(telemetry.TracingConfig{
		TracerName:         "remilia",
		IncludeEventDetail: traceCfg.IncludeEventDetail,
	}))
	eng.Use(errorHandlerMiddleware())

	// Slow handler — 运行时检查开关 + 阈值
	eng.Use(slowRequestMiddleware(bridge))

	bridge.Subscribe()
	logger.Info("[remilia] Hot-reload bridge initialized")

	a.bridge = bridge
	a.bridge.SetTracingProvider(a.tp)
}

// outboundMetricsMiddleware 按事件注入出站发送观察者（remilia_send_total{platform,status}）。
func outboundMetricsMiddleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			if plt := ctx.GetEventPlatform(); plt != "" {
				ctx.AddOutboundObserver(sender.NewOutboundObserver(plt))
			}
			return next(ctx)
		}
	}
}

// parseBackpressurePolicy 将配置字符串转换为 BackpressurePolicy。
func parseBackpressurePolicy(policy string) middleware.BackpressurePolicy {
	switch policy {
	case "drop":
		return middleware.BackpressureDrop
	case "block":
		return middleware.BackpressureBlock
	case "trywait":
		return middleware.BackpressureTryWait
	default:
		return middleware.BackpressureDrop
	}
}

// parseDuration 解析时间字符串，失败时返回默认值。
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

// setupAdaptiveLimiter 在 bot 启动后创建自适应限流器并接入引擎。
// 此时 bot.Context() 返回真正 lifecycle context，goroutine 自动随 bot 停止而退出。
func (a *app) setupAdaptiveLimiter() {
	arl := ratelimit.NewAdaptiveRateLimiterWithContext(a.bot.Context(), ratelimit.DefaultAdaptiveConfig())
	a.bridge.WatchAdaptive(arl)
	arl.Start()
	a.eng.Use(arl.Middleware())
	logger.Info("[remilia] Adaptive rate limiter started (bound to bot lifecycle)")
}

// setupAdaptiveDegradation 按配置启用自适应降级（middleware.degradation）。
//
// 与限流器一样在 bot.Start() 之后装配：StartMonitor 绑定 bot 的 lifecycle context，
// 监控 goroutine 随 bot 停止而退出。此前 middleware.degradation.* 的所有阈值
// 只被 config 校验读取，运行时从不创建控制器——配了等于没配。
func (a *app) setupAdaptiveDegradation() {
	dc := a.cfg.Middleware.Degradation
	if !dc.Enable {
		return
	}
	deg := degradation.NewAdaptiveDegradation(degradation.DegradationConfig{
		CPUThreshold:         dc.CPUThreshold,
		MemoryThreshold:      dc.MemoryThreshold,
		Strategy:             parseDegradationStrategy(dc.Strategy),
		MonitorInterval:      parseDuration(dc.MonitorInterval, 5*time.Second),
		RecoveryInterval:     parseDuration(dc.RecoveryInterval, 10*time.Second),
		DelayQueueSize:       dc.DelayQueueSize,
		EnableGoroutineLimit: dc.EnableGoroutineLimit,
		GoroutineThreshold:   dc.GoroutineThreshold,
	})
	deg.StartMonitor(a.bot.Context())
	a.eng.Use(deg.Middleware())
	a.bridge.WatchDegradation(deg)
	logger.Info("[remilia] Adaptive degradation started (bound to bot lifecycle)")
}

// parseDegradationStrategy 将配置字符串转换为降级策略，未知值按 drop 处理。
func parseDegradationStrategy(s string) degradation.DegradationStrategy {
	switch s {
	case "delay":
		return degradation.DegradationDelay
	case "simplify":
		return degradation.DegradationSimplify
	default:
		return degradation.DegradationDrop
	}
}

// 准入控制的默认预算：local 只防失控；io 允许排队，队满才拒绝。
const (
	defaultAdmissionLocalLimit = 200
	defaultAdmissionIOLimit    = 64
	defaultAdmissionIOQueue    = 200
	defaultAdmissionIOWait     = 10 * time.Second
)

// buildAdmission 把配置映射为准入控制实例（配置非法时校验阶段已拦截）。
func (a *app) buildAdmission(ac config.AdmissionConfig) *admission.Admission {
	local := admission.ClassConfig{
		Limit:       ac.Local.Limit,
		Queue:       ac.Local.Queue,
		WaitTimeout: parseDuration(ac.Local.WaitTimeout, 0),
	}
	if local.Limit <= 0 {
		local.Limit = defaultAdmissionLocalLimit
	}

	io := admission.ClassConfig{
		Limit:       ac.IO.Limit,
		Queue:       ac.IO.Queue,
		WaitTimeout: parseDuration(ac.IO.WaitTimeout, 0),
	}
	if io.Limit <= 0 && io.Queue <= 0 && ac.IO.WaitTimeout == "" {
		// 整段未配置：使用 I/O 类默认值（允许排队）。
		io = admission.ClassConfig{
			Limit:       defaultAdmissionIOLimit,
			Queue:       defaultAdmissionIOQueue,
			WaitTimeout: defaultAdmissionIOWait,
		}
	}

	return admission.New(admission.Config{
		SlowP50:    parseDuration(ac.SlowP50, 500*time.Millisecond),
		MinSamples: ac.MinSamples,
		Local:      local,
		IO:         io,
	})
}

// errorHandlerMiddleware 捕获 handler 错误并记录日志。
func errorHandlerMiddleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			err := next(ctx)
			if err != nil {
				logger.WithError(err).Error("[remilia] Handler error")
			}
			return err
		}
	}
}

// slowRequestMiddleware 检测超过阈值的慢请求并告警。
func slowRequestMiddleware(bridge *hotreload.Bridge) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			sc := bridge.GetMiddlewareConfig().SlowHandler
			if !sc.Enable {
				return next(ctx)
			}
			threshold := 3 * time.Second
			if sc.Threshold != "" {
				if d, err := time.ParseDuration(sc.Threshold); err == nil && d > 0 {
					threshold = d
				}
			}
			start := time.Now()
			err := next(ctx)
			if time.Since(start) > threshold {
				logger.WithFields(logger.Fields{
					"duration": time.Since(start),
					"cmd":      ctx.GetMessageContent(),
				}).Warn("[remilia] Slow request detected")
			}
			return err
		}
	}
}

// wrapEnabled 返回一个中间件包装器：仅在 enabled() 返回 true 时执行 inner，
// 否则透传 next。用于运行时中间件开关。
func wrapEnabled(enabled func() bool, inner eventctx.Middleware) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		innerHandler := inner(next)
		return func(ctx *eventctx.Context) error {
			if !enabled() {
				return next(ctx)
			}
			return innerHandler(ctx)
		}
	}
}
