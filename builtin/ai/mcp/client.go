package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// client.go — 单个服务器的协议客户端：握手、列工具、调工具、处理通知。
//
// 请求按数字 id 关联响应；服务器主动发来的通知（如工具列表变化）在读取循环里
// 处理并回调，不阻塞请求。

type client struct {
	cfg ServerConfig
	tr  Transport

	nextID atomic.Int64

	mu       sync.Mutex
	pending  map[int64]chan rpcResponse
	onChange func()

	done      chan struct{}
	closeOnce sync.Once

	serverInfo      serverInfo
	protocolVersion string
}

// newClient 按配置构造客户端（不连接）。
func newClient(cfg ServerConfig) (*client, error) {
	tr, err := newTransport(cfg)
	if err != nil {
		return nil, err
	}
	return &client{
		cfg:     cfg,
		tr:      tr,
		pending: make(map[int64]chan rpcResponse),
		done:    make(chan struct{}),
	}, nil
}

// newTransport 按传输类型构造传输实现。抽成包级变量以便测试注入内存传输。
var newTransport = func(cfg ServerConfig) (Transport, error) {
	var tr Transport
	switch cfg.Transport {
	case "stdio":
		t, err := newStdioTransport(cfg)
		if err != nil {
			return nil, err
		}
		tr = t
	case "http":
		t, err := newHTTPTransport(cfg)
		if err != nil {
			return nil, err
		}
		tr = t
	default:
		return nil, fmt.Errorf("mcp: %s: unknown transport %q", cfg.Name, cfg.Transport)
	}
	return tr, nil
}

// start 启动传输并完成 MCP 握手（initialize + initialized 通知）。
func (c *client) start(ctx context.Context, timeout time.Duration) error {
	if err := c.tr.Start(c.handleMessage); err != nil {
		return err
	}
	// 传输读取循环结束时关闭客户端，让在途请求立即失败。
	if w, ok := c.tr.(interface{ Wait() <-chan struct{} }); ok {
		go func() {
			select {
			case <-w.Wait():
				c.close()
			case <-c.done:
			}
		}()
	}

	hsCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var res initializeResult
	err := c.callInto(hsCtx, methodInitialize, initializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      clientInfo{Name: "remilia", Version: "1.0.0"},
	}, &res)
	if err != nil {
		return fmt.Errorf("mcp: %s: initialize: %w", c.cfg.Name, err)
	}
	c.mu.Lock()
	c.serverInfo = res.ServerInfo
	c.protocolVersion = res.ProtocolVersion
	c.mu.Unlock()
	if res.ProtocolVersion != "" && res.ProtocolVersion != ProtocolVersion {
		logger.Debugf("[MCP] %s: server protocol %q differs from client %q (continuing)",
			c.cfg.Name, res.ProtocolVersion, ProtocolVersion)
	}
	if nb, err := encodeNotification(methodInitialized, map[string]any{}); err == nil {
		_ = c.tr.Send(hsCtx, nb)
	}
	return nil
}

// listTools 拉取全部工具（自动翻页）。
func (c *client) listTools(ctx context.Context) ([]mcpTool, error) {
	var all []mcpTool
	cursor := ""
	for {
		var res listToolsResult
		if err := c.callInto(ctx, methodListTools, listToolsParams{Cursor: cursor}, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			return all, nil
		}
		cursor = res.NextCursor
	}
}

// callTool 调用一个工具。
func (c *client) callTool(ctx context.Context, name string, args map[string]any) (*callToolResult, error) {
	var res callToolResult
	if err := c.callInto(ctx, methodCallTool, callToolParams{Name: name, Arguments: args}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// callInto 发起请求并把结果反序列化到 out。
func (c *client) callInto(ctx context.Context, method string, params, out any) error {
	raw, err := c.call(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// call 发起一次请求并等待响应。
func (c *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	msg, err := encodeRequest(id, method, params)
	if err != nil {
		return nil, err
	}
	if err := c.tr.Send(ctx, msg); err != nil {
		return nil, err
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("mcp: %s: connection closed", c.cfg.Name)
	}
}

// handleMessage 处理一条入站报文：响应交给等待方，通知就地处理。
func (c *client) handleMessage(raw []byte) {
	var env rpcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		logger.Debugf("[MCP] %s: drop malformed message: %v", c.cfg.Name, err)
		return
	}
	if env.ID != nil {
		c.mu.Lock()
		ch := c.pending[*env.ID]
		c.mu.Unlock()
		if ch != nil {
			ch <- rpcResponse{ID: env.ID, Result: env.Result, Error: env.Error}
		}
		return
	}
	switch env.Method {
	case methodToolsListChanged:
		c.mu.Lock()
		cb := c.onChange
		c.mu.Unlock()
		if cb != nil {
			cb()
		}
	default:
		logger.Debugf("[MCP] %s: notification %q ignored", c.cfg.Name, env.Method)
	}
}

// setOnChange 注册工具列表变化回调。
func (c *client) setOnChange(fn func()) {
	c.mu.Lock()
	c.onChange = fn
	c.mu.Unlock()
}

// close 关闭客户端（幂等）。
func (c *client) close() {
	c.closeOnce.Do(func() {
		_ = c.tr.Close()
		close(c.done)
	})
}

// closed 在客户端关闭时关闭。
func (c *client) closed() <-chan struct{} { return c.done }
