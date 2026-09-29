package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// provider.go — 把服务器的工具物化为 Remilia 工具，并转换调用结果。
//
// 这是本子系统与 AI 之间的唯一数据出口：模型看到的是普通的 [toolkit.Tool]，
// 执行经其 ExecuteRich 回调回到对应的服务器连接。工具的审批/权限策略在此从
// 服务器配置映射而来——权威策略始终是 Remilia 的 ActionPolicy。

// serverTools 物化所有可用服务器的工具；模型函数名全局唯一且确定。
func (m *Manager) serverTools() []toolkit.Tool {
	out := make([]toolkit.Tool, 0, 16)
	used := make(map[string]struct{})
	for _, s := range m.servers {
		if !s.availability().Selectable() {
			continue
		}
		s.mu.RLock()
		tools := append([]mcpTool(nil), s.tools...)
		s.mu.RUnlock()
		// 按原始名排序后再分配模型函数名，保证集合不变时命名稳定。
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		for _, t := range tools {
			if !s.cfg.toolAllowed(t.Name) {
				continue
			}
			modelName := assignModelName(s.prefix, t.Name, used)
			used[modelName] = struct{}{}
			out = append(out, m.buildTool(s, t, modelName))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// assignModelName 生成全局唯一的模型函数名（冲突时追加序号，确定性）。
func assignModelName(prefix, original string, used map[string]struct{}) string {
	base := prefix + toolkit.SanitizeToolName(original)
	name := base
	for i := 2; ; i++ {
		if _, ok := used[name]; !ok {
			return name
		}
		name = base + "_" + strconv.Itoa(i)
	}
}

// buildTool 把一个服务器工具物化为 Remilia 工具。
func (m *Manager) buildTool(s *serverState, t mcpTool, modelName string) toolkit.Tool {
	original := t.Name
	srv := s
	exec := func(ctx context.Context, args map[string]any) (toolkit.ToolResult, error) {
		cl := srv.currentClient()
		if cl == nil {
			// 软不可用：工具仍在集合中，调用时如实失败，避免集合抖动。
			return toolkit.ToolResult{}, fmt.Errorf("外部工具服务器 %q 当前不可用，请稍后重试", srv.cfg.Name)
		}
		callCtx, cancel := context.WithTimeout(ctx, srv.cfg.timeout())
		defer cancel()
		res, err := cl.callTool(callCtx, original, args)
		if err != nil {
			return toolkit.ToolResult{}, err
		}
		return convertCallResult(res)
	}
	return toolkit.Tool{
		Name:        modelName,
		Description: t.Description,
		Source:      toolkit.MCPSource(srv.cfg.Name),
		Categories:  []string{"mcp", "mcp:" + srv.cfg.Name},
		Parameters:  parseInputSchema(t.InputSchema),
		// 审批策略以 Remilia 配置为准；服务器注解只能作为**收紧**的输入建议
		// （声明 destructive 的工具即使配置显式关闭审批也强制审批），绝不放松。
		RequiresApproval:      srv.cfg.requireApproval() || destructiveHint(t.Annotations),
		AlwaysRequireApproval: srv.cfg.AlwaysRequireApproval,
		Permissions:           srv.cfg.Permissions,
		ExecuteRich:           exec,
	}
}

// destructiveHint 报告服务器注解是否声明该工具具有破坏性。
//
// 只用于"收紧"策略：注解来自不可信对端，永远不能用来放松 Remilia 的审批/
// 权限要求。nil 或未声明一律视为非破坏性。
func destructiveHint(ann *toolAnnotations) bool {
	return ann != nil && ann.DestructiveHint != nil && *ann.DestructiveHint
}

// parseInputSchema 把 MCP 的 inputSchema 收敛为工具参数 Schema；无法解析时
// 回退到"单一 object 参数"。
func parseInputSchema(raw json.RawMessage) protocol.ToolParamSchema {
	if len(raw) > 0 {
		var sch protocol.ToolParamSchema
		if err := json.Unmarshal(raw, &sch); err == nil && sch.Type != "" {
			return sch
		}
	}
	return protocol.ToolParamSchema{
		Type: "object",
		Properties: map[string]protocol.ToolParamSchema{
			"arguments": {Type: "object", Description: "工具参数"},
		},
	}
}

// convertCallResult 把 MCP 的调用结果转换为框架的富结果。
// isError 为 true 时返回错误（由执行侧格式化为模型可见文本）。
func convertCallResult(res *callToolResult) (toolkit.ToolResult, error) {
	if res == nil {
		return toolkit.ToolResult{}, nil
	}
	if res.IsError {
		text := textOf(res.Content)
		if text == "" {
			text = "外部工具执行失败"
		}
		return toolkit.ToolResult{}, errors.New(text)
	}
	parts := make([]toolkit.ResultPart, 0, len(res.Content)+1)
	attachments := make([]platform.Attachment, 0, len(res.Content))
	for _, c := range res.Content {
		if p, ok := convertContent(c); ok {
			parts = append(parts, p)
		}
		if att, ok := attachmentOf(c); ok {
			attachments = append(attachments, att)
		}
	}
	if len(res.StructuredContent) > 0 && string(res.StructuredContent) != "null" {
		parts = append(parts, toolkit.ResultPart{Kind: toolkit.ResultStructured, Text: string(res.StructuredContent)})
	}
	return toolkit.ToolResult{Parts: parts, Attachments: attachments}, nil
}

// attachmentOf 把图片/音频内容映射为框架附件（走既有附件通道）；
// 文本/资源/结构化不产生附件。媒体与 Parts 表达同一份数据，供不同消费者使用。
func attachmentOf(c contentItem) (platform.Attachment, bool) {
	switch c.Type {
	case "image":
		return platform.Attachment{
			Kind:     platform.AttachmentKindImage,
			Data:     decodeBase64(c.Data),
			URL:      c.URI,
			MimeType: c.MimeType,
			Name:     c.Name,
		}, true
	case "audio":
		return platform.Attachment{
			Kind:     platform.AttachmentKindAudio,
			Data:     decodeBase64(c.Data),
			URL:      c.URI,
			MimeType: c.MimeType,
			Name:     c.Name,
		}, true
	default:
		return platform.Attachment{}, false
	}
}

// convertContent 转换单个内容片段；无法识别且无文本时跳过。
func convertContent(c contentItem) (toolkit.ResultPart, bool) {
	switch c.Type {
	case "text":
		return toolkit.ResultPart{Kind: toolkit.ResultText, Text: c.Text}, true
	case "image":
		return toolkit.ResultPart{
			Kind:     toolkit.ResultImage,
			Data:     decodeBase64(c.Data),
			MimeType: c.MimeType,
			URI:      c.URI,
			Name:     c.Name,
		}, true
	case "audio":
		return toolkit.ResultPart{
			Kind:     toolkit.ResultAudio,
			Data:     decodeBase64(c.Data),
			MimeType: c.MimeType,
			URI:      c.URI,
			Name:     c.Name,
		}, true
	case "resource":
		if c.Resource == nil {
			return toolkit.ResultPart{}, false
		}
		return toolkit.ResultPart{
			Kind:     toolkit.ResultResource,
			Text:     c.Resource.Text,
			MimeType: c.Resource.MimeType,
			URI:      c.Resource.URI,
			Name:     c.Name,
		}, true
	case "resource_link":
		return toolkit.ResultPart{Kind: toolkit.ResultResource, MimeType: c.MimeType, URI: c.URI, Name: c.Name}, true
	default:
		if c.Text != "" {
			return toolkit.ResultPart{Kind: toolkit.ResultText, Text: c.Text}, true
		}
		if c.URI != "" {
			return toolkit.ResultPart{Kind: toolkit.ResultResource, MimeType: c.MimeType, URI: c.URI, Name: c.Name}, true
		}
		return toolkit.ResultPart{}, false
	}
}

// textOf 拼接全部文本片段。
func textOf(items []contentItem) string {
	out := ""
	for _, c := range items {
		if c.Type == "text" && c.Text != "" {
			if out != "" {
				out += "\n"
			}
			out += c.Text
		}
	}
	return out
}

// decodeBase64 解码 base64 内容；失败返回 nil（保留 URI 等其余信息）。
func decodeBase64(s string) []byte {
	if s == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}
