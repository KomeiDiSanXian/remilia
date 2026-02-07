package main

import (
	"log"
	"strings"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/help"
)

// EchoPlugin 示例插件：回显消息
type EchoPlugin struct {
	*plugin.BasePlugin
}

// NewEchoPlugin 创建回显插件（带元数据）
func NewEchoPlugin() *EchoPlugin {
	metadata := &plugin.Metadata{
		Name:        "echo",
		Version:     "1.0.0",
		Author:      "Example Team",
		Description: "一个简单的消息回显插件",
		HelpText: `回显插件使用说明：
  /echo <消息> - 回显你发送的消息
  /reverse <消息> - 反转消息内容`,
		Category: "工具",
		Tags:     []string{"消息", "工具", "示例"},
		Homepage: "https://example.com/echo-plugin",
		Hidden:   false,
	}

	return &EchoPlugin{
		BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
	}
}

// Load 加载插件
func (p *EchoPlugin) Load(eng *engine.Engine) error {
	logger.Info("[EchoPlugin] Loading...")

	// 使用 BasePlugin.OnCommand 自动注册 Matcher
	p.OnCommand(eng, dto.C2CMessageCreate, "/echo").
		Handle(p.handleEcho)

	p.OnCommand(eng, dto.C2CMessageCreate, "/reverse").
		Handle(p.handleReverse)

	logger.Info("[EchoPlugin] Loaded successfully")
	return nil
}

func (p *EchoPlugin) handleEcho(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()
	args, _ := command.ParseCommandLine(content)

	msg := args.Get(0)
	if msg == "" {
		msg = "请提供要回显的消息"
	}

	reply := &dto.Message{
		Type:    dto.TextMessage,
		Content: "回显: " + msg,
	}
	_, err := ctx.ReplyPrivate(reply)
	return err
}

func (p *EchoPlugin) handleReverse(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()
	args, _ := command.ParseCommandLine(content)

	msg := args.Get(0)
	if msg == "" {
		msg = "请提供要反转的消息"
	}

	// 反转字符串
	runes := []rune(msg)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}

	reply := &dto.Message{
		Type:    dto.TextMessage,
		Content: "反转: " + string(runes),
	}
	_, err := ctx.ReplyPrivate(reply)
	return err
}

// WeatherPlugin 示例插件：天气查询
type WeatherPlugin struct {
	*plugin.BasePlugin
}

// NewWeatherPlugin 创建天气插件（带元数据）
func NewWeatherPlugin() *WeatherPlugin {
	metadata := &plugin.Metadata{
		Name:        "weather",
		Version:     "2.1.0",
		Author:      "Weather Team",
		Description: "查询城市天气信息",
		HelpText: `天气插件使用说明：
  /weather <城市> - 查询城市的天气信息
  
示例：
  /weather 北京
  /weather 上海`,
		Category:   "生活",
		Tags:       []string{"天气", "生活", "信息"},
		Homepage:   "https://example.com/weather-plugin",
		Repository: "https://github.com/example/weather-plugin",
		Hidden:     false,
	}

	return &WeatherPlugin{
		BasePlugin: plugin.NewBasePluginWithMetadata(metadata),
	}
}

// Load 加载插件
func (p *WeatherPlugin) Load(eng *engine.Engine) error {
	logger.Info("[WeatherPlugin] Loading...")

	// 使用 BasePlugin.OnCommand 自动注册 Matcher
	p.OnCommand(eng, dto.C2CMessageCreate, "/weather").
		Handle(p.handleWeather)

	logger.Info("[WeatherPlugin] Loaded successfully")
	return nil
}

func (p *WeatherPlugin) handleWeather(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()
	args, _ := command.ParseCommandLine(content)

	city := args.Get(0)
	if city == "" {
		city = "未知城市"
	}

	// 模拟天气查询
	reply := &dto.Message{
		Type:    dto.TextMessage,
		Content: city + " 的天气：晴朗，温度 22°C",
	}
	_, err := ctx.ReplyPrivate(reply)
	return err
}

func main() {
	// 初始化日志（使用默认配置）
	logCfg := logger.Config{
		Level:      "debug",
		Console:    true,
		File:       false,
		TimeFormat: "2006-01-02 15:04:05",
	}
	if err := logger.Init(logCfg); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}

	// 尝试加载配置（如果不存在则使用演示模式）
	cfg, err := config.Load("config.yaml")
	if err != nil {
		logger.Warn("[PluginMetadataDemo] Config file not found, running in demo mode (webhook disabled)")
		logger.Warn("[PluginMetadataDemo] This is a demonstration of plugin metadata features")
		runDemoMode()
		return
	}

	// 验证配置
	if cfg.Bot.AppID == 0 || cfg.Bot.Token == "" {
		logger.Warn("[PluginMetadataDemo] Invalid bot config, running in demo mode")
		runDemoMode()
		return
	}

	// 使用真实配置运行
	runWithConfig(cfg)
}

func runDemoMode() {
	logger.Info(strings.Repeat("=", 50))
	logger.Info("[PluginMetadataDemo] 演示模式")
	logger.Info(strings.Repeat("=", 50))

	// 只演示插件系统，不启动 webhook
	eng := engine.NewEngine()
	eng.Use(middleware.DevelopmentSet()...)

	// 创建插件管理器
	manager := plugin.NewManager(eng)

	// 注册插件
	registerPlugins(manager)

	// 显示插件信息
	logger.Info("\n" + strings.Repeat("=", 50))
	logger.Info("[PluginMetadataDemo] 已加载的插件:")
	logger.Info(strings.Repeat("=", 50))

	pluginsMetadata := manager.ListWithMetadata()
	for name, meta := range pluginsMetadata {
		logger.Info("")
		logger.Infof("  插件: %s", name)
		if meta.Version != "" {
			logger.Infof("  版本: %s", meta.Version)
		}
		if meta.Description != "" {
			logger.Infof("  描述: %s", meta.Description)
		}
		if meta.Category != "" {
			logger.Infof("  分类: %s", meta.Category)
		}
	}

	logger.Info("\n" + strings.Repeat("=", 50))
	logger.Info("[PluginMetadataDemo] 提示:")
	logger.Info("  1. 这是演示模式，webhook 未启动")
	logger.Info("  2. 要运行完整功能，请配置 config.yaml")
	logger.Info("  3. 插件元数据功能已完整展示")
	logger.Info(strings.Repeat("=", 50))
}

func runWithConfig(cfg *config.Config) {
	logger.Info("[PluginMetadataDemo] Running with config...")

	botInfo := &dto.BotInfo{
		AppID:     cfg.Bot.AppID,
		Token:     cfg.Bot.Token,
		AppSecret: cfg.Bot.Secret,
	}

	bot, err := remilia.NewBotBuilder().
		WithBotInfo(botInfo).
		WithWebhook(":9000").
		Build()
	if err != nil {
		logger.WithError(err).Fatal("[PluginMetadataDemo] Failed to build bot")
	}

	bot.Engine().Use(middleware.DevelopmentSet()...)

	// 创建插件管理器
	manager := plugin.NewManager(bot.Engine())

	// 注册插件
	registerPlugins(manager)

	logger.Info("[PluginMetadataDemo] All plugins loaded")
	logger.Info("[PluginMetadataDemo] Try these commands:")
	logger.Info("  /help plugins - 显示所有插件")
	logger.Info("  /help echo - 显示 echo 插件信息")
	logger.Info("  /help weather - 显示 weather 插件信息")
	logger.Info("  /echo Hello - 回显消息")
	logger.Info("  /weather 北京 - 查询天气")

	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[PluginMetadataDemo] Failed to start bot")
	}
	bot.WaitForShutdown()
}

func registerPlugins(manager *plugin.Manager) {
	// 注册帮助插件 - 不再需要 CommandRegistry
	helpPlugin := help.New()
	helpPlugin.SetPluginManager(manager)
	if err := manager.Register(helpPlugin); err != nil {
		logger.WithError(err).Error("[PluginMetadataDemo] Failed to register help plugin")
	}

	// 注册自定义插件
	echoPlugin := NewEchoPlugin()
	if err := manager.Register(echoPlugin); err != nil {
		logger.WithError(err).Error("[PluginMetadataDemo] Failed to register echo plugin")
	}

	weatherPlugin := NewWeatherPlugin()
	if err := manager.Register(weatherPlugin); err != nil {
		logger.WithError(err).Error("[PluginMetadataDemo] Failed to register weather plugin")
	}
}
