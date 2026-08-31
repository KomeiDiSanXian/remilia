// Package aimage ai_tool.go — 向 AI 插件暴露文生图工具。
//
// Plugin 实现 ai.ToolProvider 接口，AI 插件在容器冻结后通过
// DiscoverToolProviders 自动发现注册。工具标记 RequiresApproval：
// tool_approval=restricted 模式下生成需人工审批。
package aimage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ListTools 实现 ai.ToolProvider。未启用或未配置 base_url 时不注册工具。
func (p *Plugin) ListTools() []ai.Tool {
	if p.client == nil {
		return nil
	}
	return []ai.Tool{{
		Name:        "generate_image",
		Categories:  []string{ai.CategoryGeneral},
		Description: "根据文字描述生成图片（文生图）。生成成功后图片会自动发送到当前会话，工具只返回简短结果文本。当用户要求「画/生成一张图」、需要配图或插画时使用；prompt 应详细描述主体、风格、构图、光线等",
		Parameters: ai.ToolParamSchema{
			Type: "object",
			Properties: map[string]ai.ToolParamSchema{
				"prompt": {
					Type:        "string",
					Description: "图片描述（必填），应包含主体、风格、构图、光线等细节",
				},
				"size": {
					Type:        "string",
					Description: "生成尺寸（宽x高，如 512x512、1024x1024），默认取配置 plugins.aimage.size",
				},
				"n": {
					Type:        "integer",
					Description: "生成张数（默认 1，上限取配置 plugins.aimage.max_n）",
				},
			},
			Required: []string{"prompt"},
		},
		RequiresApproval: true,
		Execute:          p.executeGenerateImage,
	}}
}

// executeGenerateImage 是 generate_image 工具的 Execute 回调。
func (p *Plugin) executeGenerateImage(ctx context.Context, args map[string]any) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("aimage 未启用或未配置（请检查 plugins.aimage 配置）")
	}
	prompt, _ := args["prompt"].(string)
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("缺少必填参数 prompt（图片描述）")
	}

	n := 1
	if v, ok := args["n"].(float64); ok && int(v) > 0 {
		n = int(v)
	}
	if n > p.maxN() {
		n = p.maxN()
	}

	size := p.size()
	if v, ok := args["size"].(string); ok && strings.TrimSpace(v) != "" {
		size = strings.TrimSpace(v)
	}
	if _, _, err := parseSize(size); err != nil {
		return "", fmt.Errorf("无效的 size %q（应为 宽x高，如 1024x1024）", size)
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	images, err := p.client.generate(reqCtx, prompt, n)
	if err != nil {
		return "", err
	}

	// 有发送能力时直传图片到会话；无发送能力（异常上下文）时回退为提示文本
	if sender, ok := ai.ToolSenderFromContext(ctx); ok {
		atts := make([]platform.Attachment, 0, len(images))
		for _, img := range images {
			atts = append(atts, platform.Attachment{
				Kind:     platform.AttachmentKindImage,
				Data:     img.Data,
				Name:     "aimage.png",
				MimeType: img.MimeType,
			})
		}
		sendCtx, sendCancel := context.WithTimeout(ctx, 30*time.Second)
		defer sendCancel()
		if _, err := sender.ReplyToChat(sendCtx, platform.OutboundMessage{Attachments: atts}); err != nil {
			return "", fmt.Errorf("图片已生成但发送失败: %w", err)
		}
		return fmt.Sprintf("已生成并发送 %d 张图片", len(images)), nil
	}
	return fmt.Sprintf("已生成 %d 张图片（当前上下文无发送能力，可提示用户使用 /aimage 命令取图）", len(images)), nil
}
