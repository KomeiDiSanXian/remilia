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
	"time"

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
// Context 生命周期：AcquireContextFromEvent 设置 refCount=1，
// processEventContext 结束时 Release 使归零（同步路径立即归还，异步 offload 路径由池 goroutine 归还）。
func (e *Engine) ProcessPlatformEvent(event platform.Event, sender platform.Sender, caps ...platform.Capabilities) {
	if event == nil {
		logger.Warn("[engine] ProcessPlatformEvent: nil event, skipping")
		return
	}

	ctx := context.AcquireContextFromEvent(event, sender)

	if len(caps) > 0 {
		ctx.SetPlatformCapabilities(mergePlatformCaps(caps))
	}

	// 使用 processEventGuard(allowPool=true) 处理事件：
	// shutdown 保护、eventWg、panic 恢复、以及自适应 ExecPool offload。
	// ctx.Release() 在 processEventContextWithPool 内部管理。
	e.processEventGuard(ctx, true)
}

// ProcessPlatformEventEx 是 ProcessPlatformEvent 的扩展版本，额外注入机器人自身 ID。
//
// botID 非空时会注入 ctx，使 ctx.IsFromSelf() 能正确判断事件是否由机器人自身触发。
//
// 典型用法（Bot 层，适配器实现了 platform.BotIdentity）：
//
//	botID := ""
//	if bi, ok := adapter.(platform.BotIdentity); ok {
//	    botID = bi.BotID()
//	}
//	eng.ProcessPlatformEventEx(event, adapter.Sender(), botID, adapter.Capabilities())
func (e *Engine) ProcessPlatformEventEx(event platform.Event, sender platform.Sender, botID string, caps ...platform.Capabilities) {
	if event == nil {
		logger.Warn("[engine] ProcessPlatformEventEx: nil event, skipping")
		return
	}

	ctx := context.AcquireContextFromEvent(event, sender)

	if botID != "" {
		ctx.SetBotID(botID)
	}
	if len(caps) > 0 {
		ctx.SetPlatformCapabilities(mergePlatformCaps(caps))
	}

	e.processEventGuard(ctx, true)
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

// mergePlatformCaps 将多个 Capabilities 合并为一个。
//
// 布尔字段取 OR（能力并集），量化限制字段取最小非零值（更严格的约束优先）：
//   - 若两方都声明了限制，取更小值（更保守）
//   - 若一方为 0（未知），取另一方的值
func mergePlatformCaps(caps []platform.Capabilities) platform.Capabilities {
	var m platform.Capabilities
	for _, c := range caps {
		// 布尔能力取 OR（并集）
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
		// 量化限制取最小非零值（0=未知，有值时取更保守的一方）
		m.MaxTextLength = minNonZero(m.MaxTextLength, c.MaxTextLength)
		m.MaxAttachmentMB = minNonZero(m.MaxAttachmentMB, c.MaxAttachmentMB)
		m.MaxButtonsPerRow = minNonZero(m.MaxButtonsPerRow, c.MaxButtonsPerRow)
		m.MaxButtonRows = minNonZero(m.MaxButtonRows, c.MaxButtonRows)
		m.MaxEmbedFields = minNonZero(m.MaxEmbedFields, c.MaxEmbedFields)
	}
	return m
}

// minNonZero 返回 a、b 中较小的非零值。
// 若两者都为 0（均未知），返回 0。
// 若只有一个非零，返回那个值（有约束者优先）。
func minNonZero(a, b int) int {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

// processEventContext 是 ProcessEvent 的核心路由逻辑（同步版本）。
//
// 由 ProcessEvent 调用，所有 handler 均同步执行。
// Context 生命周期：末尾 ctx.Release() 使 refCount 归零。
func (e *Engine) processEventContext(ctx *context.Context) {
	e.processEventContextWithPool(ctx, false)
}

// processEventContextWithPool 是 processEventContext 的增强版本，
// 支持将慢 handler 自动 offload 到 ExecPool。
//
// allowPool=true 时（由 ProcessPlatformEvent 使用）：
//   - 默认走同步（向后兼容）
//   - 一旦检测到 handler 慢（p50 > threshold），后续调用自动走 ExecPool
//   - 池满时 fallback 同步执行
//
// allowPool=false 时（由 ProcessEvent 使用）：全部同步，保证 ProcessEvent 的同步语义。
func (e *Engine) processEventContextWithPool(ctx *context.Context, allowPool bool) {
	state := e.state.Load()

	eventType := ctx.GetEventType()

	permSpecific := state.sortedCache[eventType]
	permGeneric := state.sortedCache[""]

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

	var tempSpecific, tempGeneric []*Matcher
	if e.internals.tempManager.HasAny() {
		tempSpecific = e.internals.tempManager.Get(eventType)
		tempGeneric = e.internals.tempManager.Get("")
	}

	matchersToCheck := e.internals.matcherPool.Get()
	matchersToCheck = matchersToCheck[:0]

	defer func() {
		for i := range matchersToCheck {
			matchersToCheck[i] = nil
		}
		if cap(matchersToCheck) > MaxMatcherPoolRetainCapacity {
			matchersToCheck = matchersToCheck[:0:MaxMatcherPoolRetainCapacity]
		}
		e.internals.matcherPool.Put(matchersToCheck)
	}()

	matchersToCheck = mergeSortedMatchersSix(matchersToCheck,
		permSpecific, cmdSpecific, tempSpecific,
		permGeneric, cmdGeneric, tempGeneric)

	execPool := e.internals.execPool
	blockAll := state.block
	offloaded := false
	for _, m := range matchersToCheck {
		if !m.Match(ctx) {
			continue
		}
		ctx.SetMatcher(m)

		usePool := allowPool

		// 仅在 allowPool 时检查 ExecProfile
		if usePool {
			profile := m.execProfile
			if profile == nil {
				profile = &ExecProfile{}
				m.execProfile = profile
			}
			usePool = profile.ShouldPool() == ExecClassPool
		}

		if usePool {
			// 慢路径：offload 到 ExecPool
			// refCount 此时 = 1（AcquireContextFromEvent 的基引用）。
			// Retain 后 = 2，池 goroutine 中 Release 两次：
			//   第 1 次：归还 Retain（2→1）
			//   第 2 次：归还基引用（1→0 → 回 pool）
			ctx.Retain()
			if execPool != nil && execPool.TrySubmit(func() {
				start := time.Now()
				e.invokeHandler(ctx, m)
				profile := m.execProfile
				if profile != nil {
					profile.Record(time.Since(start))
				}
				ctx.Release() // 归还 Retain
				ctx.Release() // 归还基引用
			}) {
				offloaded = true
			} else {
				ctx.Release() // 撤销 Retain（基引用仍在）
				usePool = false
			}
		}

		if !usePool {
			// 快路径：同步执行，零额外开销
			start := time.Now()
			e.invokeHandler(ctx, m)
			if allowPool {
				if profile := m.execProfile; profile != nil {
					profile.Record(time.Since(start))
				}
			}
		}

		if m.isBlocking() || blockAll {
			break
		}
	}

	if offloaded {
		return
	}
	ctx.Release()
}
