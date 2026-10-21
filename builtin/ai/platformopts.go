package ai

import "github.com/KomeiDiSanXian/remilia/platform"

// platformopts.go — 插件构造期的依赖注入点。
//
// 目前只有平台媒体来源一项：附件下载需要知道"平台自己托管的媒体服务在哪"，
// 而这只有适配器配置清楚（见 platform.MediaOriginProvider）。未注入时退化为
// "仅运维名单 + 严格公网"，功能不受影响，只是自建实现的内网媒体需要运维在
// allow_private_media_hosts 里显式声明。
//
// 注入的是 [platform.MediaOriginSource]（只有 MediaOrigins 一个只读方法），
// 不是 *platform.Registry：插件不需要、也不应该拿到能注册/注销适配器、
// 改事件分发的框架级句柄。
//
// 注意：本文件的 Option 是插件构造选项，与 options.go 里"配置 → 子包选项"
// 的映射函数无关。

// Option 构造插件时可注入的依赖。
type Option func(*Plugin)

// WithMediaOriginSource 注入平台媒体来源：Setup 时据此读取各适配器自声明的
// 媒体来源，与 allow_private_media_hosts 合并为附件下载的受信来源。
func WithMediaOriginSource(src platform.MediaOriginSource) Option {
	return func(p *Plugin) { p.mediaSource = src }
}
