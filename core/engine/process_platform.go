package engine

// process_platform.go — 平台无关事件处理入口
//
// ProcessPlatformEvent 是 ProcessEvent 的新版平台无关入口：
//   - 接受 platform.Event + platform.Sender，无 dto.Payload / openapi.OpenAPI 依赖
//   - 内部通过 context.AcquireContextFromEvent 创建 *context.Context
//   - 其余路由/匹配/调用逻辑与 ProcessEvent 完全复用，零重复代码
//
// 迁移后 bot.handlePlatformEvent 直接调用本方法，
// 不再需要将 platform.Event 还原为 *dto.Payload。

import (
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ProcessPlatformEvent 处理来自任意平台的事件（平台无关入口）
//
// 参数：
//   - event  — 平台适配器包装的 platform.Event（QQ/Discord/Telegram 等）
//   - sender — 对应平台的消息发送器，注入 Context 供 Handler 调用 ctx.Reply()
//
// 与 ProcessEvent 的区别：
//   - 不依赖 *dto.Payload 或 openapi.OpenAPI
//   - Context 由 AcquireContextFromEvent 创建，GetEventType() 返回 platform.Event.RawType()
//   - Handler 可通过 ctx.Reply(platform.OutboundMessage) 发送回复
//   - Handler 同样可通过 ctx.GetEvent() 访问旧路径（返回 nil），或 ctx.GetPlatformEvent() 访问新路径
func (e *Engine) ProcessPlatformEvent(event platform.Event, sender platform.Sender) {
	if event == nil {
		logger.Warn("[engine] ProcessPlatformEvent: nil event, skipping")
		return
	}

	e.shutdownMu.RLock()
	if e.shutdown.Load() {
		e.shutdownMu.RUnlock()
		return
	}
	e.eventWg.Add(1)
	e.shutdownMu.RUnlock()
	defer e.eventWg.Done()

	defer func() {
		if r := recover(); r != nil {
			logger.WithFields(logger.Fields{
				"panic":    r,
				"platform": event.Platform(),
				"kind":     string(event.Kind()),
				"type":     event.RawType(),
			}).Error("[engine] Unhandled panic in ProcessPlatformEvent recovered")
		}
	}()

	// 从对象池获取 Context，由 platform.Event 初始化（无 dto.Payload）
	ctx := context.AcquireContextFromEvent(event, sender)
	defer context.ReleaseContextFromEvent(ctx)

	// 复用完全相同的路由 + 匹配 + 调用逻辑
	e.processEventContext(ctx)
}

// processEventContext 是 ProcessEvent / ProcessPlatformEvent 共享的核心逻辑。
//
// 抽取后两个公开方法都调用此函数，消除重复代码。
// 注意：调用方已持有 eventWg.Add(1)，此处不再重复 Add。
func (e *Engine) processEventContext(ctx *context.Context) {
	state := e.state.Load()

	eventType := ctx.GetEventType()

	// 获取已排序的 permanent 匹配器
	permSpecific := state.sortedCache[eventType]
	permGeneric := state.sortedCache[""]

	// 命令优化路径
	var cmdSpecific []*Matcher
	var cmdGeneric []*Matcher

	msgContent := ctx.GetMessageContent()
	if msgContent != "" {
		cmd := extractCommand(msgContent)
		if cmd != "" {
			if matchersMap, ok := state.commandIndex[cmd]; ok {
				cmdSpecific = matchersMap[eventType]
				cmdGeneric = matchersMap[""]
			}
		}
	}

	// 获取 temp 匹配器
	tempSpecific := e.services.tempManager.Get(eventType)
	tempGeneric := e.services.tempManager.Get("")

	// 从池中获取切片
	matchersToCheck := e.services.matcherPool.Get()
	matchersToCheck = matchersToCheck[:0]

	defer func() {
		for i := range matchersToCheck {
			matchersToCheck[i] = nil
		}
		if cap(matchersToCheck) > MaxMatcherPoolRetainCapacity {
			matchersToCheck = matchersToCheck[:0:MaxMatcherPoolRetainCapacity]
		}
		e.services.matcherPool.Put(matchersToCheck)
	}()

	matchersToCheck = mergeSortedMatchersSix(matchersToCheck,
		permSpecific, cmdSpecific, tempSpecific,
		permGeneric, cmdGeneric, tempGeneric)

	for _, m := range matchersToCheck {
		if m.Match(ctx) {
			setContextMatcher(ctx, m)
			e.invokeHandler(ctx, m)
			if m.isBlocking() || state.block {
				break
			}
		}
	}
}
