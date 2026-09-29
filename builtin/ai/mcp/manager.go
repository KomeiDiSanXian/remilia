package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// manager.go — 多服务器管理：连接监督、重连、软可用性与工具物化。
//
// 本类型是外部工具服务器的唯一运行实体：它把一个或多个服务器的当前工具集合
// 物化为 [toolkit.Tool]，并在集合或可用性变化时通知目录。工具**不可用**时
// （连接断开）仍保留上次已知的工具并标记为软不可用，调用时才失败——以此避免
// 工具段在 [] 与 [...] 之间抖动击穿前缀缓存。

// Manager 多个外部工具服务器的管理器。
type Manager struct {
	cfg     *Config
	servers []*serverState

	mu       sync.Mutex
	onChange func()
	ctx      context.Context
	cancel   context.CancelFunc
	started  bool
}

// serverState 单个服务器的运行状态。
type serverState struct {
	cfg    ServerConfig
	prefix string

	mu        sync.RWMutex
	client    *client
	connected bool
	tools     []mcpTool
}

// NewManager 校验配置并构造管理器（不建立连接）。
func NewManager(cfg *Config) (*Manager, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	seen := make(map[string]struct{}, len(cfg.Servers))
	prefixes := make(map[string]struct{}, len(cfg.Servers))
	states := make([]*serverState, 0, len(cfg.Servers))
	for _, sc := range cfg.Servers {
		if sc.Disabled {
			continue
		}
		key := strings.ToLower(sc.Name)
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("mcp: duplicate server name %q", sc.Name)
		}
		seen[key] = struct{}{}
		if err := validateServer(cfg, sc); err != nil {
			return nil, err
		}
		prefix := sc.ToolPrefix
		if prefix == "" {
			prefix = "mcp_" + toolkit.SanitizeToolName(sc.Name) + "_"
		}
		prefix = uniquePrefix(prefix, prefixes)
		states = append(states, &serverState{cfg: sc, prefix: prefix})
	}
	return &Manager{cfg: cfg, servers: states}, nil
}

// uniquePrefix 保证模型函数名前缀不重复（不同服务器名规范化后可能相同）。
func uniquePrefix(prefix string, used map[string]struct{}) string {
	if _, ok := used[prefix]; !ok {
		used[prefix] = struct{}{}
		return prefix
	}
	for i := 2; ; i++ {
		candidate := prefix + strconv.Itoa(i) + "_"
		if _, ok := used[candidate]; !ok {
			used[candidate] = struct{}{}
			return candidate
		}
	}
}

// ListTools 返回当前所有可暴露的工具（实现 [toolkit.ToolProvider]）。
func (m *Manager) ListTools() []toolkit.Tool {
	return m.serverTools()
}

// OnToolsChanged 注册工具集合变化回调（实现 [toolkit.ToolChangeNotifier]）。
func (m *Manager) OnToolsChanged(fn func()) {
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

// Start 启动所有服务器的连接与监督（幂等）。
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true
	servers := append([]*serverState(nil), m.servers...)
	m.mu.Unlock()

	for _, s := range servers {
		go m.supervise(s)
	}
}

// Close 停止全部监督并关闭连接。
func (m *Manager) Close() {
	m.mu.Lock()
	cancel := m.cancel
	m.started = false
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, s := range m.servers {
		if cl := s.currentClient(); cl != nil {
			cl.close()
		}
	}
}

// notify 触发一次变化通知（目录侧按名称差异决定是否推进代数）。
func (m *Manager) notify() {
	m.mu.Lock()
	cb := m.onChange
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// supervise 监督单个服务器：连接、列工具、监听断开、按需重连。
func (m *Manager) supervise(s *serverState) {
	interval := s.cfg.reconnectInterval()
	for {
		if m.ctx.Err() != nil {
			return
		}
		if !m.connectOnce(s) {
			if !s.cfg.reconnect() {
				return
			}
			if !m.sleep(interval) {
				return
			}
			if interval < maxReconnectInterval {
				interval *= 2
				if interval > maxReconnectInterval {
					interval = maxReconnectInterval
				}
			}
			continue
		}
		interval = s.cfg.reconnectInterval()

		cl := s.currentClient()
		if cl == nil {
			return
		}
		cl.setOnChange(func() { go m.refresh(s, cl) })
		select {
		case <-m.ctx.Done():
			cl.close()
			return
		case <-cl.closed():
			s.setDisconnected()
			logger.Warnf("[MCP] %s: connection closed", s.cfg.Name)
			m.notify()
		}
		if !s.cfg.reconnect() {
			return
		}
		if !m.sleep(interval) {
			return
		}
	}
}

// connectOnce 建立一次连接并拉取工具；成功返回 true。
func (m *Manager) connectOnce(s *serverState) bool {
	cl, err := newClient(s.cfg)
	if err != nil {
		logger.Warnf("[MCP] %s: %v", s.cfg.Name, err)
		s.setDisconnected()
		m.notify()
		return false
	}
	if err := cl.start(m.ctx, s.cfg.initTimeout()); err != nil {
		logger.Warnf("[MCP] %s: connect failed: %v", s.cfg.Name, err)
		cl.close()
		s.setDisconnected()
		m.notify()
		return false
	}
	listCtx, cancel := context.WithTimeout(m.ctx, s.cfg.timeout())
	tools, err := cl.listTools(listCtx)
	cancel()
	if err != nil {
		logger.Warnf("[MCP] %s: tools/list failed: %v", s.cfg.Name, err)
		cl.close()
		s.setDisconnected()
		m.notify()
		return false
	}
	s.setConnected(cl, tools)
	logger.Infof("[MCP] %s: connected (%d tools)", s.cfg.Name, len(tools))
	m.notify()
	return true
}

// refresh 响应工具列表变化通知：重新拉取并通知目录。
func (m *Manager) refresh(s *serverState, cl *client) {
	if m.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, s.cfg.timeout())
	defer cancel()
	tools, err := cl.listTools(ctx)
	if err != nil {
		logger.Warnf("[MCP] %s: refresh tools failed: %v", s.cfg.Name, err)
		return
	}
	s.setConnected(cl, tools)
	logger.Debugf("[MCP] %s: tool list refreshed (%d tools)", s.cfg.Name, len(tools))
	m.notify()
}

// sleep 等待一段可被关闭打断的时间；返回 false 表示已取消。
func (m *Manager) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-m.ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// setConnected 标记已连接并更新工具集合。
func (s *serverState) setConnected(cl *client, tools []mcpTool) {
	s.mu.Lock()
	s.client = cl
	s.connected = true
	s.tools = tools
	s.mu.Unlock()
}

// setDisconnected 标记断开；保留上次已知工具（软不可用）。
func (s *serverState) setDisconnected() {
	s.mu.Lock()
	s.client = nil
	s.connected = false
	s.mu.Unlock()
}

// currentClient 返回当前连接（无则 nil）。
func (s *serverState) currentClient() *client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

// availability 计算当前可用性：连接中为就绪；曾有过工具但断开为软不可用；
// 从未就绪为来源未加载。
func (s *serverState) availability() catalog.Availability {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.connected {
		return catalog.AvailabilityReady
	}
	if len(s.tools) > 0 {
		return catalog.AvailabilityDisconnected
	}
	return catalog.AvailabilityProvider
}
