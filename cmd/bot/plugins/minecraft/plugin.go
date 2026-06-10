// Package minecraft 提供 Minecraft 服务器状态查询功能。
//
// 命令: /mc <主机名>[:端口]
// AI 工具: query_minecraft_server
// AI 技能: minecraft_query
package minecraft

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/textimage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const cardWidthMC = 600

var (
	mcTextPrimary = color.RGBA{R: 220, G: 220, B: 225, A: 255}
	mcTextDim     = color.RGBA{R: 160, G: 160, B: 170, A: 255}
	mcAccentGreen = color.RGBA{R: 80, G: 210, B: 100, A: 255}
	mcAccentRed   = color.RGBA{R: 220, G: 80, B: 70, A: 255}
	mcBgDark      = color.RGBA{R: 30, G: 28, B: 35, A: 255}
	mcBgCard      = color.RGBA{R: 52, G: 50, B: 58, A: 255}
	mcDivider     = color.RGBA{R: 60, G: 58, B: 66, A: 255}
)

type mcPlugin struct {
	log plugin.Logger
}

// New 创建 Minecraft 服务器状态查询插件的 Descriptor。
//
// 命令:
//   - /mc <主机名>[:端口]
//   - /mc java <主机名>[:端口]
//   - /mc bedrock <主机名>[:端口]
//
// AI:
//   - query_minecraft_server(server_address) → 服务器状态文本
//   - minecraft_query — 服务器状态技能
func New() *plugin.Descriptor {
	p := &mcPlugin{}
	return &plugin.Descriptor{
		Name:    "minecraft",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "Minecraft 服务器状态查询（Java + Bedrock）",
			Category:    "工具",
			Tags:        []string{"Minecraft", "MC", "游戏", "服务器"},
			HelpText: `Minecraft 服务器状态查询插件

用法：
  /mc <主机名>[:端口]          — 自动探测 Java/Bedrock
  /mc java <主机名>[:端口]     — 强制 Java 版查询
  /mc bedrock <主机名>[:端口]  — 强制 Bedrock 版查询

不加端口时 Java 默认 25565，Bedrock 默认 19132`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log

			ctx.OnCommand("", "/mc", p.handleMC)

			if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
				aiSvc.RegisterToolProvider(p)
				aiSvc.RegisterSkillProvider(p)
			}

			return p, nil
		},
	}
}

func (p *mcPlugin) handleMC(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		return ctx.ReplyError("用法: /mc <主机名>[:端口] [/mc java /mc bedrock]")
	}

	args := parsed.Positional
	edition := ""
	var host string
	port := 0

	switch args[0] {
	case "java", "bedrock":
		edition = args[0]
		if len(args) < 2 {
			return ctx.ReplyError("用法: /mc java <主机名>[:端口]")
		}
		host = args[1]
	default:
		host = args[0]
	}

	if strings.Contains(host, ":") {
		parts := strings.SplitN(host, ":", 2)
		host = parts[0]
		fmt.Sscanf(parts[1], "%d", &port)
	}

	timeout := 10 * time.Second

	var status *MCServerStatus
	switch edition {
	case "java":
		status, err = PingJava(host, port, timeout)
	case "bedrock":
		status, err = PingBedrock(host, port, timeout)
	default:
		status, err = Ping(host, port, timeout)
	}

	if err != nil {
		return ctx.ReplyText(fmt.Sprintf("⛏ 服务器 %s 无法连接: %v", host, err))
	}

	png, imgErr := renderMCCard(status)
	if imgErr != nil {
		return ctx.ReplyText(formatMCText(status))
	}

	if _, err := ctx.Reply(platform.ImageDataMessage(png, "mc_status.png", "image/png")); err != nil {
		return err
	}
	return nil
}

func formatMCText(status *MCServerStatus) string {
	editionLabel := "Java 版"
	if status.Edition == "bedrock" {
		editionLabel = "Bedrock 版"
	}
	statusLabel := "在线"
	if !status.Online {
		statusLabel = "离线"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("⛏ %s:%d (%s)\n", status.Host, status.Port, editionLabel))
	b.WriteString(fmt.Sprintf("状态: %s", statusLabel))
	if status.Online {
		b.WriteString(fmt.Sprintf(" | 延迟: %dms", status.Latency.Milliseconds()))
	}
	b.WriteString("\n")

	if !status.Online {
		return b.String()
	}

	if status.MOTDPlain != "" {
		b.WriteString(fmt.Sprintf("MOTD: %s\n", status.MOTDPlain))
	}
	b.WriteString(fmt.Sprintf("版本: %s\n", status.Version))
	b.WriteString(fmt.Sprintf("玩家: %d / %d\n", status.Players.Online, status.Players.Max))
	if len(status.Players.List) > 0 {
		var names []string
		max := 10
		if len(status.Players.List) < max {
			max = len(status.Players.List)
		}
		for _, player := range status.Players.List[:max] {
			names = append(names, player.Name)
		}
		b.WriteString("玩家列表: " + strings.Join(names, ", "))
		if len(status.Players.List) > max {
			b.WriteString(fmt.Sprintf(" 等 %d 人", len(status.Players.List)))
		}
	}
	return b.String()
}

func renderMCCard(status *MCServerStatus) ([]byte, error) {
	if !status.Online {
		return renderMCOfflineCard(status)
	}

	canvas, err := textimage.NewCanvas(cardWidthMC,
		textimage.WithCJKFont(),
		textimage.WithFontColor(mcTextPrimary),
		textimage.WithLineHeight(1.6),
		textimage.WithBgColor(mcBgDark),
		textimage.WithPadding(24, 16),
	)
	if err != nil {
		return nil, err
	}

	if len(status.MOTD) > 0 {
		motdImg, err := renderMotdImage(status.MOTD, cardWidthMC-48, 18)
		if err == nil {
			canvas.AddImage(motdImg, textimage.WithImgAlign(textimage.AlignCenter))
			canvas.AddSpacer(4)
		}
	}

	addr := fmt.Sprintf("%s:%d", status.Host, status.Port)
	canvas.AddText(addr,
		textimage.WithFontSize(13),
		textimage.WithFontColor(mcTextDim),
		textimage.WithAlign(textimage.AlignCenter),
	)

	canvas.AddSpacer(12)
	canvas.AddDivider(textimage.WithDividerColor(mcDivider))
	canvas.AddSpacer(8)

	editionLabel := "Java"
	if status.Edition == "bedrock" {
		editionLabel = "Bedrock"
	}
	latencyStr := fmt.Sprintf("%dms", status.Latency.Milliseconds())
	if status.Latency < 0 {
		latencyStr = "N/A"
	}

	canvas.AddRow(
		textimage.RowItem{
			Text: "● 在线",
			TextOpts: []textimage.Option{
				textimage.WithFontSize(16),
				textimage.WithFontColor(mcAccentGreen),
			},
		},
		textimage.RowItem{
			Text: fmt.Sprintf("%s  |  %s", editionLabel, latencyStr),
			TextOpts: []textimage.Option{
				textimage.WithFontSize(13),
				textimage.WithFontColor(mcTextDim),
				textimage.WithAlign(textimage.AlignRight),
			},
		},
	)

	canvas.AddSpacer(16)

	canvas.AddText(fmt.Sprintf("玩家: %d / %d", status.Players.Online, status.Players.Max),
		textimage.WithFontSize(15),
	)
	canvas.AddSpacer(4)

	canvas.AddProgressBar(float64(status.Players.Online), float64(status.Players.Max),
		textimage.WithProgressHeight(10),
		textimage.WithProgressFillColor(mcAccentGreen),
		textimage.WithProgressTrackColor(mcDivider),
		textimage.WithProgressRadius(5),
	)

	canvas.AddSpacer(12)

	canvas.AddText("版本: "+status.Version,
		textimage.WithFontSize(13),
		textimage.WithFontColor(mcTextDim),
	)

	if len(status.Players.List) > 0 {
		canvas.AddSpacer(8)
		canvas.AddDivider(textimage.WithDividerColor(mcDivider))
		canvas.AddSpacer(8)

		var badges []textimage.BadgeItem
		maxPlayers := 10
		if len(status.Players.List) < maxPlayers {
			maxPlayers = len(status.Players.List)
		}
		for _, player := range status.Players.List[:maxPlayers] {
			badges = append(badges, textimage.BadgeItem{
				Text:      player.Name,
				BgColor:   mcBgCard,
				TextColor: mcTextPrimary,
			})
		}
		if len(status.Players.List) > maxPlayers {
			badges = append(badges, textimage.BadgeItem{
				Text:      fmt.Sprintf("+%d...", len(status.Players.List)-maxPlayers),
				BgColor:   mcBgCard,
				TextColor: mcTextPrimary,
			})
		}
		canvas.AddBadgeRow(badges,
			textimage.WithBadgeFontSize(12),
			textimage.WithBadgePadding(8, 4),
			textimage.WithBadgeGap(4),
		)
	}

	if len(status.Favicon) > 0 {
		faviconImg, _, err := image.Decode(bytes.NewReader(status.Favicon))
		if err == nil && faviconImg.Bounds().Dx() > 0 {
			canvas.AddImage(faviconImg,
				textimage.WithImgWidth(40),
				textimage.WithImgAlign(textimage.AlignLeft),
			)
		}
	}

	return canvas.ResultPNG()
}

func renderMCOfflineCard(status *MCServerStatus) ([]byte, error) {
	canvas, err := textimage.NewCanvas(400,
		textimage.WithCJKFont(),
		textimage.WithFontColor(mcTextPrimary),
		textimage.WithBgColor(mcBgDark),
		textimage.WithPadding(32, 24),
	)
	if err != nil {
		return nil, err
	}

	canvas.AddText("⛏ 服务器离线",
		textimage.WithFontSize(22),
		textimage.WithFontColor(mcAccentRed),
		textimage.WithAlign(textimage.AlignCenter),
	)
	canvas.AddSpacer(12)
	canvas.AddText(fmt.Sprintf("%s:%d", status.Host, status.Port),
		textimage.WithFontSize(14),
		textimage.WithFontColor(mcTextDim),
		textimage.WithAlign(textimage.AlignCenter),
	)
	canvas.AddSpacer(8)
	canvas.AddText("服务器无法连接或已关闭",
		textimage.WithFontSize(13),
		textimage.WithFontColor(mcTextDim),
		textimage.WithAlign(textimage.AlignCenter),
	)

	return canvas.ResultPNG()
}

func renderMotdImage(segments []MotdSegment, maxWidth int, fontSize float64) (image.Image, error) {
	fontPath := textimage.SystemCJKFontPath()
	if fontPath == "" {
		return nil, fmt.Errorf("no CJK font available")
	}

	raw, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, err
	}

	var parsed *opentype.Font
	if isTTCBytes(raw) {
		col, err := opentype.ParseCollection(raw)
		if err != nil {
			return nil, err
		}
		parsed, err = col.Font(0)
		if err != nil {
			return nil, err
		}
	} else {
		parsed, err = opentype.Parse(raw)
		if err != nil {
			return nil, err
		}
	}

	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: fontSize, DPI: 72})
	if err != nil {
		return nil, err
	}
	defer face.Close()

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()

	totalW := 0
	for _, seg := range segments {
		w := font.MeasureString(face, seg.Text).Ceil()
		totalW += w
	}
	if totalW < 1 {
		totalW = 1
	}
	if totalW > maxWidth {
		totalW = maxWidth
	}

	lineH := (metrics.Ascent + metrics.Descent).Ceil()
	if lineH < 1 {
		lineH = 1
	}

	img := image.NewRGBA(image.Rect(0, 0, totalW, lineH))

	x := fixed.Int26_6(0)
	for _, seg := range segments {
		w := font.MeasureString(face, seg.Text)
		if x.Round()+w.Round() > maxWidth {
			break
		}
		d := &font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(seg.Color),
			Face: face,
			Dot:  fixed.P(x.Round(), ascent),
		}
		d.DrawString(seg.Text)
		x += w
	}

	return img, nil
}

func isTTCBytes(data []byte) bool {
	return len(data) >= 4 && data[0] == 0x74 && data[1] == 0x74 &&
		data[2] == 0x63 && data[3] == 0x66
}

// ListTools 返回 AI 可调用的工具列表。实现 ai.ToolProvider。
func (p *mcPlugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "query_minecraft_server",
			Categories:  []string{"minecraft"},
			Description: "查询 Minecraft 服务器的状态，包括玩家数、版本、延迟等信息",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"server_address": {
						Type:        "string",
						Description: "服务器地址，格式：主机名[:端口]（Java 版默认 25565，Bedrock 版默认 19132）",
					},
				},
				Required: []string{"server_address"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				addr, _ := args["server_address"].(string)
				if addr == "" {
					return "", fmt.Errorf("请提供服务器地址")
				}
				host := addr
				port := 0
				if strings.Contains(host, ":") {
					parts := strings.SplitN(host, ":", 2)
					host = parts[0]
					fmt.Sscanf(parts[1], "%d", &port)
				}
				status, err := Ping(host, port, 10*time.Second)
				if err != nil {
					return fmt.Sprintf("服务器 %s 无法连接: %v", addr, err), nil
				}
				return formatMCText(status), nil
			},
		},
	}
}

// ListSkills 返回 AI 技能列表。实现 ai.SkillProvider。
func (p *mcPlugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "minecraft_query",
			Description: "Minecraft 服务器状态查询",
			Prompt: `你是一个 Minecraft 服务器状态查询助手。
当用户询问某个 Minecraft 服务器的状态时，使用 query_minecraft_server 工具进行查询。
返回的信息包括：服务器在线状态、MOTD、版本、在线玩家数和延迟。

如果服务器无法连接，请用友好的语气告知用户。`,
			Tools: p.ListTools(),
		},
	}
}

// HealthCheckers 返回插件的健康探针。实现 health.CheckProvider。
func (p *mcPlugin) HealthCheckers() []health.Checker {
	return []health.Checker{}
}
