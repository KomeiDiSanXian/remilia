package onebot

import (
	"net/url"
	"strings"
)

// media.go — 向框架声明"本适配器托管的媒体来源"。
//
// OneBot 实现（NapCat / LLOneBot / Lagrange 等）通常把收到的图片、语音、
// 文件挂在自己的 HTTP 文件服务上，附件 URL 因此指向回环或内网地址。插件
// 下载这类附件时，若套用"仅 https + 公网"的通用策略就会被误伤。来源只有
// 适配器配置知道，所以由这一层声明（见 platform.MediaOriginProvider）。

// MediaOrigins 实现 platform.MediaOriginProvider。
func (a *ForwardWSAdapter) MediaOrigins() []string { return mediaOrigins(a.config) }

// MediaOrigins 实现 platform.MediaOriginProvider。
func (a *HTTPPostAdapter) MediaOrigins() []string { return mediaOrigins(a.config) }

// MediaOrigins 实现 platform.MediaOriginProvider。
//
// 反向 WS 模式下适配器只知道本地监听地址（媒体在实现那一侧），因此只有
// 显式配置 media_base_url 时才返回来源。
func (a *ReverseWSAdapter) MediaOrigins() []string { return mediaOrigins(a.config) }

// mediaOrigins 按优先级返回受信媒体来源：
//
//  1. 运维显式配置的 media_base_url；
//  2. 未配置时退回 API 端点同源地址（实现与媒体服务同端口时开箱可用）。
func mediaOrigins(cfg Config) []string {
	if origin, ok := mediaOrigin(cfg.MediaBaseURL); ok {
		return []string{origin}
	}
	if origin, ok := mediaOrigin(cfg.URL); ok {
		return []string{origin}
	}
	return nil
}

// mediaOrigin 把配置地址归一化为 "scheme://host[:port]"。
//
// ws/wss 视作 http/https：媒体直链走的是 HTTP，但同源判断按 scheme + 主机 +
// 端口进行，API 的 WS 端口与媒体端口相同时才应命中。
func mediaOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	case "http", "https":
	default:
		return "", false
	}
	return scheme + "://" + strings.ToLower(u.Host), true
}
