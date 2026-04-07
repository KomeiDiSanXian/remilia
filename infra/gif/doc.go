// Package gif 提供逐帧 GIF 动图编码器。
//
// 支持传入任意 [image.Image] 类型，内部自动完成颜色量化，
// 将每一帧转换为 GIF 规范要求的 256 色索引格式。
// 默认启用 Floyd-Steinberg 抖动算法，减少渐变色带；
// 可通过 [WithDithering] 关闭以换取更快的量化速度。
//
// # 快速开始
//
//	enc := gif.New(gif.WithLoopCount(0)) // 0 = 无限循环
//
//	for _, img := range frames {
//	    if err := enc.AddFrame(img, 50); err != nil { // 每帧 50 毫秒
//	        return err
//	    }
//	}
//
//	data, err := enc.Bytes()
//
// # 调色板
//
// 默认使用 216 色网页安全调色板（6×6×6 RGB 立方体 + 40 级灰阶，共 256 色）。
// 可通过 [WithPalette] 传入自定义调色板：
//
//	enc := gif.New(gif.WithPalette(myPalette))
//
// # 帧处置方式
//
// 每帧默认使用 [image/gif.DisposalBackground]（帧间清除为背景色）。
// 若需要逐帧精细控制处置方式，请直接调用 [Encoder.AddPaletted]。
package gif
