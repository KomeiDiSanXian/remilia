package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var (
	benchmarkOpenAIToolsSink       []OpenAITool
	benchmarkAnthropicToolsSink    []AnthropicTool
	benchmarkProviderPayloadSink   []byte
)

func benchmarkToolSpecs(count int) []ToolSpec {
	parameters := ToolParamSchema{
		Type: "object",
		Properties: map[string]ToolParamSchema{
			"query": {Type: "string", Description: "查询内容或关键词"},
			"limit": {Type: "integer", Description: "最多返回的结果数量"},
			"filters": {
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"from": {Type: "string"},
					"to":   {Type: "string"},
				},
			},
			"tags": {
				Type:  "array",
				Items: &ToolParamSchema{Type: "string"},
			},
		},
		Required: []string{"query"},
	}
	specs := make([]ToolSpec, count)
	for i := range specs {
		specs[i] = ToolSpec{
			Name:        fmt.Sprintf("search_tool_%03d", i),
			Description: "按关键词检索历史记录、天气预报或日程信息。",
			Parameters:  parameters,
		}
	}
	return specs
}

func benchmarkChatMessages(count int) []Message {
	if count <= 0 {
		return nil
	}
	const body = "用户正在讨论天气预报、日程安排和历史任务，需要结合已有上下文继续处理。"
	messages := make([]Message, 0, count)
	messages = append(messages, Message{Role: RoleSystem, Content: strings.Repeat("稳定系统提示词。", 24)})
	for cycle := 0; len(messages) < count; cycle++ {
		callID := fmt.Sprintf("call_%04d", cycle)
		conversation := [...]Message{
			{Role: RoleUser, Content: body},
			{Role: RoleAssistant, Content: "我先查询相关信息。", ToolCalls: []ToolCall{{
				ID:        callID,
				Name:      "search_tool_001",
				Arguments: map[string]any{"query": "weather forecast", "limit": 5},
			}}},
			{Role: RoleTool, ToolCallID: callID, Content: strings.Repeat("检索到一条与当前任务相关的记录。", 3)},
			{Role: RoleAssistant, Content: "这是根据检索结果整理的答复。"},
		}
		for _, message := range conversation {
			if len(messages) == count {
				break
			}
			messages = append(messages, message)
		}
	}
	return messages
}

func benchmarkRequest(tools, messages int) *ChatRequest {
	return &ChatRequest{
		Model:       "benchmark-model",
		Messages:    benchmarkChatMessages(messages),
		Tools:       benchmarkToolSpecs(tools),
		Temperature: 0.2,
		TopP:        0.9,
		MaxTokens:   2048,
	}
}

func BenchmarkToolDefinitionConversion(b *testing.B) {
	for _, count := range []int{8, 32, 100} {
		specs := benchmarkToolSpecs(count)
		b.Run(fmt.Sprintf("tools_%d/openai", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkOpenAIToolsSink = ToOpenAITools(specs)
			}
		})
		b.Run(fmt.Sprintf("tools_%d/anthropic", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkAnthropicToolsSink = ToAnthropicTools(specs)
			}
		})
	}
}

func BenchmarkProviderRequestPreparation(b *testing.B) {
	for _, workload := range []struct {
		name            string
		tools, messages int
	}{
		{name: "typical", tools: 8, messages: 16},
		{name: "large_history", tools: 32, messages: 64},
		{name: "large_toolset", tools: 100, messages: 128},
	} {
		request := benchmarkRequest(workload.tools, workload.messages)
		b.Run(workload.name+"/openai", func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				body := openaiChatRequest{
					Model:       RequestModel("benchmark-model", request.Model),
					Messages:    toOpenAIMessages(request.Messages),
					Temperature: request.Temperature,
					TopP:        request.TopP,
					MaxTokens:   requestMaxTokens(2048, request.MaxTokens),
				}
				if len(request.Tools) > 0 {
					body.Tools = ToOpenAITools(request.Tools)
				}
				payload, err := json.Marshal(body)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkProviderPayloadSink = payload
			}
		})

		b.Run(workload.name+"/anthropic", func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				body := anthropicChatRequest{
					Model:       RequestModel("benchmark-model", request.Model),
					MaxTokens:   requestMaxTokens(2048, request.MaxTokens),
					System:      extractAnthropicSystem(request.Messages),
					Messages:    toAnthropicMessages(request.Messages),
					Temperature: request.Temperature,
					TopP:        request.TopP,
				}
				if len(request.Tools) > 0 {
					body.Tools = ToAnthropicTools(request.Tools)
				}
				payload, err := json.Marshal(body)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkProviderPayloadSink = payload
			}
		})
	}
}

func BenchmarkMultimodalRequestPreparation64KiB(b *testing.B) {
	imageData := make([]byte, 64<<10)
	for i := range imageData {
		imageData[i] = byte(i * 31)
	}
	request := &ChatRequest{
		Model: "benchmark-vision-model",
		Messages: []Message{
			{Role: RoleSystem, Content: "请结合图片和文字回答。"},
			{Role: RoleUser, ContentParts: []ContentPart{
				{Type: ContentPartText, Text: "请描述这张图片。"},
				{Type: ContentPartImage, MimeType: "image/jpeg", Data: imageData},
			}},
		},
		MaxTokens: 1024,
	}

	b.Run("openai", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			body := openaiChatRequest{
				Model:     request.Model,
				Messages:  toOpenAIMessages(request.Messages),
				MaxTokens: request.MaxTokens,
			}
			payload, err := json.Marshal(body)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkProviderPayloadSink = payload
		}
	})

	b.Run("anthropic", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			body := anthropicChatRequest{
				Model:     request.Model,
				MaxTokens: request.MaxTokens,
				System:    extractAnthropicSystem(request.Messages),
				Messages:  toAnthropicMessages(request.Messages),
			}
			payload, err := json.Marshal(body)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkProviderPayloadSink = payload
		}
	})
}
