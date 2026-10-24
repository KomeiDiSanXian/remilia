package main

import (
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/discord"
	"github.com/KomeiDiSanXian/remilia/platform/milky"
	"github.com/KomeiDiSanXian/remilia/platform/onebot"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform/satori"
	"github.com/KomeiDiSanXian/remilia/platform/telegram"
	"github.com/KomeiDiSanXian/remilia/platform/terminal"
)

// setupPlatforms 创建平台注册表：注册所有启用适配器，
// 未配置任何平台时回退到 Terminal 适配器便于本地开发。
func (a *app) setupPlatforms() {
	reg := platform.NewRegistry().WithObserver(platform.MetricsObserver())
	registerPlatforms(reg, a.cfg)
	if reg.Len() == 0 {
		logger.Warn("[remilia] No platform configured, using Terminal adapter for development")
		reg.Register(terminal.NewAdapter(
			terminal.WithPrompt("Bot> "),
			terminal.WithBotName("DevBot"),
		))
	}
	a.reg = reg
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

// subscribePlatformHotReload 订阅平台热更新：仅当 bot.* 配置实际变化时同步平台适配器，
// 避免修改日志级别等无关字段导致连接断开。
func (a *app) subscribePlatformHotReload() {
	bot := a.bot
	var lastBotCfg = &a.cfg.Bot // 启动时的 bot 配置
	var lastBotMu sync.Mutex    // 保护 lastBotCfg 并发读写
	config.Subscribe(func(newCfg *config.Config) {
		if newCfg == nil {
			return
		}
		lastBotMu.Lock()
		changed := lastBotCfg.HasChanged(&newCfg.Bot)
		if changed {
			*lastBotCfg = newCfg.Bot
		}
		lastBotMu.Unlock()
		if !changed {
			return
		}
		desired := buildDesiredAdapters(newCfg)
		if err := bot.SyncPlatforms(desired); err != nil {
			logger.WithError(err).Error("[remilia] Failed to sync platforms")
		}
	})
}

// platformFactories 返回当前配置中所有启用的平台及其创建函数。
func platformFactories(cfg *config.Config) map[string]func() (platform.Adapter, error) {
	factories := make(map[string]func() (platform.Adapter, error))

	if c := cfg.Bot.QQ; c != nil {
		factories["qq"] = func() (platform.Adapter, error) {
			info := &dto.BotInfo{
				QQNum: c.BotID, AppID: c.AppID,
				Token: c.Token, AppSecret: c.Secret,
			}
			// 接入方式由 qq.mode 决定：webhook（默认）监听官方回调，
			// ws 主动连接官方 WebSocket 网关（无公网回调地址时使用）。
			if c.ResolveMode() == config.QQModeWS {
				if c.Intents != 0 {
					return qq.NewWSAdapterWithIntents(info, qq.Intents(c.Intents)), nil
				}
				return qq.NewWSAdapter(info), nil
			}
			addr := fmt.Sprintf("%s:%d", c.Webhook.Host, c.Webhook.Port)
			return qq.NewWebhookServerAdapter(addr, info), nil
		}
	}

	if c := cfg.Bot.OneBot; c != nil {
		factories["onebot"] = func() (platform.Adapter, error) {
			obCfg := onebot.Config{
				URL: c.URL, ListenAddr: c.ListenAddr, Token: c.Token, Secret: c.Secret,
				MediaBaseURL: c.MediaBaseURL,
			}
			// 三种接入方式共用同一份配置，仅传输层不同：
			// forward_ws（默认）主动连接、reverse_ws 监听 OneBot 连入、
			// http_post 监听事件上报并把 API 调用发往 url。
			switch c.ResolveMode() {
			case config.OneBotModeReverseWS:
				obCfg.Mode = onebot.ModeReverseWS
				return onebot.NewReverseWSAdapter(obCfg), nil
			case config.OneBotModeHTTPPost:
				obCfg.Mode = onebot.ModeHTTPPost
				return onebot.NewHTTPPostAdapter(obCfg), nil
			case config.OneBotModeForwardWS:
				obCfg.Mode = onebot.ModeForwardWS
				return onebot.NewForwardWSAdapter(obCfg), nil
			default:
				return nil, fmt.Errorf("unsupported onebot.mode %q", c.Mode)
			}
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
