// textimage-demo demonstrates the infra/textimage module by generating
// several example images that cover typical bot-message rendering scenarios.
package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

func main() {
	outDir := "output"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal("mkdir", err)
	}

	// Report which CJK font will be used.
	cjkPath := textimage.SystemCJKFontPath()
	if cjkPath == "" {
		fmt.Println("[WARN] no system CJK font found – Chinese text may render as boxes")
	} else {
		fmt.Printf("[INFO] CJK font: %s\n\n", cjkPath)
	}

	examples := []struct {
		name string
		fn   func(dir string) error
	}{
		{"01_hello_world", exHelloWorld},
		{"02_dark_banner", exDarkBanner},
		{"03_notification_card", exNotificationCard},
		{"04_word_wrap_article", exWordWrapArticle},
		{"05_right_aligned_status", exRightAlignedStatus},
		{"06_multiline_log", exMultilineLog},
		{"07_large_title", exLargeTitle},
		{"08_warning_card", exWarningCard},
		{"09_canvas_health_report", exCanvasHealthReport},
		{"10_canvas_avatar_row", exCanvasAvatarRow},
		{"11_bg_image_stretch", exBgImageStretch},
		{"12_bg_frosted_glass", exBgFrostedGlass},
		{"13_canvas_bg_health_report", exCanvasBgHealthReport},
		{"14_multi_panel_bg", exMultiPanelBg},
		{"15_new_features_showcase", exNewFeaturesShowcase},
		{"16_badge_row", exBadgeRow},
		{"17_bar_chart", exBarChart},
		{"18_badge_and_chart_card", exBadgeAndChartCard},
	}

	for _, e := range examples {
		if err := e.fn(outDir); err != nil {
			fmt.Fprintf(os.Stderr, "[FAIL] %s: %v\n", e.name, err)
			continue
		}
		fmt.Printf("[OK]   %s\n", e.name)
	}
	fmt.Println("\nAll images saved to:", outDir)
}

// ─── example 1: 简单 Hello World ─────────────────────────────────────────────

func exHelloWorld(dir string) error {
	r, err := textimage.New(
		textimage.WithCJKFont(),
		textimage.WithFontSize(32),
		textimage.WithPadding(28, 22),
		textimage.WithFontColor(color.RGBA{R: 30, G: 30, B: 30, A: 255}),
		textimage.WithBgColor(color.RGBA{R: 255, G: 255, B: 255, A: 255}),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "01_hello_world.png"), "Hello, World!  你好，世界！")
}

// ─── example 2: 深色主题 Banner ───────────────────────────────────────────────

func exDarkBanner(dir string) error {
	r, err := textimage.New(
		textimage.WithCJKFont(),
		textimage.WithSize(640, 90),
		textimage.WithFontSize(30),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithBgColor(color.RGBA{R: 18, G: 18, B: 24, A: 255}),
		textimage.WithFontColor(color.RGBA{R: 120, G: 200, B: 255, A: 255}),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "02_dark_banner.png"), "Remilia 框架  ·  v2.0  高性能聊天机器人引擎")
}

// ─── example 3: 通知卡片（多行 + 左对齐）────────────────────────────────────

func exNotificationCard(dir string) error {
	const msg = "📢  系统通知\n\n服务器将于今晚 22:00 进行例行维护，\n预计停机时间约 30 分钟。\n\n请提前保存您的工作，感谢配合！"

	r, err := textimage.New(
		textimage.WithCJKFont(),
		textimage.WithSize(500, 0),
		textimage.WithFontSize(18),
		textimage.WithPadding(28, 22),
		textimage.WithLineHeight(1.8),
		textimage.WithBgColor(color.RGBA{R: 255, G: 251, B: 230, A: 255}),
		textimage.WithFontColor(color.RGBA{R: 60, G: 40, B: 0, A: 255}),
		textimage.WithAlign(textimage.AlignLeft),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "03_notification_card.png"), msg)
}

// ─── example 4: 长文本自动折行（文章段落）───────────────────────────────────

func exWordWrapArticle(dir string) error {
	const article = "关于 Remilia 框架的设计理念\n\nRemilia 是一个面向聊天机器人场景的轻量级 Go 框架。它以模块化为核心，允许开发者按需组合中间件、插件与基础设施组件，从而构建高可维护性的机器人应用。\n\n本模块 infra/textimage 提供了将任意文本渲染为图片的能力，适用于：通知推送、排行榜展示、错误日志可视化等场景。"

	r, err := textimage.New(
		textimage.WithCJKFont(),
		textimage.WithSize(580, 0),
		textimage.WithMaxWidth(540),
		textimage.WithFontSize(16),
		textimage.WithPadding(20, 20),
		textimage.WithLineHeight(1.75),
		textimage.WithBgColor(color.RGBA{R: 248, G: 248, B: 252, A: 255}),
		textimage.WithFontColor(color.RGBA{R: 40, G: 40, B: 55, A: 255}),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "04_word_wrap_article.png"), article)
}

// ─── example 5: 右对齐状态行 ─────────────────────────────────────────────────

func exRightAlignedStatus(dir string) error {
	const status = "机器人状态\n运行时间:  72 小时 14 分\n消息总数:  1,024,388\n网络延迟:  12 ms\n当前版本:  v2.0.0"

	r, err := textimage.New(
		textimage.WithCJKFont(),
		textimage.WithSize(380, 0),
		textimage.WithFontSize(17),
		textimage.WithPadding(24, 18),
		textimage.WithLineHeight(1.9),
		textimage.WithAlign(textimage.AlignRight),
		textimage.WithBgColor(color.RGBA{R: 14, G: 22, B: 38, A: 255}),
		textimage.WithFontColor(color.RGBA{R: 180, G: 220, B: 180, A: 255}),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "05_right_aligned_status.png"), status)
}

// ─── example 6: 多行日志输出 ─────────────────────────────────────────────────

func exMultilineLog(dir string) error {
	const logs = "2026-03-23 17:00:01 [INFO]  Bot started\n2026-03-23 17:00:02 [INFO]  Connecting to gateway...\n2026-03-23 17:00:03 [INFO]  Connected  (shard 0/1)\n2026-03-23 17:00:10 [DEBUG] heartbeat sent\n2026-03-23 17:01:05 [INFO]  Message received from user#1234\n2026-03-23 17:01:05 [DEBUG] dispatching command: /help\n2026-03-23 17:01:06 [INFO]  Command handled in 1.2ms\n2026-03-23 17:02:33 [WARN]  Rate limit approaching (80%)\n2026-03-23 17:05:00 [ERROR] upstream timeout, retrying (1/3)"

	r, err := textimage.New(
		// Log content is ASCII only; no CJK font needed here.
		textimage.WithFontSize(13),
		textimage.WithPadding(16, 14),
		textimage.WithLineHeight(1.6),
		textimage.WithBgColor(color.RGBA{R: 12, G: 12, B: 12, A: 255}),
		textimage.WithFontColor(color.RGBA{R: 180, G: 180, B: 180, A: 255}),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "06_multiline_log.png"), logs)
}

// ─── example 7: 大号居中标题 ─────────────────────────────────────────────────

func exLargeTitle(dir string) error {
	r, err := textimage.New(
		textimage.WithCJKFont(),
		textimage.WithSize(640, 160),
		textimage.WithFontSize(56),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithBgColor(color.RGBA{R: 255, G: 255, B: 255, A: 255}),
		textimage.WithFontColor(color.RGBA{R: 60, G: 20, B: 120, A: 255}),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "07_large_title.png"), "Remilia  雷米莉亚")
}

// ─── example 8: 警告卡片（红色系）───────────────────────────────────────────

func exWarningCard(dir string) error {
	const warn = "⚠  安全警告\n\n检测到异常登录行为：\n  • IP:  203.0.113.42（未知地区）\n  • 时间:  2026-03-23 16:58:11\n  • 尝试次数:  7\n\n账号已被临时锁定，请立即修改密码。"

	r, err := textimage.New(
		textimage.WithCJKFont(),
		textimage.WithSize(520, 0),
		textimage.WithFontSize(16),
		textimage.WithPadding(26, 20),
		textimage.WithLineHeight(1.75),
		textimage.WithBgColor(color.RGBA{R: 60, G: 10, B: 10, A: 255}),
		textimage.WithFontColor(color.RGBA{R: 255, G: 200, B: 200, A: 255}),
		textimage.WithAlign(textimage.AlignLeft),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "08_warning_card.png"), warn)
}

// ─── example 9: Canvas — 机器人健康报告卡片 ──────────────────────────────────
//
// Demonstrates mixing a circular avatar, a title row, spacers, and a
// multi-section text report on a single dark Canvas.

func exCanvasHealthReport(dir string) error {
	const report = "系统报告  ·  2026-03-23  17:48\n" +
		"────────────────────────────────\n" +
		"运行时间      72 小时 14 分\n" +
		"内存占用      48 MB / 256 MB\n" +
		"CPU 使用率   3.2 %\n" +
		"处理消息数   1,024,388\n" +
		"活跃用户数   2,731\n" +
		"网络延迟      12 ms\n" +
		"────────────────────────────────\n" +
		"状态: 🟢 一切正常"

	// Synthesise a fake avatar: 96×96 blue circle placeholder.
	avatar := makeSolidRGBA(96, 96, color.RGBA{R: 80, G: 120, B: 200, A: 255})

	dark := color.RGBA{R: 22, G: 22, B: 32, A: 255}
	textClr := color.RGBA{R: 220, G: 220, B: 230, A: 255}

	c, err := textimage.NewCanvas(520,
		textimage.WithCJKFont(),
		textimage.WithFontSize(15),
		textimage.WithBgColor(dark),
		textimage.WithFontColor(textClr),
		textimage.WithPadding(18, 0),
		textimage.WithLineHeight(1.7),
	)
	if err != nil {
		return err
	}

	c.AddSpacer(16)

	// Header row: circular avatar on the left, bot name + version on the right.
	if err := c.AddRow(
		textimage.RowItem{
			Width: 96,
			Image: avatar,
			ImageOpts: []textimage.ImageOption{
				textimage.WithImgCircle(),
				textimage.WithImgAlign(textimage.AlignCenter),
			},
		},
		textimage.RowItem{
			Text: "Remilia Bot\nv2.0.0  ·  shard 0/1",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(20),
				textimage.WithLineHeight(1.5),
			},
		},
	); err != nil {
		return err
	}

	c.AddSpacer(12)

	if err := c.AddText(report); err != nil {
		return err
	}

	c.AddSpacer(16)

	return writeCanvasPNG(c, filepath.Join(dir, "09_canvas_health_report.png"))
}

// ─── example 10: Canvas — 头像 + 文字并排行 ───────────────────────────────────
//
// Demonstrates AddRow with rounded-corner image thumbnails and text beside them.

func exCanvasAvatarRow(dir string) error {
	type user struct {
		r, g, b uint8
		name    string
		status  string
	}
	users := []user{
		{80, 160, 240, "Alice", "🟢 在线  ·  等级 42"},
		{240, 120, 80, "Bob", "🟡 离开  ·  等级 37"},
		{100, 200, 120, "Carol", "🔴 忙碌  ·  等级 55"},
	}

	bg := color.RGBA{R: 28, G: 28, B: 36, A: 255}
	c, err := textimage.NewCanvas(480,
		textimage.WithCJKFont(),
		textimage.WithFontSize(15),
		textimage.WithBgColor(bg),
		textimage.WithFontColor(color.RGBA{R: 215, G: 215, B: 225, A: 255}),
		textimage.WithLineHeight(1.6),
	)
	if err != nil {
		return err
	}

	if err := c.AddText("用户列表",
		textimage.WithFontSize(20),
		textimage.WithAlign(textimage.AlignCenter),
	); err != nil {
		return err
	}
	c.AddSpacer(8)

	for _, u := range users {
		thumb := makeSolidRGBA(56, 56, color.RGBA{R: u.r, G: u.g, B: u.b, A: 255})
		if err := c.AddRow(
			textimage.RowItem{
				Width: 72,
				Image: thumb,
				ImageOpts: []textimage.ImageOption{
					textimage.WithImgRoundRadius(10),
					textimage.WithImgAlign(textimage.AlignCenter),
					textimage.WithImgPadding(8, 6),
				},
			},
			textimage.RowItem{
				Text: u.name + "\n" + u.status,
			},
		); err != nil {
			return err
		}
		c.AddSpacer(4)
	}

	return writeCanvasPNG(c, filepath.Join(dir, "10_canvas_avatar_row.png"))
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// makeSolidRGBA returns a w×h image filled with c.
func makeSolidRGBA(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

// writeCanvasPNG writes the canvas result to path as a PNG file.
func writeCanvasPNG(c *textimage.Canvas, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return c.RenderToWriter(f)
}

func fatal(msg string, err error) {
	fmt.Fprintf(os.Stderr, "fatal: %s: %v\n", msg, err)
	os.Exit(1)
}

// ─── example 11: 图片背景 + BgFitFill ────────────────────────────────────────
//
// 用合成的紫蓝渐变图作为背景，拉伸填充，白色文字直接叠加在图上。

func exBgImageStretch(dir string) error {
	bg := makeGradient(640, 200,
		color.RGBA{R: 60, G: 20, B: 120, A: 255},
		color.RGBA{R: 20, G: 80, B: 180, A: 255},
	)
	r, err := textimage.New(
		textimage.WithCJKFont(),
		textimage.WithSize(640, 200),
		textimage.WithFontSize(28),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.RGBA{R: 255, G: 255, B: 255, A: 255}),
		textimage.WithPadding(30, 40),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "11_bg_image_stretch.png"), "Remilia Bot\n图片背景渲染演示")
}

// ─── example 12: 背景图 + 毛玻璃遮罩（圆角矩形） ─────────────────────────────
//
// 渐变背景 + 每行文字背后带圆角毛玻璃底板（blur 12 + 半透明黑 + 圆角 10px）。

func exBgFrostedGlass(dir string) error {
	bg := makeGradient(560, 0,
		color.RGBA{R: 255, G: 120, B: 40, A: 255},
		color.RGBA{R: 20, G: 160, B: 200, A: 255},
	)
	const msg = "系统状态  ·  毛玻璃遮罩\n\nCPU  3.2 %\n内存  48 MB / 256 MB\n延迟  12 ms\n状态  🟢 正常"

	r, err := textimage.New(
		textimage.WithCJKFont(),
		textimage.WithSize(560, 0),
		textimage.WithFontSize(19),
		textimage.WithPadding(40, 28),
		textimage.WithLineHeight(1.9),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.RGBA{R: 255, G: 255, B: 255, A: 255}),
		// 毛玻璃：模糊半径 12，叠加半透明黑色，圆角矩形形状
		textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 110}, 12),
		textimage.WithTextBackdropPadding(14, 6),
		textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 10),
	)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RenderToFile(filepath.Join(dir, "12_bg_frosted_glass.png"), msg)
}

// ─── example 13: Canvas + 图片背景 + 机器人自检报告 ──────────────────────────
//
// 完整的机器人自检场景：渐变背景图 + 圆形头像 + 系统报告文字（毛玻璃遮罩）。

func exCanvasBgHealthReport(dir string) error {
	const report = "Remilia Bot  ·  系统自检报告\n" +
		"────────────────────────────────\n" +
		"运行时间      72 小时 14 分\n" +
		"内存占用      48 MB / 256 MB\n" +
		"CPU 使用率   3.2 %\n" +
		"处理消息数   1,024,388\n" +
		"网络延迟      12 ms\n" +
		"────────────────────────────────\n" +
		"状态: 🟢 一切正常"

	// 合成渐变背景（深蓝 → 深紫）。
	bg := makeGradient(580, 400,
		color.RGBA{R: 10, G: 20, B: 60, A: 255},
		color.RGBA{R: 50, G: 10, B: 80, A: 255},
	)
	// 合成头像：蓝色圆形占位图。
	avatar := makeCircleAvatar(96, color.RGBA{R: 80, G: 140, B: 230, A: 255})

	c, err := textimage.NewCanvas(580,
		textimage.WithCJKFont(),
		textimage.WithFontSize(15),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.RGBA{R: 230, G: 235, B: 255, A: 255}),
		textimage.WithPadding(24, 0),
		textimage.WithLineHeight(1.75),
		// 默认文字块不需要遮罩（会在 AddText 时按需覆盖）
	)
	if err != nil {
		return err
	}

	c.AddSpacer(20)

	// 头像居中行。
	if err := c.AddImage(avatar,
		textimage.WithImgCircle(),
		textimage.WithImgWidth(96),
		textimage.WithImgAlign(textimage.AlignCenter),
	); err != nil {
		return err
	}

	c.AddSpacer(14)

	// 报告文字 + 毛玻璃圆角矩形遮罩。
	if err := c.AddText(report,
		textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 20, A: 140}, 14),
		textimage.WithTextBackdropPadding(16, 8),
		textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 8),
	); err != nil {
		return err
	}

	c.AddSpacer(20)

	return writeCanvasPNG(c, filepath.Join(dir, "13_canvas_bg_health_report.png"))
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// makeGradient 返回 w×h 的水平线性渐变图（from → to）。
// h == 0 时高度设为 w / 3。
func makeGradient(w, h int, from, to color.RGBA) image.Image {
	if h <= 0 {
		h = w / 3
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		t := float64(x) / float64(w-1)
		r := uint8(math.Round(float64(from.R)*(1-t) + float64(to.R)*t))
		g := uint8(math.Round(float64(from.G)*(1-t) + float64(to.G)*t))
		b := uint8(math.Round(float64(from.B)*(1-t) + float64(to.B)*t))
		for y := 0; y < h; y++ {
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

// makeCircleAvatar 返回 size×size 的纯色填充图（用于模拟头像）。
func makeCircleAvatar(size int, c color.RGBA) image.Image {
	return makeSolidRGBA(size, size, c)
}

// ─── example 15: 新功能综合演示 ───────────────────────────────────────────────
//
// 演示五个新特性：
//   1. LinearGradient / RadialGradient — 内置渐变背景，无需手动循环
//   2. WithTextShadow              — 文字硬边/软化阴影
//   3. Canvas.AddDivider           — 水平分隔线
//   4. Canvas.AddProgressBar       — 进度条（CPU / 内存 / 延迟）
//   5. WithImgOpacity              — 图片透明度（水印效果）

func exNewFeaturesShowcase(dir string) error {
	// ① 用内置渐变生成背景（深蓝 → 深紫 → 深青，斜 135°）
	bg := textimage.LinearGradient(600, 520, 135,
		textimage.Stop(0.0, color.RGBA{R: 10, G: 20, B: 60, A: 255}),
		textimage.Stop(0.5, color.RGBA{R: 40, G: 10, B: 80, A: 255}),
		textimage.Stop(1.0, color.RGBA{R: 5, G: 50, B: 70, A: 255}),
	)

	// ② 水印：半透明头像占位图叠加在画布左下（使用 WithImgOpacity）
	watermark := makeSolidRGBA(80, 80, color.RGBA{R: 180, G: 180, B: 200, A: 255})

	textClr := color.RGBA{R: 230, G: 235, B: 255, A: 255}
	dimClr := color.RGBA{R: 150, G: 155, B: 180, A: 255}

	c, err := textimage.NewCanvas(600,
		textimage.WithCJKFont(),
		textimage.WithFontSize(15),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(textClr),
		textimage.WithPadding(28, 0),
		textimage.WithLineHeight(1.75),
	)
	if err != nil {
		return err
	}

	c.AddSpacer(20)

	// 标题行：圆形头像 + 机器人名称（含文字阴影）
	avatar := makeSolidRGBA(72, 72, color.RGBA{R: 80, G: 130, B: 220, A: 255})
	if err := c.AddRow(
		textimage.RowItem{
			Width: 80,
			Image: avatar,
			ImageOpts: []textimage.ImageOption{
				textimage.WithImgCircle(),
				textimage.WithImgAlign(textimage.AlignCenter),
			},
		},
		textimage.RowItem{
			Text: "Remilia Bot\nv2.1.0  ·  shard 0/1  ·  2026-03-24",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(19),
				textimage.WithLineHeight(1.5),
				// ③ 文字软化阴影
				textimage.WithTextShadow(color.RGBA{A: 180}, 1, 2, 4),
			},
		},
	); err != nil {
		return err
	}

	c.AddSpacer(14)

	// ④ 分隔线
	c.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 100, G: 110, B: 160, A: 180}),
		textimage.WithDividerInset(0),
		textimage.WithDividerPadding(2),
	)

	c.AddSpacer(10)

	// 系统指标文字（含文字阴影 + 毛玻璃遮罩）
	if err := c.AddText("系统指标",
		textimage.WithFontSize(13),
		textimage.WithFontColor(dimClr),
		textimage.WithTextShadow(color.RGBA{A: 150}, 1, 1, 2),
	); err != nil {
		return err
	}

	c.AddSpacer(4)

	// ⑤ 进度条：CPU
	if err := c.AddText("CPU  3.2 %",
		textimage.WithFontSize(14),
		textimage.WithTextShadow(color.RGBA{A: 160}, 1, 1, 2),
	); err != nil {
		return err
	}
	c.AddProgressBar(3.2, 100,
		textimage.WithProgressFillColor(color.RGBA{R: 80, G: 210, B: 120, A: 255}),
		textimage.WithProgressTrackColor(color.RGBA{R: 40, G: 45, B: 70, A: 220}),
		textimage.WithProgressHeight(10),
		textimage.WithProgressRadius(5),
		textimage.WithProgressPadding(0, 2),
	)

	// 进度条：内存
	if err := c.AddText("内存  48 / 256 MB  (18.8 %)",
		textimage.WithFontSize(14),
		textimage.WithTextShadow(color.RGBA{A: 160}, 1, 1, 2),
	); err != nil {
		return err
	}
	c.AddProgressBar(48, 256,
		textimage.WithProgressFillColor(color.RGBA{R: 100, G: 160, B: 240, A: 255}),
		textimage.WithProgressTrackColor(color.RGBA{R: 40, G: 45, B: 70, A: 220}),
		textimage.WithProgressHeight(10),
		textimage.WithProgressRadius(5),
		textimage.WithProgressPadding(0, 2),
	)

	// 进度条：处理消息速率（告警红）
	if err := c.AddText("QPS  87 / 100  (告警阈值 80 %)",
		textimage.WithFontSize(14),
		textimage.WithFontColor(color.RGBA{R: 255, G: 180, B: 140, A: 255}),
		textimage.WithTextShadow(color.RGBA{A: 160}, 1, 1, 2),
	); err != nil {
		return err
	}
	c.AddProgressBar(87, 100,
		textimage.WithProgressFillColor(color.RGBA{R: 230, G: 80, B: 60, A: 255}),
		textimage.WithProgressTrackColor(color.RGBA{R: 40, G: 45, B: 70, A: 220}),
		textimage.WithProgressHeight(10),
		textimage.WithProgressRadius(5),
		textimage.WithProgressPadding(0, 2),
	)

	c.AddSpacer(10)
	c.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 100, G: 110, B: 160, A: 120}),
		textimage.WithDividerInset(0),
		textimage.WithDividerPadding(2),
	)
	c.AddSpacer(8)

	// 状态行（毛玻璃卡片 + 文字阴影）
	if err := c.AddText("🟢  状态正常  ·  延迟 12 ms  ·  活跃用户 2,731",
		textimage.WithFontSize(15),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithTextBackdrop(color.NRGBA{R: 30, G: 120, B: 60, A: 120}, 8),
		textimage.WithTextBackdropPadding(20, 8),
		textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 20),
		textimage.WithTextBackdropMode(textimage.BackdropModeBlock),
		textimage.WithTextShadow(color.RGBA{A: 200}, 0, 2, 3),
	); err != nil {
		return err
	}

	c.AddSpacer(12)

	// 水印：半透明图片叠加在右侧（WithImgOpacity）
	if err := c.AddImage(watermark,
		textimage.WithImgWidth(40),
		textimage.WithImgCircle(),
		textimage.WithImgAlign(textimage.AlignRight),
		textimage.WithImgOpacity(0.25),
		textimage.WithImgPadding(28, 4),
	); err != nil {
		return err
	}

	c.AddSpacer(16)

	return writeCanvasPNG(c, filepath.Join(dir, "15_new_features_showcase.png"))
}

// ─── example 14: 背景图 + 多个独立毛玻璃文字面板 ─────────────────────────────
//
// 演示 BackdropModeBlock：在同一张背景图上，用 Canvas 垂直排列多个文字块，
// 每个块都是独立的圆角毛玻璃卡片（blur + 半透明底色 + 圆角矩形）。
// 这正是"背景图上画几块高斯模糊区域、在其中填充文字"的典型用法。

func exMultiPanelBg(dir string) error {
	// 斜向渐变背景（暖橙 → 深青）模拟真实场景背景图。
	bg := makeGradient(620, 520,
		color.RGBA{R: 220, G: 80, B: 20, A: 255},
		color.RGBA{R: 10, G: 80, B: 140, A: 255},
	)

	c, err := textimage.NewCanvas(620,
		textimage.WithCJKFont(),
		textimage.WithFontSize(16),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.RGBA{R: 255, G: 255, B: 255, A: 255}),
		textimage.WithPadding(32, 0),
		textimage.WithLineHeight(1.8),
	)
	if err != nil {
		return err
	}

	c.AddSpacer(24)

	// ── 面板 1：标题卡片（大字，亮色底板）────────────────────────────────────
	if err := c.AddText(
		"Remilia Bot  ·  系统自检",
		textimage.WithFontSize(26),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithTextBackdrop(color.NRGBA{R: 255, G: 255, B: 255, A: 50}, 18),
		textimage.WithTextBackdropPadding(20, 14),
		textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 16),
		textimage.WithTextBackdropMode(textimage.BackdropModeBlock),
	); err != nil {
		return err
	}

	c.AddSpacer(18)

	// ── 面板 2：系统指标（深色卡片，多行）────────────────────────────────────
	if err := c.AddText(
		"CPU 使用率    3.2 %\n内存占用      48 MB / 256 MB\n运行时间      72 小时 14 分\n处理消息数   1,024,388",
		textimage.WithFontSize(15),
		textimage.WithTextBackdrop(color.NRGBA{R: 0, G: 0, B: 0, A: 150}, 16),
		textimage.WithTextBackdropPadding(24, 12),
		textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 12),
		textimage.WithTextBackdropMode(textimage.BackdropModeBlock),
	); err != nil {
		return err
	}

	c.AddSpacer(18)

	// ── 面板 3：警告信息（红色半透明底板）────────────────────────────────────
	if err := c.AddText(
		"⚠  检测到速率限制告警\n当前 QPS 已达阈值的 87 %，请关注。",
		textimage.WithFontSize(15),
		textimage.WithFontColor(color.RGBA{R: 255, G: 220, B: 180, A: 255}),
		textimage.WithTextBackdrop(color.NRGBA{R: 160, G: 20, B: 0, A: 160}, 14),
		textimage.WithTextBackdropPadding(24, 12),
		textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 12),
		textimage.WithTextBackdropMode(textimage.BackdropModeBlock),
	); err != nil {
		return err
	}

	c.AddSpacer(18)

	// ── 面板 4：状态标签（单行，椭圆形遮罩）──────────────────────────────────
	if err := c.AddText(
		"🟢  状态正常  ·  延迟 12 ms",
		textimage.WithFontSize(16),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithTextBackdrop(color.NRGBA{R: 20, G: 120, B: 40, A: 170}, 10),
		textimage.WithTextBackdropPadding(32, 12),
		textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 24),
		textimage.WithTextBackdropMode(textimage.BackdropModeBlock),
	); err != nil {
		return err
	}

	c.AddSpacer(24)

	return writeCanvasPNG(c, filepath.Join(dir, "14_multi_panel_bg.png"))
}

// ─── example 16: 徽章行（Badge Row）──────────────────────────────────────────
//
// 演示 Canvas.AddBadgeRow / AddBadge：
//   - 多个彩色圆角标签横向排列
//   - 通过 WithBadgeFontSize / WithBadgeRadius / WithBadgePadding 调节样式
//   - 不同用途的配色方案：状态、版本、环境、告警

func exBadgeRow(dir string) error {
	bgDark := color.RGBA{R: 22, G: 22, B: 32, A: 255}
	dimText := color.RGBA{R: 160, G: 165, B: 190, A: 255}
	white := color.RGBA{R: 230, G: 235, B: 255, A: 255}

	c, err := textimage.NewCanvas(560,
		textimage.WithCJKFont(),
		textimage.WithFontSize(15),
		textimage.WithBgColor(bgDark),
		textimage.WithFontColor(white),
		textimage.WithPadding(20, 0),
		textimage.WithLineHeight(1.6),
	)
	if err != nil {
		return err
	}

	c.AddSpacer(18)

	// ── 标题 ─────────────────────────────────────────────────────────────────
	if err := c.AddText("徽章 / 标签  示例",
		textimage.WithFontSize(20),
		textimage.WithAlign(textimage.AlignCenter),
	); err != nil {
		return err
	}

	c.AddSpacer(10)
	c.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 80, G: 85, B: 120, A: 160}),
		textimage.WithDividerInset(0),
		textimage.WithDividerPadding(4),
	)

	// ── 场景一：服务运行状态 ──────────────────────────────────────────────────
	if err := c.AddText("服务状态",
		textimage.WithFontSize(13),
		textimage.WithFontColor(dimText),
	); err != nil {
		return err
	}
	if err := c.AddBadgeRow(
		[]textimage.BadgeItem{
			{Text: "🟢 API Server", BgColor: color.RGBA{R: 40, G: 160, B: 80, A: 255}},
			{Text: "🟢 Database", BgColor: color.RGBA{R: 40, G: 160, B: 80, A: 255}},
			{Text: "🟡 Cache", BgColor: color.RGBA{R: 200, G: 140, B: 30, A: 255}},
			{Text: "🔴 Queue", BgColor: color.RGBA{R: 190, G: 50, B: 50, A: 255}},
		},
		textimage.WithBadgeFontSize(13),
		textimage.WithBadgeRadius(12),
		textimage.WithBadgePadding(10, 5),
		textimage.WithBadgeGap(8),
		textimage.WithBadgeRowPadding(0, 6),
	); err != nil {
		return err
	}

	// ── 场景二：版本与环境 ────────────────────────────────────────────────────
	if err := c.AddText("版本 / 环境",
		textimage.WithFontSize(13),
		textimage.WithFontColor(dimText),
	); err != nil {
		return err
	}
	if err := c.AddBadgeRow(
		[]textimage.BadgeItem{
			{Text: "v2.1.0", BgColor: color.RGBA{R: 70, G: 90, B: 200, A: 255}},
			{Text: "Go 1.25", BgColor: color.RGBA{R: 0, G: 130, B: 160, A: 255}},
			{Text: "production", BgColor: color.RGBA{R: 160, G: 60, B: 200, A: 255}},
			{Text: "shard 0/1", BgColor: color.RGBA{R: 90, G: 90, B: 110, A: 255}},
		},
		textimage.WithBadgeFontSize(13),
		textimage.WithBadgeRadius(10),
		textimage.WithBadgePadding(10, 5),
		textimage.WithBadgeGap(8),
		textimage.WithBadgeRowPadding(0, 6),
	); err != nil {
		return err
	}

	// ── 场景三：功能标签（单行多样式）────────────────────────────────────────
	if err := c.AddText("功能标签",
		textimage.WithFontSize(13),
		textimage.WithFontColor(dimText),
	); err != nil {
		return err
	}
	if err := c.AddBadgeRow(
		[]textimage.BadgeItem{
			{
				Text:      "⭐ 核心",
				BgColor:   color.RGBA{R: 200, G: 150, B: 20, A: 255},
				TextColor: color.RGBA{R: 30, G: 20, B: 0, A: 255}, // 深色文字
			},
			{Text: "🔌 插件化", BgColor: color.RGBA{R: 60, G: 120, B: 200, A: 255}},
			{Text: "🛡 多平台", BgColor: color.RGBA{R: 50, G: 160, B: 130, A: 255}},
			{Text: "📊 可观测", BgColor: color.RGBA{R: 140, G: 60, B: 190, A: 255}},
			{Text: "🔄 热重载", BgColor: color.RGBA{R: 80, G: 80, B: 100, A: 255}},
		},
		textimage.WithBadgeFontSize(13),
		textimage.WithBadgeRadius(14),
		textimage.WithBadgePadding(12, 6),
		textimage.WithBadgeGap(6),
		textimage.WithBadgeRowPadding(0, 6),
	); err != nil {
		return err
	}

	// ── 场景四：单个大号徽章居中（AddBadge 便捷方法）─────────────────────────
	if err := c.AddText("单个徽章",
		textimage.WithFontSize(13),
		textimage.WithFontColor(dimText),
	); err != nil {
		return err
	}
	if err := c.AddBadge(
		textimage.BadgeItem{
			Text:      "🏆  今日最活跃用户：Alice",
			BgColor:   color.RGBA{R: 200, G: 140, B: 20, A: 255},
			TextColor: color.RGBA{R: 40, G: 20, B: 0, A: 255},
		},
		textimage.WithBadgeFontSize(15),
		textimage.WithBadgeRadius(16),
		textimage.WithBadgePadding(18, 8),
		textimage.WithBadgeRowPadding(0, 6),
	); err != nil {
		return err
	}

	c.AddSpacer(18)

	return writeCanvasPNG(c, filepath.Join(dir, "16_badge_row.png"))
}

// ─── example 17: 横向条形图（Bar Chart）──────────────────────────────────────
//
// 演示 Canvas.AddBarChart：
//   - 系统资源监控图表（CPU、内存、磁盘、网络）
//   - 不同语义配色（绿/蓝/黄/红）
//   - maxValue 自定义 vs 自动计算
//   - WithBarShowValue / WithBarFontColor / WithBarLabelWidth 选项

func exBarChart(dir string) error {
	bgDark := color.RGBA{R: 18, G: 20, B: 30, A: 255}
	white := color.RGBA{R: 225, G: 230, B: 255, A: 255}

	c, err := textimage.NewCanvas(560,
		textimage.WithCJKFont(),
		textimage.WithFontSize(15),
		textimage.WithBgColor(bgDark),
		textimage.WithFontColor(white),
		textimage.WithPadding(24, 0),
		textimage.WithLineHeight(1.6),
	)
	if err != nil {
		return err
	}

	c.AddSpacer(18)

	// 标题
	if err := c.AddText("系统资源监控  ·  2026-04-07  12:00",
		textimage.WithFontSize(18),
		textimage.WithAlign(textimage.AlignCenter),
	); err != nil {
		return err
	}
	c.AddSpacer(8)
	c.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 70, G: 80, B: 130, A: 160}),
		textimage.WithDividerInset(0),
		textimage.WithDividerPadding(4),
	)

	// ── 图表一：CPU / 内存 / 磁盘 / 网络（百分比）───────────────────────────
	if err := c.AddText("资源占用 (%)",
		textimage.WithFontSize(13),
		textimage.WithFontColor(color.RGBA{R: 150, G: 155, B: 185, A: 255}),
	); err != nil {
		return err
	}
	c.AddSpacer(4)
	if err := c.AddBarChart(
		[]textimage.BarItem{
			{Label: "CPU", Value: 32,
				Color: color.RGBA{R: 80, G: 210, B: 120, A: 255}},
			{Label: "内存", Value: 58,
				Color: color.RGBA{R: 80, G: 160, B: 240, A: 255}},
			{Label: "磁盘 I/O", Value: 71,
				Color: color.RGBA{R: 240, G: 180, B: 60, A: 255}},
			{Label: "网络带宽", Value: 89,
				Color: color.RGBA{R: 230, G: 80, B: 70, A: 255}},
		},
		100,
		textimage.WithBarHeight(18),
		textimage.WithBarLabelWidth(90),
		textimage.WithBarValueWidth(45),
		textimage.WithBarSpacing(6),
		textimage.WithBarFontSize(13),
		textimage.WithBarFontColor(color.RGBA{R: 210, G: 215, B: 240, A: 255}),
		textimage.WithBarPaddingX(0),
		textimage.WithBarTrackColor(color.RGBA{R: 40, G: 45, B: 65, A: 230}),
		textimage.WithBarShowValue(true),
	); err != nil {
		return err
	}

	c.AddSpacer(14)
	c.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 60, G: 65, B: 100, A: 120}),
		textimage.WithDividerInset(0),
		textimage.WithDividerPadding(2),
	)

	// ── 图表二：插件请求量（自动 maxValue，不显示数值）───────────────────────
	if err := c.AddText("各插件请求量（近 1 小时）",
		textimage.WithFontSize(13),
		textimage.WithFontColor(color.RGBA{R: 150, G: 155, B: 185, A: 255}),
	); err != nil {
		return err
	}
	c.AddSpacer(4)
	if err := c.AddBarChart(
		[]textimage.BarItem{
			{Label: "天气查询", Value: 1842},
			{Label: "签到", Value: 3571},
			{Label: "抽签", Value: 980},
			{Label: "翻译", Value: 2234},
			{Label: "AI 对话", Value: 4108},
		},
		0, // 自动取最大值
		textimage.WithBarHeight(16),
		textimage.WithBarLabelWidth(80),
		textimage.WithBarSpacing(5),
		textimage.WithBarFontSize(13),
		textimage.WithBarFontColor(color.RGBA{R: 200, G: 205, B: 230, A: 255}),
		textimage.WithBarPaddingX(0),
		textimage.WithBarDefaultColor(color.RGBA{R: 100, G: 140, B: 220, A: 255}),
		textimage.WithBarTrackColor(color.RGBA{R: 38, G: 42, B: 62, A: 220}),
		textimage.WithBarValueWidth(55),
		textimage.WithBarShowValue(true),
	); err != nil {
		return err
	}

	c.AddSpacer(18)

	return writeCanvasPNG(c, filepath.Join(dir, "17_bar_chart.png"))
}

// ─── example 18: 徽章 + 条形图综合卡片 ───────────────────────────────────────
//
// 将 Badge Row 与 Bar Chart 组合到一张完整的机器人状态卡片中，
// 并配合渐变背景图、圆形头像、分隔线等元素呈现真实场景效果。

func exBadgeAndChartCard(dir string) error {
	// 深紫蓝渐变背景
	bg := textimage.LinearGradient(620, 600, 135,
		textimage.Stop(0.0, color.RGBA{R: 8, G: 12, B: 45, A: 255}),
		textimage.Stop(0.5, color.RGBA{R: 30, G: 10, B: 60, A: 255}),
		textimage.Stop(1.0, color.RGBA{R: 5, G: 35, B: 55, A: 255}),
	)

	avatar := makeSolidRGBA(80, 80, color.RGBA{R: 90, G: 140, B: 230, A: 255})

	textClr := color.RGBA{R: 225, G: 230, B: 255, A: 255}
	dimClr := color.RGBA{R: 145, G: 150, B: 185, A: 255}

	c, err := textimage.NewCanvas(620,
		textimage.WithCJKFont(),
		textimage.WithFontSize(15),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(textClr),
		textimage.WithPadding(28, 0),
		textimage.WithLineHeight(1.7),
	)
	if err != nil {
		return err
	}

	c.AddSpacer(20)

	// ── 头部：头像 + 名称 + 版本 ─────────────────────────────────────────────
	if err := c.AddRow(
		textimage.RowItem{
			Width: 88,
			Image: avatar,
			ImageOpts: []textimage.ImageOption{
				textimage.WithImgCircle(),
				textimage.WithImgWidth(72),
				textimage.WithImgAlign(textimage.AlignCenter),
			},
		},
		textimage.RowItem{
			Text: "Remilia Bot\nv2.1.0  ·  shard 0/1  ·  2026-04-07",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(20),
				textimage.WithLineHeight(1.5),
				textimage.WithTextShadow(color.RGBA{A: 160}, 1, 2, 3),
			},
		},
	); err != nil {
		return err
	}

	c.AddSpacer(12)

	// ── 徽章行：运行状态 ─────────────────────────────────────────────────────
	if err := c.AddBadgeRow(
		[]textimage.BadgeItem{
			{Text: "🟢 在线", BgColor: color.RGBA{R: 35, G: 155, B: 75, A: 255}},
			{Text: "⚡ v2.1.0", BgColor: color.RGBA{R: 60, G: 90, B: 200, A: 255}},
			{Text: "production", BgColor: color.RGBA{R: 130, G: 50, B: 185, A: 255}},
			{Text: "Go 1.25", BgColor: color.RGBA{R: 0, G: 120, B: 155, A: 255}},
		},
		textimage.WithBadgeFontSize(12),
		textimage.WithBadgeRadius(10),
		textimage.WithBadgePadding(10, 5),
		textimage.WithBadgeGap(7),
		textimage.WithBadgeRowPadding(0, 4),
	); err != nil {
		return err
	}

	c.AddSpacer(10)
	c.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 90, G: 100, B: 160, A: 140}),
		textimage.WithDividerInset(0),
		textimage.WithDividerPadding(3),
	)
	c.AddSpacer(8)

	// ── 条形图：资源占用 ──────────────────────────────────────────────────────
	if err := c.AddText("资源占用",
		textimage.WithFontSize(13),
		textimage.WithFontColor(dimClr),
	); err != nil {
		return err
	}
	c.AddSpacer(4)
	if err := c.AddBarChart(
		[]textimage.BarItem{
			{Label: "CPU", Value: 32,
				Color: color.RGBA{R: 75, G: 210, B: 115, A: 255}},
			{Label: "内存", Value: 58,
				Color: color.RGBA{R: 80, G: 160, B: 240, A: 255}},
			{Label: "QPS", Value: 87,
				Color: color.RGBA{R: 230, G: 90, B: 70, A: 255}},
		},
		100,
		textimage.WithBarHeight(16),
		textimage.WithBarLabelWidth(70),
		textimage.WithBarValueWidth(42),
		textimage.WithBarSpacing(5),
		textimage.WithBarFontSize(13),
		textimage.WithBarFontColor(color.RGBA{R: 205, G: 210, B: 240, A: 255}),
		textimage.WithBarPaddingX(0),
		textimage.WithBarTrackColor(color.RGBA{R: 35, G: 40, B: 65, A: 220}),
		textimage.WithBarShowValue(true),
	); err != nil {
		return err
	}

	c.AddSpacer(10)
	c.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 70, G: 80, B: 130, A: 100}),
		textimage.WithDividerInset(0),
		textimage.WithDividerPadding(3),
	)
	c.AddSpacer(8)

	// ── 条形图：近 1 小时各功能调用量 ────────────────────────────────────────
	if err := c.AddText("功能调用量（近 1 小时）",
		textimage.WithFontSize(13),
		textimage.WithFontColor(dimClr),
	); err != nil {
		return err
	}
	c.AddSpacer(4)
	if err := c.AddBarChart(
		[]textimage.BarItem{
			{Label: "AI 对话", Value: 4108},
			{Label: "签到", Value: 3571},
			{Label: "天气", Value: 1842},
			{Label: "翻译", Value: 2234},
		},
		0,
		textimage.WithBarHeight(15),
		textimage.WithBarLabelWidth(75),
		textimage.WithBarValueWidth(50),
		textimage.WithBarSpacing(5),
		textimage.WithBarFontSize(13),
		textimage.WithBarFontColor(color.RGBA{R: 195, G: 200, B: 230, A: 255}),
		textimage.WithBarPaddingX(0),
		textimage.WithBarDefaultColor(color.RGBA{R: 95, G: 135, B: 215, A: 255}),
		textimage.WithBarTrackColor(color.RGBA{R: 32, G: 37, B: 58, A: 215}),
		textimage.WithBarShowValue(true),
	); err != nil {
		return err
	}

	c.AddSpacer(12)
	c.AddDivider(
		textimage.WithDividerColor(color.RGBA{R: 60, G: 70, B: 115, A: 100}),
		textimage.WithDividerInset(0),
		textimage.WithDividerPadding(2),
	)
	c.AddSpacer(8)

	// ── 底部徽章：服务器节点状态 ──────────────────────────────────────────────
	if err := c.AddText("节点状态",
		textimage.WithFontSize(13),
		textimage.WithFontColor(dimClr),
	); err != nil {
		return err
	}
	if err := c.AddBadgeRow(
		[]textimage.BadgeItem{
			{Text: "node-01 🟢", BgColor: color.RGBA{R: 35, G: 140, B: 70, A: 255}},
			{Text: "node-02 🟢", BgColor: color.RGBA{R: 35, G: 140, B: 70, A: 255}},
			{Text: "node-03 🟡", BgColor: color.RGBA{R: 185, G: 130, B: 25, A: 255}},
			{Text: "node-04 🔴", BgColor: color.RGBA{R: 175, G: 45, B: 45, A: 255}},
		},
		textimage.WithBadgeFontSize(12),
		textimage.WithBadgeRadius(8),
		textimage.WithBadgePadding(9, 4),
		textimage.WithBadgeGap(6),
		textimage.WithBadgeRowPadding(0, 5),
	); err != nil {
		return err
	}

	c.AddSpacer(20)

	return writeCanvasPNG(c, filepath.Join(dir, "18_badge_and_chart_card.png"))
}
