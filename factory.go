package remilia

// factory.go — 此文件已清空。
//
// NewBotWithDefault（QQ 平台专属便捷构造函数）已移至 platform/qq 包：
//
//	bot, err := qq.NewBotWithDefault(":8080", botInfo)
//
// 或直接使用通用 BotBuilder（推荐方式）：
//
//	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot, err := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
