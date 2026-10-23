// toolctx.go — 工具调用源信息（会话视图）。
//
// ToolSource 描述"这次调用发生在哪个会话、由谁发起"，由装配侧放进
// [Invocation.Source]；工具经 [ToolSourceFromInvocation] 读取。原始平台发送器
// 不进入 Invocation：需要主动推送的端口（如 ReminderScheduler）由装配侧在
// 构造时持有 sender，工具只消费端口。
//
// 工具回调不接触事件上下文，与 send_message / send_to 保持同一安全边界。
package toolkit

// ToolSource 插件可读取的会话源信息（经 [ToolSourceFromInvocation] 提取）。
type ToolSource struct {
	// UserID 当前发送者 ID。
	UserID string
	// ChatID 当前会话 ID（群或私聊）。
	ChatID string
	// Platform 当前平台标识（如 qq / telegram）。
	Platform string
	// IsGroup 是否为群聊会话。
	IsGroup bool
}
