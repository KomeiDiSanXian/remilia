// textimage-demo demonstrates the infra/textimage module by generating
// several example images that cover typical bot-message rendering scenarios.
package main

import (
	"fmt"
	"image"
	"image/color"
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
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
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
