// agentstate_test.go — 代理状态归属表的守卫用例。
//
// 这些断言把"哪些状态属于代理、其中哪些跨重启保留"钉成可执行契约：
// 归属表与结构体字段一一对应，落库字段并集恰为持久化 schema，
// 会话内存态不进入序列化，且并发原语豁免名单不得夹带业务状态。
package ai

import (
	"reflect"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 代理状态（Agent State）的逻辑归属。
//
// 这份清单只服务于本文件的断言，因此放在测试里：它不参与任何运行时逻辑，
// 放在生产包会让读者误以为运行时有"状态归属表"。
//
// 关键约定：**逻辑归属 != 持久化 schema**。
// 状态"属于代理"不代表它必须落库；反过来，落库字段也不等于全部代理状态。
// 承载字段的 `json:"-"` 标签与 SessionRecord 的序列化共同决定持久化边界，
// 两者都按"行为变更"对待——改动需先在进度文档裁决，不随重构顺带调整。
//
// 覆盖是穷尽且分层的：
//
//   - 承载字段必须真实存在，分类（Class）与承载形态必须一致；
//   - Session 覆盖**穷尽**：Session 上每个字段要么被清单登记，要么落在并发
//     原语豁免名单（本文件的 agentStateInfrastructure），新增会话状态漏登记会被拦下；
//   - Record 覆盖**精确**：落库字段的并集必须恰为 SessionRecord 的字段集合；
//   - Plugin 覆盖同样**穷尽**：Plugin（含五个 owner 嵌入结构）上的每个字段
//     要么登记为进程级代理状态，要么落在非代理字段名单
//     （本文件的 agentStatePluginNonAgent，按用途分组），新增字段漏分类会被拦下。
//
// "非代理字段"不等于无关紧要：服务句柄（如 sm/reg/history）本身就是清单里
// 会话状态的容器或外部依赖，装配管道（如 def/lifecycleCtx）与静态配置
// （如 cfg）也不随对话演进——它们不进归属表，是刻意的范围界定而非遗漏。

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
	// agentStateProcessPersisted 存活于插件进程级管理器，但自带持久化：
	// 跨重启保留，且不随单个会话记录落库（与 agentStatePersisted 的区别在
	// 于后者的载体是 SessionRecord）。
	agentStateProcessPersisted
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
			Session: []string{"signal"},
			Note:    "回合活跃状态与中断信号由同一原子指针承载：用户抢占/停止生成",
		},
		{
			Name: "session_deleted", Class: agentStateSessionMemory,
			Session: []string{"deleted"},
			Note:    "删除墓碑：Delete 后在途回合收尾不再把会话写回存储",
		},
		{
			Name: "session_load_failure", Class: agentStateSessionMemory,
			Session: []string{"loadFailed"},
			Note: "加载失败降级标记：存储读失败后落库前必须先恢复并合并旧历史，" +
				"否则整记录 upsert 会把库里那份历史写成空",
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
		{
			Name: "approval", Class: agentStateProcessMemory,
			Plugin: []string{"approvals"},
			Note:   "待用户按钮/文本应答的动作审批请求：按会话索引，超时按拒绝处理，重启即丢",
		},
		{
			Name: "long_term_memory", Class: agentStateProcessPersisted,
			Plugin: []string{"memory"},
			Note:   "长期事实记忆：LevelDB（data/ai_memory）自带持久化，不随 SessionRecord 落库",
		},
		{
			Name: "task", Class: agentStateProcessPersisted,
			Plugin: []string{"tasks"},
			Note:   "任务记录：storage 可用时为 GORM（ai_tasks 表）自带持久化，否则为进程内存；不随 SessionRecord 落库",
		},
		{
			Name: "exec_evidence", Class: agentStateProcessMemory,
			Plugin: []string{"execEvidence"},
			Note:   "最近一次进程执行结果（会话级，进程内）：计划完成闸门的验证证据，重启后重置",
		},
		{
			Name: "task_usage", Class: agentStateProcessMemory,
			Plugin: []string{"taskUsage"},
			Note:   "任务预算计数（会话级，进程内）：工具调用次数与起始时间，超限时任务失败",
		},
		{
			Name: "group_policy", Class: agentStateProcessPersisted,
			Plugin: []string{"groupPolicies"},
			Note:   "per-group 工具策略/提示词：LevelDB（data/ai）自带持久化，属进程级配置态",
		},
	}
}

// agentStateSessionField 取 Session 上的字段描述，缺失即失败。
func agentStateSessionField(t *testing.T, name string) reflect.StructField {
	t.Helper()
	field, ok := reflect.TypeFor[session.Session]().FieldByName(name)
	require.Truef(t, ok, "Session has no field %q", name)
	return field
}

// agentStateCarriers 按承载结构聚合归属表里声明的字段名。
func agentStateCarriers() (sess, record, plugin map[string]string) {
	sess = map[string]string{}
	record = map[string]string{}
	plugin = map[string]string{}
	for _, item := range agentStateInventory() {
		for _, name := range item.Session {
			sess[name] = item.Name
		}
		for _, name := range item.Record {
			record[name] = item.Name
		}
		for _, name := range item.Plugin {
			plugin[name] = item.Name
		}
	}
	return sess, record, plugin
}

// TestAgentStateInventoryCarriersExist 归属表引用的承载字段必须真实存在，
// 且每一项都要有业务语义名、归属说明与至少一个承载字段。
func TestAgentStateInventoryCarriersExist(t *testing.T) {
	sessionType := reflect.TypeFor[session.Session]()
	recordType := reflect.TypeFor[session.Record]()
	pluginType := reflect.TypeFor[Plugin]()

	for _, item := range agentStateInventory() {
		require.NotEmptyf(t, item.Name, "state entry must carry a business name")
		require.NotEmptyf(t, item.Note, "state %q must explain its ownership", item.Name)
		assert.NotEmptyf(t, len(item.Session)+len(item.Record)+len(item.Plugin),
			"state %q must declare at least one carrier field", item.Name)

		for _, name := range item.Session {
			_, ok := sessionType.FieldByName(name)
			assert.Truef(t, ok, "state %q references missing Session field %q", item.Name, name)
		}
		for _, name := range item.Record {
			_, ok := recordType.FieldByName(name)
			assert.Truef(t, ok, "state %q references missing Record field %q", item.Name, name)
		}
		for _, name := range item.Plugin {
			_, ok := pluginType.FieldByName(name)
			assert.Truef(t, ok, "state %q references missing Plugin field %q", item.Name, name)
		}
	}
}

// TestAgentStateClassesMatchCarriers 分类必须与承载形态一致：落库状态要有
// Record 承载，会话内存态只挂 Session，进程级状态只挂 Plugin。
func TestAgentStateClassesMatchCarriers(t *testing.T) {
	for _, item := range agentStateInventory() {
		switch item.Class {
		case agentStatePersisted:
			assert.NotEmptyf(t, item.Record, "persisted state %q must name a record carrier", item.Name)
			assert.Emptyf(t, item.Plugin, "persisted state %q must not live on Plugin", item.Name)
		case agentStateSessionMemory:
			assert.NotEmptyf(t, item.Session, "session state %q must name Session carriers", item.Name)
			assert.Emptyf(t, item.Record, "session state %q must not be persisted", item.Name)
			assert.Emptyf(t, item.Plugin, "session state %q must not live on Plugin", item.Name)
		case agentStateProcessMemory:
			assert.NotEmptyf(t, item.Plugin, "process state %q must name Plugin carriers", item.Name)
			assert.Emptyf(t, item.Session, "process state %q must not live on Session", item.Name)
			assert.Emptyf(t, item.Record, "process state %q must not be persisted", item.Name)
		case agentStateProcessPersisted:
			assert.NotEmptyf(t, item.Plugin, "process-persisted state %q must name Plugin carriers", item.Name)
			assert.Emptyf(t, item.Session, "process-persisted state %q must not live on Session", item.Name)
			assert.Emptyf(t, item.Record, "process-persisted state %q must not live on SessionRecord", item.Name)
		default:
			t.Errorf("state %q has unknown class %d", item.Name, item.Class)
		}
	}
}

// TestAgentStateInventoryUnique 一项业务状态只登记一次，一个承载字段只归属一项。
func TestAgentStateInventoryUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, item := range agentStateInventory() {
		assert.Falsef(t, seen[item.Name], "duplicate state entry %q", item.Name)
		seen[item.Name] = true
	}

	sess, record, plugin := agentStateCarriers()
	assertCarrierOwnersUnique(t, "Session", sess, agentStateInventory(), func(i agentStateItem) []string { return i.Session })
	assertCarrierOwnersUnique(t, "Record", record, agentStateInventory(), func(i agentStateItem) []string { return i.Record })
	assertCarrierOwnersUnique(t, "Plugin", plugin, agentStateInventory(), func(i agentStateItem) []string { return i.Plugin })
}

// assertCarrierOwnersUnique 校验同一承载结构上的字段不被多项状态重复声明。
func assertCarrierOwnersUnique(t *testing.T, carrier string, owners map[string]string, items []agentStateItem, fields func(agentStateItem) []string) {
	t.Helper()
	count := map[string]int{}
	for _, item := range items {
		for _, name := range fields(item) {
			count[name]++
		}
	}
	for name, n := range count {
		assert.Equalf(t, 1, n, "%s field %q is claimed by %d states (owner %q)", carrier, name, n, owners[name])
	}
}

// TestAgentStatePersistedCoversRecordSchema 落库状态的 Record 并集必须恰为持久化
// schema：归属表即持久化边界的唯一事实来源，新增列却忘记登记会被拦下。
func TestAgentStatePersistedCoversRecordSchema(t *testing.T) {
	want := map[string]bool{}
	typ := reflect.TypeFor[session.Record]()
	for field := range typ.Fields() {
		want[field.Name] = true
	}

	got := map[string]bool{}
	for _, item := range agentStateInventory() {
		if item.Class != agentStatePersisted {
			continue
		}
		for _, name := range item.Record {
			got[name] = true
		}
	}

	assert.Equalf(t, want, got, "persisted carriers must match the Session record schema exactly")
}

// agentStateInfrastructure Session 上只做并发保护、不承载业务状态的字段。
// 归属表覆盖的是业务状态；这些字段在此显式豁免，且必须保持为纯同步原语
// （守卫用例会校验类型），避免用豁免名单夹带真实状态。
//
// 豁免名单只服务于下面这条守卫用例，因此与用例同址，不进入生产代码。
func agentStateInfrastructure() []string {
	return []string{"mu", "turnMu"}
}

// agentStatePluginNonAgent Plugin 上明确不属于"代理状态"的字段，按用途分组。
//
// 归属表登记的是随对话演进的状态；本名单登记的是装配管道、服务句柄、静态
// 配置与后台守卫。Plugin 覆盖守卫（TestAgentStateCoversPluginState）要求
// Plugin 的每个字段二选一落入这两处，因此新增字段时必须显式归类，不会静默溜过。
//
// 与 agentStateInfrastructure 一样，它只服务于下面这条守卫用例，因此与用例
// 同址，不进入生产代码。
func agentStatePluginNonAgent() map[string]string {
	return map[string]string{
		// 静态配置与服务句柄（装配期注入，不随对话演进）。
		"cfg":              "插件配置（config.yaml）",
		"prov":             "LLM 提供商实例",
		"coord":            "命令协调器句柄（engine.Reader）",
		"reg":              "工具注册表",
		"skillReg":         "技能注册表",
		"perms":            "RBAC 权限插件句柄",
		"history":          "消息历史服务（messagelog）",
		"emb":              "文本向量缓存（嵌入服务）",
		"syncer":           "事件处理器（vevent）",
		"sm":               "会话管理器：清单里的会话状态由它承载",
		"fsmEngine":        "FSM 引擎句柄",
		"files":            "工作区文件访问管理器（由 workspace_roots 配置构造，静态服务句柄）",
		"proc":             "受控进程执行器（由 workspace_roots 与 exec_allowlist 构造，静态服务句柄）",
		"remote":           "远程执行节点客户端（exec_node=remote 时装配，静态服务句柄）",
		"attachmentClient": "附件下载客户端（受信来源 = 配置名单 + 平台自声明，静态服务句柄）",
		"mediaSource":      "平台媒体来源的只读视图（通过 WithMediaOriginSource 注入，Setup 期读取媒体来源）",
		"dataDir":          "插件自有持久化数据的根目录（WithDataDir 注入，默认 data）",
		// 装配管道与并发原语。
		"cmdMu":             "cmdPatterns 的读写锁",
		"cmdPatterns":       "工具名 → 命令模式映射（发现结果）",
		"catalogGen":        "目录内容版本与快照聚合状态（进程级，随注册演进）",
		"realCmdMu":         "真实命令通道的串行化互斥",
		"defOnce":           "触发命令定义的懒初始化",
		"def":               "触发命令定义（由 cfg 派生）",
		"registeredTrigger": "实际注册了触发命令 matcher 的前缀（空 = 未注册）",
		"lifecycleCtx":      "插件生命周期上下文",
		"lifecycleCancel":   "生命周期上下文的取消函数",
		// 后台任务守卫与限流（去重/节流，不是业务状态）。
		"summaryMu":   "summaries 的互斥锁",
		"summaries":   "同一会话摘要任务的在途去重集合",
		"actionMu":    "actionRate 的互斥锁",
		"actionRate":  "按钮/命令动作的限流状态",
		"replyGate":   "群自主发言接话策略的概率器（smart 档），信号数据实时取自 messagelog",
		"taskMu":      "任务记录同步的串行化互斥（不承载业务状态）",
		"evidenceMu":  "execEvidence 的互斥锁",
		"taskUsageMu": "taskUsage 的互斥锁",
	}
}

// TestAgentStateCoversSessionState Session 上每个字段要么被归属表覆盖，要么落
// 在并发原语豁免名单里；豁免名单只允许出现纯同步原语，避免夹带业务状态。
func TestAgentStateCoversSessionState(t *testing.T) {
	covered, _, _ := agentStateCarriers()

	exempt := map[string]bool{}
	for _, name := range agentStateInfrastructure() {
		exempt[name] = true
		field := agentStateSessionField(t, name)
		assert.Equalf(t, reflect.TypeFor[sync.Mutex](), field.Type,
			"infrastructure exemption %q may only cover a sync.Mutex, got %s", name, field.Type)
	}

	typ := reflect.TypeFor[session.Session]()
	for field := range typ.Fields() {
		name := field.Name
		if covered[name] != "" || exempt[name] {
			continue
		}
		t.Errorf("Session field %q is neither registered agent state nor a concurrency primitive", name)
	}
}

// pluginFieldNames 递归收集 Plugin 上的全部字段名（含匿名嵌入的 owner 结构）。
// Plugin 直属的 cfg/prov 与五个 owner 结构的字段都会出现，守卫用例据此要求
// 每个字段要么登记为代理状态，要么落在非代理名单里。
func pluginFieldNames() []string {
	var names []string
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for field := range t.Fields() {
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				walk(field.Type)
				continue
			}
			names = append(names, field.Name)
		}
	}
	walk(reflect.TypeFor[Plugin]())
	return names
}

// TestAgentStateCoversPluginState Plugin（含 owner 嵌入结构）上每个字段要么被
// 归属表登记为进程级代理状态，要么落在 agentStatePluginNonAgent 名单里；
// 新增字段漏分类会被拦下。名单本身也不得夹带已登记的字段或虚构字段。
func TestAgentStateCoversPluginState(t *testing.T) {
	_, _, covered := agentStateCarriers()
	exempt := agentStatePluginNonAgent()

	pluginType := reflect.TypeFor[Plugin]()
	for _, name := range pluginFieldNames() {
		if covered[name] != "" || exempt[name] != "" {
			continue
		}
		t.Errorf("Plugin field %q is neither registered agent state nor a declared non-agent field", name)
	}

	for name, reason := range exempt {
		assert.NotEmptyf(t, reason, "non-agent field %q must explain why it is exempt", name)
		assert.Emptyf(t, covered[name], "field %q is both registered agent state and declared non-agent", name)
		_, ok := pluginType.FieldByName(name)
		assert.Truef(t, ok, "non-agent list references missing Plugin field %q", name)
	}
}

// TestAgentStateNonPersistedFieldsStayHiddenFromJSON 非落库的 Session 字段不得
// 进入 JSON：必须是未导出字段，或显式标注 json:"-"。
func TestAgentStateNonPersistedFieldsStayHiddenFromJSON(t *testing.T) {
	persisted := map[string]bool{}
	for _, item := range agentStateInventory() {
		if item.Class != agentStatePersisted {
			continue
		}
		for _, name := range item.Session {
			persisted[name] = true
		}
	}

	typ := reflect.TypeFor[session.Session]()
	for field := range typ.Fields() {
		if persisted[field.Name] || !field.IsExported() {
			continue
		}
		assert.Equalf(t, "-", field.Tag.Get("json"),
			"non-persisted Session field %q must be tagged json:\"-\"", field.Name)
	}
}

// TestAgentStatePersistenceRoundTrip 落库状态跨重启逐项还原，
