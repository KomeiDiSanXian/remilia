// capabilities.go — 内置动作所需的**能力端口**（由消费方声明）。
//
// 动作的 Execute 回调只能经 context 拿到调用方注入的最小端口，每个端口只暴露
// 完成该动作所需的操作，不含选择、策略、模型决策与跨能力编排：
//
//   - MemoryAccess：长期记忆的写入 / 计数 / 精确删除（memory_add / memory_forget）
//   - TodoAccess：会话待办清单（todo_*）
//   - ReminderAccess / ReminderScheduler：提醒的查询取消与创建（set_reminder / …）
//
// 端口的实现（记忆存储、待办管理器、提醒调度）由装配侧适配后注入本包，
// 因此本包不反向依赖插件实例。字段为 nil 表示该能力在当前部署/配置下不可用，
// 动作据此走原有的降级分支。
package catalog

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// MemoryAccess 长期记忆的读写端口。
//
// 作用域以"种类 + 标识"表达（user / group 与对应的用户、会话 ID），
// 存储键的具体格式由装配侧决定。
type MemoryAccess interface {
	// Add 写入一条事实（与自动抽取共用去重合并与上限淘汰）。
	Add(scopeKind, scopeID, text string)
	// Count 返回指定作用域已保存的事实条数。
	Count(scopeKind, scopeID string) int
	// Remove 精确删除一条事实，返回是否命中。
	Remove(scopeKind, scopeID, text string) bool
}

// TodoItem 一条待办。
type TodoItem struct {
	// ID 会话内序号（T1、T2 …）。
	ID string
	// Text 待办内容。
	Text string
	// Done 是否已完成。
	Done bool
}

// TodoAccess 会话待办清单端口。
type TodoAccess interface {
	// Add 追加一条待办，返回生成的 ID。
	Add(chatID, text string) string
	// List 返回指定会话的全部待办（按添加顺序）。
	List(chatID string) []TodoItem
	// SetDone 更新完成状态，返回是否命中。
	SetDone(chatID, id string, done bool) bool
	// Remove 删除一条待办，返回是否命中。
	Remove(chatID, id string) bool
}

// ReminderItem 一条活跃提醒。
type ReminderItem struct {
	// ID 会话内序号（R1、R2 …）。
	ID string
	// Text 提醒内容。
	Text string
	// DueAt 到期时间。
	DueAt time.Time
}

// ReminderAccess 定时提醒的查询与取消端口。
type ReminderAccess interface {
	// List 返回指定会话的全部活跃提醒。
	List(chatID string) []ReminderItem
	// Remove 取消一条提醒，返回是否命中。
	Remove(chatID, id string) bool
}

// ReminderScheduler 定时提醒的创建端口。
//
// 与 [ReminderAccess] 分开是因为创建提醒需要捕获平台发送器用于到期推送，
// 而只读/取消不需要——发送器由装配侧在构造端口时持有，动作不接触原始
// 发送能力。发送器缺失时装配侧不注入本端口，动作据此返回"不可用"。
type ReminderScheduler interface {
	// Schedule 注册一条提醒到会话，返回面向用户的确认文本。
	Schedule(chat platform.ChatInfo, duration time.Duration, content string) string
}

// Capabilities 一次动作调用可用的能力端口集合。
type Capabilities struct {
	Memory    MemoryAccess
	Todos     TodoAccess
	Reminders ReminderAccess
	Scheduler ReminderScheduler
	// Files 工作区只读文件访问（workspace_roots 配置时由装配侧注入）。
	Files FileAccess
	// FileWrite 工作区写入（workspace_roots 且 workspace_allow_write 时注入）。
	FileWrite FileWriteAccess
	// Process 受控进程执行（workspace_roots 与 exec_allowlist 同时配置时注入）。
	Process ProcessRunner
}

// CapabilitiesFrom 返回本次调用的能力端口；未注入时返回零值与 false。
//
// 动作层应经本函数读取，不要直接调用 inv.Port[Capabilities]，这样端口键
// 只在一处定义。
func CapabilitiesFrom(inv *toolkit.Invocation) (Capabilities, bool) {
	return inv.Port[Capabilities]()
}

// AttachCapabilities 把能力端口挂到调用上下文（nil 视为未注入）。
func AttachCapabilities(inv *toolkit.Invocation, caps Capabilities) {
	inv.SetPort[Capabilities](caps)
}
