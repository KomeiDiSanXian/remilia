// Package aimage 提供文生图（Text-to-Image）能力：AI 工具 generate_image 与 /aimage 命令。
//
// 支持两种后端（plugins.aimage.provider 切换）：
//   - openai：OpenAI 兼容 /images/generations 端点（DALL-E、SiliconFlow 等中转）
//   - sdwebui：本地 Stable Diffusion WebUI /sdapi/v1/txt2img（AUTOMATIC1111 风格）
//
// 生成的图片统一物化为二进制后直传会话（不走 URL 转发，规避过期与 SSRF 问题）。
// AI 工具标记 RequiresApproval：tool_approval=restricted 时生成需人工审批。
package aimage

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Plugin 图片生成插件实例。
type Plugin struct {
	cfg    plugin.ConfigReader
	log    plugin.Logger
	client *imageClient
}

// New 创建图片生成插件的 Descriptor。
//
// 命令:
//   - /aimage <提示词>               生成一张图（尺寸取配置，默认 1024x1024）
//   - /aimage <提示词> -size 512x512 指定尺寸（宽x高）
//   - /aimage <提示词> -n 2          一次生成多张（上限 max_n，默认 3）
//
// AI:
//   - generate_image(prompt, size?, n?) → 生成后直传当前会话
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "aimage",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "文生图：AI 工具 generate_image 与 /aimage 命令（OpenAI 兼容 / Stable Diffusion WebUI）",
			Category:    "工具",
			Tags:        []string{"图片生成", "AI", "文生图"},
			HelpText: `图片生成 — 根据文字描述生成图片

用法：
  /aimage <提示词>                 生成一张图（尺寸取配置，默认 1024x1024）
  /aimage <提示词> -size 512x512   指定尺寸（宽x高）
  /aimage <提示词> -n 2            一次生成多张（上限 max_n，默认 3）

后端由 plugins.aimage.provider 配置：
  - openai：OpenAI 兼容 /images/generations（DALL-E、SiliconFlow 等）
  - sdwebui：本地 Stable Diffusion WebUI /sdapi/v1/txt2img`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.cfg = ctx.Config
			if !p.enabled() {
				ctx.Log.Info("[aimage] disabled by config, skip setup")
				return p, nil
			}

			client, err := newImageClient(
				p.provider(), p.baseURL(), p.apiKey(), p.model(), p.size(),
				p.steps(), p.cfgScale(), p.negativePrompt(), p.timeout(), p.proxy(),
			)
			if err != nil {
				return nil, fmt.Errorf("aimage: 初始化客户端失败: %w", err)
			}
			p.client = client

			def := command.NewDef("aimage").Description("根据文字描述生成图片").
				Arg("prompt", "图片描述，应包含主体、风格、构图、光线等细节", true).
				Flag("size", "", "生成尺寸（宽x高，如 512x512、1024x1024）", command.ArgTypeString).
				Flag("n", "", "生成张数（默认 1，上限 max_n）", command.ArgTypeInt).
				Example("/aimage 一只戴帽子的橘猫，水彩风格").Example("/aimage 赛博朋克城市 -size 768x512").Build()
			ctx.OnCommandDefWith("", "/aimage", def, p.handleAimage, eventctx.OnMentionedBotOrNoMentions())
			return p, nil
		},
	}
}

// handleAimage 处理 /aimage 命令。
func (p *Plugin) handleAimage(ctx *eventctx.Context) error {
	if p.client == nil {
		ctx.ReplyError("aimage 未启用或未配置（请检查 plugins.aimage 配置）")
		return nil
	}
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		ctx.ReplyError("用法: /aimage <提示词> [-size 宽x高] [-n 张数]")
		return nil
	}
	prompt := strings.TrimSpace(strings.Join(parsed.Positional, " "))
	if prompt == "" {
		ctx.ReplyError("用法: /aimage <提示词> [-size 宽x高] [-n 张数]")
		return nil
	}

	n := 1
	if v, ok := parsed.Flags["n"]; ok {
		nv, perr := strconv.Atoi(v)
		if perr != nil || nv <= 0 {
			ctx.ReplyError("无效的 -n 参数（应为正整数）")
			return nil
		}
		n = nv
	}
	if n > p.maxN() {
		n = p.maxN()
	}

	size := p.size()
	if v, ok := parsed.Flags["size"]; ok && strings.TrimSpace(v) != "" {
		size = strings.TrimSpace(v)
	}
	if _, _, serr := parseSize(size); serr != nil {
		ctx.ReplyError(fmt.Sprintf("无效的尺寸 %q（应为 宽x高，如 1024x1024）", size))
		return nil
	}

	// 生成耗时较长，先回一条进度提示
	ctx.ReplyText("🎨 正在生成图片，请稍候…")

	reqCtx, cancel := context.WithTimeout(ctx.Context(), p.timeout())
	defer cancel()
	images, err := p.client.generate(reqCtx, prompt, n)
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("图片生成失败: %v", err))
		return nil
	}

	caps := ctx.GetPlatformCapabilities()
	captionOK := caps.Has(platform.CapCaption)
	for _, img := range images {
		att := platform.Attachment{
			Kind:     platform.AttachmentKindImage,
			Data:     img.Data,
			Name:     "aimage.png",
			MimeType: img.MimeType,
		}
		if captionOK {
			// 支持图文同发的平台：图片 + 提示词 caption 一条消息
			ctx.Reply(platform.TextMessage(prompt).WithAttachments(att))
		} else {
			// QQ 等富媒体会丢弃文本：图片单独发
			ctx.Reply(platform.OutboundMessage{Attachments: []platform.Attachment{att}})
		}
	}
	return nil
}

// ── 配置读取（Setup 时读取一次，修改需重启）───────────────────────────

func (p *Plugin) enabled() bool {
	if p.cfg == nil {
		return false
	}
	return p.cfg.GetBool("enabled", false)
}

func (p *Plugin) provider() string {
	if p.cfg == nil {
		return providerOpenAI
	}
	return p.cfg.GetString("provider", providerOpenAI)
}

func (p *Plugin) baseURL() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("base_url", "")
}

func (p *Plugin) apiKey() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("api_key", "")
}

func (p *Plugin) model() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("model", "")
}

func (p *Plugin) size() string {
	if p.cfg == nil {
		return "1024x1024"
	}
	return p.cfg.GetString("size", "1024x1024")
}

func (p *Plugin) maxN() int {
	if p.cfg == nil {
		return 3
	}
	n := p.cfg.GetInt("max_n", 3)
	if n <= 0 {
		return 3
	}
	return n
}

func (p *Plugin) timeout() time.Duration {
	if p.cfg == nil {
		return 120 * time.Second
	}
	return p.cfg.GetDuration("timeout", 120*time.Second)
}

func (p *Plugin) steps() int {
	if p.cfg == nil {
		return 20
	}
	return p.cfg.GetInt("steps", 20)
}

func (p *Plugin) cfgScale() float64 {
	if p.cfg == nil {
		return 7
	}
	return p.cfg.GetFloat64("cfg_scale", 7)
}

func (p *Plugin) negativePrompt() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("negative_prompt", "")
}

func (p *Plugin) proxy() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("proxy", "")
}
