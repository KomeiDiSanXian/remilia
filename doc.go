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

# QQ-Specific Usage (Backward Compatible)

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

	// QQ convenience matchers (still work)
	eng.OnC2C(eventctx.OnCommand("/ping")).Handle(pingHandler)
	eng.OnGroupAt(eventctx.OnCommand("/hello")).Handle(helloHandler)

# Multi-Platform

Connect multiple platforms to a single Bot instance:

	bot, err := remilia.NewBotBuilder().
	    WithPlatformAdapter(qqAdapter).
	    WithPlatformAdapter(discordAdapter).
	    WithEngine(eng).
	    Build()

# Adapter Interface

Two adapter interfaces are supported:

  - PlatformAdapter (recommended): platform-agnostic, handler receives platform.Event
  - Adapter (deprecated): QQ-specific, handler receives *dto.Payload

# Health Checking

	status := bot.Health()
	fmt.Printf("Status: %s, Uptime: %v\n", status.Status, status.Uptime)

# Lifecycle

Bot uses the lifecycle package: components start in order, stop in reverse.
Failed startup triggers automatic rollback.
*/
package remilia
