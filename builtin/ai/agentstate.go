// Package ai agentstate.go — 代理状态（Agent State）的逻辑归属。
//
// 本文件不引入新的存储，也不改变任何持久化语义。它把"一次会话里属于代理
// 状态的东西"集中登记出来，回答两个问题：
//
//  1. 这项状态是什么（业务语义名）；
//  2. 它跨进程重启是否保留（随会话落库 / 仅本进程内存 / 进程级管理器）。
//
// 关键约定：**逻辑归属 ≠ 持久化 schema**。
// 状态"属于代理"不代表它必须落库；反过来，落库字段也不等于全部代理状态。
// 承载字段的 `json:"-"` 标签与 SessionRecord 的序列化共同决定持久化边界，
// 两者都按"行为变更"对待——改动需先在进度文档裁决，不随重构顺带调整。
//
// 清单由 agentstate_test.go 校验：声明为落库的字段必须真实存在于 SessionRecord，
// 分类与承载形态必须一致；落库字段的并集必须恰为 SessionRecord 的字段集合；
// 且 Session 上每个业务字段都必须被清单覆盖（新增状态忘记登记会被测试拦下，
// 纯并发原语除外）。
package ai

// agentStateClass 代理状态的持久化归属。
type agentStateClass uint8

const (
	// agentStatePersisted 随会话落库：进程重启后由 Record 还原。
	agentStatePersisted agentStateClass = iota
	// agentStateSessionMemory 仅存活于本进程的会话对象：重启后重置。
	agentStateSessionMemory
	// agentStateProcessMemory 存活于插件进程级管理器：重启后重置，
	// 且按会话/作用域索引（不属于单个 Session 对象）。
	agentStateProcessMemory
)

// agentStateItem 一项代理状态及其归属。
type agentStateItem struct {
	// Name 业务语义名（与工具/命令里的说法一致）。
	Name string
	// Class 持久化归属。
	Class agentStateClass
	// Session Session 上的承载字段（会话级状态；无则空）。
	Session []string
	// Record Record 上的承载字段（落库状态；无则空）。
	Record []string
	// Plugin Plugin 上的承载字段（进程级状态；无则空）。
	Plugin []string
	// Note 归属说明。
	Note string
}

// agentStateInventory 代理状态清单：逻辑归属与持久化边界的唯一对照表。
func agentStateInventory() []agentStateItem {
	return []agentStateItem{
		{
			Name: "conversation", Class: agentStatePersisted,
			Session: []string{"ID", "UserID", "ChatID", "Messages", "CreatedAt", "UpdatedAt", "CallCount", "ToolCount"},
			Record:  []string{"ID", "UserID", "ChatID", "Messages", "CreatedAt", "UpdatedAt", "CallCount", "ToolCount"},
			Note:    "会话主体：对话历史与计数；附件二进制不落库，重启后降级为文本占位",
		},
		{
			Name: "plan", Class: agentStatePersisted,
			Session: []string{"plan"}, Record: []string{"Plan"},
			Note: "任务计划：跨重启继续执行；内存态由 plan.go 的访问器读写",
		},
		{
			Name: "pending_image", Class: agentStatePersisted,
			Session: []string{"pendingImage"}, Record: []string{"PendingImages"},
			Note: "先发图再发字的未消费图片引用：窗口内跨重启仍可合并",
		},
		{
			Name: "interrupt", Class: agentStateSessionMemory,
			Session: []string{"turnActive", "interruptCh", "interruptOne"},
			Note:    "回合活跃标志与中断信号：用户抢占/停止生成",
		},
		{
			Name: "tool_set_stability", Class: agentStateSessionMemory,
			Session: []string{"toolSet"},
			Note:    "工具集稳定状态（sticky 并集与闲置衰减），抑制 ToolSet 抖动",
		},
		{
			Name: "action_selection_cache", Class: agentStateSessionMemory,
			Session: []string{"selCache"},
			Note:    "动作选择缓存：关键词相似或 TTL 内复用上次选择",
		},
		{
			Name: "history_retrieval_cache", Class: agentStateSessionMemory,
			Session: []string{"ragCache"},
			Note:    "历史消息检索缓存：命中即零检索",
		},
		{
			Name: "action_failures", Class: agentStateSessionMemory,
			Session: []string{"toolFailures"},
			Note:    "各动作连续失败次数：重试预算与反思引导，成功后归零",
		},
		{
			Name: "action_trace", Class: agentStateSessionMemory,
			Session: []string{"trace"},
			Note:    "动作调用追踪：诊断用途",
		},
		{
			Name: "plan_auto", Class: agentStateSessionMemory,
			Session: []string{"planAutoRounds", "planAutoStopped"},
			Note:    "计划后台自动推进的轮次与停止标记",
		},
		{
			Name: "image_overflow_notified", Class: agentStateSessionMemory,
			Session: []string{"imageOverflowNotified"},
			Note:    "本次会话是否已提示过图片数量超限",
		},
		{
			Name: "attachment_cache", Class: agentStateSessionMemory,
			Session: []string{"contentCache"},
			Note:    "附件二进制内存缓存（按 URL key），仅本次会话有效",
		},
		{
			Name: "todo", Class: agentStateProcessMemory,
			Plugin: []string{"todos"},
			Note:   "待办：进程级管理器按 chatID 索引，当前不落库（重启即丢）",
		},
		{
			Name: "reminder", Class: agentStateProcessMemory,
			Plugin: []string{"reminders"},
			Note:   "提醒：进程级管理器按 chatID 索引，当前不落库（重启即丢）",
		},
	}
}
