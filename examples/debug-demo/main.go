package main

import (
	"os"
	"strconv"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/core/help"
	"github.com/KomeiDiSanXian/remilia/plugins/core/permission"
	"github.com/KomeiDiSanXian/remilia/plugins/dev/debug"
)

func main() {
	// 初始化日志
	logger.Info("启动 Debug 插件演示程序...")

	// 从环境变量读取配置
	appIDStr := os.Getenv("BOT_APPID")
	token := os.Getenv("BOT_TOKEN")

	if appIDStr == "" || token == "" {
		logger.Fatal("请设置环境变量 BOT_APPID 和 BOT_TOKEN")
	}

	// 转换 AppID 为 uint64
	appID, err := strconv.ParseUint(appIDStr, 10, 64)
	if err != nil {
		logger.Fatal("BOT_APPID 必须是有效的数字")
	}

	// 创建 Bot 信息
	botInfo := &dto.BotInfo{
		AppID: appID,
		Token: token,
	}

	// 创建 engine
	eng := engine.NewEngine()

	// 创建插件管理器
	pm := plugin.NewManager(eng)

	// 1. 注册权限插件（v2 API）
	logger.Info("注册权限插件...")
	if err := pm.RegisterV2(permission.New()); err != nil {
		logger.Errorf("注册权限插件失败: %v", err)
	}

	// 从容器获取权限插件 API 并配置测试权限
	if permAPI, exists := pm.GetContainer().Get("permission_api"); exists {
		if permPlugin, ok := permAPI.(*permission.Plugin); ok {
			setupTestPermissions(permPlugin)
		}
	}

	// 2. 注册 Debug 插件（v2 API）
	logger.Info("注册 Debug 插件...")
	if err := pm.RegisterV2(debug.New()); err != nil {
		logger.Fatalf("注册 Debug 插件失败: %v", err)
	}

	// 3. 注册帮助插件（v2 API）
	logger.Info("注册帮助插件...")
	if err := pm.RegisterV2(help.New()); err != nil {
		logger.Errorf("注册帮助插件失败: %v", err)
	}

	// 创建 Webhook 适配器
	adapter := remilia.NewWebhookServerAdapter(":8080", botInfo)

	// 创建机器人
	bot := remilia.NewBotWithInfo(adapter, eng, botInfo)
	if bot == nil {
		logger.Fatal("创建机器人失败")
	}

	logger.Info("机器人已创建，可以开始使用 Debug 插件了！")
	logger.Info("")
	logger.Info("可用的调试命令：")
	logger.Info("  /debug event     - 查看事件详情")
	logger.Info("  /debug ctx       - 查看上下文信息")
	logger.Info("  /debug matcher <命令> - 查看命令匹配器")
	logger.Info("  /debug runtime   - 查看运行时信息")
	logger.Info("  /debug commands  - 查看所有命令")
	logger.Info("  /debug plugins   - 查看所有插件")
	logger.Info("  /debug bench <命令> - 性能测试")
	logger.Info("  /debug stats     - 系统统计")
	logger.Info("")
	logger.Info("Webhook 服务器监听在 :8080")
	logger.Info("按 Ctrl+C 停止...")

	// 启动机器人（阻塞）
	if err := bot.Start(); err != nil {
		logger.Fatalf("启动机器人失败: %v", err)
	}
}

// setupTestPermissions 配置测试权限
func setupTestPermissions(permPlugin *permission.Plugin) {
	// 为了演示，这里配置一些测试用户
	// 在实际使用中，应该从配置文件或数据库读取

	// 从环境变量读取管理员用户 ID
	adminUserID := os.Getenv("ADMIN_USER_ID")
	if adminUserID == "" {
		logger.Warn("未设置 ADMIN_USER_ID 环境变量，所有用户都将有权限使用 Debug 命令")
		return
	}

	// 授予管理员所有 debug 权限
	permissions := []string{
		"debug.view",
		"debug.bench",
		"admin",
	}

	for _, perm := range permissions {
		if err := permPlugin.Grant(adminUserID, perm); err != nil {
			logger.Errorf("授予权限 %s 失败: %v", perm, err)
		} else {
			logger.Infof("已授予用户 %s 权限: %s", adminUserID, perm)
		}
	}
}
