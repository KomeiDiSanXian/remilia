// Package terminal 提供基于终端/控制台的平台适配器，用于调试和分析。
//
// 基于 golang.org/x/term 实现，支持行编辑、历史记录（上下方向键）和
// Tab 补全（通过 WithCompletionFunc 配置）。
//
// 该适配器从 stdin 读取用户输入并将其作为 platform.Event 传递给处理函数，
// Bot 的回复输出到 stdout。适用于：
//   - 在不依赖外部服务的情况下测试 Handler 和命令
//   - 交互式调试 Bot 逻辑
//   - 在简单环境中演示 Bot 能力
//
// 终端特性：
//   - 行编辑（退格、光标移动、删除等）
//   - 历史记录（上下方向键导航，默认保存最近 100 条）
//   - Tab 补全（需通过 WithCompletionFunc 提供补全函数）
//   - 原始模式（终端 stdin 自动设为 raw mode，退出时自动恢复）
//
// 实现的接口：
//
//	platform.Adapter            — 核心适配器接口（Platform / Start / Stop / Sender / Capabilities / IsRunning）
//	platform.Sender             — 消息发送（Adapter.Sender() 返回自身）
//	platform.MessageEditor      — 消息编辑（终端重新打印编辑后的消息）
//	platform.MessageDeleter     — 消息删除（终端标记已删除）
//	platform.TypingNotifier     — 输入指示（终端打印 "[Bot 正在输入...]"）
//	platform.ReactionSender     — 表情回应（终端显示表情标记）
//	platform.BotIdentity        — 机器人身份信息（可配置 BotID / BotName）
//	platform.RecoverableAdapter — 断连通知（EOF / 读取错误时触发）
//	platform.GroupInfoProvider  — 群组信息查询（返回模拟数据）
//	platform.GroupManager       — 群组管理（踢人 / 禁言 / 设置管理员）
//	platform.AvatarProvider     — 用户头像查询（占位符 URL）
//	platform.SessionNotifier    — 主动推送（输出到终端）
//	platform.AutoModerator      — 自动审核（模拟删除 / 禁言）
//	platform.InvitationHandler  — 邀请处理（模拟接受 / 拒绝）
//
// 使用示例（独立适配器 + Tab 补全）：
//
//	adapter := terminal.NewAdapter(
//	    terminal.WithCompletionFunc(func(prefix string) []string {
//	        return reg.Complete(prefix)
//	    }),
//	)
//
//	eng := engine.NewEngine()
//	go func() {
//	    _ = adapter.Start(ctx, func(event platform.Event) {
//	        eng.ProcessPlatformEvent(event, adapter)
//	    })
//	}()
//
// 选项（Option）：
//
//	adapter := terminal.NewAdapter(
//	    terminal.WithPrompt("Bot> "),              // 自定义输入提示符
//	    terminal.WithWelcomeMessage("欢迎使用\n"),  // 自定义欢迎信息
//	    terminal.WithBotID("my-bot"),               // 设置机器人 ID
//	    terminal.WithBotName("测试机器人"),           // 设置机器人名称
//	    terminal.WithInput(reader),                 // 自定义输入源（测试用）
//	    terminal.WithOutput(writer),                // 自定义输出目标（测试用）
//	    terminal.WithCompletionFunc(fn),            // Tab 补全回调
//	)
//
// 测试辅助方法：
//
//	adapter.SimulateMessage("hello")          // 模拟私聊消息
//	adapter.SimulateGroupMessage("hi", "g1")  // 模拟群聊消息
//	adapter.Messages()                        // 获取已发送消息列表
//	adapter.LastMessage()                     // 获取最后一条消息
//	adapter.Clear()                           // 清除消息记录
package terminal
