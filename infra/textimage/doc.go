// Package textimage 将文本字符串（以及文本与图片的混合布局）转换为
// 适合通过聊天机器人平台发送的光栅图像。
//
// # 核心渲染器（纯文本）
//
// [Renderer] 将纯字符串转换为 [image.Image]。
//
//   - 支持自定义字体或内置字体（默认使用 Go Regular）
//   - 可配置字号、DPI、文字颜色、背景颜色
//   - 自动换行与多行渲染（识别 "\n"）
//   - 左对齐 / 居中 / 右对齐
//   - PNG 和 JPEG 输出（字节、文件或 [io.Writer]）
//
// 基本用法：
//
//	r, _ := textimage.New(textimage.WithFontSize(24), textimage.WithCJKFont())
//	defer r.Close()
//	png, _ := r.RenderToPNG("Hello, 世界！")
//
// # Canvas（文本与图片混合合成器）
//
// [Canvas] 将文本块、图片、间隔符和并排行垂直堆叠为单张合成图片，
// 非常适合健康报告、排行榜或包含头像与文字的状态卡片等丰富 Bot 响应。
//
//   - [Canvas.AddText]       — 全宽自动换行文本块
//   - [Canvas.AddImage]      — 预解码图片，支持缩放、对齐及圆形/圆角裁剪
//   - [Canvas.AddImageBytes] — 一步解码并添加（PNG / JPEG / GIF …）
//   - [Canvas.AddSpacer]     — 垂直空白
//   - [Canvas.AddRow]        — 水平并排的多列单元格（如头像 + 文字）
//
// 示例 — 带圆形头像的 Bot 健康状态卡片：
//
//	c, _ := textimage.NewCanvas(640,
//	    textimage.WithCJKFont(),
//	    textimage.WithFontSize(16),
//	    textimage.WithBgColor(color.RGBA{R: 30, G: 30, B: 40, A: 255}),
//	    textimage.WithFontColor(color.White),
//	)
//	_ = c.AddRow(
//	    textimage.RowItem{
//	        Width: 72, Image: avatarImg,
//	        ImageOpts: []textimage.ImageOption{textimage.WithImgCircle()},
//	    },
//	    textimage.RowItem{Text: "MyBot  v1.2.3\n🟢 Online"},
//	)
//	_ = c.AddSpacer(8)
//	_ = c.AddText(systemReport)
//	pngBytes, _ := c.ResultPNG()
//
// # 系统字体辅助函数
//
// [SystemCJKFontPath] 返回当前操作系统上找到的最佳 CJK 字体路径。
// 平台相关的搜索逻辑位于同级的 sysfont_windows.go / sysfont_darwin.go /
// sysfont_unix.go 文件中，通过 Go 编译标签在编译期选择。
//
// [WithCJKFont] 是一个便捷 [Option]，会自动调用 [SystemCJKFontPath]。
package textimage
