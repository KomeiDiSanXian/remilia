package engine

// process_platform.go — 平台无关事件处理入口
//
// ProcessPlatformEvent 是 ProcessEvent 的平台无关入口：
//   - 接受 platform.Event + platform.Sender，无 dto.Payload / openapi.OpenAPI 依赖
//   - 内部通过 context.AcquireContextFromEvent 创建 *context.Context，交由 ProcessEvent 处理
//   - shutdown 保护、eventWg 计数、panic 恢复等生命周期职责统一由 ProcessEvent 承担
//
// 能力注入：
//   - 可传入多个 platform.Capabilities，所有布尔字段取 OR（能力并集）后注入 ctx
//   - 未传入时不注入，ctx.GetPlatformCapabilities() 返回零值

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
//   - caps   — （可选）一个或多个平台能力声明；多个时取 OR 并集注入 ctx
//
// 典型用法：
//
//	eng.ProcessPlatformEvent(event, adapter.Sender(), adapter.Capabilities())
//
// 生命周期（shutdown 保护 / eventWg / panic 恢复）委托给 ProcessEvent 统一管理。
func (e *Engine) ProcessPlatformEvent(event platform.Event, sender platform.Sender, caps ...platform.Capabilities) {
	if event == nil {
		logger.Warn("[engine] ProcessPlatformEvent: nil event, skipping")
		return
	}

	ctx := context.AcquireContextFromEvent(event, sender)
	defer context.ReleaseContextFromEvent(ctx)

	if len(caps) > 0 {
		ctx.SetPlatformCapabilities(mergePlatformCaps(caps))
	}

	// 委托给 ProcessEvent：shutdown 保护、eventWg、panic 恢复均在那里统一处理。
	e.ProcessEvent(ctx)
}

// ProcessPlatformEventBatch 批量处理来自任意平台的事件（平台无关入口）。
//
// nil 事件将被跳过；sender 和 caps 对整批事件共用（同一平台来源）。
func (e *Engine) ProcessPlatformEventBatch(events []platform.Event, sender platform.Sender, caps ...platform.Capabilities) {
	for _, event := range events {
		if event != nil {
			e.ProcessPlatformEvent(event, sender, caps...)
		}
	}
}

// mergePlatformCaps 将多个 Capabilities 的布尔字段取 OR，返回能力并集。
//
// 设计意图：多适配器或多来源的能力声明通过并集合并，
// 避免因"只取第一个"而遗漏某些来源宣告的能力。
func mergePlatformCaps(caps []platform.Capabilities) platform.Capabilities {
	var m platform.Capabilities
	for _, c := range caps {
		m.Markdown = m.Markdown || c.Markdown
		m.Buttons = m.Buttons || c.Buttons
		m.MultiAttachment = m.MultiAttachment || c.MultiAttachment
		m.MessageEdit = m.MessageEdit || c.MessageEdit
		m.MessageDelete = m.MessageDelete || c.MessageDelete
		m.Embeds = m.Embeds || c.Embeds
		m.FileUpload = m.FileUpload || c.FileUpload
		m.GuildSupport = m.GuildSupport || c.GuildSupport
		m.Reactions = m.Reactions || c.Reactions
		m.ThreadReply = m.ThreadReply || c.ThreadReply
		m.TypingIndicator = m.TypingIndicator || c.TypingIndicator
		m.MentionAll = m.MentionAll || c.MentionAll
		m.VoiceChannel = m.VoiceChannel || c.VoiceChannel
	}
	return m
}

// processEventContext 是 ProcessEvent 的核心路由逻辑。
//
// 调用方已完成 shutdown 检查和 eventWg.Add(1)，此处专注匹配与分发。
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

	// 获取 temp 匹配器（O4：HasAny() 快速跳过空 tempManager，避免 8 次 RLock）
	var tempSpecific, tempGeneric []*Matcher
	if e.services.tempManager.HasAny() {
		tempSpecific = e.services.tempManager.Get(eventType)
		tempGeneric = e.services.tempManager.Get("")
	}

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

	// O6：提前读取 state.block，Go 编译器不会跨不透明函数调用提升字段读取，
	// 显式缓存避免每轮循环通过指针重新加载。
	blockAll := state.block
	for _, m := range matchersToCheck {
		if m.Match(ctx) {
			ctx.SetMatcher(m)
			e.invokeHandler(ctx, m)
			if m.isBlocking() || blockAll {
				break
			}
		}
	}
}
