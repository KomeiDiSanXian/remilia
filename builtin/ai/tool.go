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

const CategoryGeneral = "general"

// categorySelectToolName 是路由阶段使用的工具名称。
const categorySelectToolName = "select_toolset"

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
	Execute          func(ctx context.Context, args map[string]any) (string, error)
}

// SkillProvider 插件可通过实现此接口向 AI 插件注册自定义 Skill。
//
// 使用示例：
//
//	if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
//	    aiSvc.RegisterSkillProvider(mySkillProvider)
//	}
type SkillProvider interface {
	ListSkills() []Skill
}

// ToolProvider 插件可通过实现此接口并提供给 AI 插件显式注册自定义工具。
//
// 其他插件在自己的 Setup 中通过 [plugin.TryService] 获取 AI 插件服务实例
// 后调用 [Plugin.RegisterToolProvider] 注册工具集。此接口是暴露需要权限的
// 工具给 AI 的推荐方式——插件自行控制哪些工具可被 AI 调用，并在 Execute 中
// 完成权限校验。
//
// 使用示例：
//
//	if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
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
