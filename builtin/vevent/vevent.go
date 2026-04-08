// Package vevent 提供虚拟事件注入能力，允许插件或测试代码向引擎注入合成事件。
//
// # 快速上手
//
//	pm.Register(vevent.New(engine))
//
//	// 在其他插件 Setup 中：
//	vev := plugin.Must[*vevent.Plugin](ctx, "vevent")
//
//	// 注入一条群消息（触发已注册的 /ping handler）
//	vev.Inject(platform.EventKindGroupMessage, "/ping",
//	    vevent.WithChat(platform.ChatInfo{ID: "group-1", IsGroup: true}),
//	    vevent.WithSender(platform.UserInfo{ID: "user-42"}),
//	)
//
// # 独立使用（不通过插件系统）
//
//	inj := vevent.NewInjector(engine)
//	inj.Inject(platform.EventKindGroupMessage, "/hello",
//	    vevent.WithChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
//	)
package vevent

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// EventProcessor 抽象引擎的事件处理入口，允许注入合成事件而无需直接依赖 *engine.Engine。
// *engine.Engine 实现了此接口（ProcessPlatformEvent / ProcessPlatformEventEx 方法签名一致）。
type EventProcessor interface {
	ProcessPlatformEvent(event platform.Event, sender platform.Sender, caps ...platform.Capabilities)
}

// Plugin 虚拟事件注入器，通过 plugin.Must[*vevent.Plugin](ctx, "vevent") 获取。
type Plugin struct {
	ep EventProcessor
}

// NewPlugin 创建 Plugin 实例（不注册到插件系统，供直接使用或测试）。
func NewPlugin(ep EventProcessor) *Plugin {
	return &Plugin{ep: ep}
}

// Injector 是独立使用（不经过插件系统）的虚拟事件注入器。
// 功能与 Plugin 相同，但不需要注册到 PluginManager。
type Injector = Plugin

// NewInjector 创建独立的虚拟事件注入器（等同于 NewPlugin，语义更清晰）。
func NewInjector(ep EventProcessor) *Injector {
	return NewPlugin(ep)
}

// ── 注入 API ────────────────────────────────────────────────────────────────

// InjectOption 配置合成事件的可选选项，底层代理到 [platform.SyntheticOption]。
type InjectOption = platform.SyntheticOption

// 便捷别名，重新导出 platform 包中的 WithSynthetic* 选项，避免调用方多次 import。

// WithSender 设置发送者信息。
var WithSender = platform.WithSyntheticSender

// WithChat 设置会话信息（群/私聊）。
var WithChat = platform.WithSyntheticChat

// WithPlatform 覆盖 Platform() 返回的平台名称（默认 "synthetic"）。
var WithPlatform = platform.WithSyntheticPlatform

// WithID 覆盖事件 ID（默认自动生成 UUID）。
var WithID = platform.WithSyntheticID

// WithAttachments 设置附件列表。
var WithAttachments = platform.WithSyntheticAttachments

// Inject 向引擎注入一个合成事件（异步，不等待 handler 完成）。
//
//   - kind：事件类型，如 platform.EventKindGroupMessage
//   - content：消息文本内容（触发命令时需包含 "/" 前缀，如 "/ping"）
//   - opts：可选配置（发送者、会话、时间戳等）
//
// Inject 使用 [platform.NoopSender] 作为发送器（回复会被丢弃）。
// 若需要捕获回复，请使用 [Plugin.InjectWithSender]。
func (p *Plugin) Inject(kind platform.EventKind, content string, opts ...InjectOption) {
	evt := platform.NewSyntheticEvent(kind, content, opts...)
	p.ep.ProcessPlatformEvent(evt, &platform.NoopSender{})
}

// InjectWithSender 向引擎注入合成事件并使用自定义 Sender（可捕获 ctx.Reply 调用）。
func (p *Plugin) InjectWithSender(kind platform.EventKind, content string, sender platform.Sender, opts ...InjectOption) {
	evt := platform.NewSyntheticEvent(kind, content, opts...)
	p.ep.ProcessPlatformEvent(evt, sender)
}

// InjectEvent 直接注入已构造好的 platform.Event（最大灵活度）。
func (p *Plugin) InjectEvent(event platform.Event, sender platform.Sender, caps ...platform.Capabilities) {
	p.ep.ProcessPlatformEvent(event, sender, caps...)
}

// InjectSync 同步注入并等待所有 handler 处理完成（阻塞直到 ctx 取消或所有 handler 返回）。
//
// 注意：引擎的 ProcessPlatformEvent 本身是同步的（在当前 goroutine 中执行所有 handler），
// 因此 InjectSync 与 Inject 行为相同，此方法主要用于语义明确的测试代码。
func (p *Plugin) InjectSync(_ stdctx.Context, kind platform.EventKind, content string, opts ...InjectOption) {
	p.Inject(kind, content, opts...)
}

// ── 插件描述符 ─────────────────────────────────────────────────────────────

// New 创建虚拟事件注入插件的描述符，注册到 PluginManager 后可通过依赖注入获取。
//
//	pm.Register(vevent.New(engine))
//	// 其他插件中：
//	vev := plugin.Must[*vevent.Plugin](ctx, "vevent")
//	vev.Inject(platform.EventKindGroupMessage, "/cmd")
func New(ep EventProcessor) *plugin.Descriptor {
	p := NewPlugin(ep)
	return &plugin.Descriptor{
		Name:    "vevent",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "虚拟事件注入：向引擎注入合成 platform.Event，用于测试、调试和跨插件触发",
			Category:    "开发工具",
			Tags:        []string{"测试", "调试", "虚拟事件", "注入"},
			HelpText:    "vevent 插件提供虚拟（合成）事件注入能力，不依赖真实平台连接。",
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			return p, nil
		},
	}
}
