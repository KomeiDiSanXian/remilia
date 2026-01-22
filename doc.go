/*
Package remilia provides a high-level wrapper around the core Engine with lifecycle management.

Bot is the main entry point for building event-driven applications with Remilia framework.
It provides:
  - Lifecycle management (start/stop)
  - Health checking
  - Configuration management
  - Integration with Engine and Adapter

Basic Usage:

	import (
	    "context"
	    "log"
	    "time"

	    "github.com/KomeiDiSanXian/remilia"
	    "github.com/KomeiDiSanXian/remilia/core/engine"
	    context2 "github.com/KomeiDiSanXian/remilia/core/context"
	    "github.com/KomeiDiSanXian/remilia/openapi/dto"
	)

	// Create Engine and Adapter
	eng := engine.NewEngine()
	adapter := myAdapter // implements remilia.Adapter

	// Register event handlers
	eng.OnC2C(context2.OnCommand("hello")).Handle(func(ctx *context2.Context) error {
	    ctx.ReplyPrivate(&dto.Message{Content: "Hello!"})
	    return nil
	})

	// Create Bot
	bot := remilia.NewBot(adapter, eng,
	    remilia.WithName("my-bot"),
	    remilia.WithVersion("1.0.0"),
	    remilia.WithDebug(true),
	)

	// Start Bot
	if err := bot.Start(); err != nil {
	    log.Fatal(err)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bot.Shutdown(ctx)

Health Checking:

	status := bot.Health()
	fmt.Printf("Status: %s\n", status.Status)
	fmt.Printf("Uptime: %v\n", status.Uptime)
	for name, component := range status.Components {
	    fmt.Printf("  %s: %s - %s\n", name, component.Status, component.Message)
	}

Lifecycle Management:

Bot uses the lifecycle package to manage component startup and shutdown:
  - Components start in order
  - Components stop in reverse order
  - Failed startup triggers automatic rollback
  - Shutdown continues even if a component fails

Configuration:

Bot configuration can be set via options:
  - WithName(name) - Set bot name
  - WithVersion(version) - Set bot version
  - WithDebug(bool) - Enable debug logging
  - WithConfig(*Config) - Set full configuration

Adapter Interface:

The Adapter interface connects event sources to the Bot:

	type Adapter interface {
	    Start(ctx context.Context, handleFunc func(*dto.Payload)) error
	    Shutdown(ctx context.Context) error
	}

You can use the built-in WebHook adapter or implement your own:

	// Using WebHook adapter
	webhook := myWebHook // implements remilia.WebHook interface
	adapter := remilia.NewWebhookAdapter(webhook)

For more information, see:
  - github.com/KomeiDiSanXian/remilia/core/engine - Core engine implementation
  - github.com/KomeiDiSanXian/remilia/core/context - Context and rules
  - github.com/KomeiDiSanXian/remilia/lifecycle - Lifecycle management
*/
package remilia
