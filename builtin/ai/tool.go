package ai

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolParamSchema JSON Schema 格式的工具参数描述。
// 用于向 LLM 描述工具的输入参数结构，符合 OpenAI tool calling 的 JSON Schema 规范。
type ToolParamSchema struct {
	Type        string                      `json:"type"`
	Description string                      `json:"description,omitempty"`
	Properties  map[string]ToolParamSchema  `json:"properties,omitempty"`
	Items       *ToolParamSchema            `json:"items,omitempty"`
	Required    []string                    `json:"required,omitempty"`
	Enum        []string                    `json:"enum,omitempty"`
}

// Tool 描述一个可供 AI 调用的工具。
type Tool struct {
	Name        string
	Description string
	Parameters  ToolParamSchema
	Execute     func(ctx context.Context, args map[string]any) (string, error)
}

// ToolProvider 插件可通过实现此接口向 AI 显式注册自定义工具。
//
// AI 插件在 Setup 时通过容器扫描所有已注册的 ToolProvider 实现。
// 此接口是暴露需要权限的工具给 AI 的推荐方式——插件自行控制
// 哪些工具可被 AI 调用，并在 Execute 中完成权限校验。
//
// 使用示例：
//
//	func (p *MyPlugin) ListTools() []ai.Tool {
//	    return []ai.Tool{{
//	        Name:        "send_notice",
//	        Description: "发送群公告（仅管理员可用）",
//	        Parameters:  ai.ToolParamSchema{...},
//	        Execute: func(ctx context.Context, args map[string]any) (string, error) {
//	            // 在此处进行权限校验
//	            return "公告已发送", nil
//	        },
//	    }}
//	}
//
// 安全提示：不要在 ListTools 中暴露可以被 AI 滥用执行危险操作的接口。
type ToolProvider interface {
	ListTools() []Tool
}

// ToolRegistry 工具注册表，管理所有可供 AI 调用的工具。
type ToolRegistry struct {
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

func (r *ToolRegistry) Register(t Tool) {
	r.tools[t.Name] = t
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *ToolRegistry) List() []Tool {
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
	Name        string         `json:"name"`
	Description string         `json:"description"`
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
	Name        string         `json:"name"`
	Description string         `json:"description"`
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
	ID    string `json:"id"`
	Type  string `json:"type"`
	Index int    `json:"index"`
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


