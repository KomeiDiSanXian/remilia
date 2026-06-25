package main

import (
	"context"
	"fmt"
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
	"github.com/KomeiDiSanXian/remilia/platform/terminal"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/router"
)

func setupPlatforms(cfg *config.Config) *platform.Registry {
	reg := platform.NewRegistry()

	if c := cfg.Bot.QQ; c != nil {
		addr := fmt.Sprintf("%s:%d", c.Webhook.Host, c.Webhook.Port)
		reg.Register(qq.NewWebhookServerAdapter(addr, &dto.BotInfo{
			QQNum: c.BotID, AppID: c.AppID,
			Token: c.Token, AppSecret: c.Secret,
		}))
		logger.Infof("[remilia] Registered QQ adapter on %s", addr)
	}

	if c := cfg.Bot.OneBot; c != nil {
		reg.Register(onebot.NewForwardWSAdapter(onebot.Config{
			URL: c.URL, Token: c.Token, Secret: c.Secret,
			Mode: onebot.ModeForwardWS,
		}))
		logger.Infof("[remilia] Registered OneBot adapter: %s", c.URL)
	}

	if c := cfg.Bot.Discord; c != nil {
		a, err := discord.NewAdapter(c.Token)
		if err != nil {
			logger.WithError(err).Error("[remilia] Failed to create Discord adapter, skipping")
		} else {
			reg.Register(a)
			logger.Info("[remilia] Registered Discord adapter")
		}
	}

	if c := cfg.Bot.Satori; c != nil {
		a, err := satori.NewAdapter(satori.Config{
			ServerURL: c.ServerURL, Token: c.Token,
			Platform: c.Platform, UserID: c.UserID,
		})
		if err != nil {
			logger.WithError(err).Error("[remilia] Failed to create Satori adapter, skipping")
		} else {
			reg.Register(a)
			logger.Infof("[remilia] Registered Satori adapter: %s", c.ServerURL)
		}
	}

	if c := cfg.Bot.Milky; c != nil {
		a, err := milky.NewAdapter(milky.Config{
			BaseURL: c.BaseURL, AccessToken: c.AccessToken,
		})
		if err != nil {
			logger.WithError(err).Error("[remilia] Failed to create Milky adapter, skipping")
		} else {
			reg.Register(a)
			logger.Infof("[remilia] Registered Milky adapter: %s", c.BaseURL)
		}
	}

	if reg.Len() == 0 {
		logger.Warn("[remilia] No platform configured, using Terminal adapter for development")
		reg.Register(terminal.NewAdapter(
			terminal.WithPrompt("Bot> "),
			terminal.WithBotName("DevBot"),
		))
	}

	return reg
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

	// Recover — 配置开关
	if mc.Recover {
		mws = append(mws, middleware.Recover())
	}

	// Logging — 配置开关
	if mc.Logging {
		mws = append(mws, middleware.Logging())
	}

	// Auth（白名单）— 配置开关，从 whitelist 构造鉴权函数
	if mc.Auth.Enable && len(mc.Auth.Whitelist) > 0 {
		allowSet := make(map[string]struct{}, len(mc.Auth.Whitelist))
		for _, id := range mc.Auth.Whitelist {
			allowSet[id] = struct{}{}
		}
		mws = append(mws, middleware.Auth(func(ctx *eventctx.Context) bool {
			_, ok := allowSet[ctx.GetChatInfo().ID]
			return ok
		}))
	}

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

	// Backpressure — 配置驱动
	if mc.Backpressure.Limit > 0 {
		policy := parseBackpressurePolicy(mc.Backpressure.Policy)
		timeout := parseDuration(mc.Backpressure.WaitTimeout, 200*time.Millisecond)
		mws = append(mws, middleware.Backpressure(mc.Backpressure.Limit, policy, timeout))
	}

	eng.Use(mws...)

	// Metrics — 配置开关
	if mc.Metrics {
		eng.Use(telemetry.PrometheusMetrics("remilia"))
	}

	eng.Use(telemetry.Tracing(telemetry.TracingConfig{
		TracerName:         "remilia",
		IncludeEventDetail: traceCfg.IncludeEventDetail,
	}))
	eng.Use(errorHandlerMiddleware())

	// Slow handler — 配置开关 + 阈值
	slowThreshold := 3 * time.Second
	if mc.SlowHandler.Enable && mc.SlowHandler.Threshold != "" {
		if d, err := time.ParseDuration(mc.SlowHandler.Threshold); err == nil && d > 0 {
			slowThreshold = d
		}
	}
	eng.Use(slowRequestMiddleware(slowThreshold))

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

func slowRequestMiddleware(threshold time.Duration) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
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
