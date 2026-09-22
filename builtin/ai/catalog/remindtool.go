// remindtool.go — 定时提醒动作（set_reminder / list_reminders / cancel_reminder）。
//
// 与 /ai remind 子命令共用同一份提醒管理器（由装配侧经 ReminderAccess /
// ReminderScheduler 注入）：AI 可在对话中主动设置、查询、取消提醒，
// 到期仍由装配侧的推送逻辑送达原会话。
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/platform"
)

const (
	// SetReminderToolName 设置提醒的动作名。
	SetReminderToolName = "set_reminder"
	// ListRemindersToolName 列出提醒的动作名。
	ListRemindersToolName = "list_reminders"
	// CancelReminderToolName 取消提醒的动作名。
	CancelReminderToolName = "cancel_reminder"
)

// BuildReminderTools 构建定时提醒动作集（general 类别，恒被选中）。
func BuildReminderTools() []toolkit.Tool {
	return []toolkit.Tool{
		{
			Name:        SetReminderToolName,
			Categories:  []string{toolkit.CategoryGeneral},
			Description: "在当前会话设置一条定时提醒，到期后主动推送提醒消息到本会话。适合用户提出\"XX分钟后提醒我\"等诉求",
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"duration": {Type: "string", Description: "提醒时长，支持 30秒/5分钟/1小时/2天 或 30s/5m/1h/2d，也支持纯数字（分钟）"},
					"content":  {Type: "string", Description: "提醒内容"},
				},
				Required: []string{"duration", "content"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolkit.ToolSourceFromContext(ctx)
				if !ok {
					return "", errors.New("当前上下文无提醒能力")
				}
				caps, capsOK := capabilitiesFromContext(ctx)
				if !capsOK || caps.Scheduler == nil {
					return "", errors.New("提醒功能不可用")
				}
				sender, senderOK := toolkit.PlatformSenderFromContext(ctx)
				if !senderOK {
					return "", errors.New("无法获取平台发送器，提醒不可用")
				}
				durationStr, _ := args["duration"].(string)
				content, _ := args["content"].(string)
				d, err := ParseRemindDuration(strings.TrimSpace(durationStr))
				if err != nil {
					return "", fmt.Errorf("无法解析时长 %q：支持 30秒/5分钟/1小时/2天 或 30s/5m/1h/2d", durationStr)
				}
				content = strings.TrimSpace(content)
				if content == "" {
					return "", errors.New("提醒内容不能为空")
				}
				confirm := caps.Scheduler.Schedule(platform.ChatInfo{ID: src.ChatID, IsGroup: src.IsGroup}, sender, d, content)
				return confirm, nil
			},
		},
		{
			Name:        ListRemindersToolName,
			Categories:  []string{toolkit.CategoryGeneral},
			Description: "列出当前会话的所有活跃定时提醒（ID、内容、剩余时间），供用户确认或取消",
			Parameters: protocol.ToolParamSchema{
				Type:       "object",
				Properties: map[string]protocol.ToolParamSchema{},
			},
			Execute: func(ctx context.Context, _ map[string]any) (string, error) {
				src, ok := toolkit.ToolSourceFromContext(ctx)
				caps, capsOK := capabilitiesFromContext(ctx)
				if !ok || !capsOK || caps.Reminders == nil {
					return "当前会话没有活跃的提醒", nil
				}
				items := caps.Reminders.List(src.ChatID)
				if len(items) == 0 {
					return "当前会话没有活跃的提醒", nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "活跃提醒（%d 条）：\n", len(items))
				for _, r := range items {
					fmt.Fprintf(&b, "- %s — %s（%s 后触发）\n", r.ID, r.Text, FormatRemindDuration(time.Until(r.DueAt)))
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        CancelReminderToolName,
			Categories:  []string{toolkit.CategoryGeneral},
			Description: "取消当前会话的一条定时提醒，id 来自 list_reminders",
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"id": {Type: "string", Description: "提醒 ID（如 R1，见 list_reminders）"},
				},
				Required: []string{"id"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolkit.ToolSourceFromContext(ctx)
				caps, capsOK := capabilitiesFromContext(ctx)
				if !ok || !capsOK || caps.Reminders == nil {
					return "", errors.New("提醒功能不可用")
				}
				id, _ := args["id"].(string)
				id = strings.TrimSpace(id)
				if id == "" {
					return "", errors.New("请指定要取消的提醒 ID（见 list_reminders）")
				}
				if caps.Reminders.Remove(src.ChatID, id) {
					return fmt.Sprintf("已取消提醒 %s", id), nil
				}
				return fmt.Sprintf("未找到提醒 %s（ID 见 list_reminders）", id), nil
			},
		},
	}
}
