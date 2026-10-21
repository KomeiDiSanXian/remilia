package sauce

import (
	"github.com/KomeiDiSanXian/remilia/platform"
)

// Option 构造插件时的可选项。
type Option func(*Plugin)

// WithMediaOriginSource 注入平台媒体来源（只读视图）：图片下载的受信来源
// 据此并入适配器自声明的媒体源（见 [Plugin.mediaOrigins]）。未注入时仅使用
// 运维名单。
//
// 注入的是 [platform.MediaOriginSource] 而不是 *platform.Registry：图片下载
// 只需要知道"平台托管的媒体服务在哪"，不需要能注册/注销适配器的框架句柄。
func WithMediaOriginSource(src platform.MediaOriginSource) Option {
	return func(p *Plugin) { p.mediaSource = src }
}

// mediaOrigins 汇总图片下载允许的受信来源：
//
//  1. 运维配置 allow_private_media_hosts（"host" 或 "host:port"）；
//  2. 平台适配器自声明的媒体来源（如 OneBot 的 media_base_url，或与 API
//     端点同源的媒体服务）——自建实现的媒体常落在回环/内网，由真正知道
//     来源的一层声明，避免运维为每个实现手抄一遍主机名。
//
// 两部分都只来自运维配置/框架代码，不接受用户输入；命中即等于对该目标
// 开放内网直连，因此合并时不做任何通配放宽。
func (p *Plugin) mediaOrigins() []string {
	var origins []string
	if p.cfg != nil {
		origins = append(origins, p.cfg.GetStringSlice("allow_private_media_hosts", nil)...)
	}
	if p.mediaSource != nil {
		origins = append(origins, p.mediaSource.MediaOrigins()...)
	}
	return origins
}
