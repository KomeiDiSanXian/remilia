// todotool.go — 会话内待办清单动作（todo_add / todo_list / todo_done / todo_remove）。
//
// 按会话（群/私聊）隔离，存储由装配侧注入的 TodoAccess 提供：
//   - todo_add：新增一条待办
//   - todo_list：列出待办（可过滤 pending/done）
//   - todo_done：标记完成
//   - todo_remove：删除一条
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

const (
	// TodoAddToolName 新增待办的动作名。
	TodoAddToolName = "todo_add"
	// TodoListToolName 列出待办的动作名。
	TodoListToolName = "todo_list"
	// TodoDoneToolName 标记待办完成的动作名。
	TodoDoneToolName = "todo_done"
	// TodoRemoveToolName 删除待办的动作名。
	TodoRemoveToolName = "todo_remove"
)

// BuildTodoTools 构建待办动作集（general 类别，恒被选中）。
func BuildTodoTools() []toolkit.Tool {
	return []toolkit.Tool{
		{
			Name:        TodoAddToolName,
			Categories:  []string{toolkit.CategoryGeneral},
			Description: "在当前会话新增一条待办事项。适合用户要求\"记一下/列个待办\"的场景",
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"item": {Type: "string", Description: "待办事项内容"},
				},
				Required: []string{"item"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolkit.ToolSourceFromContext(ctx)
				caps, capsOK := capabilitiesFromContext(ctx)
				if !ok || !capsOK || caps.Todos == nil {
					return "", errors.New("待办功能不可用")
				}
				item, _ := args["item"].(string)
				item = strings.TrimSpace(item)
				if item == "" {
					return "", errors.New("待办内容不能为空")
				}
				id := caps.Todos.Add(src.ChatID, item)
				return fmt.Sprintf("已添加待办 %s：%s", id, item), nil
			},
		},
		{
			Name:        TodoListToolName,
			Categories:  []string{toolkit.CategoryGeneral},
			Description: "列出当前会话的待办事项。filter 可选 pending（未完成）/done（已完成），默认全部",
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"filter": {Type: "string", Description: "过滤：pending=未完成 / done=已完成 / 空=全部", Enum: []string{"pending", "done"}},
				},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolkit.ToolSourceFromContext(ctx)
				caps, capsOK := capabilitiesFromContext(ctx)
				if !ok || !capsOK || caps.Todos == nil {
					return "当前会话没有待办事项", nil
				}
				filter, _ := args["filter"].(string)
				filter = strings.ToLower(strings.TrimSpace(filter))
				items := caps.Todos.List(src.ChatID)
				var pending, done []TodoItem
				for _, it := range items {
					if it.Done {
						done = append(done, it)
					} else {
						pending = append(pending, it)
					}
				}
				switch filter {
				case "pending":
					items = pending
				case "done":
					items = done
				}
				if len(items) == 0 {
					if filter == "" {
						return "当前会话没有待办事项", nil
					}
					return "没有符合该状态的待办事项", nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "待办事项（%d 条）：\n", len(items))
				for _, it := range items {
					mark := "⬜"
					if it.Done {
						mark = "✅"
					}
					fmt.Fprintf(&b, "- %s %s — %s\n", mark, it.ID, it.Text)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        TodoDoneToolName,
			Categories:  []string{toolkit.CategoryGeneral},
			Description: "把一条待办标记为已完成（id 来自 todo_list）",
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"id": {Type: "string", Description: "待办 ID（如 T1，见 todo_list）"},
				},
				Required: []string{"id"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolkit.ToolSourceFromContext(ctx)
				caps, capsOK := capabilitiesFromContext(ctx)
				if !ok || !capsOK || caps.Todos == nil {
					return "", errors.New("待办功能不可用")
				}
				id, _ := args["id"].(string)
				id = strings.TrimSpace(id)
				if id == "" {
					return "", errors.New("请指定待办 ID（见 todo_list）")
				}
				if caps.Todos.SetDone(src.ChatID, id, true) {
					return fmt.Sprintf("待办 %s 已完成", id), nil
				}
				return fmt.Sprintf("未找到待办 %s（ID 见 todo_list）", id), nil
			},
		},
		{
			Name:        TodoRemoveToolName,
			Categories:  []string{toolkit.CategoryGeneral},
			Description: "删除一条待办（id 来自 todo_list）。用户要求\"划掉/删掉\"某项时调用",
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"id": {Type: "string", Description: "待办 ID（如 T1，见 todo_list）"},
				},
				Required: []string{"id"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolkit.ToolSourceFromContext(ctx)
				caps, capsOK := capabilitiesFromContext(ctx)
				if !ok || !capsOK || caps.Todos == nil {
					return "", errors.New("待办功能不可用")
				}
				id, _ := args["id"].(string)
				id = strings.TrimSpace(id)
				if id == "" {
					return "", errors.New("请指定待办 ID（见 todo_list）")
				}
				if caps.Todos.Remove(src.ChatID, id) {
					return fmt.Sprintf("已删除待办 %s", id), nil
				}
				return fmt.Sprintf("未找到待办 %s（ID 见 todo_list）", id), nil
			},
		},
	}
}
