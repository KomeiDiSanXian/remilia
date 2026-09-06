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
	"github.com/KomeiDiSanXian/remilia/infra/imagekit"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
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
		att := p.attachmentFor(img)
		if captionOK {
			// 支持图文同发的平台：图片 + 提示词 caption 一条消息
			ctx.Reply(platform.TextMessage(prompt).WithAttachments(att))
		} else {
			// 不支持图文同发的平台：图片单独发
			ctx.Reply(platform.OutboundMessage{Attachments: []platform.Attachment{att}})
		}
	}

	// QQ 平台：单图生成完成后追加一条"变体建议"提示键盘。按钮均为 type=2
	//（点击后自动把 /aimage 变体命令填入输入框，由用户确认发送），不产生
	// 互动回调，规避 QQ webhook 互动事件投递不可靠的问题（见 docs/FAQ.md）。
	// 多图场景跳过，避免被动回复条数占用过高。
	if len(images) == 1 && p.variantKeyboardEnabled() && ctx.GetEventPlatform() == "qq" {
		ctx.Reply(p.variantKeyboardMessage(prompt))
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

// sendMaxBytes 返回发送图片前允许的最大体积（字节）。
// <=0 表示不限制体积（不因体积触发压缩）。
func (p *Plugin) sendMaxBytes() int64 {
	if p.cfg == nil {
		return imagekit.DefaultMaxBytes
	}
	return int64(p.cfg.GetInt("send_max_bytes", int(imagekit.DefaultMaxBytes)))
}

// sendMaxDimension 返回发送图片前允许的最大边长（像素）。
// <=0 表示不限制边长。
func (p *Plugin) sendMaxDimension() int {
	if p.cfg == nil {
		return imagekit.DefaultMaxDimension
	}
	return p.cfg.GetInt("send_max_dimension", imagekit.DefaultMaxDimension)
}

// attachmentFor 将生成结果转换为待发送附件。
//
// 发送前经 infra/imagekit 压缩到配置的体积/边长上限内：体积与边长均未超限
// 时原样返回；GIF 与解码失败的数据不重编码，避免阻塞发送。文件名后缀跟随
// 压缩后的真实 MIME（PNG 重编码后可能变为 JPEG）。
func (p *Plugin) attachmentFor(img imageResult) platform.Attachment {
	comp := imagekit.Compress(img.Data, img.MimeType, imagekit.Options{
		MaxBytes:     p.sendMaxBytes(),
		MaxDimension: p.sendMaxDimension(),
	})
	if comp.Reencoded && p.log != nil {
		p.log.Warnf("[aimage] 生成图超出发送限制，已压缩（%d → %d bytes）", len(img.Data), len(comp.Data))
	}
	return platform.Attachment{
		Kind:     platform.AttachmentKindImage,
		Data:     comp.Data,
		Name:     "aimage" + extByMime(comp.Mime),
		MimeType: comp.Mime,
	}
}

// extByMime 根据 MIME 返回图片文件扩展名（JPEG 统一 .jpg）。
func extByMime(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

// variantKeyboardEnabled 返回是否在 QQ 平台生成完成后附带"变体建议"提示键盘
// （plugins.aimage.qq_variant_keyboard，默认 true）。
func (p *Plugin) variantKeyboardEnabled() bool {
	if p.cfg == nil {
		return true
	}
	return p.cfg.GetBool("qq_variant_keyboard", true)
}

// variantKeyboardMessage 构造携带 QQ 提示键盘的文本消息。
//
// 提示键盘按钮文案即点击后填入输入框的 /aimage 命令，因此按钮 Label 直接
// 使用完整命令；原提示词过长时截断，避免按钮文案过长。
func (p *Plugin) variantKeyboardMessage(prompt string) platform.OutboundMessage {
	msg := platform.TextMessage("✨ 已生成，试试这些变体？（点击按钮自动填入指令，确认后发送）")
	return qq.ApplyExtra(msg, qq.MessageExtra{PromptKeyboard: p.variantKeyboard(prompt)})
}

// variantKeyboard 构造 QQ"变体建议"提示键盘：首行为同款重绘与风格变体，
// 次行为常用尺寸。返回 nil 表示提示词为空（无可变体建议）。
func (p *Plugin) variantKeyboard(prompt string) *dto.PromptKeyboard {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil
	}
	// 截断不补省略号：按钮文案即填入输入框的完整命令，省略号会原样进入
	// 重生成的提示词。
	prompt = clipRunes(prompt, 24)
	return dto.NewPromptKeyboard(
		[]dto.PromptKeyboardButton{
			variantButton("/aimage " + prompt),
			variantButton("/aimage " + prompt + "，水彩风格"),
			variantButton("/aimage " + prompt + "，赛博朋克风"),
		},
		[]dto.PromptKeyboardButton{
			variantButton("/aimage " + prompt + " -size 1344x768"),
			variantButton("/aimage " + prompt + " -size 768x1344"),
			variantButton("/aimage " + prompt + " -size 1024x1024"),
		},
	)
}

// variantButton 构造单颗提示键盘按钮（type=2：点击后把文案填入输入框）。
func variantButton(cmd string) dto.PromptKeyboardButton {
	return dto.PromptKeyboardButton{
		RenderData: dto.PromptKeyboardRenderData{Label: cmd, Style: 2},
		Action:     dto.PromptKeyboardAction{Type: 2},
	}
}

// clipRunes 按字符数截断字符串，不追加任何占位符（区别于 client.truncateRunes）。
func clipRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
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
