package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// protocol_handshake.go — 握手流程：以 initialize / notifications/initialized
// 建立连接级会话（HTTP 上可下发 Mcp-Session-Id），协议版本在握手里协商。
//
// 这是 MCP 在无状态修订版之前的标准形态。变更通知不需要额外订阅：stdio 上经
// 共享通道到达，HTTP 上由传输打开独立的 GET 事件流推送。

// handshakeSession 握手流程的 [protocolSession] 实现。
type handshakeSession struct{ c *client }

// Connect 完成 initialize 握手并发送 initialized 通知。
func (s *handshakeSession) Connect(ctx context.Context, timeout time.Duration) error {
	c := s.c
	hsCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var res initializeResult
	err := c.callInto(hsCtx, methodInitialize, initializeParams{
		ProtocolVersion: HandshakeProtocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      clientInfo{Name: clientName, Version: clientVersion},
	}, &res)
	if err != nil {
		return fmt.Errorf("mcp: %s: initialize: %w", c.cfg.Name, err)
	}
	negotiated := res.ProtocolVersion
	if negotiated == "" {
		negotiated = HandshakeProtocolVersion
	}
	if res.ServerInfo.Name != "" {
		c.setServerInfo(res.ServerInfo)
	}
	switch {
	case !SupportsProtocolVersion(negotiated):
		logger.Warnf("[MCP] %s: server selected protocol %q, which this client does not declare support for; continuing on the common subset",
			c.cfg.Name, negotiated)
	case negotiated != HandshakeProtocolVersion:
		logger.Debugf("[MCP] %s: negotiated protocol %q (client offered %q)",
			c.cfg.Name, negotiated, HandshakeProtocolVersion)
	}
	// HTTP 传输须在初始化后的每个请求上携带协商出的协议版本头。
	c.setProtocolVersion(negotiated)
	if nb, err := encodeNotification(methodInitialized, map[string]any{}); err == nil {
		_ = c.tr.Send(hsCtx, nb)
	}
	return nil
}

// ListTools 拉取全部工具（自动翻页）。
func (s *handshakeSession) ListTools(ctx context.Context) ([]mcpTool, error) {
	var all []mcpTool
	cursor := ""
	for {
		var res listToolsResult
		if err := s.c.callInto(ctx, methodListTools, listToolsParams{Cursor: cursor}, &res); err != nil {
			return nil, err
		}
		if err := checkResultType(res.ResultType); err != nil {
			return nil, err
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			return all, nil
		}
		cursor = res.NextCursor
	}
}

// CallTool 调用一个工具。
func (s *handshakeSession) CallTool(ctx context.Context, name string, args map[string]any) (*callToolResult, error) {
	var res callToolResult
	if err := s.c.callInto(ctx, methodCallTool, callToolParams{Name: name, Arguments: args}, &res); err != nil {
		return nil, err
	}
	if err := checkResultType(res.ResultType); err != nil {
		return nil, err
	}
	return &res, nil
}

// Watch 在握手流程下为空实现：变更通知由传输的既有通道（stdio 共享读取循环、
// HTTP 的独立 GET 事件流）送达，无需显式订阅。
func (s *handshakeSession) Watch(context.Context) error { return nil }
