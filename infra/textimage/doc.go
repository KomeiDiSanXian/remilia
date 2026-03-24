// Package textimage 将文本字符串（以及文本与图片的混合布局）转换为
// 适合通过聊天机器人平台发送的光栅图像。
//
// # 核心渲染器（纯文本）
//
// [Renderer] 将纯字符串转换为 [image.Image]。
//
//   - 支持自定义字体或内置字体（默认使用 Go Regular）
//   - 可配置字号、DPI、文字颜色、背景颜色
//   - 自动换行与多行渲染（识别 "\n"）；CJK 无空格文本自动字符级换行
//   - 左对齐 / 居中 / 右对齐
//   - 背景图片（[BgFitMode]：Stretch / Fill / Fit / Center / Tile）
//   - 文字遮罩（[WithTextBackdrop]）：毛玻璃模糊 + 半透明底色，
//     形状可选矩形 / 圆角矩形 / 椭圆（[BackdropShape]），
//     覆盖范围可选逐行或整块（[BackdropMode]）
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
// [Canvas] 将文本块、图片、间隔符和并排行垂直堆叠为单张合成图片。
// 文本块在 [Canvas.Result] 时才渲染，因此文字遮罩的模糊操作能够读取
// Canvas 级背景图片（[WithBgImage]）的真实像素，而非预渲染的占位颜色。
//
//   - [Canvas.AddText]       — 全宽自动换行文本块（支持逐行/整块遮罩）
//   - [Canvas.AddImage]      — 预解码图片，支持缩放、对齐及圆形/圆角裁剪
//   - [Canvas.AddImageBytes] — 一步解码并添加（PNG / JPEG / GIF …）
//   - [Canvas.AddSpacer]     — 垂直空白
//   - [Canvas.AddRow]        — 水平并排的多列单元格（如头像 + 文字）
//
// 示例 — 背景图 + 多个毛玻璃文字面板（机器人自检报告）：
//
//	bg := loadImage("wallpaper.png")
//	c, _ := textimage.NewCanvas(640,
//	    textimage.WithCJKFont(),
//	    textimage.WithBgImage(bg, textimage.BgFitFill),
//	    textimage.WithFontColor(color.White),
//	)
//	_ = c.AddImage(avatarImg, textimage.WithImgCircle(), textimage.WithImgWidth(80),
//	    textimage.WithImgAlign(textimage.AlignCenter))
//	_ = c.AddSpacer(12)
//	_ = c.AddText(systemReport,
//	    textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 140}, 14),
//	    textimage.WithTextBackdropPadding(16, 8),
//	    textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 10),
//	    textimage.WithTextBackdropMode(textimage.BackdropModeBlock),
//	)
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
