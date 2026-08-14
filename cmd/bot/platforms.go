package main

import (
	"fmt"

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
