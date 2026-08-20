// Package ai tool.go — 工具系统：类型定义、注册表、序列化与解析。
//
// 本文件包含：
//   - Tool / ToolParamSchema：LLM 可调用的工具类型定义
//   - ToolRegistry：线程安全的工具注册表
//   - ToolProvider / SkillProvider：插件注册工具的接口
//   - WithCallerInfo / CallerInfoFromContext：调用者身份注入
//   - toOpenAITools / toAnthropicTools：工具列表的提供商格式转换
//   - parseOpenAIToolCalls / parseAnthropicToolCalls：工具调用响应解析
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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

// ToolParamSchema JSON Schema 格式的工具参数描述。
// 用于向 LLM 描述工具的输入参数结构，符合 OpenAI tool calling 的 JSON Schema 规范。
type ToolParamSchema struct {
	Type        string                     `json:"type"`
	Description string                     `json:"description,omitempty"`
	Properties  map[string]ToolParamSchema `json:"properties,omitempty"`
	Items       *ToolParamSchema           `json:"items,omitempty"`
	Required    []string                   `json:"required,omitempty"`
	Enum        []string                   `json:"enum,omitempty"`
}

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
	Parameters  ToolParamSchema
	// RequiresApproval 工具执行前是否需要用户审批（tool_approval=restricted 时生效）。
	RequiresApproval bool
	// AlwaysRequireApproval 无论 tool_approval 模式（含 off）都强制审批。
	// 适用于 send_to 这类影响其他会话的高风险工具——off 模式下
	// RequiresApproval 会被豁免，本字段确保审批不可关闭。
	AlwaysRequireApproval bool
	// Permissions 工具执行所需的 RBAC 权限列表（如 "bilibili.manage"）。
	// 非空时 executeTool 在调用前**强制校验**调用者权限（任一命中即放行），
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
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry 创建空的工具注册表。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register 注册一个工具。同名工具仅首次注册生效，后续注册静默忽略。
// 调用者应保证在 [processWithTools] 开始前完成注册。
// 若工具名称不合法（不匹配 ^[a-zA-Z0-9_-]+$），会自动修正并记录警告。
func (r *ToolRegistry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !validToolNameRegex.MatchString(t.Name) {
		original := t.Name
		t.Name = sanitizeToolName(t.Name)
		logger.Warnf("[AI] Tool name %q is invalid (must match ^[a-zA-Z0-9_-]+$), sanitized to %q", original, t.Name)
	}
	if _, exists := r.tools[t.Name]; exists {
		logger.Warnf("[AI] Tool %q already registered, skipping duplicate", t.Name)
		return
	}
	r.tools[t.Name] = t
}

// Get 按名称查找工具。第二个返回值为 false 表示未找到。
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Remove 按名称删除工具。返回是否实际删除了某个工具。
// 用于显式注册的工具覆盖同名的自动发现命令工具。
func (r *ToolRegistry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return false
	}
	delete(r.tools, name)
	return true
}

// List 返回当前注册的所有工具的切片副本。每次调用创建新切片。
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// --- OpenAI 格式的工具序列化 ---

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  ToolParamSchema `json:"parameters"`
}

func toOpenAITools(tools []Tool) []openaiTool {
	out := make([]openaiTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}

// --- Anthropic 格式的工具序列化 ---

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema ToolParamSchema `json:"input_schema"`
}

func toAnthropicTools(tools []Tool) []anthropicTool {
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}
	return out
}

// --- 工具调用解析 ---

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Index    int    `json:"index"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func parseOpenAIToolCalls(raw []openaiToolCall) ([]ToolCall, error) {
	calls := make([]ToolCall, 0, len(raw))
	for _, tc := range raw {
		// 跳过空 tool call（无 name 说明是幽灵 chunk）
		if tc.Function.Name == "" {
			continue
		}
		tcID := tc.ID
		if tcID == "" {
			tcID = fmt.Sprintf("call_%s_%d", tc.Function.Name, tc.Index)
		}
		args := make(map[string]any)
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("ai: parse tool call args: %w", err)
			}
		}
		calls = append(calls, ToolCall{
			ID:        tcID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}
	return calls, nil
}

// anthropicContentBlock 表示 Anthropic 响应中的一个 content block。
// 可以是文本块或工具调用块。
type anthropicContentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

func parseAnthropicToolCalls(blocks []anthropicContentBlock) []ToolCall {
	calls := make([]ToolCall, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "tool_use" {
			continue
		}
		args := make(map[string]any)
		if block.Input != nil {
			switch v := block.Input.(type) {
			case map[string]any:
				args = v
			default:
				b, _ := json.Marshal(v)
				json.Unmarshal(b, &args)
			}
		}
		calls = append(calls, ToolCall{
			ID:        block.ID,
			Name:      block.Name,
			Arguments: args,
		})
	}
	return calls
}
