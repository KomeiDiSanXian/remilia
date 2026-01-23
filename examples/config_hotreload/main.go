//go:build example
// +build example

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/sirupsen/logrus"
)

func main() {
	// Example 1: Basic usage with manual callback
	example1()

	// Example 2: Auto-restart pattern
	// example2()

	// Example 3: Dynamic configuration with validation
	// example3()
}

// Example 1: Basic usage with custom callback
func example1() {
	logrus.Info("Example 1: Basic configuration hot-reload")

	// Create watcher
	watcher, err := config.NewWatcher("config.yaml")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to create config watcher")
	}
	defer watcher.Stop()

	// Add callback to handle configuration changes
	watcher.AddCallback(func(oldConfig, newConfig *config.Config) error {
		logrus.Info("Configuration changed!")

		// Log the changes
		if oldConfig.Log.Level != newConfig.Log.Level {
			logrus.WithFields(logrus.Fields{
				"old": oldConfig.Log.Level,
				"new": newConfig.Log.Level,
			}).Info("Log level changed")

			// Apply log level change dynamically
			level, _ := logrus.ParseLevel(newConfig.Log.Level)
			logrus.SetLevel(level)
		}

		if oldConfig.Middleware.RateLimit != newConfig.Middleware.RateLimit {
			logrus.WithFields(logrus.Fields{
				"old": oldConfig.Middleware.RateLimit,
				"new": newConfig.Middleware.RateLimit,
			}).Info("Rate limit setting changed")
		}

		return nil
	})

	// Start watching
	watcher.Start()

	// Print initial configuration
	cfg := watcher.GetConfig()
	logrus.WithFields(logrus.Fields{
		"app_id":    cfg.Bot.AppID,
		"log_level": cfg.Log.Level,
		"port":      cfg.Server.Port,
	}).Info("Initial configuration loaded")

	// Simulate application running
	logrus.Info("Application running... Modify config.yaml to see hot-reload in action")

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logrus.Info("Shutting down...")

	// Print statistics
	stats := watcher.GetStats()
	logrus.WithFields(logrus.Fields{
		"reload_count": stats.ReloadCount,
		"failed_count": stats.FailedCount,
		"last_reload":  stats.LastReloadTime,
	}).Info("Configuration watcher statistics")
}

// Example 2: Auto-restart pattern (for components that need restart)
func example2() {
	logrus.Info("Example 2: Auto-restart pattern")

	// Simulated component that needs restart on config change
	type AppComponent struct {
		config *config.Config
		cancel context.CancelFunc
	}

	var currentComponent *AppComponent

	// Restart function
	restartFunc := func(newConfig *config.Config) error {
		logrus.Info("Restarting component with new configuration...")

		// Stop old component
		if currentComponent != nil && currentComponent.cancel != nil {
			currentComponent.cancel()
			time.Sleep(100 * time.Millisecond) // Give time to cleanup
		}

		// Start new component with new config
		ctx, cancel := context.WithCancel(context.Background())
		currentComponent = &AppComponent{
			config: newConfig,
			cancel: cancel,
		}

		// Start component (simulated)
		go func() {
			<-ctx.Done()
			logrus.Info("Component stopped")
		}()

		logrus.Info("Component restarted successfully")
		return nil
	}

	// Create watcher with auto-restart
	watcher, err := config.WatchWithAutoRestart("config.yaml", restartFunc)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to create config watcher")
	}
	defer watcher.Stop()

	// Start initial component
	if err := restartFunc(watcher.GetConfig()); err != nil {
		logrus.WithError(err).Fatal("Failed to start component")
	}

	// Start watching
	watcher.Start()

	logrus.Info("Application running with auto-restart enabled")

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
}

// Example 3: Dynamic configuration with validation
func example3() {
	logrus.Info("Example 3: Dynamic configuration with validation")

	// Create watcher with custom debounce delay
	watcher, err := config.NewWatcher(
		"config.yaml",
		config.WithDebounceDelay(500*time.Millisecond),
	)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to create config watcher")
	}
	defer watcher.Stop()

	// Add validation callback
	watcher.AddCallback(func(oldConfig, newConfig *config.Config) error {
		// Custom validation beyond basic config validation

		// Example: Reject if concurrency limit is too low
		if newConfig.Concurrency.Limit < 10 && newConfig.Concurrency.Limit != 0 {
			logrus.Warn("Rejecting config: concurrency limit too low")
			return logrus.WithFields(logrus.Fields{
				"limit": newConfig.Concurrency.Limit,
			}).Error("concurrency limit must be at least 10 or 0 (unlimited)")
		}

		// Example: Warn if changing critical settings
		if oldConfig.Server.Port != newConfig.Server.Port {
			logrus.WithFields(logrus.Fields{
				"old": oldConfig.Server.Port,
				"new": newConfig.Server.Port,
			}).Warn("Server port changed - restart required!")
		}

		return nil
	})

	// Add metrics callback
	watcher.AddCallback(func(oldConfig, newConfig *config.Config) error {
		// Track configuration changes in metrics
		logrus.Info("Recording configuration change in metrics")
		// metrics.IncrementConfigReload()
		return nil
	})

	// Start watching
	watcher.Start()

	// Periodic stats logging
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			stats := watcher.GetStats()
			logrus.WithFields(logrus.Fields{
				"reload_count": stats.ReloadCount,
				"failed_count": stats.FailedCount,
			}).Info("Configuration watcher stats")
		}
	}()

	logrus.Info("Application running with advanced validation")

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
}

// Example 4: Integration with Bot
func exampleBotIntegration() {
	logrus.Info("Example 4: Bot integration with hot-reload")

	// This would be in your actual bot setup
	/*
		watcher, err := config.NewWatcher("config.yaml")
		if err != nil {
			logrus.WithError(err).Fatal("Failed to create config watcher")
		}
		defer watcher.Stop()

		// Handle dynamic settings
		watcher.AddCallback(func(oldConfig, newConfig *config.Config) error {
			// Update log level dynamically
			if oldConfig.Log.Level != newConfig.Log.Level {
				level, _ := logrus.ParseLevel(newConfig.Log.Level)
				logrus.SetLevel(level)
				logrus.Info("Log level updated")
			}

			// Update middleware settings
			if oldConfig.Middleware != newConfig.Middleware {
				// bot.UpdateMiddleware(newConfig.Middleware)
				logrus.Info("Middleware settings updated")
			}

			// Critical changes require restart
			if oldConfig.Bot.AppID != newConfig.Bot.AppID {
				return fmt.Errorf("bot.app_id change requires manual restart")
			}

			return nil
		})

		watcher.Start()

		// Create bot with initial config
		cfg := watcher.GetConfig()
		bot := remilia.NewBotWithDefault(&dto.BotInfo{
			AppID:     cfg.Bot.AppID,
			QQNum:     cfg.Bot.BotID,
			Token:     cfg.Bot.Token,
			AppSecret: cfg.Bot.Secret,
		})

		bot.Start()
		defer bot.Shutdown()

		// Application runs...
	*/
}
