// Package ai todotool.go — 会话内待办清单工具（todo_add / todo_list / todo_done / todo_remove）。
//
// 按会话（群/私聊）隔离，进程内存储（与定时提醒一致，重启后失效）：
//   - todo_add：新增一条待办
//   - todo_list：列出待办（可过滤 pending/done）
//   - todo_done：标记完成
//   - todo_remove：删除一条
package ai

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const (
	todoAddToolName    = "todo_add"
	todoListToolName   = "todo_list"
	todoDoneToolName   = "todo_done"
	todoRemoveToolName = "todo_remove"
)

// todoItem 一条待办。
type todoItem struct {
	// ID 会话内序号（T1、T2 …）。
	ID string
	// Text 待办内容。
	Text string
	// Done 是否已完成。
	Done bool
}

// todoManager 会话级待办管理（进程内存储）。
type todoManager struct {
	mu    sync.Mutex
	items map[string][]todoItem // chatID → 待办列表
	seq   map[string]int
}

func newTodoManager() *todoManager {
	return &todoManager{
		items: make(map[string][]todoItem),
		seq:   make(map[string]int),
	}
}

// add 新增一条待办，返回生成的 ID。
func (m *todoManager) add(chatID, text string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq[chatID]++
	id := "T" + strconv.Itoa(m.seq[chatID])
	m.items[chatID] = append(m.items[chatID], todoItem{ID: id, Text: text})
	return id
}

// list 返回指定会话的待办（按添加顺序）。
func (m *todoManager) list(chatID string) []todoItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]todoItem, len(m.items[chatID]))
	copy(out, m.items[chatID])
	return out
}

// setDone 标记指定会话的待办完成/未完成，返回是否命中。
func (m *todoManager) setDone(chatID, id string, done bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.items[chatID] {
		if m.items[chatID][i].ID == id {
			m.items[chatID][i].Done = done
			return true
		}
	}
	return false
}

// remove 删除指定会话的待办，返回是否命中。
func (m *todoManager) remove(chatID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.items[chatID]
	for i := range items {
		if items[i].ID == id {
			m.items[chatID] = append(items[:i], items[i+1:]...)
			return true
		}
	}
	return false
}

// clearDone 清除指定会话中已完成的待办，返回清除条数。
func (m *todoManager) clearDone(chatID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.items[chatID]
	kept := items[:0]
	cleared := 0
	for _, it := range items {
		if it.Done {
			cleared++
			continue
		}
		kept = append(kept, it)
	}
	m.items[chatID] = kept
	return cleared
}

// clearAll 清空指定会话的全部待办，返回清除条数。
func (m *todoManager) clearAll(chatID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.items[chatID])
	m.items[chatID] = nil
	return n
}

// buildTodoTools 构建待办工具集（general 类别，恒被选中）。
func (p *Plugin) buildTodoTools() []Tool {
	return []Tool{
		{
			Name:        todoAddToolName,
			Categories:  []string{CategoryGeneral},
			Description: "在当前会话新增一条待办事项。适合用户要求\"记一下/列个待办\"的场景",
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"item": {Type: "string", Description: "待办事项内容"},
				},
				Required: []string{"item"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolSourceFromContext(ctx)
				if !ok || src.p.todos == nil {
					return "", errors.New("待办功能不可用")
				}
				item, _ := args["item"].(string)
				item = strings.TrimSpace(item)
				if item == "" {
					return "", errors.New("待办内容不能为空")
				}
				id := src.p.todos.add(src.chatID, item)
				return fmt.Sprintf("已添加待办 %s：%s", id, item), nil
			},
		},
		{
			Name:        todoListToolName,
			Categories:  []string{CategoryGeneral},
			Description: "列出当前会话的待办事项。filter 可选 pending（未完成）/done（已完成），默认全部",
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"filter": {Type: "string", Description: "过滤：pending=未完成 / done=已完成 / 空=全部", Enum: []string{"pending", "done"}},
				},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolSourceFromContext(ctx)
				if !ok || src.p.todos == nil {
					return "当前会话没有待办事项", nil
				}
				filter, _ := args["filter"].(string)
				filter = strings.ToLower(strings.TrimSpace(filter))
				items := src.p.todos.list(src.chatID)
				var pending, done []todoItem
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
			Name:        todoDoneToolName,
			Categories:  []string{CategoryGeneral},
			Description: "把一条待办标记为已完成（id 来自 todo_list）",
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"id": {Type: "string", Description: "待办 ID（如 T1，见 todo_list）"},
				},
				Required: []string{"id"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolSourceFromContext(ctx)
				if !ok || src.p.todos == nil {
					return "", errors.New("待办功能不可用")
				}
				id, _ := args["id"].(string)
				id = strings.TrimSpace(id)
				if id == "" {
					return "", errors.New("请指定待办 ID（见 todo_list）")
				}
				if src.p.todos.setDone(src.chatID, id, true) {
					return fmt.Sprintf("待办 %s 已完成", id), nil
				}
				return fmt.Sprintf("未找到待办 %s（ID 见 todo_list）", id), nil
			},
		},
		{
			Name:        todoRemoveToolName,
			Categories:  []string{CategoryGeneral},
			Description: "删除一条待办（id 来自 todo_list）。用户要求\"划掉/删掉\"某项时调用",
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"id": {Type: "string", Description: "待办 ID（如 T1，见 todo_list）"},
				},
				Required: []string{"id"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolSourceFromContext(ctx)
				if !ok || src.p.todos == nil {
					return "", errors.New("待办功能不可用")
				}
				id, _ := args["id"].(string)
				id = strings.TrimSpace(id)
				if id == "" {
					return "", errors.New("请指定待办 ID（见 todo_list）")
				}
				if src.p.todos.remove(src.chatID, id) {
					return fmt.Sprintf("已删除待办 %s", id), nil
				}
				return fmt.Sprintf("未找到待办 %s（ID 见 todo_list）", id), nil
			},
		},
	}
}
