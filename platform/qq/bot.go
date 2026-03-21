package qq

// bot.go — QQ 平台专属的 Bot 便捷构造函数。

// BotFactory 是 Bot 构造辅助工厂，避免 import cycle（qq 包不直接依赖根包 remilia）。
//
// 由于 Go 包循环依赖的限制，qq 包无法直接调用 remilia.NewBotBuilder()，
// 因此以文档形式提供等效的构建示例，实际 Bot 构建仍由调用方完成。
//
// 推荐构建方式（完全等价于原 remilia.NewBotWithDefault）：
//
//	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
//	bot, err := remilia.NewBotBuilder().
//	    WithPlatformAdapter(adapter).
//	    Build()
//
// 若需要更多选项：
//
//	adapter := qq.NewWebhookServerAdapterWithConfig(":8080", botInfo, cfg.Webhook)
//	bot, err := remilia.NewBotBuilder().
//	    WithPlatformAdapter(adapter).
//	    WithName("my-bot").
//	    WithDebug(true).
//	    Build()
