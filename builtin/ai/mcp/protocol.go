package mcp

import (
	"encoding/json"
	"slices"
)

// protocol.go — MCP 协议类型与版本协商。
//
// 只覆盖本接入用到的能力：initialize 握手、tools/list、tools/call 与
// tools/list_changed 通知。其余能力（resources / prompts / sampling 等）
// 不在此声明。

// ProtocolVersion 是客户端在 initialize 里声明的 MCP 协议版本：取本接入支持的
// 最新一版。服务器会在 initialize 结果里返回它**选定**的版本，客户端应以该版本
// 为准继续（见 [SupportedProtocolVersions] 与 client 的版本协商）。
//
// 为什么停在 2025-11-25：这是最后一个保留 initialize / notifications/initialized
// 握手与 Mcp-Session-Id 会话的修订版。2026-07-28 改为无状态协议——去掉握手与会话
// id、版本与客户端能力改经每请求的 _meta 传递、以 server/discover 与
// subscriptions/listen 取代 GET 流与资源订阅、结果新增必填的 resultType——需要
// 另起一套流程，本接入暂不声明该版本。对端返回更旧或未知版本时按两端共同子集
// 尽力继续。
const ProtocolVersion = "2025-11-25"

// SupportedProtocolVersions 列出本接入声明可互通的协议版本（新→旧）。对端在
// initialize 中选定的版本若在此列，直接沿用；不在列时仍按"两端共同子集"尽力
// 继续，但调用方应记录告警。
var SupportedProtocolVersions = []string{
	"2025-11-25",
	"2025-06-18",
	"2025-03-26",
}

// SupportsProtocolVersion 报告版本是否在本接入声明支持之列。
func SupportsProtocolVersion(v string) bool {
	return slices.Contains(SupportedProtocolVersions, v)
}

// clientInfo 客户端标识。
type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeParams initialize 请求参数。
type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      clientInfo     `json:"clientInfo"`
}

// serverInfo 服务器标识。
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeResult initialize 响应结果。
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
	Instructions    string         `json:"instructions"`
}

// toolAnnotations MCP 工具注解。仅作为策略推导的**输入建议**：
// Remilia 的 ActionPolicy 才是权威，注解不能直接决定是否审批/放行。
type toolAnnotations struct {
	Title           string `json:"title"`
	ReadOnlyHint    *bool  `json:"readOnlyHint"`
	DestructiveHint *bool  `json:"destructiveHint"`
	IdempotentHint  *bool  `json:"idempotentHint"`
	OpenWorldHint   *bool  `json:"openWorldHint"`
}

// mcpTool 服务器声明的一个工具。
type mcpTool struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema json.RawMessage  `json:"inputSchema"`
	Annotations *toolAnnotations `json:"annotations"`
}

// listToolsParams tools/list 请求参数（分页游标）。
type listToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// listToolsResult tools/list 响应结果。
type listToolsResult struct {
	Tools      []mcpTool `json:"tools"`
	NextCursor string    `json:"nextCursor"`
}

// callToolParams tools/call 请求参数。
type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// resourceContent 内联资源。
type resourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Blob     string `json:"blob"`
}

// contentItem tools/call 结果中的一个内容片段。
type contentItem struct {
	Type     string           `json:"type"`
	Text     string           `json:"text"`
	Data     string           `json:"data"`
	MimeType string           `json:"mimeType"`
	URI      string           `json:"uri"`
	Name     string           `json:"name"`
	Resource *resourceContent `json:"resource"`
}

// callToolResult tools/call 响应结果。
type callToolResult struct {
	Content           []contentItem   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// 通知方法名。
const (
	methodToolsListChanged = "notifications/tools/list_changed"
	methodInitialized      = "notifications/initialized"
)

// 请求方法名。
const (
	methodInitialize = "initialize"
	methodPing       = "ping"
	methodListTools  = "tools/list"
	methodCallTool   = "tools/call"
)
