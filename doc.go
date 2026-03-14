/*
Package remilia provides a high-level wrapper around the core engine with lifecycle management.

Bot is the main entry point for building event-driven applications with Remilia framework.
It provides:
  - Lifecycle management (start/stop)
  - Health checking
  - Configuration management
  - Multi-platform event handling via platform.PlatformAdapter

# Platform-Agnostic Usage (Recommended)

Register handlers using platform-agnostic event matching:

	import (
	    "context"
	    "log"

	    "github.com/KomeiDiSanXian/remilia"
	    "github.com/KomeiDiSanXian/remilia/core/engine"
	    eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	    "github.com/KomeiDiSanXian/remilia/platform"
	)

	eng := engine.NewEngine()

	// Register a command handler that works on any platform
	eng.OnCommand("", "/hello").
	    Handle(func(ctx *eventctx.Context) error {
	        return ctx.Reply(platform.TextMessage("Hello!"))
	    })

	// Build Bot with a PlatformAdapter
	bot, err := remilia.NewBotBuilder().
	    WithPlatformAdapter(qqAdapter). // platform.PlatformAdapter
	    WithEngine(eng).
	    Build()

# QQ-Specific Usage (Legacy QQ Path)

For QQ bots, dto.BotInfo and the webhook adapter are still supported:

	import "github.com/KomeiDiSanXian/remilia/openapi/dto"

	bot, err := remilia.NewBotBuilder().
	    WithBotInfo(&dto.BotInfo{
	        AppID:     123456,
	        Token:     "your-token",
	        AppSecret: "your-secret",
	    }).
	    WithWebhook(":8080").
	    WithEngine(eng).
	    Build()

	// Platform-agnostic matchers (recommended for all platforms)
	eng.OnEventKind(platform.EventKindPrivateMessage, eventctx.OnCommand("/ping")).Handle(pingHandler)
	eng.OnEventKind(platform.EventKindGroupMessage, eventctx.OnCommand("/hello")).Handle(helloHandler)

# Multi-Platform

Connect multiple platforms to a single Bot instance:

	bot, err := remilia.NewBotBuilder().
	    WithPlatformAdapter(qqAdapter).
	    WithPlatformAdapter(discordAdapter).
	    WithEngine(eng).
	    Build()

# Adapter Interface

One adapter interface is supported:

  - PlatformAdapter (recommended): platform-agnostic, handler receives platform.Event

# Health Checking

	status := bot.Health()
	fmt.Printf("Status: %s, Uptime: %v\n", status.Status, status.Uptime)

# Lifecycle

Bot uses the lifecycle package: components start in order, stop in reverse.
Failed startup triggers automatic rollback.
*/
package remilia
