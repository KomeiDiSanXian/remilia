// Package ai capability.go — 内置动作能力的组合根。
//
// 动作侧（builtin/ai/catalog）声明它需要的最小能力端口，插件内部的具体管理器
// 在这里、也只在装配点被适配成端口：未启用的能力保持为 nil 端口
// （注意不能把类型化 nil 指针赋给端口，否则动作侧的 nil 判定会失效），
// 动作据此走原有降级分支。
//
// 适配器只转发到同一份底层管理器，端口不复制状态、不含策略。
package ai

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// toolCapabilities 组装本次动作调用可用的能力端口。
func (p *Plugin) toolCapabilities() catalog.Capabilities {
	var caps catalog.Capabilities
	if p == nil {
		return caps
	}
	if p.memory != nil {
		caps.Memory = memoryAccessor{m: p.memory}
	}
	if p.todos != nil {
		caps.Todos = todoAccessor{m: p.todos}
	}
	if p.reminders != nil {
		caps.Reminders = reminderAccessor{m: p.reminders}
		caps.Scheduler = pluginReminderScheduler{p: p}
	}
	return caps
}

// memoryAccessor 把长期记忆存储适配为 [catalog.MemoryAccess]。
//
// 作用域键（user:<id> / group:<id>）由这里构造，动作侧只传"种类 + 标识"，
// 键格式只有一处定义（见 memory.go 的 userScope / groupScope）。
type memoryAccessor struct{ m *memoryStore }

// Add 写入一条事实。
func (a memoryAccessor) Add(scopeKind, scopeID, text string) {
	a.m.Add(a.scope(scopeKind, scopeID), text)
}

// Count 返回指定作用域的事实条数。
func (a memoryAccessor) Count(scopeKind, scopeID string) int {
	return len(a.m.Facts(a.scope(scopeKind, scopeID)))
}

// Remove 精确删除一条事实。
func (a memoryAccessor) Remove(scopeKind, scopeID, text string) bool {
	return a.m.Remove(a.scope(scopeKind, scopeID), text)
}

// scope 把动作侧的作用域种类与标识映射为存储键。
func (a memoryAccessor) scope(kind, id string) string {
	if kind == "group" {
		return groupScope(id)
	}
	return userScope(id)
}

// todoAccessor 把会话待办管理器适配为 [catalog.TodoAccess]。
type todoAccessor struct{ m *todoManager }

// Add 追加一条待办。
func (a todoAccessor) Add(chatID, text string) string { return a.m.add(chatID, text) }

// List 返回指定会话的全部待办。
func (a todoAccessor) List(chatID string) []catalog.TodoItem {
	items := a.m.list(chatID)
	out := make([]catalog.TodoItem, 0, len(items))
	for _, it := range items {
		out = append(out, catalog.TodoItem{ID: it.ID, Text: it.Text, Done: it.Done})
	}
	return out
}

// SetDone 更新完成状态。
func (a todoAccessor) SetDone(chatID, id string, done bool) bool {
	return a.m.setDone(chatID, id, done)
}

// Remove 删除一条待办。
func (a todoAccessor) Remove(chatID, id string) bool { return a.m.remove(chatID, id) }

// reminderAccessor 把提醒管理器适配为 [catalog.ReminderAccess]。
type reminderAccessor struct{ m *reminderManager }

// List 返回指定会话的全部活跃提醒。
func (a reminderAccessor) List(chatID string) []catalog.ReminderItem {
	items := a.m.list(chatID)
	out := make([]catalog.ReminderItem, 0, len(items))
	for _, r := range items {
		out = append(out, catalog.ReminderItem{ID: r.ID, Text: r.Text, DueAt: r.At})
	}
	return out
}

// Remove 取消一条提醒。
func (a reminderAccessor) Remove(chatID, id string) bool { return a.m.remove(chatID, id) }

// pluginReminderScheduler 把插件内部的提醒创建流程适配为 [catalog.ReminderScheduler]。
//
// 它需要插件的生命周期与推送能力，因此是本包唯一的提醒创建适配器，
// 且只在装配点构造。
type pluginReminderScheduler struct{ p *Plugin }

// Schedule 注册提醒并只返回确认文本（提醒对象本身由管理器持有）。
func (s pluginReminderScheduler) Schedule(chat platform.ChatInfo, sender platform.Sender, duration time.Duration, content string) string {
	_, confirm := s.p.addReminder(chat, sender, duration, content)
	return confirm
}
