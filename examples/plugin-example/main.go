//go:build example
// +build example

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetLevel(logrus.InfoLevel)

	// 创建 Engine
	eng := engine.NewEngine()

	// 添加全局中间件
	eng.Use(
		middleware.Logging(),
		middleware.Recover(),
	)

	// 创建插件管理器
	manager := plugin.NewManager(eng)

	// 添加生命周期监听器
	manager.AddListener(&LoggingListener{})

	// 注册插件
	registerPlugins(manager)

	// 创建并启动 Bot
	secret := getEnv("BOT_SECRET", "your-webhook-secret")
	port := getEnv("BOT_PORT", "8080")

	adapter := remilia.NewWebhookAdapter(":"+port, secret)
	bot := remilia.NewBot(adapter, eng)

	logrus.Info("Starting bot with plugins...")
	if err := bot.Start(); err != nil {
		logrus.Fatal(err)
	}

	// 演示插件热重载
	go demonstrateReload(manager)

	bot.WaitForShutdown()
}

func registerPlugins(manager *plugin.Manager) {
	// 1. Greeter 插件
	greeter := NewGreeterPlugin()
	if err := manager.Register(greeter); err != nil {
		logrus.WithError(err).Error("Failed to register greeter plugin")
	}

	// 2. Counter 插件
	counter := NewCounterPlugin()
	if err := manager.Register(counter); err != nil {
		logrus.WithError(err).Error("Failed to register counter plugin")
	}

	// 3. Timer 插件
	timer := NewTimerPlugin()
	if err := manager.Register(timer); err != nil {
		logrus.WithError(err).Error("Failed to register timer plugin")
	}

	logrus.Info("All plugins registered")
}

// demonstrateReload 演示插件重载
func demonstrateReload(manager *plugin.Manager) {
	time.Sleep(30 * time.Second)

	logrus.Info("Reloading greeter plugin...")
	if err := manager.Reload("greeter"); err != nil {
		logrus.WithError(err).Error("Failed to reload plugin")
	} else {
		logrus.Info("Plugin reloaded successfully")
	}
}

// ===== Greeter Plugin =====

type GreeterPlugin struct {
	*plugin.BasePlugin
	greeting string
}

func NewGreeterPlugin() *GreeterPlugin {
	return &GreeterPlugin{
		BasePlugin: plugin.NewBasePlugin("greeter"),
		greeting:   "你好",
	}
}

func (p *GreeterPlugin) Load(eng *engine.Engine) error {
	logrus.Info("[Greeter] Loading plugin...")

	// 注册 /greet 命令
	matcher := engine.NewMatcher().
		OnCommand("/greet").
		SetHandler(p.handleGreet)

	p.AddMatcher(matcher)
	eng.RegisterMatcher(matcher)

	// 注册 /setgreeting 命令
	matcher2 := engine.NewMatcher().
		OnCommand("/setgreeting").
		SetHandler(p.handleSetGreeting)

	p.AddMatcher(matcher2)
	eng.RegisterMatcher(matcher2)

	return nil
}

func (p *GreeterPlugin) handleGreet(ctx *eventctx.Context) error {
	name := ctx.GetPlainText()
	if name == "" {
		name = ctx.GetAuthor()
	}

	response := fmt.Sprintf("%s, %s!", p.greeting, name)
	return ctx.Reply(response)
}

func (p *GreeterPlugin) handleSetGreeting(ctx *eventctx.Context) error {
	newGreeting := ctx.GetPlainText()
	if newGreeting == "" {
		return ctx.Reply("用法: /setgreeting <问候语>")
	}

	p.greeting = newGreeting
	return ctx.Reply("问候语已更新为: " + newGreeting)
}

// ===== Counter Plugin =====

type CounterPlugin struct {
	*plugin.BasePlugin
	count int
}

func NewCounterPlugin() *CounterPlugin {
	return &CounterPlugin{
		BasePlugin: plugin.NewBasePlugin("counter"),
		count:      0,
	}
}

func (p *CounterPlugin) Load(eng *engine.Engine) error {
	logrus.Info("[Counter] Loading plugin...")

	// /count 命令
	matcher := engine.NewMatcher().
		OnCommand("/count").
		SetHandler(p.handleCount)

	p.AddMatcher(matcher)
	eng.RegisterMatcher(matcher)

	// /resetcount 命令
	matcher2 := engine.NewMatcher().
		OnCommand("/resetcount").
		SetHandler(p.handleReset)

	p.AddMatcher(matcher2)
	eng.RegisterMatcher(matcher2)

	return nil
}

func (p *CounterPlugin) handleCount(ctx *eventctx.Context) error {
	p.count++
	return ctx.Reply(fmt.Sprintf("计数: %d", p.count))
}

func (p *CounterPlugin) handleReset(ctx *eventctx.Context) error {
	p.count = 0
	return ctx.Reply("计数器已重置")
}

func (p *CounterPlugin) Reload(eng *engine.Engine) error {
	logrus.Info("[Counter] Reloading plugin (preserving count)...")

	// 保存状态
	oldCount := p.count

	// 卸载
	if err := p.Unload(eng); err != nil {
		return err
	}

	// 重新加载
	if err := p.Load(eng); err != nil {
		return err
	}

	// 恢复状态
	p.count = oldCount

	logrus.WithField("count", p.count).Info("[Counter] Reload complete")
	return nil
}

// ===== Timer Plugin =====

type TimerPlugin struct {
	*plugin.BasePlugin
	startTime time.Time
}

func NewTimerPlugin() *TimerPlugin {
	return &TimerPlugin{
		BasePlugin: plugin.NewBasePlugin("timer"),
		startTime:  time.Now(),
	}
}

func (p *TimerPlugin) Load(eng *engine.Engine) error {
	logrus.Info("[Timer] Loading plugin...")

	p.startTime = time.Now()

	// /uptime 命令
	matcher := engine.NewMatcher().
		OnCommand("/uptime").
		SetHandler(p.handleUptime)

	p.AddMatcher(matcher)
	eng.RegisterMatcher(matcher)

	// /time 命令
	matcher2 := engine.NewMatcher().
		OnCommand("/time").
		SetHandler(p.handleTime)

	p.AddMatcher(matcher2)
	eng.RegisterMatcher(matcher2)

	return nil
}

func (p *TimerPlugin) handleUptime(ctx *eventctx.Context) error {
	uptime := time.Since(p.startTime)
	return ctx.Reply(fmt.Sprintf("运行时间: %v", uptime.Round(time.Second)))
}

func (p *TimerPlugin) handleTime(ctx *eventctx.Context) error {
	now := time.Now()
	return ctx.Reply(fmt.Sprintf("当前时间: %s", now.Format("2006-01-02 15:04:05")))
}

// ===== Lifecycle Listener =====

type LoggingListener struct{}

func (l *LoggingListener) OnPluginLoaded(name string) {
	logrus.WithField("plugin", name).Info("Plugin loaded")
}

func (l *LoggingListener) OnPluginUnloaded(name string) {
	logrus.WithField("plugin", name).Info("Plugin unloaded")
}

func (l *LoggingListener) OnPluginReloaded(name string) {
	logrus.WithField("plugin", name).Info("Plugin reloaded")
}

func (l *LoggingListener) OnPluginError(name string, operation string, err error) {
	logrus.WithFields(logrus.Fields{
		"plugin":    name,
		"operation": operation,
		"error":     err,
	}).Error("Plugin error")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
