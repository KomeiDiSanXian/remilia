package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/middleware/dedup"
	"github.com/KomeiDiSanXian/remilia/middleware/hotreload"
	"github.com/KomeiDiSanXian/remilia/middleware/ratelimit"
	"github.com/KomeiDiSanXian/remilia/middleware/resilience"
	"github.com/KomeiDiSanXian/remilia/middleware/telemetry"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/discord"
	"github.com/KomeiDiSanXian/remilia/platform/milky"
	"github.com/KomeiDiSanXian/remilia/platform/onebot"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform/satori"
	"github.com/KomeiDiSanXian/remilia/platform/telegram"
	"github.com/KomeiDiSanXian/remilia/platform/terminal"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/router"
)

func setupPlatforms(cfg *config.Config) *platform.Registry {
	reg := platform.NewRegistry()
	registerPlatforms(reg, cfg)
	if reg.Len() == 0 {
		logger.Warn("[remilia] No platform configured, using Terminal adapter for development")
		reg.Register(terminal.NewAdapter(
			terminal.WithPrompt("Bot> "),
			terminal.WithBotName("DevBot"),
		))
	}
	return reg
}

// registerPlatforms 根据 cfg 将所有已启用的平台适配器注册到 reg 中。
// 供 setupPlatforms 和平台热更新 listener 复用。
func registerPlatforms(reg *platform.Registry, cfg *config.Config) {
	for name, factory := range platformFactories(cfg) {
		a, err := factory()
		if err != nil {
			logger.WithError(err).Errorf("[remilia] Failed to create %s adapter, skipping", name)
			continue
		}
		reg.Register(a)
		logger.Infof("[remilia] Registered %s adapter", name)
	}
}

// buildDesiredAdapters 根据 cfg 构建期望的平台适配器集合。
// 供平台热更新 listener 使用。
func buildDesiredAdapters(cfg *config.Config) map[string]platform.Adapter {
	desired := make(map[string]platform.Adapter)
	for name, factory := range platformFactories(cfg) {
		a, err := factory()
		if err != nil {
			logger.WithError(err).Errorf("[remilia] Failed to create %s adapter for hot-swap, skipping", name)
			continue
		}
		desired[name] = a
	}
	return desired
}

// platformFactories 返回当前配置中所有启用的平台及其创建函数。
func platformFactories(cfg *config.Config) map[string]func() (platform.Adapter, error) {
	factories := make(map[string]func() (platform.Adapter, error))

	if c := cfg.Bot.QQ; c != nil {
		factories["qq"] = func() (platform.Adapter, error) {
			addr := fmt.Sprintf("%s:%d", c.Webhook.Host, c.Webhook.Port)
			return qq.NewWebhookServerAdapter(addr, &dto.BotInfo{
				QQNum: c.BotID, AppID: c.AppID,
				Token: c.Token, AppSecret: c.Secret,
			}), nil
		}
	}

	if c := cfg.Bot.OneBot; c != nil {
		factories["onebot"] = func() (platform.Adapter, error) {
			return onebot.NewForwardWSAdapter(onebot.Config{
				URL: c.URL, Token: c.Token, Secret: c.Secret,
				Mode: onebot.ModeForwardWS,
			}), nil
		}
	}

	if c := cfg.Bot.Discord; c != nil {
		factories["discord"] = func() (platform.Adapter, error) {
			return discord.NewAdapter(c.Token)
		}
	}

	if c := cfg.Bot.Satori; c != nil {
		factories["satori"] = func() (platform.Adapter, error) {
			return satori.NewAdapter(satori.Config{
				ServerURL: c.ServerURL, Token: c.Token,
				Platform: c.Platform, UserID: c.UserID,
			})
		}
	}

	if c := cfg.Bot.Milky; c != nil {
		factories["milky"] = func() (platform.Adapter, error) {
			return milky.NewAdapter(milky.Config{
				BaseURL: c.BaseURL, AccessToken: c.AccessToken,
			})
		}
	}

	if c := cfg.Bot.Telegram; c != nil {
		factories["telegram"] = func() (platform.Adapter, error) {
			return telegram.NewPollingAdapter(telegram.Config{
				Token:       c.Token,
				PollTimeout: c.PollTimeout,
			})
		}
	}

	return factories
}

// setupMiddleware 创建中间件链（不含自适应限流器）并返回热重载桥接器。
// 自适应限流器需要绑 bot.Context()，由调用方在 bot.Start() 之后 setupAdaptiveLimiter 完成。
func setupMiddleware(eng *engine.Engine, traceCfg *tracing.Config, cfg *config.Config) *hotreload.Bridge {
	mc := cfg.Middleware
	bridge := hotreload.NewBridge()

	mws := make([]eventctx.Middleware, 0)

	// 始终注册的基础中间件
	mws = append(mws, middleware.RequestID())
	mws = append(mws, middleware.Timeout(30*time.Second))

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

	// Backpressure — 运行时检查开关，Limit 仅创建时确定
	if mc.Backpressure.Limit > 0 {
		policy := parseBackpressurePolicy(mc.Backpressure.Policy)
		wTimeout := parseDuration(mc.Backpressure.WaitTimeout, 200*time.Millisecond)
		innerBP := middleware.Backpressure(mc.Backpressure.Limit, policy, wTimeout)
		mws = append(mws, wrapEnabled(func() bool {
			return bridge.GetMiddlewareConfig().Backpressure.Limit > 0
		}, innerBP))
	}

	eng.Use(mws...)

	// Metrics — 运行时检查开关
	eng.Use(wrapEnabled(func() bool { return bridge.GetMiddlewareConfig().Metrics }, telemetry.PrometheusMetrics("remilia")))

	eng.Use(telemetry.Tracing(telemetry.TracingConfig{
		TracerName:         "remilia",
		IncludeEventDetail: traceCfg.IncludeEventDetail,
	}))
	eng.Use(errorHandlerMiddleware())

	// Slow handler — 运行时检查开关 + 阈值
	eng.Use(slowRequestMiddleware(bridge))

	bridge.Subscribe()
	logger.Info("[remilia] Hot-reload bridge initialized")

	return bridge
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
func setupAdaptiveLimiter(eng *engine.Engine, bridge *hotreload.Bridge, botCtx context.Context) {
	arl := ratelimit.NewAdaptiveRateLimiterWithContext(botCtx, ratelimit.DefaultAdaptiveConfig())
	bridge.WatchAdaptive(arl)
	arl.Start()
	eng.Use(arl.Middleware())
	logger.Info("[remilia] Adaptive rate limiter started (bound to bot lifecycle)")
}

func setupRouter(bot *remilia.Bot, eng *engine.Engine) *fsm.Manager {
	fsmMgr := fsm.NewManager(nil)
	rtr := router.New(eng, fsmMgr.Engine())
	rtr.Route(router.WithCommandPrefix())
	bot.UseRouter(rtr)
	return fsmMgr
}

func setupPluginManager(bot *remilia.Bot, eng *engine.Engine, cfg *config.Config) *plugin.Manager {
	cp := plugin.NewYAMLConfigProvider(cfg)
	pm := plugin.NewManager(eng, plugin.WithConfigProvider(cp))
	pm.SetStrictDeps(false)
	bot.UsePlugins(pm)
	return pm
}

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
