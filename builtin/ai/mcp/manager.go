package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
	cfg       ServerConfig
	prefix    string
	refreshCh chan struct{}

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
		states = append(states, &serverState{cfg: sc, prefix: prefix, refreshCh: make(chan struct{}, 1)})
	}
	// 预置 0 值序列，让"从未连上"的服务器也能在监控里被看到。
	for _, s := range states {
		recordServerUp(s.cfg.Name, false)
		recordServerTools(s.cfg.Name, 0)
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

// supervise 监督单个服务器：连接、列工具、监听断开与列表变化、按需重连。
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
		// 连接存续期间串行处理工具列表变化通知（与断开互斥），避免并发的
		// 刷新把集合改来改去。
		live := true
		for live {
			select {
			case <-m.ctx.Done():
				cl.close()
				return
			case <-cl.closed():
				live = false
				s.setDisconnected()
				logger.Warnf("[MCP] %s: connection closed", s.cfg.Name)
				m.notify()
			case <-s.refreshCh:
				m.refreshStable(s, cl)
			}
		}
		if !s.cfg.reconnect() {
			return
		}
		recordReconnect(s.cfg.Name)
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
		recordConnect(s.cfg.Name, "error")
		s.setDisconnected()
		m.notify()
		return false
	}
	// 变化通知在握手期间也可能到达：连接一建立就注册回调，避免漏掉早期通知。
	cl.setOnChange(func() { m.requestRefresh(s) })
	if err := cl.start(m.ctx, s.cfg.initTimeout()); err != nil {
		logger.Warnf("[MCP] %s: connect failed: %v", s.cfg.Name, err)
		recordConnect(s.cfg.Name, "error")
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
		recordConnect(s.cfg.Name, "error")
		cl.close()
		s.setDisconnected()
		m.notify()
		return false
	}
	recordConnect(s.cfg.Name, "ok")
	if !s.hasTools() || sameToolSet(tools, s.currentTools()) {
		s.setConnected(cl, tools)
		logger.Infof("[MCP] %s: connected (%d tools)", s.cfg.Name, len(tools))
		m.notify()
		return true
	}
	// 重连后集合已变化：先恢复调用能力（用新连接），但集合要等稳定窗口确认后
	// 再切换，避免服务器重启期间读到半截列表而推进目录代数。
	s.setClient(cl)
	logger.Infof("[MCP] %s: reconnected, waiting for tool list to stabilize", s.cfg.Name)
	m.stabilize(s, cl, tools)
	return true
}

// requestRefresh 请求对该服务器做一次带稳定的刷新；突发通知会被合并
// （通道容量为 1 且非阻塞发送）。
func (m *Manager) requestRefresh(s *serverState) {
	select {
	case s.refreshCh <- struct{}{}:
	default:
	}
}

// refreshStable 响应工具列表变化通知：先读出候选集合，只有它在稳定窗口后
// 仍然不变才纳入，避免服务器重启/抖动期间集合反复变化。
func (m *Manager) refreshStable(s *serverState, cl *client) {
	if m.ctx.Err() != nil {
		return
	}
	candidate, err := m.fetchTools(s, cl)
	if err != nil {
		logger.Warnf("[MCP] %s: refresh tools failed: %v", s.cfg.Name, err)
		return
	}
	if sameToolSet(candidate, s.currentTools()) {
		return
	}
	m.stabilize(s, cl, candidate)
}

// stabilize 采用一份"候选"集合：等待稳定窗口后在窗口另一端复读，只有两次
// 读到完全一致的集合才真正切换；否则交还监督循环，等下一轮继续确认收敛。
func (m *Manager) stabilize(s *serverState, cl *client, candidate []mcpTool) {
	if !m.waitStable(cl, s.cfg.stabilizeWindow()) {
		return
	}
	confirmed, err := m.fetchTools(s, cl)
	if err != nil {
		logger.Warnf("[MCP] %s: confirm tools failed: %v", s.cfg.Name, err)
		return
	}
	if !sameToolSet(candidate, confirmed) {
		// 集合仍在变化：重新排队，等下一轮再确认。
		recordToolListChange(s.cfg.Name, "jitter")
		m.requestRefresh(s)
		return
	}
	s.setConnected(cl, confirmed)
	recordToolListChange(s.cfg.Name, "adopted")
	logger.Debugf("[MCP] %s: tool list stabilized (%d tools)", s.cfg.Name, len(confirmed))
	m.notify()
}

// fetchTools 以配置的超时拉取一次工具列表。
func (m *Manager) fetchTools(s *serverState, cl *client) ([]mcpTool, error) {
	ctx, cancel := context.WithTimeout(m.ctx, s.cfg.timeout())
	defer cancel()
	return cl.listTools(ctx)
}

// waitStable 等待稳定窗口；连接关闭或管理器停止时提前返回 false。
func (m *Manager) waitStable(cl *client, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-m.ctx.Done():
		return false
	case <-cl.closed():
		return false
	case <-t.C:
		return true
	}
}

// sameToolSet 报告两份工具列表是否声明同一组工具。比较顺序无关，且比较完整
// 定义（同名工具改 schema/注解也算变化）。
func sameToolSet(a, b []mcpTool) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]mcpTool(nil), a...)
	bs := append([]mcpTool(nil), b...)
	sort.Slice(as, func(i, j int) bool { return as[i].Name < as[j].Name })
	sort.Slice(bs, func(i, j int) bool { return bs[i].Name < bs[j].Name })
	for i := range as {
		rawA, errA := json.Marshal(as[i])
		rawB, errB := json.Marshal(bs[i])
		if errA != nil || errB != nil || !bytes.Equal(rawA, rawB) {
			return false
		}
	}
	return true
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
	recordServerUp(s.cfg.Name, true)
	recordServerTools(s.cfg.Name, len(tools))
}

// setClient 只标记连接可用、保留现有工具集合（重连时先恢复调用能力）。
func (s *serverState) setClient(cl *client) {
	s.mu.Lock()
	s.client = cl
	s.connected = true
	s.mu.Unlock()
	recordServerUp(s.cfg.Name, true)
}

// currentTools 返回当前采用的工具集合副本。
func (s *serverState) currentTools() []mcpTool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]mcpTool(nil), s.tools...)
}

// hasTools 报告当前是否已有采用的工具集合。
func (s *serverState) hasTools() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tools) > 0
}

// setDisconnected 标记断开；保留上次已知工具（软不可用）。
func (s *serverState) setDisconnected() {
	s.mu.Lock()
	s.client = nil
	s.connected = false
	s.mu.Unlock()
	recordServerUp(s.cfg.Name, false)
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
