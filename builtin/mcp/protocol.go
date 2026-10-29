package mcp

import (
	"encoding/json"
	"fmt"
	"slices"
)

// protocol.go — MCP 协议类型、版本与元数据约定。
//
// 只覆盖本接入用到的能力：工具发现（tools/list、tools/call）、工具列表变化
// 通知（tools/list_changed），以及两种会话流程所需的握手/元数据。resources、
// prompts、sampling、tasks 等能力不在此声明。
//
// 协议版本按日期命名，两代流程并不相同：
//   - 握手流程（2025-03-26 … 2025-11-25）：initialize / notifications/initialized
//     建立连接级会话，HTTP 上可下发 Mcp-Session-Id；
//   - 无状态流程（2026-07-28 起）：没有握手与会话，版本与客户端能力改经每个
//     请求的 _meta 传递，变更通知经 subscriptions/listen 订阅。
//
// 两种流程的实现在 protocol_handshake.go / protocol_stateless.go，客户端只
// 依赖 [protocolSession] 抽象。

// HandshakeProtocolVersion 是握手流程在 initialize 中声明的版本：最后一个
// 保留 initialize/notifications/initialized 握手与 Mcp-Session-Id 会话的修订版。
const HandshakeProtocolVersion = "2025-11-25"

// StatelessProtocolVersion 是无状态流程使用的版本（也是首选版本）。
const StatelessProtocolVersion = "2026-07-28"

// SupportedProtocolVersions 列出本接入可互通的协议版本（新→旧）。协商时从对端
// 声明的版本集合里挑出这里最新的一个。
var SupportedProtocolVersions = []string{
	StatelessProtocolVersion,
	"2025-11-25",
	"2025-06-18",
	"2025-03-26",
}

// SupportsProtocolVersion 报告版本是否在本接入声明支持之列。
func SupportsProtocolVersion(v string) bool {
	return slices.Contains(SupportedProtocolVersions, v)
}

// bestSupportedVersion 从候选里挑出本接入支持的最新版本（新→旧扫描）。
func bestSupportedVersion(candidates []string) (string, bool) {
	for _, v := range SupportedProtocolVersions {
		if slices.Contains(candidates, v) {
			return v, true
		}
	}
	return "", false
}

// 结果类型：2026-07-28 起结果必带 resultType；更早的服务器会省略，按 complete
// 处理（见 [checkResultType]）。
const (
	resultTypeComplete      = "complete"
	resultTypeInputRequired = "input_required"
)

// 协议自定义的 JSON-RPC 错误码（见 MCP 的 Error Codes）。
const (
	// rpcCodeUnsupportedProtocolVersion 服务器不支持所请求版本，data 里带回
	// 它支持的版本列表。现代服务器专用：据此可把它与"握手时代"区分开。
	rpcCodeUnsupportedProtocolVersion = -32022
	// rpcCodeMethodNotFound 对端不认识该方法：判"握手时代"的主要信号之一。
	rpcCodeMethodNotFound = -32601
)

// _meta 保留键（无状态流程）。命名规则见 MCP 的 MetaObject。
const (
	metaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	metaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	metaClientInfo         = "io.modelcontextprotocol/clientInfo"
	metaServerInfo         = "io.modelcontextprotocol/serverInfo"
	metaSubscriptionID     = "io.modelcontextprotocol/subscriptionId"
)

// 客户端自报身份（仅用于展示/日志，不参与安全决策）。
const (
	clientName    = "remilia"
	clientVersion = "1.0.0"
)

// clientInfo 客户端标识。
type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeParams initialize 请求参数（握手流程）。
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
	ResultType      string         `json:"resultType"`
}

// resultMeta 结果里的 _meta：无状态流程下服务器在此自报身份。
type resultMeta struct {
	ServerInfo serverInfo `json:"io.modelcontextprotocol/serverInfo"`
}

// discoverParams server/discover 请求参数（无状态流程）。
type discoverParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// discoverResult server/discover 结果。
type discoverResult struct {
	ResultType        string         `json:"resultType"`
	SupportedVersions []string       `json:"supportedVersions"`
	Capabilities      map[string]any `json:"capabilities"`
	Instructions      string         `json:"instructions"`
	CacheScope        string         `json:"cacheScope"`
	TTLMs             int64          `json:"ttlMs"`
	Meta              resultMeta     `json:"_meta"`
}

// subscriptionFilter subscriptions/listen 的 opt-in 通知集合。
type subscriptionFilter struct {
	ToolsListChanged     bool `json:"toolsListChanged,omitempty"`
	PromptsListChanged   bool `json:"promptsListChanged,omitempty"`
	ResourcesListChanged bool `json:"resourcesListChanged,omitempty"`
}

// subscriptionsListenParams subscriptions/listen 请求参数。
type subscriptionsListenParams struct {
	Notifications subscriptionFilter `json:"notifications"`
	Meta          map[string]any     `json:"_meta,omitempty"`
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
	Cursor string         `json:"cursor,omitempty"`
	Meta   map[string]any `json:"_meta,omitempty"`
}

// listToolsResult tools/list 响应结果。
type listToolsResult struct {
	Tools      []mcpTool `json:"tools"`
	NextCursor string    `json:"nextCursor"`
	ResultType string    `json:"resultType"`
}

// callToolParams tools/call 请求参数。
type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Meta      map[string]any `json:"_meta,omitempty"`
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
	ResultType        string          `json:"resultType"`
}

// metaCarrier 由需要携带 _meta 的请求参数实现（无状态流程填充）。
type metaCarrier interface {
	getMeta() map[string]any
	setMeta(map[string]any)
}

func (p *listToolsParams) getMeta() map[string]any  { return p.Meta }
func (p *listToolsParams) setMeta(m map[string]any) { p.Meta = m }

func (p *callToolParams) getMeta() map[string]any  { return p.Meta }
func (p *callToolParams) setMeta(m map[string]any) { p.Meta = m }

func (p *discoverParams) getMeta() map[string]any  { return p.Meta }
func (p *discoverParams) setMeta(m map[string]any) { p.Meta = m }

func (p *subscriptionsListenParams) getMeta() map[string]any  { return p.Meta }
func (p *subscriptionsListenParams) setMeta(m map[string]any) { p.Meta = m }

// checkResultType 校验结果类型：缺失或 complete 视为完成；input_required 表示
// 服务器要求经多轮往返补充输入（MRTR），本接入不支持故如实报错。
func checkResultType(rt string) error {
	switch rt {
	case "", resultTypeComplete:
		return nil
	case resultTypeInputRequired:
		return fmt.Errorf("mcp: result requires additional input (multi-round-trip) which this client does not support")
	default:
		return fmt.Errorf("mcp: unsupported resultType %q", rt)
	}
}

// 通知方法名。
const (
	methodToolsListChanged          = "notifications/tools/list_changed"
	methodInitialized               = "notifications/initialized"
	methodSubscriptionsAcknowledged = "notifications/subscriptions/acknowledged"
)

// 请求方法名。
const (
	methodInitialize          = "initialize"
	methodPing                = "ping"
	methodListTools           = "tools/list"
	methodCallTool            = "tools/call"
	methodDiscover            = "server/discover"
	methodSubscriptionsListen = "subscriptions/listen"
)
