package mcp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// protocol_stateless.go — 无状态流程（2026-07-28 起）：没有握手与会话。
//
// 版本、客户端能力与身份改经**每个请求**的 _meta 传递；对端是否支持所请求的
// 版本由 server/discover 回答（或由任一请求的 UnsupportedProtocolVersionError
// 回答）。变更通知不再是服务器自主推送，而要用 subscriptions/listen 显式订阅。
//
// 与握手流程的边界：本文件只描述"无状态方言怎么谈"。HTTP 头镜像在传输层，
// 进程/连接生命周期在传输层，准入、SSRF/TLS、命令与环境白名单等安全边界在
// security.go——它们都不感知本文件。

// errLegacyServer 表示对端不是现代（无状态）实现：客户端据此回退到握手流程。
var errLegacyServer = errors.New("mcp: server does not implement the stateless flow")

// eraMismatch 把探测失败归一为"对端不是无状态实现"，并保留原始原因便于诊断。
func eraMismatch(cause error) error {
	return fmt.Errorf("%w: %w", errLegacyServer, cause)
}

// statelessSession 无状态流程的 [protocolSession] 实现。
type statelessSession struct{ c *client }

// Connect 探测对端是否说无状态方言。
//
// 无状态流程的版本不靠握手协商，因此先按首选版本声明（HTTP 上协议版本头与
// 报文体的 _meta 必须一致），再由 server/discover 的 supportedVersions 确认
// 对端确实支持这一版。任何探测失败都判为"非现代对端"并交出 [errLegacyServer]：
// 现代服务器会用可识别的 JSON-RPC 错误应答（如 UnsupportedProtocolVersionError、
// 方法不存在），而握手时代服务器只会返回空响应或实现自定义的错误。二者的正确
// 处置都是改用握手流程——后者按握手协商版本，正对应"用对端支持的版本重试"。
func (s *statelessSession) Connect(ctx context.Context, timeout time.Duration) error {
	c := s.c
	pvCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c.setProtocolVersion(StatelessProtocolVersion)
	params := discoverParams{}
	s.fillMeta(&params)
	var res discoverResult
	if err := c.callInto(pvCtx, methodDiscover, params, &res); err != nil {
		// 现代服务器会用可识别的错误作答，先把它与"握手时代"区分开记下来，
		// 便于排障（二者的处置相同：改用握手流程）。
		var rpcErr *rpcError
		if errors.As(err, &rpcErr) && rpcErr.Code == rpcCodeUnsupportedProtocolVersion {
			logger.Debugf("[MCP] %s: server rejected %s: %s", c.cfg.Name, StatelessProtocolVersion, rpcErr.Message)
		}
		return eraMismatch(err)
	}
	if err := checkResultType(res.ResultType); err != nil {
		return err
	}
	best, ok := bestSupportedVersion(res.SupportedVersions)
	if !ok || best != StatelessProtocolVersion {
		return eraMismatch(fmt.Errorf("server advertises versions %v", res.SupportedVersions))
	}
	if res.Meta.ServerInfo.Name != "" {
		c.setServerInfo(res.Meta.ServerInfo)
	}
	if res.Instructions != "" {
		logger.Debugf("[MCP] %s: server instructions: %s", c.cfg.Name, res.Instructions)
	}
	return nil
}

// ListTools 拉取全部工具（自动翻页）。
func (s *statelessSession) ListTools(ctx context.Context) ([]mcpTool, error) {
	var all []mcpTool
	cursor := ""
	for {
		params := listToolsParams{Cursor: cursor}
		s.fillMeta(&params)
		var res listToolsResult
		if err := s.c.callInto(ctx, methodListTools, params, &res); err != nil {
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
func (s *statelessSession) CallTool(ctx context.Context, name string, args map[string]any) (*callToolResult, error) {
	params := callToolParams{Name: name, Arguments: args}
	s.fillMeta(&params)
	var res callToolResult
	if err := s.c.callInto(ctx, methodCallTool, params, &res); err != nil {
		return nil, err
	}
	if err := checkResultType(res.ResultType); err != nil {
		return nil, err
	}
	return &res, nil
}

// Watch 订阅工具列表变化（subscriptions/listen）。订阅是长连请求，故异步进行：
// HTTP 上它是响应流，stdio 上它是共享通道里的一条订阅。流意外结束后按配置的
// 重连间隔退避重订阅；客户端关闭或 ctx 取消即退出。
func (s *statelessSession) Watch(ctx context.Context) error {
	go s.listen(ctx)
	return nil
}

// listen 维护订阅长连。
func (s *statelessSession) listen(ctx context.Context) {
	c := s.c
	backoff := c.cfg.reconnectInterval()
	for {
		if !c.alive(ctx) {
			return
		}
		msg, err := s.listenMessage()
		if err != nil {
			logger.Warnf("[MCP] %s: build subscription request: %v", c.cfg.Name, err)
			return
		}
		if err := c.tr.SendStream(ctx, msg); err != nil && c.alive(ctx) {
			logger.Debugf("[MCP] %s: subscription stream ended: %v", c.cfg.Name, err)
		}
		if !c.alive(ctx) || !c.cfg.reconnect() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-time.After(backoff):
		}
		if backoff < maxReconnectInterval {
			backoff *= 2
			if backoff > maxReconnectInterval {
				backoff = maxReconnectInterval
			}
		}
	}
}

// listenMessage 构造一次 subscriptions/listen 请求。每次（重）订阅都用新的 id：
// 订阅 id 即该请求的 id，重订阅视为一条新订阅。
func (s *statelessSession) listenMessage() ([]byte, error) {
	params := subscriptionsListenParams{Notifications: subscriptionFilter{ToolsListChanged: true}}
	s.fillMeta(&params)
	return encodeRequest(s.c.nextRequestID(), methodSubscriptionsListen, params)
}

// fillMeta 给请求参数补上无状态流程要求的 _meta：协议版本、客户端能力与身份。
// 已存在的键不覆盖，便于单个请求追加专用元数据。
func (s *statelessSession) fillMeta(m metaCarrier) {
	out := map[string]any{
		metaProtocolVersion:    StatelessProtocolVersion,
		metaClientCapabilities: map[string]any{},
		metaClientInfo:         clientInfo{Name: clientName, Version: clientVersion},
	}
	maps.Copy(out, m.getMeta())
	m.setMeta(out)
}
