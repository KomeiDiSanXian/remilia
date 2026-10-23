// tool.go — 工具系统：类型定义、注册表与扩展接口。
//
// 本文件包含：
//   - Tool：LLM 可调用的工具类型定义
//   - ToolRegistry：线程安全的工具注册表
//   - ToolProvider / SkillProvider：插件注册工具的接口
//   - WithCallerInfo / Authorizer：调用者身份与调用授权的 context 注入
//
// context 槽位约定：只有必须跨 `Execute(ctx context.Context, ...)` 这类
// 标准库接口边界的**调用期能力**才放 std context（调用者、授权、发送、
// 目标解析、工具来源、计划访问、能力集、技能调用栈）；事件级服务与状态
// 留在 *core/context.Context 的扩展里。同一类信息共用一个槽位（授权器与
// 调用者身份共用 caller-info 槽），不要为每个 setter 各开一个键。
//
// 协议格式（参数 Schema、工具声明序列化与调用解析）见 builtin/ai/protocol。
package toolkit

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ctxKeyCallerInfo 是 context 中存储工具调用者信息的键。
//
// 同一槽位也保存调用授权器（Authorizer）：两者都是"这次工具调用以谁的
// 身份、能做哪些事"，成对出现；合并存放避免为每个能力再开一个 context 槽。
type ctxKeyCallerInfoType struct{}

// callerState 是 caller-info 槽位的值。
type callerState struct {
	user       platform.UserInfo
	authorizer Authorizer
}

// WithCallerInfo 将调用者信息注入 context，供工具 Execute 回调进行权限校验。
func WithCallerInfo(ctx context.Context, caller platform.UserInfo) context.Context {
	state, _ := ctx.Value(ctxKeyCallerInfoType{}).(callerState)
	state.user = caller
	return context.WithValue(ctx, ctxKeyCallerInfoType{}, state)
}

// CallerInfoFromContext 从 context 中提取工具调用者信息。
// 若 context 中无调用者信息，返回零值 UserInfo 和 false。
func CallerInfoFromContext(ctx context.Context) (platform.UserInfo, bool) {
	state, ok := ctx.Value(ctxKeyCallerInfoType{}).(callerState)
	return state.user, ok
}

// Authorizer 判断当前调用者能否在技能内部执行某个工具。
//
// 技能内部的工具循环拿不到事件上下文（只有 std context），无法直接做 RBAC
// 与人工审批；装配侧按事件上下文构造授权器，随 caller-info 槽位传播：
// 注入阶段按它过滤工具列表，执行阶段再复核一次。
//
// 返回 (false, reason) 时 reason 会作为工具错误回填给模型。
type Authorizer interface {
	CanUse(tool Tool) (bool, string)
}

// AuthorizerFunc 把函数适配为 Authorizer。
type AuthorizerFunc func(tool Tool) (bool, string)

// CanUse 实现 Authorizer。
func (f AuthorizerFunc) CanUse(tool Tool) (bool, string) { return f(tool) }

// WithAuthorizer 将授权器注入 context（与调用者信息共用一个槽位）。
func WithAuthorizer(ctx context.Context, a Authorizer) context.Context {
	state, _ := ctx.Value(ctxKeyCallerInfoType{}).(callerState)
	state.authorizer = a
	return context.WithValue(ctx, ctxKeyCallerInfoType{}, state)
}

// AuthorizerFromContext 从 context 中提取授权器。
func AuthorizerFromContext(ctx context.Context) (Authorizer, bool) {
	state, ok := ctx.Value(ctxKeyCallerInfoType{}).(callerState)
	if !ok || state.authorizer == nil {
		return nil, false
	}
	return state.authorizer, true
}

// ctxKeyToolSender 是 context 中存储工具消息发送能力的键。
type ctxKeyToolSenderType struct{}

// TargetResolver 工具可用的目标解析能力（只读，无副作用）。
//
// 与 [ToolSender.SendTo] 分开声明：解析不发送、不需要审批，供需要把模型给出的
// 人名/别名/原始 ID 转成平台 ID 的工具（send_to 的目标、send_message 的 @ 提及）
// 复用同一套规则。实现方负责按当前调用者与会话上下文解析，并拒绝无法唯一
// 确定的目标（返回候选列表）。
//
// [ToolSender] 上的 ResolveTarget 是同一能力的旧位置（send_to 的审批预览仍
// 直接使用它）；动作层一律经本接口消费解析能力，不再依赖 ToolSender 的额外
// 方法——注入由执行路径的适配器统一完成（见 builtin/ai/mentions.go）。
type TargetResolver interface {
	// Resolve 解析目标（内置别名/本会话近期发言者昵称/已加入群群名/原始 ID），
	// 返回 (目标, 展示文本, 错误)。isGroup 提示仅在按原始 ID 兜底时生效。
	Resolve(ctx context.Context, raw string, isGroup bool) (ChatTarget, string, error)
}

// WithTargetResolver 将目标解析能力注入 context，供工具 Execute 回调使用。
func WithTargetResolver(ctx context.Context, r TargetResolver) context.Context {
	return context.WithValue(ctx, ctxKeyTargetResolverType{}, r)
}

// TargetResolverFromContext 从 context 中提取目标解析能力。
// 若 context 中无解析能力，返回 nil 和 false。
func TargetResolverFromContext(ctx context.Context) (TargetResolver, bool) {
	r, ok := ctx.Value(ctxKeyTargetResolverType{}).(TargetResolver)
	return r, ok
}

// ctxKeyTargetResolverType 是 context 中存储目标解析能力的键。
type ctxKeyTargetResolverType struct{}

// ToolSender 工具可用的消息发送能力。
//
// 由 AI 插件在工具执行前注入工具调用的 context（见 [WithToolSender]），
// 实现方通过 [ToolSenderFromContext] 提取后即可发送消息：
//   - [ToolSender.ReplyToChat] 向当前会话发送消息（无需审批）
//   - [ToolSender.SendTo] 向指定用户/群发送消息（仅审批通过后被注入）
type ToolSender interface {
	// ReplyToChat 向当前会话发送一条消息，返回平台发送结果。
	ReplyToChat(ctx context.Context, msg platform.OutboundMessage) (platform.SendResult, error)
	// SendTo 向指定用户/群发送一条消息，返回平台发送结果。
	// 仅当该工具调用通过了审批门（sendToAllowed）时可用，否则返回错误。
	SendTo(ctx context.Context, target ChatTarget, msg platform.OutboundMessage) (platform.SendResult, error)
	// ResolveTarget 将目标（内置别名/近期发言者昵称/已加入群群名/原始 ID）
	// 自动解析为 ChatTarget，返回 (目标, 展示文本, 错误)。
	// isGroup 提示仅在按原始 ID 兜底时生效；只读操作，不受审批门控。
	ResolveTarget(ctx context.Context, raw string, isGroup bool) (ChatTarget, string, error)
}

// ChatTarget 消息发送的目标（用户私聊或群聊）。
type ChatTarget struct {
	// ID 目标用户/群 ID（平台内唯一标识符）。
	ID string
	// IsGroup 是否为群聊目标。
	IsGroup bool
}

// WithToolSender 将消息发送能力注入 context，供工具 Execute 回调使用。
func WithToolSender(ctx context.Context, s ToolSender) context.Context {
	return context.WithValue(ctx, ctxKeyToolSenderType{}, s)
}

// ToolSenderFromContext 从 context 中提取消息发送能力。
// 若 context 中无发送能力，返回 nil 和 false。
func ToolSenderFromContext(ctx context.Context) (ToolSender, bool) {
	s, ok := ctx.Value(ctxKeyToolSenderType{}).(ToolSender)
	return s, ok
}

const CategoryGeneral = "general"

// Tool 描述一个可供 AI 调用的工具。
//
// 字段说明：
//   - Name: 工具名称，LLM 通过此名称调用工具。需唯一，建议使用 snake_case
//   - Description: 工具描述，LLM 据此决定是否调用。应清晰说明工具能力和使用场景
//   - Categories: 工具所属类别列表。一个工具可属于多个类别（如 ["space","science"]）。
//     空切片视为 ["general"]。
//   - Selection: 工具在选择中的保留级别；nil（零值）表示按 Categories 派生
//     （空或含 general → SelectionBaseline，否则 SelectionOptional）。
//     用 new(SelectionMandatory) 显式声明可让工具在任何选择策略下都不被淘汰。
//   - Parameters: JSON Schema 格式的参数描述，LLM 据此生成调用参数
//   - RequiresApproval: 标记该工具需要人工审批后才执行（配合 tool_approval=restricted
//     模式使用；always 模式下所有工具都审批）。显式注册的敏感工具（如文件操作、
//     命令执行）建议标记为 true。
//   - GrantsSendTo: 审批通过后授予 SendTo（向其他会话推送）能力。仅 send_to
//     这类动作应声明，避免其他工具借用一次审批获得越会话发送能力。
//   - Execute: 工具执行回调，接收 context 和参数，返回结果文本或错误
//
// Execute 的调用方通常已通过 [WithCallerInfo] 注入了调用者身份，
// 实现方可通过 [CallerInfoFromContext] 提取调用者信息进行权限校验。
type Tool struct {
	Name        string
	Description string
	Categories  []string // "general"、"space"、"weather"、"admin" 等
	// Source 动作来源命名空间（见 identity.go）。空值等价于 SourceBuiltin；
	// MCP 等外部来源使用 "mcp:<server>"，用于身份、去重与路由。
	Source ActionSource
	// Selection 工具在选择中的保留级别；nil（零值）表示按 Categories 派生
	// （空或含 general → SelectionBaseline，否则 SelectionOptional）。
	// 用 new(SelectionMandatory) 显式声明（Go 1.26+ 的 new(表达式) 形式）。
	Selection  *SelectionClass
	Parameters protocol.ToolParamSchema
	// RequiresApproval 工具执行前是否需要用户审批（tool_approval=restricted 时生效）。
	RequiresApproval bool
	// AlwaysRequireApproval 无论 tool_approval 模式（含 off）都强制审批。
	// 适用于 send_to 这类影响其他会话的高风险工具——off 模式下
	// RequiresApproval 会被豁免，本字段确保审批不可关闭。
	AlwaysRequireApproval bool
	// GrantsSendTo 审批通过后是否授予 SendTo（向其他会话推送）能力。
	// 与 AlwaysRequireApproval 正交：前者决定"何时必须审批"，本字段决定
	// "审批解锁什么"。只有 send_to 这类动作应声明，避免 always 审批模式下
	// 任意工具借用一次审批就获得越会话发送能力。
	GrantsSendTo bool
	// Permissions 工具执行所需的 RBAC 权限列表（如 "bilibili.manage"）。
	// 非空时 executeToolResult 在调用前**强制校验**调用者权限（任一命中即放行），
	// 不依赖插件自觉——权限不足返回可读错误、不执行。
	// 支持格式与框架命令一致：resource.action / resource:action / resource。
	Permissions []string
	Execute     func(ctx context.Context, args map[string]any) (string, error)
	// ExecuteRich 可选：返回富结果（文本 + 多模态片段）。非空时优先于 Execute，
	// 供需要携带图片/音频/资源/结构化数据的工具（如外部工具服务器）使用；
	// 既有工具只实现 Execute 即可，行为不变。
	ExecuteRich func(ctx context.Context, args map[string]any) (ToolResult, error)
}

// SkillProvider 插件可通过实现此接口向 AI 插件注册自定义 Skill。
//
// 使用示例：
//
//	if aiSvc, ok := ctx.TryService[*ai.Plugin]("ai"); ok {
//	    aiSvc.RegisterSkillProvider(mySkillProvider)
//	}
type SkillProvider interface {
	ListSkills() []Skill
}

// ToolProvider 插件可通过实现此接口并提供给 AI 插件显式注册自定义工具。
//
// 其他插件在自己的 Setup 中通过 [(*SetupContext).TryService] 获取 AI 插件服务实例
// 后调用 [Plugin.RegisterToolProvider] 注册工具集。此接口是暴露需要权限的
// 工具给 AI 的推荐方式——插件自行控制哪些工具可被 AI 调用，并在 Execute 中
// 完成权限校验。
//
// 使用示例：
//
//	if aiSvc, ok := ctx.TryService[*ai.Plugin]("ai"); ok {
//	    aiSvc.RegisterToolProvider(myToolProvider)
//	}
//
// 安全提示：不要在 ListTools 中暴露可以被 AI 滥用执行危险操作的接口。
type ToolProvider interface {
	ListTools() []Tool
}

// ToolRegistry 管理所有可供 AI 调用的工具，按工具名索引。
//
// 注册表内部保存的是动作记录：管线货币（[Action]）与执行载荷分开存放，
// 对外只提供两种视图——[ToolRegistry.Actions] / [ToolRegistry.Action] 给
// 选择、序列化与策略评估，[ToolRegistry.Get] / [ToolRegistry.List] 给
// 需要执行载荷的执行路径与兼容调用方。
//
// 因此"模型能看到什么动作"与"动作怎么执行"在类型层面就是分开的，
// 选择管线拿不到 Execute，也就不可能误用。
type ToolRegistry struct {
	mu      sync.RWMutex
	entries map[string]registeredAction
}

// registeredAction 注册表内的一条记录：动作视图 + 执行载荷。
//
// 执行载荷只在这里持有，且只经 [ToolRegistry.Get] / [ToolRegistry.List] 取出，
// 保证 Execute 只在执行路径上出现。
type registeredAction struct {
	action  Action
	execute func(ctx context.Context, args map[string]any) (string, error)
	// executeRich 富结果执行载荷（可选）；与 execute 一样只存放在注册表里，
	// 只经 Get/List 的执行视图取出。
	executeRich func(ctx context.Context, args map[string]any) (ToolResult, error)
	// source 动作来源：动作视图（Action）不承载来源，注册表在这里保真保存，
	// 保证 Get/List 还原的执行视图与注册时一致。
	source ActionSource
	// selection 记录注册时是否显式声明了保留级别（nil = 未声明，按类别派生）。
	// 动作视图只存解析后的值，无法区分"显式 Optional"与"未声明"，
	// 因此这里保留一份副本，保证 Get/List 还原的执行视图与注册时逐字段一致。
	selection *SelectionClass
}

// cloneTool 隔离注册表边界上的可变数据；执行回调本身是不可变函数值，直接复制。
func cloneTool(t Tool) Tool {
	t.Categories = slices.Clone(t.Categories)
	t.Parameters = protocol.CloneToolParamSchema(t.Parameters)
	t.Permissions = slices.Clone(t.Permissions)
	if t.Selection != nil {
		selection := *t.Selection
		t.Selection = &selection
	}
	return t
}

// CloneTool 深拷贝工具中可变的类别、权限、选择级别与参数 Schema。
// 执行回调是函数值，按值复制即可。
func CloneTool(t Tool) Tool { return cloneTool(t) }

// tool 把注册记录还原为工具，逐字段与注册时一致。
func (e registeredAction) tool() Tool {
	return cloneTool(Tool{
		Name:                  e.action.Spec.Name,
		Description:           e.action.Spec.Description,
		Categories:            e.action.Spec.Categories,
		Source:                e.source,
		Selection:             e.selection,
		Parameters:            e.action.Spec.Parameters,
		RequiresApproval:      e.action.Policy.RequiresApproval,
		AlwaysRequireApproval: e.action.Policy.AlwaysRequireApproval,
		GrantsSendTo:          e.action.Policy.GrantsSendTo,
		Permissions:           e.action.Policy.Permissions,
		Execute:               e.execute,
		ExecuteRich:           e.executeRich,
	})
}

// NewToolRegistry 创建空的工具注册表。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{entries: make(map[string]registeredAction)}
}

// Register 注册一个工具。同名工具仅首次注册生效，后续注册静默忽略。
// 调用者应保证在本轮动作集组装完成前完成注册。
// 若工具名称不合法（不匹配 ^[a-zA-Z0-9_-]+$），会自动修正并记录警告。
func (r *ToolRegistry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t = normalizeToolName(t)
	if _, exists := r.entries[t.Name]; exists {
		logger.Warnf("[AI] Tool %q already registered, skipping duplicate", t.Name)
		return
	}
	r.put(t)
}

// Upsert 登记工具，同名工具会被新定义覆盖（而非被 first-wins 丢弃）。
//
// 供"工具集合会运行时变化"的来源（如外部工具服务器）刷新集合使用：来源重连后
// 同名工具的描述/schema 可能变化，需要就地更新。目录装配侧应在模型可见定义
// 或策略变化时推进目录版本，以使依赖旧定义的选择缓存失效。
func (r *ToolRegistry) Upsert(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.put(normalizeToolName(t))
}

// normalizeToolName 校验并修正名称，保证注册表内名称合法。
func normalizeToolName(t Tool) Tool {
	if !ValidToolName(t.Name) {
		original := t.Name
		t.Name = SanitizeToolName(t.Name)
		logger.Warnf("[AI] Tool name %q is invalid (must match ^[a-zA-Z0-9_-]+$), sanitized to %q", original, t.Name)
	}
	return t
}

// put 写入一条注册记录（调用方须持有写锁并已规范化名称）。
func (r *ToolRegistry) put(t Tool) {
	record := registeredAction{action: ActionOf(t), execute: t.Execute, executeRich: t.ExecuteRich, source: t.Source}
	if t.Selection != nil {
		// 复制而非共享指针：注册后调用方再改自己的 Selection 不应影响注册表。
		v := *t.Selection
		record.selection = &v
	}
	r.entries[t.Name] = record
}

// Get 按名称查找工具（执行视图，含执行载荷）。第二个返回值为 false 表示未找到。
// 管线一律使用 [ToolRegistry.Actions] / [ToolRegistry.Action]。
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	if !ok {
		return Tool{}, false
	}
	return e.tool(), true
}

// Action 按名称查找动作（管线视图：描述 + 策略，不含执行载荷）。
// 第二个返回值为 false 表示未找到。
func (r *ToolRegistry) Action(name string) (Action, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	if !ok {
		return Action{}, false
	}
	return CloneAction(e.action), true
}

// Remove 按名称删除工具。返回是否实际删除了某个工具。
// 用于显式注册的工具覆盖同名的自动发现命令工具。
func (r *ToolRegistry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[name]; !ok {
		return false
	}
	delete(r.entries, name)
	return true
}

// List 返回当前注册的所有工具（执行视图）的切片副本，按工具名升序排序。
// 每次调用创建新切片。管线一律使用 [ToolRegistry.Actions]。
//
// 排序是提示词前缀缓存的前提：注册表底层是 map，直接遍历的迭代顺序在
// 不同调用之间是随机的，同一会话每轮请求的 tools 段会因此发生重排，
// LLM 侧的前缀缓存（DeepSeek 磁盘缓存、OpenAI/Anthropic prompt cache）
// 会在 tools 段整段失效。按名称排序后，工具集不变时序列化结果字节稳定。
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.tool())
	}
	slices.SortFunc(out, func(a, b Tool) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Actions 返回当前注册的所有动作（管线视图，不含执行载荷），按名称升序排序。
// 排序理由同 [ToolRegistry.List]：工具集不变时序列化结果字节稳定。
func (r *ToolRegistry) Actions() []Action {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Action, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, CloneAction(e.action))
	}
	slices.SortFunc(out, func(a, b Action) int { return strings.Compare(a.Spec.Name, b.Spec.Name) })
	return out
}
