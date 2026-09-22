// toolschema.go — 工具在 LLM API 上的线格式：参数 Schema、工具声明与
// 两种提供商的序列化 / 解析。
//
// 本文件只认识"发给模型看的那几个字段"（名称、描述、参数），不认识工具的
// 类别、保留级别、审批与权限——那些是选择与策略层的事，留在上层。
package protocol

import (
	"encoding/json"
	"fmt"
)

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

// ToolSpec 一个工具在协议层的声明：只有模型需要知道的字段。
// 上层的动作描述（含类别、保留级别等）在序列化前收敛成它。
type ToolSpec struct {
	Name        string
	Description string
	Parameters  ToolParamSchema
}

// --- OpenAI 格式的工具序列化 ---

// OpenAITool OpenAI tools 数组中的一项。
type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction OpenAI function 声明。
type OpenAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  ToolParamSchema `json:"parameters"`
}

// ToOpenAITools 把工具声明序列化为 OpenAI tools 数组。
func ToOpenAITools(specs []ToolSpec) []OpenAITool {
	out := make([]OpenAITool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, OpenAITool{
			Type: "function",
			// OpenAI 的 function 字段与工具声明字段一一对应，直接转换。
			Function: OpenAIFunction(spec),
		})
	}
	return out
}

// --- Anthropic 格式的工具序列化 ---

// AnthropicTool Anthropic tools 数组中的一项。
type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema ToolParamSchema `json:"input_schema"`
}

// ToAnthropicTools 把工具声明序列化为 Anthropic tools 数组。
func ToAnthropicTools(specs []ToolSpec) []AnthropicTool {
	out := make([]AnthropicTool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, AnthropicTool{
			Name:        spec.Name,
			Description: spec.Description,
			InputSchema: spec.Parameters,
		})
	}
	return out
}

// --- 工具调用解析 ---

// OpenAIToolCall 响应里的一个 OpenAI tool call。
type OpenAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Index    int    `json:"index"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ParseOpenAIToolCalls 把 OpenAI 的 tool_calls 解析为内部调用表示。
func ParseOpenAIToolCalls(raw []OpenAIToolCall) ([]ToolCall, error) {
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

// AnthropicContentBlock 表示 Anthropic 响应中的一个 content block。
// 可以是文本块、图片块（原生图像输出）或工具调用块。
type AnthropicContentBlock struct {
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Source *anthropicImgSrc `json:"source,omitempty"` // type=image 时的图片数据
	ID     string           `json:"id,omitempty"`
	Name   string           `json:"name,omitempty"`
	Input  any              `json:"input,omitempty"`
}

// ParseAnthropicToolCalls 从 Anthropic 响应的 content blocks 中解析工具调用。
func ParseAnthropicToolCalls(blocks []AnthropicContentBlock) []ToolCall {
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
