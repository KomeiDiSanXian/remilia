// Package minecraft 提供 Minecraft 服务器状态查询功能。
//
// 命令: /mc <主机名>[:端口]
// AI 工具: query_minecraft_server
// AI 技能: minecraft_query
package minecraft

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
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

			mcDef := command.NewDef("mc").Description("Minecraft 服务器状态查询").
				Arg("host", "服务器地址，支持 主机名:端口", true).
				Arg("edition", "强制版本 java/bedrock（可选，自动探测）", false).
				Example("/mc mc.hypixel.net").Example("/mc java mc.hypixel.net").Example("/mc bedrock 192.168.1.1:19132").Build()
			ctx.OnCommandDefWith("", "/mc", mcDef, p.handleMC, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
		},
	}
}

func (p *mcPlugin) handleMC(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		ctx.ReplyError("用法: /mc <主机名>[:端口] [/mc java /mc bedrock]")
		return nil
	}

	args := parsed.Positional
	edition := ""
	var host string
	port := 0

	switch args[0] {
	case "java", "bedrock":
		edition = args[0]
		if len(args) < 2 {
			ctx.ReplyError("用法: /mc java <主机名>[:端口]")
			return nil
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
		ctx.ReplyText(fmt.Sprintf("⛏ 服务器 %s 无法连接: %v", host, err))
		return nil
	}

	png, imgErr := renderMCCard(status)
	if imgErr != nil {
		ctx.ReplyText(formatMCText(status))
		return nil
	}

	if ctx.Reply(platform.ImageDataMessage(png, "mc_status.png", "image/png")); err != nil {
		return err
	}
	return nil
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
