// Package ai todo.go — 会话级待办管理器。
//
// 按会话（群/私聊）隔离，进程内存储（与定时提醒一致，重启后失效）。
// 动作侧（todo_* 工具与 /ai todo 子命令）经 catalog.TodoAccess 端口使用它，
// 适配见 capability.go；因此本文件只有状态与并发控制，不含动作参数解析。
package ai

import (
	"strconv"
	"sync"
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
