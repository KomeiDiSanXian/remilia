package engine

// process_platform.go — 平台无关事件处理入口
//
// ProcessPlatformEvent 是 ProcessEvent 的平台无关入口：
//   - 接受 platform.Event + platform.Sender，无 dto.Payload / openapi.OpenAPI 依赖
//   - 内部通过 context.NewContextFromEvent 创建 *context.Context，交由 ProcessEvent 处理
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
func (e *Engine) ProcessPlatformEvent(event platform.Event, sender platform.Sender, caps ...platform.Capabilities) {
	if event == nil {
		logger.Warn("[engine] ProcessPlatformEvent: nil event, skipping")
		return
	}

	ctx := context.NewContextFromEvent(event, sender)

	if len(caps) > 0 {
		ctx.SetPlatformCapabilities(mergePlatformCaps(caps))
	}

	e.processEventGuard(ctx)
}

// ProcessPlatformEventSync 同步处理平台事件，强制 handler 在当前 goroutine 执行。
// 与 ProcessPlatformEvent 的区别：不会 offload handler 到 ExecPool。
func (e *Engine) ProcessPlatformEventSync(event platform.Event, sender platform.Sender, caps ...platform.Capabilities) {
	if event == nil {
		logger.Warn("[engine] ProcessPlatformEventSync: nil event, skipping")
		return
	}

	ctx := context.NewContextFromEvent(event, sender)

	if len(caps) > 0 {
		ctx.SetPlatformCapabilities(mergePlatformCaps(caps))
	}

	e.ProcessEventSync(ctx)
}

// ProcessPlatformEventEx 是 ProcessPlatformEvent 的扩展版本，额外注入机器人自身 ID。
//
// botID 非空时会注入 ctx，使 ctx.IsFromSelf() 能正确判断事件是否由机器人自身触发。
func (e *Engine) ProcessPlatformEventEx(event platform.Event, sender platform.Sender, botID string, caps ...platform.Capabilities) {
	if event == nil {
		logger.Warn("[engine] ProcessPlatformEventEx: nil event, skipping")
		return
	}

	ctx := context.NewContextFromEvent(event, sender)

	if botID != "" {
		ctx.SetBotID(botID)
	}
	if len(caps) > 0 {
		ctx.SetPlatformCapabilities(mergePlatformCaps(caps))
	}

	e.processEventGuard(ctx)
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

// processEventContextWithPool 是事件核心路由逻辑。
// 慢 handler 自动 offload 到 ExecPool，池满时 fallback 同步。
// 同时注入 Dispatcher 到 Context，使 Handler 可通过 ctx.Reply() 异步发送。
func (e *Engine) processEventContextWithPool(ctx *context.Context) {
	ctx.SetDispatcher(e.dispatcher)
	e.processEventMatchers(ctx, true)
}

// ProcessEventSync 处理事件，强制所有 handler 在当前 goroutine 同步执行。
// 不会 offload 到 ExecPool，用于需要确保 handler 执行完毕后再继续的场景
// （如 AI 插件工具调用捕获命令回复）。
// 与 processEventContextWithPool 一样会注入 Dispatcher，使 handler 可调用 ctx.Reply()。
func (e *Engine) ProcessEventSync(ctx *context.Context) {
	ctx.SetDispatcher(e.dispatcher)
	e.processEventMatchers(ctx, false)
}

// processEventMatchers 是事件路由的核心逻辑，可控制是否允许 offload 到 ExecPool。
func (e *Engine) processEventMatchers(ctx *context.Context, allowPool bool) {
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

	execPool := e.internals.execPool
	blockAll := state.block
	channelKey := MakeChannelKey(ctx.GetEventPlatform(), ctx.GetChatInfo().ID)

	iter := acquireMergeIter(
		permSpecific, cmdSpecific, tempSpecific,
		permGeneric, cmdGeneric, tempGeneric,
	)
	defer releaseMergeIter(iter)

	for iter.Next() {
		m := iter.Matcher()

		// 无 Handler 的匹配器无需执行任何操作，直接跳过 Match/SetMatcher/invokeHandler
		// hasHandler 在 Handle() 中设置，由 InvalidateSortedCache 触发索引重建后，
		// 无 Handler 的匹配器将不再出现在运行时列表中。
		// 兜底：若 hasHandler atomic 未置位，检查 m.Handler 防止直接赋值场景。
		if !m.hasHandler.Load() {
			if m.Handler == nil {
				continue
			}
			m.hasHandler.Store(true)
		}

		if !m.Match(ctx) {
			continue
		}

		profile := m.execProfile

		if allowPool && profile != nil && profile.ShouldPool() == ExecClassPool {
			if execPool != nil && execPool.TrySubmit(func() {
				ctx.SetMatcher(m)
				start := time.Now()
				e.invokeHandler(ctx, m)
				if p := m.execProfile; p != nil {
					p.Record(time.Since(start))
				}
			}) {
				continue
			}
		}

		ctx.SetMatcher(m)
		start := time.Now()
		e.invokeHandler(ctx, m)
		if profile != nil {
			profile.Record(time.Since(start))
		}

		if m.isBlocking(channelKey) || blockAll {
			break
		}
	}
}
