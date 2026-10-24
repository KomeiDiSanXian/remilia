package remilia

// 性能分析服务器实现在 infra/pprof。
//
// 这里只保留类型别名，原因有两点：
//   - 保持 remilia.PprofServer / remilia.PprofConfig 等既有公开 API 可用
//     （别名不改变调用方写法）；
//   - 让 middleware 等上层组件直接引用 infra/pprof，而不必依赖顶层 Bot 包。
//
// 类型别名（type X = Y）而非新类型定义：调用方传入/取出的是同一个类型，
// 不会产生需要显式转换的兼容层。

import "github.com/KomeiDiSanXian/remilia/infra/pprof"

// PprofConfig pprof 配置。
//
// Deprecated: 直接使用 infra/pprof.Config。
type PprofConfig = pprof.Config

// PprofServer pprof 服务器。
//
// Deprecated: 直接使用 infra/pprof.Server。
type PprofServer = pprof.Server
