package main

import (
	"os"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/core/help"
	"github.com/KomeiDiSanXian/remilia/plugins/core/permission"
	"github.com/KomeiDiSanXian/remilia/plugins/dev/debug"
)

func main() {
	// 初始化日志
	logger.Info("🚀 启动 Debug 子命令演示程序...")

	cfg, err := config.Load("config.yaml")

	if err != nil {
		return
	}

	botInfo := &dto.BotInfo{
		AppID:     cfg.Bot.AppID,
		Token:     cfg.Bot.Token,
		AppSecret: cfg.Bot.Secret,
	}

	// 创建 Engine
	eng := engine.NewEngine()

	// 创建插件管理器
	pm := plugin.NewManager(eng)

	// 1. 注册 Permission 插件（Debug 插件依赖）
	logger.Info("📦 加载 Permission 插件...")
	permPlugin := permission.New()
	if err := pm.Register(permPlugin); err != nil {
		logger.Fatal("注册 Permission 插件失败: " + err.Error())
	}

	// 添加管理员权限（用于演示）
	adminUserID := os.Getenv("ADMIN_USER_ID")
	if adminUserID != "" {
		// 分配管理员角色（admin 角色拥有所有权限）
		if err := permPlugin.AssignRole(adminUserID, "admin"); err != nil {
			logger.Warn("分配管理员角色失败: " + err.Error())
		} else {
			logger.Info("✅ 已授予用户 " + adminUserID + " 管理员权限")
		}
	}

	// 2. 注册 Debug 插件
	logger.Info("🐛 加载 Debug 插件...")
	debugPlugin := debug.New()
	debugPlugin.SetDevMode(true) // 启用开发模式（允许所有用户使用）
	debugPlugin.SetPermissionPlugin(permPlugin)
	debugPlugin.SetPluginManager(pm)
	if err := pm.Register(debugPlugin); err != nil {
		logger.Fatal("注册 Debug 插件失败: " + err.Error())
	}

	// 3. 注册 Help 插件（可以查看所有命令）
	logger.Info("📚 加载 Help 插件...")
	helpPlugin := help.New()
	if err := pm.Register(helpPlugin); err != nil {
		logger.Fatal("注册 Help 插件失败: " + err.Error())
	}

	// 4. 添加一些示例命令用于测试
	logger.Info("➕ 注册示例命令...")

	// 示例命令 1: /hello
	eng.OnCommand(dto.C2CMessageCreate, "/hello").
		SetDescription("打招呼命令").
		SetUsage("/hello").
		SetCategory("示例").
		Handle(func(ctx *eventctx.Context) error {
			logger.Info("[/hello] 收到命令")
			// 注意：实际回复需要使用 OpenAPI 客户端
			return nil
		})

	// 示例命令 2: /echo
	eng.OnCommand(dto.C2CMessageCreate, "/echo").
		SetDescription("回声命令").
		SetUsage("/echo <消息>").
		SetCategory("示例").
		Handle(func(ctx *eventctx.Context) error {
			content := ctx.GetMessageContent()
			logger.Infof("[/echo] 收到内容: %s", content)
			return nil
		})

	// 示例命令 3: /weather（带群聊支持）
	eng.OnCommand(dto.C2CMessageCreate, "/weather").
		SetDescription("天气查询").
		SetUsage("/weather <城市>").
		SetCategory("工具").
		Handle(func(ctx *eventctx.Context) error {
			logger.Info("[/weather] 收到天气查询")
			return nil
		})

	eng.OnCommand(dto.GroupAtMessageCreate, "/weather").
		SetDescription("天气查询").
		SetUsage("/weather <城市>").
		SetCategory("工具").
		Handle(func(ctx *eventctx.Context) error {
			logger.Info("[/weather] 收到天气查询（群聊）")
			return nil
		})

	// 加载所有插件（Register 会自动调用 Load）
	logger.Info("🔄 插件已全部注册并加载")

	// 创建 Bot
	logger.Info("🤖 创建 Bot...")
	bot, err := remilia.NewBotBuilder().
		WithBotInfo(botInfo).
		WithWebhook(":9000").
		WithEngine(eng).
		Build()
	if err != nil {
		logger.Fatal("创建 Bot 失败: " + err.Error())
	}

	bot.Engine().Use(middleware.DevelopmentSet()...)

	// 显示可用的调试命令
	logger.Info("\n" + getUsageInfo())

	// 启动 Bot
	logger.Info("▶️  启动 Bot...")
	if err := bot.Start(); err != nil {
		logger.Fatal("启动失败: " + err.Error())
	}

	logger.Info("✅ Bot 已启动，等待消息...")
	logger.Info("💡 提示：在私聊中发送 /debug 查看所有调试命令")
	logger.Info("💡 提示：在私聊中发送 /help 查看所有可用命令")

	bot.WaitForShutdown()
}

// getUsageInfo 返回使用说明
func getUsageInfo() string {
	return `
╔════════════════════════════════════════════════════════════════╗
║          Debug 子命令演示程序 - 使用说明                        ║
╚════════════════════════════════════════════════════════════════╝

📌 Debug 插件子命令：

  🔍 事件调试：
     /debug event          - 显示当前事件的详细信息
     /debug ctx            - 显示当前上下文的所有信息
     /debug matcher <命令> - 查看命令匹配器的详细信息

  🔧 系统调试：
     /debug runtime        - 显示运行时信息（goroutine、内存等）
     /debug commands       - 显示所有注册的命令
     /debug plugins        - 显示所有插件的状态

  📊 性能分析：
     /debug bench <命令>   - 测试命令的执行性能
     /debug stats          - 显示系统统计信息

  💡 帮助：
     /debug                - 显示所有子命令的帮助信息
     /help                 - 查看所有可用命令
     /help debug           - 查看 debug 命令的详细信息

📝 示例用法：

  1. 查看所有调试命令：
     发送: /debug

  2. 查看当前事件信息：
     发送: /debug event

  3. 查看所有注册的命令：
     发送: /debug commands

  4. 查看特定命令的匹配器信息：
     发送: /debug matcher hello

  5. 测试命令性能：
     发送: /debug bench echo

  6. 查看运行时信息：
     发送: /debug runtime

🔐 权限说明：

  - 如果设置了 ADMIN_USER_ID 环境变量，该用户将拥有所有 debug 权限
  - 如果未设置，Debug 插件处于开发模式，允许所有用户使用
  - 生产环境建议关闭开发模式，并通过权限系统控制访问

🎯 测试流程建议：

  1. 先发送 /help 查看所有命令
  2. 发送 /debug 查看调试子命令
  3. 发送 /debug commands 查看命令注册情况
  4. 发送 /debug plugins 查看插件状态
  5. 发送 /debug event 查看事件详情
  6. 发送 /debug runtime 查看系统运行状态

════════════════════════════════════════════════════════════════
`
}
