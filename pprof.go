package remilia

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/pprof"
)

// 性能分析服务器实现在 infra/pprof。
//
// 这里只做类型别名与构造函数转发，原因有两点：
//   - 保持 remilia.PprofServer / remilia.PprofConfig / remilia.NewPprofServer
//     等既有公开 API 可用（别名与转发不改变调用方写法）；
//   - 让 middleware 等上层组件直接引用 infra/pprof，而不必依赖顶层 Bot 包。
//
// 类型别名（type X = Y）而非新类型定义：调用方传入/取出的是同一个类型，
// 不会产生需要显式转换的兼容层。

// PprofConfig pprof 配置。
//
// Deprecated: 直接使用 infra/pprof.Config。
type PprofConfig = pprof.Config

// PprofServer pprof 服务器。
//
// Deprecated: 直接使用 infra/pprof.Server。
type PprofServer = pprof.Server

// DefaultPprofConfig 返回默认配置。
//
// Deprecated: 直接使用 infra/pprof.DefaultConfig。
func DefaultPprofConfig() PprofConfig {
	return pprof.DefaultConfig()
}

// NewPprofServer 创建 pprof 服务器。
//
// Deprecated: 直接使用 infra/pprof.NewServer。
func NewPprofServer(config PprofConfig) *PprofServer {
	return pprof.NewServer(config)
}

// CaptureTrace 捕获执行追踪。
//
// ctx 可用于提前中止追踪（例如收到停止信号时）。
// 传入 context.Background() 则等待完整的 duration。
//
// Deprecated: 直接使用 infra/pprof.CaptureTrace。
func CaptureTrace(ctx context.Context, duration time.Duration, filename string) error {
	return pprof.CaptureTrace(ctx, duration, filename)
}
