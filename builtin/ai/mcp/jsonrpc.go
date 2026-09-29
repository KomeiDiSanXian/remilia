package mcp

import (
	"encoding/json"
	"fmt"
)

// jsonrpc.go — JSON-RPC 2.0 报文编解码（MCP 的底层线格式）。
//
// 只实现 MCP 用到的子集：请求 / 响应 / 通知 / 错误。请求与响应按数字 id
// 关联；通知没有 id。

const jsonrpcVersion = "2.0"

// rpcRequest 一条请求。
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcNotification 一条通知（无 id，无需响应）。
type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcError JSON-RPC 错误对象。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error 实现 error。
func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	if e.Data != nil {
		return fmt.Sprintf("rpc error %d: %s (%v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// rpcResponse 一条响应。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcEnvelope 收报文时的统一视图：区分响应（有 id）与通知（无 id）。
type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// encodeRequest 序列化一条请求。
func encodeRequest(id int64, method string, params any) ([]byte, error) {
	return json.Marshal(rpcRequest{JSONRPC: jsonrpcVersion, ID: id, Method: method, Params: params})
}

// encodeNotification 序列化一条通知。
func encodeNotification(method string, params any) ([]byte, error) {
	return json.Marshal(rpcNotification{JSONRPC: jsonrpcVersion, Method: method, Params: params})
}
