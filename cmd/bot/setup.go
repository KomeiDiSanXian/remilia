package main

import (
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
		addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		reg.Register(qq.NewWebhookServerAdapter(addr, &dto.BotInfo{
			QQNum: c.BotID, AppID: c.AppID,
			Token: c.Token, AppSecret: c.Secret,
		}))
		logger.Infof("[bot] Registered QQ adapter on %s", addr)
	}

	if c := cfg.Bot.OneBot; c != nil {
		reg.Register(onebot.NewForwardWSAdapter(onebot.Config{
			URL: c.URL, Token: c.Token, Secret: c.Secret,
			Mode: onebot.ModeForwardWS,
		}))
		logger.Infof("[bot] Registered OneBot adapter: %s", c.URL)
	}

	if c := cfg.Bot.Discord; c != nil {
		a, err := discord.NewAdapter(c.Token)
		if err != nil {
			logger.WithError(err).Error("[bot] Failed to create Discord adapter, skipping")
		} else {
			reg.Register(a)
			logger.Info("[bot] Registered Discord adapter")
		}
	}

	if c := cfg.Bot.Satori; c != nil {
		a, err := satori.NewAdapter(satori.Config{
			ServerURL: c.ServerURL, Token: c.Token,
			Platform: c.Platform, UserID: c.UserID,
		})
		if err != nil {
			logger.WithError(err).Error("[bot] Failed to create Satori adapter, skipping")
		} else {
			reg.Register(a)
			logger.Infof("[bot] Registered Satori adapter: %s", c.ServerURL)
		}
	}

	if c := cfg.Bot.Milky; c != nil {
		a, err := milky.NewAdapter(milky.Config{
			BaseURL: c.BaseURL, AccessToken: c.AccessToken,
		})
		if err != nil {
			logger.WithError(err).Error("[bot] Failed to create Milky adapter, skipping")
		} else {
			reg.Register(a)
			logger.Infof("[bot] Registered Milky adapter: %s", c.BaseURL)
		}
	}

	if reg.Len() == 0 {
		logger.Warn("[bot] No platform configured, using Terminal adapter for development")
		reg.Register(terminal.NewAdapter(
			terminal.WithPrompt("Bot> "),
			terminal.WithBotName("DevBot"),
		))
	}

	return reg
}

func setupMiddleware(eng *engine.Engine, traceCfg *tracing.Config) {
	eng.Use(middleware.ProductionSet()...)
	eng.Use(telemetry.PrometheusMetrics("remilia"))
	eng.Use(telemetry.Tracing(telemetry.TracingConfig{
		TracerName:         "remilia",
		IncludeEventDetail: traceCfg.IncludeEventDetail,
	}))
	eng.Use(errorHandlerMiddleware())
	eng.Use(slowRequestMiddleware(3 * time.Second))
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
				logger.WithError(err).Error("[bot] Handler error")
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
				}).Warn("[bot] Slow request detected")
			}
			return err
		}
	}
}
