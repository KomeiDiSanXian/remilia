package platform

// capabilities.go — 平台能力声明
//
// 包含：
//   - [CapabilityFlag]  — 布尔能力的位掩码类型（可向前兼容地扩展）
//   - [Capabilities]    — 平台能力集合（保持原有字段，向后兼容）
//
// # 两种方式并存说明
//
// Capabilities 结构体保留原有的布尔字段，用于初始化赋值（字面量语法友好）。
// CapabilityFlag 位掩码用于"检查某能力是否存在"的运行时判断：
//
//	caps := adapter.Capabilities()
//
//	// 方式 A：字段访问（赋值时使用）
//	caps.Markdown = true
//
//	// 方式 B：Has() 检查（条件判断时使用，未来新能力无需改变 Capabilities 布局）
//	if caps.Has(CapMarkdown) {
//	    // 使用 Markdown 能力
//	}

// ────────────────────────────────────────────────────────────────────────────
// CapabilityFlag — 位掩码能力标志
// ────────────────────────────────────────────────────────────────────────────

// CapabilityFlag 是平台布尔能力的位掩码类型。
//
// 新增能力时只需追加新常量，无需修改 Capabilities 结构体布局。
// 建议在 Handler 中优先使用 [Capabilities.Has] 进行能力检查。
type CapabilityFlag uint64

const (
	CapMarkdown        CapabilityFlag = 1 << iota // 支持 Markdown 格式消息
	CapButtons                                    // 支持交互按钮（内联键盘等）
	CapMultiAttachment                            // 支持在一条消息中发送多个附件
	CapMessageEdit                                // 支持编辑已发送消息
	CapMessageDelete                              // 支持删除/撤回消息
	CapEmbeds                                     // 支持富文本嵌入卡片
	CapFileUpload                                 // 支持二进制文件直传（非 URL）
	CapGuildSupport                               // 有服务器/频道层级（ChatInfo.ParentID 有效）
	CapReactions                                  // 支持表情回应
	CapThreadReply                                // 支持消息回复链/引用回复
	CapTypingIndicator                            // 支持"正在输入"状态
	CapMentionAll                                 // 支持 @全体成员
	CapVoiceChannel                               // 支持语音频道
	CapCaption                                    // 支持在同一条消息内同时携带文本与附件（图文同发）
	CapForward                                    // 支持合并转发（发送与接收）
)

// ────────────────────────────────────────────────────────────────────────────
// Capabilities
// ────────────────────────────────────────────────────────────────────────────

// Capabilities 声明平台支持的特性集合。
//
// 平台适配器通过 Capabilities() 返回此结构，允许 Handler 在运行时
// 做跨平台特性检测，实现"渐进增强"策略（优先使用丰富特性，降级到纯文本）。
//
// 示例（字段访问方式）：
//
//	caps := ctx.GetPlatformCapabilities()
//	if caps.Embeds {
//	    msg = platform.TextMessage("").WithEmbeds(myEmbed)
//	} else {
//	    msg = platform.MarkdownMessage(myEmbed.Title + "\n" + myEmbed.Description)
//	}
//
// 示例（Has() 方式，推荐用于条件判断）：
//
//	if caps.Has(platform.CapEmbeds) {
//	    msg = platform.TextMessage("").WithEmbeds(myEmbed)
//	}
type Capabilities struct {
	// Markdown 是否支持 Markdown 格式消息
	Markdown bool
	// Buttons 是否支持交互按钮（内联键盘等）
	Buttons bool
	// MultiAttachment 是否支持在一条消息中发送多个附件
	MultiAttachment bool
	// MessageEdit 是否支持编辑已发送消息（实现 MessageEditor）
	MessageEdit bool
	// MessageDelete 是否支持删除/撤回消息（实现 MessageDeleter）
	MessageDelete bool
	// Embeds 是否支持富文本嵌入卡片
	Embeds bool
	// FileUpload 是否支持二进制文件直传（非 URL，Attachment.Data）
	FileUpload bool
	// GuildSupport 是否有服务器/频道层级（ChatInfo.ParentID 有效）
	GuildSupport bool
	// Reactions 是否支持表情回应（Discord/Telegram/QQ 均支持）
	Reactions bool
	// ThreadReply 是否支持消息回复链/引用回复
	ThreadReply bool
	// TypingIndicator 是否支持"正在输入"状态
	TypingIndicator bool
	// MentionAll 是否支持 @全体成员
	MentionAll bool
	// VoiceChannel 是否支持语音频道（Discord Stage/VC）
	VoiceChannel bool
	// Caption 是否支持在同一条消息内同时携带文本与附件（图文同发）。
	// Telegram（媒体 caption）、Discord（content+附件）、OneBot（CQ 码混排）、
	// Satori（元素列表）支持；QQ 富媒体消息会丢弃文本，不支持。
	Caption bool
	// Forward 是否支持合并转发（发送与接收）。
	// 例：OneBot/QQ 原生支持；Discord/Telegram 不支持（出站转发时降级）。
	Forward bool

	// ── 量化限制（0 = 无已知限制或平台未公开）────────────────────────────

	// MaxTextLength 单条文本消息最大字符数。
	// 例：Discord=2000，Telegram=4096，QQ=0（未公开）。
	MaxTextLength int
	// MaxAttachmentMB 单个附件最大大小（MB）。
	// 例：Discord=8，Telegram=50，QQ=0（未公开）。
	MaxAttachmentMB int
	// MaxButtonsPerRow 每行最多按钮数（0=无已知限制）。
	// 例：Discord/QQ=5。
	MaxButtonsPerRow int
	// MaxButtonRows 最多按钮行数（0=无已知限制）。
	// 例：Discord/QQ=5。
	MaxButtonRows int
	// MaxEmbedFields 单个 Embed 最多字段数（0=无已知限制）。
	// 例：Discord=25。
	MaxEmbedFields int
}

// Has 报告平台是否具备指定的能力标志。
//
// 推荐在条件判断中使用此方法代替直接访问布尔字段，
// 以便将来通过追加 [CapabilityFlag] 常量引入新能力而不修改 Capabilities 布局：
//
//	if caps.Has(platform.CapMarkdown | platform.CapEmbeds) {
//	    // 同时支持 Markdown 和 Embeds
//	}
func (c Capabilities) Has(flags CapabilityFlag) bool {
	return c.flags()&flags == flags
}

// flags 将布尔字段汇总为 CapabilityFlag 位掩码，供 Has() 内部使用。
func (c Capabilities) flags() CapabilityFlag {
	var f CapabilityFlag
	if c.Markdown {
		f |= CapMarkdown
	}
	if c.Buttons {
		f |= CapButtons
	}
	if c.MultiAttachment {
		f |= CapMultiAttachment
	}
	if c.MessageEdit {
		f |= CapMessageEdit
	}
	if c.MessageDelete {
		f |= CapMessageDelete
	}
	if c.Embeds {
		f |= CapEmbeds
	}
	if c.FileUpload {
		f |= CapFileUpload
	}
	if c.GuildSupport {
		f |= CapGuildSupport
	}
	if c.Reactions {
		f |= CapReactions
	}
	if c.ThreadReply {
		f |= CapThreadReply
	}
	if c.TypingIndicator {
		f |= CapTypingIndicator
	}
	if c.MentionAll {
		f |= CapMentionAll
	}
	if c.VoiceChannel {
		f |= CapVoiceChannel
	}
	if c.Caption {
		f |= CapCaption
	}
	if c.Forward {
		f |= CapForward
	}
	return f
}
