// sendtool.go — AI 消息发送动作（send_message / send_to）。
//
// 为 AI 补齐消息发送能力：
//   - send_message: 向当前会话发送中间进度消息（文本/Markdown/图片/@提及），
//     多步骤任务中向用户展示阶段性输出；无需审批
//   - send_to: 向指定用户/群推送消息；强制审批（AlwaysRequireApproval）
//     且需要 ai.message.send 权限
//
// 安全模型：
//   - ToolSender 经 context 注入（执行侧构造），动作回调不接触事件上下文，
//     签名保持不变
//   - SendTo 能力仅在动作调用通过审批门后注入，嵌套 Skill 调用继承同一
//     context，无法绕过审批
//   - 审批前预解析目标：审批消息展示解析后的目标（如 张三（12345）），
//     目标无效时模型可在批准前获知并调整
//   - 每次对话处理（一轮运行）内发送次数由装配侧的预算约束（原子计数，
//     并行执行安全）
package catalog

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/platform"
)

const (
	// SendMessageToolName 向当前会话发送消息的动作名。
	SendMessageToolName = "send_message"
	// SendToToolName 向指定会话推送消息的动作名。
	SendToToolName = "send_to"

	// SendToPermission send_to 动作所需的 RBAC 权限（审批之上叠加）。
	SendToPermission = "ai.message.send"

	// MaxSendMessageRunes 单条动作消息的最大字符数。
	MaxSendMessageRunes = 4000

	// SendTimeout 单次发送的等待超时。
	SendTimeout = 15 * time.Second
)

// SendOptions 构建发送类动作的装配选项。
type SendOptions struct {
	// Markdown 最终回复是否使用 Markdown 渲染（插件 markdown 配置）。
	// 为 false 时动作默认发纯文本，除非调用方显式指定 format=markdown。
	Markdown bool
}

// BuildSendTools 构建 AI 消息发送动作列表（send_message / send_to，默认启用）。
func BuildSendTools(opts SendOptions) []toolkit.Tool {
	msgProps := map[string]protocol.ToolParamSchema{
		"message":     {Type: "string", Description: "消息文本内容"},
		"format":      {Type: "string", Description: "消息格式：默认跟随插件 markdown 配置（推荐不传）；显式指定 text=纯文本 / markdown=Markdown", Enum: []string{"text", "markdown"}},
		"image_url":   {Type: "string", Description: "可选，附带一张图片的 URL"},
		"mention_ids": {Type: "array", Items: &protocol.ToolParamSchema{Type: "string"}, Description: "可选，需要 @ 的用户 ID 列表"},
	}

	sendMessage := toolkit.Tool{
		Name:        SendMessageToolName,
		Categories:  []string{toolkit.CategoryGeneral},
		Description: "向当前会话发送一条消息（文本/Markdown/图片/@提及）。需要多轮工具调用的任务中，每完成一步先调用本工具报告进度（如\"正在搜索…\"\"第 1 步完成\"），再继续下一步；单轮即可完成的任务不要调用。最终答复直接作为回复文本返回，不要使用本工具",
		Parameters: protocol.ToolParamSchema{
			Type:       "object",
			Properties: msgProps,
			Required:   []string{"message"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			sender, ok := toolkit.ToolSenderFromContext(ctx)
			if !ok {
				return "", fmt.Errorf("当前上下文无消息发送能力")
			}
			msg, err := BuildOutboundMessage(args, opts.Markdown)
			if err != nil {
				return "", err
			}
			sendCtx, cancel := context.WithTimeout(ctx, SendTimeout)
			defer cancel()
			if _, err := sender.ReplyToChat(sendCtx, msg); err != nil {
				return "", err
			}
			return "消息已发送", nil
		},
	}

	sendToProps := make(map[string]protocol.ToolParamSchema, len(msgProps)+2)
	maps.Copy(sendToProps, msgProps)
	sendToProps["target"] = protocol.ToolParamSchema{
		Type:        "string",
		Description: "目标：本群/我/对方、本会话内近期发言者的昵称、已加入群的群名，或用户/群原始 ID（is_group 指定类型）",
	}
	sendToProps["is_group"] = protocol.ToolParamSchema{
		Type:        "boolean",
		Description: "仅 target 为原始 ID 时生效：true=群聊目标，默认 false=私聊用户；昵称/群名解析时以匹配结果为准",
	}

	sendTo := toolkit.Tool{
		Name:        SendToToolName,
		Categories:  []string{toolkit.CategoryGeneral},
		Description: "向指定用户或群发送一条消息。target 自动解析：本群/我/对方、本会话内近期发言者的昵称（如\"给张三发消息\"）、已加入群的群名、或用户/群原始 ID。发送前需要发起者审批且具备 ai.message.send 权限",
		// RequiresApproval 兼容 restricted 审批模式；AlwaysRequireApproval
		// 保证 off 模式下也必须审批。
		RequiresApproval:      true,
		AlwaysRequireApproval: true,
		Permissions:           []string{SendToPermission},
		Parameters: protocol.ToolParamSchema{
			Type:       "object",
			Properties: sendToProps,
			Required:   []string{"target", "message"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			sender, ok := toolkit.ToolSenderFromContext(ctx)
			if !ok {
				return "", fmt.Errorf("当前上下文无消息发送能力")
			}
			raw, _ := args["target"].(string)
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return "", fmt.Errorf("target 不能为空")
			}
			isGroup := false
			if v, ok := args["is_group"].(bool); ok {
				isGroup = v
			}
			target, display, err := sender.ResolveTarget(ctx, raw, isGroup)
			if err != nil {
				return "", err
			}
			msg, err := BuildOutboundMessage(args, opts.Markdown)
			if err != nil {
				return "", err
			}
			sendCtx, cancel := context.WithTimeout(ctx, SendTimeout)
			defer cancel()
			if _, err := sender.SendTo(sendCtx, target, msg); err != nil {
				return "", err
			}
			return "消息已发送到 " + display, nil
		},
	}

	return []toolkit.Tool{sendMessage, sendTo}
}

// BuildOutboundMessage 将 LLM 参数组装为平台消息（文本/Markdown/图片/@提及）。
//
// 格式选择与 AI 最终回复一致：默认跟随 markdown 配置（默认 true 即 Markdown
// 渲染、平台不支持时自动降级纯文本），format 参数可显式覆盖
// （"text"=纯文本 / "markdown"=Markdown）。
func BuildOutboundMessage(args map[string]any, markdown bool) (platform.OutboundMessage, error) {
	text, _ := args["message"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return platform.OutboundMessage{}, fmt.Errorf("message 不能为空")
	}
	if n := len([]rune(text)); n > MaxSendMessageRunes {
		return platform.OutboundMessage{}, fmt.Errorf("message 过长（最多 %d 字）", MaxSendMessageRunes)
	}

	useMarkdown := markdown
	if format, _ := args["format"].(string); format != "" {
		useMarkdown = format == "markdown"
	}

	// 与最终回复一致：Markdown 模式只填 Markdown 字段
	// （平台不支持时由 Sender 自动降级纯文本）。
	msg := platform.OutboundMessage{}
	if useMarkdown {
		msg.Markdown = text
	} else {
		msg.Text = text
	}
	if url, _ := args["image_url"].(string); strings.TrimSpace(url) != "" {
		msg.Attachments = append(msg.Attachments, platform.Attachment{
			Kind: platform.AttachmentKindImage,
			URL:  strings.TrimSpace(url),
		})
	}
	if mentions, ok := args["mention_ids"].([]any); ok {
		for _, m := range mentions {
			if s, ok := m.(string); ok && strings.TrimSpace(s) != "" {
				msg.Mentions = append(msg.Mentions, strings.TrimSpace(s))
			}
		}
	}
	return msg, nil
}
