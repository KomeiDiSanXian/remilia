package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// client.go — 单个服务器的协议客户端：报文收发、请求-响应关联与会话编排。
//
// 请求按数字 id 关联响应；服务器主动发来的通知（如工具列表变化）在读取循环里
// 处理并回调，不阻塞请求。
//
// 客户端本身不关心"协议方言"：握手流程与无状态流程各由一个 [protocolSession]
// 实现（protocol_handshake.go / protocol_stateless.go），二者共用这里的 id
// 分配、等待队列与通知路由。

// protocolSession 一条协议流程（协议方言）的实现。两种流程共享 client 的
// 收发设施，只在"怎么协商、怎么订阅"上不同。
type protocolSession interface {
	// Connect 建立连接并完成该流程的协商（握手流程的 initialize；无状态流程的
	// server/discover）。对端时代不符时返回 [errLegacyServer]。
	Connect(ctx context.Context, timeout time.Duration) error
	// ListTools 拉取全部工具（自动翻页）。
	ListTools(ctx context.Context) ([]mcpTool, error)
	// CallTool 调用一个工具。
	CallTool(ctx context.Context, name string, args map[string]any) (*callToolResult, error)
	// Watch 开始接收服务器推送的变更通知。握手流程的通知经既有通道到达，为空
	// 实现；无状态流程需显式订阅（subscriptions/listen）。
	Watch(ctx context.Context) error
}

type client struct {
	cfg  ServerConfig
	tr   Transport
	sess protocolSession

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

// session 返回当前生效的协议流程；尚未连接时为 nil。
func (c *client) session() protocolSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sess
}

// setSession 记录生效的协议流程。
func (c *client) setSession(s protocolSession) {
	c.mu.Lock()
	c.sess = s
	c.mu.Unlock()
}

// connect 选择协议流程并完成协商。
//
// 显式允许时先探测无状态流程；对端不是现代实现（返回 [errLegacyServer]）时
// 回退到握手流程。时代判定是对端的属性而非单次请求的属性，因此一次探测的结论
// 即代表该对端。
func (c *client) connect(ctx context.Context, timeout time.Duration) error {
	if c.cfg.AllowStateless {
		sess := &statelessSession{c: c}
		err := sess.Connect(ctx, timeout)
		if err == nil {
			c.setSession(sess)
			return nil
		}
		if !errors.Is(err, errLegacyServer) {
			return err
		}
		logger.Debugf("[MCP] %s: server does not speak the stateless flow, falling back to the initialize handshake: %v", c.cfg.Name, err)
		recordProtocolFallback(c.cfg.Name)
		// 清掉探测时带上的版本，交给握手流程重新协商。
		c.setProtocolVersion("")
	}
	sess := &handshakeSession{c: c}
	if err := sess.Connect(ctx, timeout); err != nil {
		return err
	}
	c.setSession(sess)
	return nil
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
	if err := c.connect(ctx, timeout); err != nil {
		return err
	}
	recordProtocolNegotiation(c.cfg.Name, c.currentProtocolVersion())
	return nil
}

// setProtocolVersion 记录生效的协议版本；HTTP 传输还须在后续请求头上携带它。
func (c *client) setProtocolVersion(v string) {
	c.mu.Lock()
	c.protocolVersion = v
	c.mu.Unlock()
	if setter, ok := c.tr.(protocolVersionSetter); ok {
		setter.SetProtocolVersion(v)
	}
}

// currentProtocolVersion 返回当前生效的协议版本（未协商出时为空）。
func (c *client) currentProtocolVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.protocolVersion
}

// setServerInfo 记录对端自报的身份（仅用于日志/展示）。
func (c *client) setServerInfo(info serverInfo) {
	c.mu.Lock()
	c.serverInfo = info
	c.mu.Unlock()
}

// listTools 拉取全部工具（自动翻页）。
func (c *client) listTools(ctx context.Context) ([]mcpTool, error) {
	sess := c.session()
	if sess == nil {
		return nil, fmt.Errorf("mcp: %s: client is not connected", c.cfg.Name)
	}
	return sess.ListTools(ctx)
}

// callTool 调用一个工具。
func (c *client) callTool(ctx context.Context, name string, args map[string]any) (*callToolResult, error) {
	sess := c.session()
	if sess == nil {
		return nil, fmt.Errorf("mcp: %s: client is not connected", c.cfg.Name)
	}
	return sess.CallTool(ctx, name, args)
}

// watch 开始接收服务器推送的变更通知（见 [protocolSession.Watch]）。
func (c *client) watch(ctx context.Context) error {
	sess := c.session()
	if sess == nil {
		return fmt.Errorf("mcp: %s: client is not connected", c.cfg.Name)
	}
	return sess.Watch(ctx)
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

// call 发起一次请求并等待响应，顺带记录请求次数与耗时。
func (c *client) call(ctx context.Context, method string, params any) (raw json.RawMessage, err error) {
	start := time.Now()
	defer func() { recordRPCRequest(c.cfg.Name, method, time.Since(start), err) }()

	id := c.nextRequestID()
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

// nextRequestID 分配一个连接内唯一的请求 id。无状态流程的订阅流需要在请求之外
// 自行分配 id，故单独暴露。
func (c *client) nextRequestID() int64 { return c.nextID.Add(1) }

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
		// 无状态流程下该通知经 subscriptions/listen 到达（带订阅 id）；本接入只
		// 订阅工具列表变化，故不区分来源：任何工具列表变化都触发刷新。
		recordRPCNotification(c.cfg.Name, env.Method)
		c.mu.Lock()
		cb := c.onChange
		c.mu.Unlock()
		if cb != nil {
			cb()
		}
	default:
		if env.Method != "" {
			recordRPCNotification(c.cfg.Name, env.Method)
		}
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

// alive 报告连接是否仍可用：ctx 未取消且客户端未关闭。
func (c *client) alive(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}
