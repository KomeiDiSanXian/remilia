// pluginparts.go — Plugin 字段的职责分区。
//
// Plugin 是装配根。它的字段按"谁负责这项能力"分成五组，各组以匿名结构体
// 嵌入 Plugin：
//
//   - catalogState：工具/技能目录（发现、注册、内置工具构建、权限过滤）
//   - contextState：上下文供给（动态上下文、历史检索、长期记忆）
//   - executionState：动作执行（真实命令通道、审批闸门）
//   - runtimeState：回合运行时（消息入口、LLM 循环、会话与生命周期）
//   - adminState：管理与命令（子命令、群策略、用量、按钮、提醒、待办）
//
// 仍然直属 Plugin 的只有 cfg 与 prov：它们被几乎所有分组读写，是装配根自己
// 持有的共享依赖，不属于任何单一 owner。
//
// 方法同样按这条边界归位：只依赖单一 owner 字段（含该 owner 的其他方法）的
// 方法定义在该 owner 上，经嵌入提升后仍以 p.<方法名> 访问；只有需要跨 owner
// 编排（例如同时读 cfg 与某个 owner 的状态）的方法留在 Plugin 上。
//
// 嵌入而非命名持有，是为了让 p.reg / p.sm 这类既有访问继续可用——方法签名与
// 调用点无需改动，本文件只改变字段的物理归属。字段名在各组之间不得重复，
// 否则提升会二义（由 pluginparts_test.go 守卫）。
package ai

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/procexec"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/workspace"
	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	"github.com/KomeiDiSanXian/remilia/builtin/vevent"
)

// catalogState 工具/技能目录 owner 的字段。
type catalogState struct {
	// coord 命令协调器（engine.Reader），用于启动时发现已注册的命令。
	coord engine.Reader
	// reg 工具注册表，管理所有可供 LLM 调用的工具。
	// 职责边界：reg 只负责"登记与按名查找"这一兼容注册 API；目录的聚合、
	// 快照与代数（catalogGen）由本 owner 承担，Selector/Invoker 各自只读其一。
	reg *toolkit.ToolRegistry
	// catalogGen 保存目录版本、快照互斥与来源覆盖状态。保留原子代数接口，
	// 同时在其内部统一保护“版本 + 注册表动作”的快照边界。
	catalogGen catalogRuntime
	// skillReg 技能注册表，管理所有已注册的 Skill。
	skillReg *toolkit.SkillRegistry
	// perms RBAC 权限插件（用于工具级权限校验与按角色注入）。
	perms *permission.Plugin
	// cmdMu 保护 cmdPatterns 的并发读写（发现与执行并行）。
	cmdMu sync.RWMutex
	// cmdPatterns 工具名到完整命令模式的映射，用于 execution.RunCommand 构造合成事件。
	cmdPatterns map[string]string
	// files 工作区只读文件访问管理器（workspace_roots 配置时启用）。
	// 路径边界、符号链接与大小限制见 builtin/ai/workspace。
	files *workspace.Manager
	// proc 受控进程执行器（workspace_roots 与 exec_allowlist 同时配置时启用）。
	// 命令白名单、环境变量清洗与进程树回收见 builtin/ai/procexec。
	proc *procexec.Manager
}

// catalogRuntime 将目录代数与其聚合状态放在同一字段中，保证快照和变更
// 原子一致；嵌入 Uint64 保留 catalogGen.Add/Load 的兼容调用。
type catalogRuntime struct {
	atomic.Uint64
	mu                  sync.RWMutex
	baseTools           map[string]toolkit.Tool
	baseCommandPatterns map[string]string
	providers           []*toolProviderBinding
	winners             map[string]catalogWinner
}

// toolExecutionSnapshot 将一次工具调用解析出的定义、策略与命令模式绑定在一起。
// 它只在单次调用中传递，不持有目录锁；动态更新不改变已经完成解析的调用。
type toolExecutionSnapshot struct {
	tool           toolkit.Tool
	commandPattern string
	hasCommand     bool
	found          bool
}

func (s toolExecutionSnapshot) action() (toolkit.Action, bool) {
	if !s.found {
		return toolkit.Action{}, false
	}
	return toolkit.ActionOf(s.tool), true
}

// contextState 上下文 owner 的字段。
type contextState struct {
	// history 消息历史提供者（messagelog），用于回复上下文与群聊最近消息窗口。
	// 默认 messagelog.Default()；messagelog 未启用时查询为空，相关功能自动降级为 no-op。
	history *messagelog.Logger
	// emb 文本向量缓存（embedding_base_url 配置时启用）。
	// 工具选择与记忆检索共用；为 nil 时两者均退化为纯关键词打分。
	emb *retrieval.TextVectorCache
	// memory 长期事实记忆（memory_enabled 配置时启用）。
	// LevelDB 持久化（data/ai_memory）；store 为 nil 时功能关闭。
	memory *memoryStore
}

// executionState 执行 owner 的字段。
type executionState struct {
	// syncer 事件处理器（vevent），用于 execution.RunCommand 合成事件触发真实命令。
	syncer vevent.EventProcessor
	// realCmdMu 并行工具执行时真实命令路径（syncer）的串行化互斥。
	realCmdMu sync.Mutex
	// approvals 命令执行审批管理器（tool_approval）。实现见 builtin/ai/execution。
	approvals *execution.ApprovalManager
}

// runtimeState 回合运行时 owner 的字段。
type runtimeState struct {
	// sm 会话管理器，负责会话的 LRU 缓存、持久化、过期清理。
	sm *session.SessionManager
	// tasks 任务记录存储：会话计划在计划动作的调用点镜像为持久任务
	// （见 tasks.go）。storage 插件可用时为 GORM（ai_tasks 表），
	// 否则为进程内存；未装配时为零值，访问一律经 taskStore() 退化。
	tasks task.Store
	// taskMu 串行化"读取活动任务 → 刷新 → 保存"的镜像过程：计划动作可并行
	// 执行（tool_parallel），不加锁会让较旧的计划快照后写、回退已完成步骤。
	taskMu sync.Mutex
	// execEvidence 会话最近一次进程执行结果（进程内）。
	// 用于计划完成闸门：最近一次验证命令失败时拒绝把计划标记为全部完成。
	execEvidence map[string]execEvidence
	// evidenceMu 保护 execEvidence。
	evidenceMu sync.Mutex
	// triggerCmd 触发命令前缀（如 "/ai"），用于 cleanMessage 时剥离。
	triggerCmd string
	// defOnce / def 缓存触发命令定义（buildAIDefinition 构建全部子命令）。
	// 注册触发命令 matcher 与判定"正文是否已由该 matcher 接管"（triggerParses）
	// 必须使用同一份定义，且该定义逐消息重建不划算。
	defOnce sync.Once
	def     *command.Definition
	// lifecycleCtx 插件生命周期上下文，插件关闭时取消，用于替代 context.Background()。
	lifecycleCtx context.Context
	// lifecycleCancel 取消 lifecycleCtx 的函数。
	lifecycleCancel context.CancelFunc
}

// adminState 管理与命令 owner 的字段。
type adminState struct {
	// fsmEngine 内置 FSM 引擎，用于技能注册等两步对话流程。
	fsmEngine *fsm.Engine
	// reminders 定时提醒管理器（/ai remind）。
	// 依赖 plugin.SessionNotifier 主动推送；进程内存储，重启后失效。
	reminders *reminderManager
	// todos 会话内待办清单管理器（todo_* 工具）。
	// 进程内存储，重启后失效。
	todos *todoManager
	// groupPolicies per-group 工具策略/提示词（/ai group）。
	// LevelDB 持久化（data/ai）；store 为 nil 时纯内存（测试场景）。
	groupPolicies *groupPolicyManager
	// summaryMu / summaries 防止同一会话重复触发 /ai summary 产生无界后台 goroutine。
	summaryMu sync.Mutex
	summaries map[string]bool
	// actionMu / actionRate 记录同一会话操作按钮（"重新生成"/"清空会话"）
	// 与相应文本命令（/ai retry、/ai reset）的限流状态（触发冷却 + 忙时提示
	// 节流，见 qqaction.go）。键为 action + sessionID。惰性初始化并按需清理。
	actionMu   sync.Mutex
	actionRate map[string]qqActionRateState
	// replyGate 群自主发言接话策略（group_reply_policy=smart）的运行时状态：
	// 概率器等（信号数据本身实时取自 messagelog）。见 replygate.go。
	replyGate *replyGateState
}
