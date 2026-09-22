// tool.go — 工具系统：类型定义、注册表与扩展接口。
//
// 本文件包含：
//   - Tool：LLM 可调用的工具类型定义
//   - ToolRegistry：线程安全的工具注册表
//   - ToolProvider / SkillProvider：插件注册工具的接口
//   - WithCallerInfo / CallerInfoFromContext：调用者身份注入
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
type ctxKeyCallerInfoType struct{}

// WithCallerInfo 将调用者信息注入 context，供工具 Execute 回调进行权限校验。
func WithCallerInfo(ctx context.Context, caller platform.UserInfo) context.Context {
	return context.WithValue(ctx, ctxKeyCallerInfoType{}, caller)
}

// CallerInfoFromContext 从 context 中提取工具调用者信息。
// 若 context 中无调用者信息，返回零值 UserInfo 和 false。
func CallerInfoFromContext(ctx context.Context) (platform.UserInfo, bool) {
	caller, ok := ctx.Value(ctxKeyCallerInfoType{}).(platform.UserInfo)
	return caller, ok
}

// ctxKeyToolSender 是 context 中存储工具消息发送能力的键。
type ctxKeyToolSenderType struct{}

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
//   - Parameters: JSON Schema 格式的参数描述，LLM 据此生成调用参数
//   - RequiresApproval: 标记该工具需要人工审批后才执行（配合 tool_approval=restricted
//     模式使用；always 模式下所有工具都审批）。显式注册的敏感工具（如文件操作、
//     命令执行）建议标记为 true。
//   - Execute: 工具执行回调，接收 context 和参数，返回结果文本或错误
//
// Execute 的调用方通常已通过 [WithCallerInfo] 注入了调用者身份，
// 实现方可通过 [CallerInfoFromContext] 提取调用者信息进行权限校验。
type Tool struct {
	Name        string
	Description string
	Categories  []string // "general"、"space"、"weather"、"admin" 等
	Parameters  protocol.ToolParamSchema
	// RequiresApproval 工具执行前是否需要用户审批（tool_approval=restricted 时生效）。
	RequiresApproval bool
	// AlwaysRequireApproval 无论 tool_approval 模式（含 off）都强制审批。
	// 适用于 send_to 这类影响其他会话的高风险工具——off 模式下
	// RequiresApproval 会被豁免，本字段确保审批不可关闭。
	AlwaysRequireApproval bool
	// Permissions 工具执行所需的 RBAC 权限列表（如 "bilibili.manage"）。
	// 非空时 executeToolResult 在调用前**强制校验**调用者权限（任一命中即放行），
	// 不依赖插件自觉——权限不足返回可读错误、不执行。
	// 支持格式与框架命令一致：resource.action / resource:action / resource。
	Permissions []string
	Execute     func(ctx context.Context, args map[string]any) (string, error)
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
}

// tool 把注册记录还原为工具，逐字段与注册时一致。
func (e registeredAction) tool() Tool {
	return Tool{
		Name:                  e.action.Spec.Name,
		Description:           e.action.Spec.Description,
		Categories:            e.action.Spec.Categories,
		Parameters:            e.action.Spec.Parameters,
		RequiresApproval:      e.action.Policy.RequiresApproval,
		AlwaysRequireApproval: e.action.Policy.AlwaysRequireApproval,
		Permissions:           e.action.Policy.Permissions,
		Execute:               e.execute,
	}
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
	if !ValidToolName(t.Name) {
		original := t.Name
		t.Name = SanitizeToolName(t.Name)
		logger.Warnf("[AI] Tool name %q is invalid (must match ^[a-zA-Z0-9_-]+$), sanitized to %q", original, t.Name)
	}
	if _, exists := r.entries[t.Name]; exists {
		logger.Warnf("[AI] Tool %q already registered, skipping duplicate", t.Name)
		return
	}
	r.entries[t.Name] = registeredAction{action: ActionOf(t), execute: t.Execute}
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
	return e.action, true
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
		out = append(out, e.action)
	}
	slices.SortFunc(out, func(a, b Action) int { return strings.Compare(a.Spec.Name, b.Spec.Name) })
	return out
}
