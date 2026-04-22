package platform

import stdctx "context"

// ────────────────────────────────────────────────────────────────────────────
// Adapter
// ────────────────────────────────────────────────────────────────────────────

// Adapter 是平台适配器的核心接口。
//
// 每个平台（QQ、Discord、Telegram 等）实现此接口，
// 框架核心通过此接口接收事件和发送消息，不依赖任何平台 SDK。
//
// 生命周期：
//
//	Start() ──→ [事件循环，持续调用 handler] ──→ Stop()
type Adapter interface {
	// Platform 返回平台标识符（小写，如 "qq"、"discord"、"telegram"）
	Platform() string

	// Start 启动适配器事件循环（阻塞，直到 ctx 取消或出错）
	//
	// 每收到一个事件，调用 handler(event)。
	// handler 应快速返回（框架内部会在 goroutine 中处理）。
	Start(ctx stdctx.Context, handler func(Event)) error

	// Stop 优雅停止适配器
	Stop(ctx stdctx.Context) error

	// Sender 返回该平台的消息发送接口
	Sender() Sender

	// Capabilities 返回该平台支持的特性集合。
	// 用于 Handler 做跨平台特性检测，实现渐进增强策略。
	Capabilities() Capabilities

	// IsRunning 返回适配器当前是否处于运行状态。
	//
	// 在 Start() 成功启动后返回 true，Stop() 完成后返回 false。
	// 用于健康检查和监控，实现应保证并发安全。
	IsRunning() bool
}

// ────────────────────────────────────────────────────────────────────────────
// RecoverableAdapter（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// RecoverableAdapter 可选接口：支持感知断连事件的适配器实现此接口。
//
// 适配器在 Start() 内部自动重连时，每次意外断连应调用已注册的 fn，
// 允许框架或应用层触发告警、更新监控指标等副作用。
//
// 使用示例：
//
//	if ra, ok := adapter.(platform.RecoverableAdapter); ok {
//	    unregister := ra.OnDisconnect(func(err error) {
//	        metrics.RecordDisconnect(adapter.Platform())
//	        logger.Warnf("adapter %s disconnected: %v", adapter.Platform(), err)
//	    })
//	    defer unregister() // 不再需要时注销回调
//	}
type RecoverableAdapter interface {
	Adapter
	// OnDisconnect 注册断连回调，返回注销函数。
	//
	// fn 在适配器每次意外断连时被调用，err 为断连原因。
	// 多次调用将追加（而非覆盖）回调，互不影响。
	// 调用返回的 unregister 函数可注销该特定回调；传入 nil 时为空操作。
	OnDisconnect(fn func(err error)) (unregister func())
}

// ────────────────────────────────────────────────────────────────────────────
// BotIdentity（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// BotIdentity 是机器人自身身份信息的可选接口。
//
// 支持获取机器人自身 ID/名称的平台适配器应实现此接口，
// 便于 Handler 做"防止自回复"判断、日志标注等操作。
//
// 使用示例：
//
//	// 防止自回复
//	if botID := platform.GetBotID(adapter); botID != "" {
//	    if event.Sender().ID == botID {
//	        return // 忽略自身发出的消息
//	    }
//	}
//
//	// 直接类型断言（需要同时访问多个字段时更高效）
//	if bi, ok := adapter.(platform.BotIdentity); ok {
//	    log.Printf("bot %s (%s) online", bi.BotName(), bi.BotID())
//	}
type BotIdentity interface {
	// BotID 返回机器人在当前平台的唯一标识符。
	//
	// 与 event.Sender().ID 对比可判断事件是否由机器人自身触发。
	// 平台未提供或尚未连接时返回空字符串。
	BotID() string

	// BotName 返回机器人的显示名称（昵称/用户名）。
	//
	// 平台未提供时返回空字符串。
	BotName() string
}

// GetBotID 安全获取适配器的机器人唯一 ID。
//
// 若适配器未实现 [BotIdentity] 或平台尚未返回 ID，返回空字符串。
//
// 使用示例：
//
//	if platform.GetBotID(adapter) == event.Sender().ID {
//	    return // 忽略自身发出的消息
//	}
func GetBotID(a Adapter) string {
	if bi, ok := a.(BotIdentity); ok {
		return bi.BotID()
	}
	return ""
}

// GetBotName 安全获取适配器的机器人显示名称。
//
// 若适配器未实现 [BotIdentity]，返回空字符串。
func GetBotName(a Adapter) string {
	if bi, ok := a.(BotIdentity); ok {
		return bi.BotName()
	}
	return ""
}

// ────────────────────────────────────────────────────────────────────────────
// AdapterObserver
// ────────────────────────────────────────────────────────────────────────────

// AdapterObserver 接收 Registry 适配器生命周期事件，用于可观测性集成。
//
// 所有方法均在调用方 goroutine 中**同步**执行，实现应保证非阻塞（如仅递增计数器）。
// 如需进行耗时操作（日志写入、网络调用），请在实现内部使用异步队列。
//
// 使用示例（注册 metrics 观察者）：
//
//	reg := platform.NewRegistry().WithObserver(mc.PlatformObserver())
//	reg.StartAll(ctx, handler)
type AdapterObserver interface {
	// OnAdapterStarted 适配器 goroutine 启动时调用（Start 开始阻塞前）。
	OnAdapterStarted(platform string)
	// OnAdapterStopped 适配器 goroutine 退出时调用（无论是否有错误）。
	OnAdapterStopped(platform string)
	// OnAdapterError 适配器以非 context 取消/超时错误退出时调用。
	// errMsg 为 error.Error() 文本，避免直接传递 error 接口引起 alloc。
	OnAdapterError(platform, errMsg string)
	// OnAdapterDisconnect RecoverableAdapter 意外断连时调用。
	OnAdapterDisconnect(platform string, err error)
}
