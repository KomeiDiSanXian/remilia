// Package health 提供统一的健康检查树（HealthTree）模型。
//
// 核心概念：
//   - Checker：单个健康检查单元，实现 Check(ctx) CheckResult
//   - Node：树中的任意节点（叶子或分支），统一结构
//   - Check：检查器管理器，提供 Register / Check / HTTPHandler
//
// 注册方式：
//
//	check.Register(probe, "system", "dependencies", "openai")
//	check.RegisterWithKind(adapter, "adapter", "system", "bots", "remilia", "qq")
//
// 输出视图：
//   - GET /health          → 摘要视图（SummaryReport），人类可读
//   - GET /health?view=full → 完整树视图（CheckResponse），监控/调试用
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Status 表示健康状态。
type Status string

const (
	Healthy   Status = "healthy"
	Degraded  Status = "degraded"
	Unhealthy Status = "unhealthy"
	Critical  Status = "critical"
)

// Level 健康级别（用于数值比较）。
type Level int

const (
	HealthyLevel   Level = iota // 0
	DegradedLevel               // 1
	UnhealthyLevel              // 2
	CriticalLevel               // 3
)

// StatusToLevel 将状态转换为可比较的级别。
func StatusToLevel(status Status) Level {
	switch status {
	case Healthy:
		return HealthyLevel
	case Degraded:
		return DegradedLevel
	case Unhealthy:
		return UnhealthyLevel
	case Critical:
		return CriticalLevel
	default:
		return UnhealthyLevel
	}
}

// LevelToStatus 将级别转换为状态。
func LevelToStatus(level Level) Status {
	switch level {
	case HealthyLevel:
		return Healthy
	case DegradedLevel:
		return Degraded
	case UnhealthyLevel:
		return Unhealthy
	case CriticalLevel:
		return Critical
	default:
		return Unhealthy
	}
}

// Checker 定义单个健康检查单元。
//
// 实现方需提供：
//   - Name() — 唯一标识，用于树中定位和日志
//   - Check() — 执行检查，返回状态、元数据、耗时
type Checker interface {
	Name() string
	Check(ctx context.Context) CheckResult
}

// CheckResult 是单个检查器的原始结果，内部使用。
//
//   - Error: 故障描述，会映射到 Node.Message
//   - MaxSeverity: 该检查贡献给上层聚合的最大严重级别，空表示不限制
type CheckResult struct {
	Status      Status         `json:"status"`
	Error       string         `json:"error,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Duration    time.Duration  `json:"-"`
	MaxSeverity Status         `json:"-"`
}

// Node 是健康检查树中的统一节点。
//
// 每个节点包含名称、类型、状态、元数据，以及可选的子节点列表。
// 叶子节点由 Checker 执行产生；分支节点由 Register 路径自动创建。
//
// 序列化规则：
//   - Status: group 类型不输出，由框架自动聚合
//   - DurationMs: 仅执行探测的节点（adapter / dependency）输出
//   - Message: 仅在非 healthy 时输出
//   - Children: 仅在分支节点输出
type Node struct {
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Status     Status         `json:"status,omitempty"`
	Message    string         `json:"message,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	Children   []*Node        `json:"children,omitempty"`
}

// CheckResponse 是 /health?view=full 输出的完整树视图。
type CheckResponse struct {
	Status    Status    `json:"status"`
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildTime string    `json:"build_time"`
	Time      time.Time `json:"time"`
	Root      *Node     `json:"root"`
}

// CategorySummary 是某一类资源的健康统计。
type CategorySummary struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Degraded  int `json:"degraded,omitempty"`
	Unhealthy int `json:"unhealthy,omitempty"`
}

// SummarySection 是摘要视图的分类汇总。
type SummarySection struct {
	Bots           CategorySummary `json:"bots"`
	Dependencies   CategorySummary `json:"dependencies"`
	Infrastructure Status          `json:"infrastructure"`
}

// IssueItem 是摘要视图中的异常项，仅包含非 healthy 的叶子节点。
type IssueItem struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// SummaryReport 是 /health 默认输出的摘要视图。
type SummaryReport struct {
	Status    Status         `json:"status"`
	Version   string         `json:"version"`
	Commit    string         `json:"commit"`
	BuildTime string         `json:"build_time"`
	Time      time.Time      `json:"time"`
	Summary   SummarySection `json:"summary"`
	Issues    []IssueItem    `json:"issues"`
}

// internalNode 是检查器树的内部节点。
type internalNode struct {
	name     string
	kind     string
	checker  Checker
	children map[string]*internalNode
}

// Check 管理一组检查器，提供注册、执行和 HTTP 输出。
//
// 使用示例：
//
//	c := health.NewCheck()
//	c.Register(probe, "system", "dependencies", "openai")
//	c.Register(adapter, "system", "bots", "remilia", "adapters", "qq")
//	http.Handle("/health", c)
type Check struct {
	mu      sync.Mutex
	root    *internalNode
	timeout atomic.Int64

	// Version / Commit / BuildTime 由上层注入，输出时自动填充到响应中。
	Version   string
	Commit    string
	BuildTime string

	cacheMu      sync.RWMutex
	cachedResult *CheckResponse
	cacheTime    time.Time
	cacheTTL     time.Duration
}

// DefaultCacheTTL 是健康检查结果的默认缓存时间。
const DefaultCacheTTL = 8 * time.Second

// NewCheck 创建健康检查管理器。
func NewCheck() *Check {
	c := &Check{cacheTTL: DefaultCacheTTL}
	c.timeout.Store(int64(3 * time.Second))
	return c
}

// SetTimeout 设置每个检查器的执行超时，支持链式调用。
func (h *Check) SetTimeout(timeout time.Duration) *Check {
	h.timeout.Store(int64(timeout))
	return h
}

// SetCacheTTL 设置结果缓存时间。设为 0 禁用缓存。
func (h *Check) SetCacheTTL(ttl time.Duration) *Check {
	h.cacheMu.Lock()
	h.cacheTTL = ttl
	h.cachedResult = nil
	h.cacheTime = time.Time{}
	h.cacheMu.Unlock()
	return h
}

// AddChecker 注册一个无路径的检查器（用于测试或简单场景），
// 等价于 Register(checker, "system", checker.Name())。
func (h *Check) AddChecker(checker Checker) {
	h.Register(checker, "system", checker.Name())
}

// Register 将检查器注册到树中指定路径。
//
// path 至少一段，中间节点自动创建，kind 从路径自动推导：
//
//	system/bots/{name}/...        → kind=bot
//	system/adapters/{name}        → kind=adapter
//	system/dependencies/{name}    → kind=dependency
//	其他中间目录                  → kind=group
//
// 如需显式指定 kind，请使用 [Check.RegisterWithKind]。
func (h *Check) Register(checker Checker, path ...string) {
	h.register(checker, "", path...)
}

// RegisterWithKind 注册检查器并显式指定叶子节点的 kind。
//
//	check.RegisterWithKind(probe, "dependency", "system", "deps", "openai")
func (h *Check) RegisterWithKind(checker Checker, kind string, path ...string) {
	h.register(checker, kind, path...)
}

func (h *Check) register(checker Checker, kindOverride string, path ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(path) == 0 {
		return
	}
	if h.root == nil {
		h.root = &internalNode{name: path[0]}
		h.root.kind = deduceKind(h.root.name, "", true)
	}
	current := h.root
	for i := 1; i < len(path); i++ {
		name := path[i]
		next, ok := current.children[name]
		if !ok {
			if current.children == nil {
				current.children = make(map[string]*internalNode)
			}
			next = &internalNode{name: name}
			next.kind = deduceKind(next.name, current.kind, false)
			current.children[name] = next
		}
		current = next
	}
	if kindOverride != "" {
		current.kind = kindOverride
	}
	current.checker = checker
}

func deduceKind(name, parentKind string, isRoot bool) string {
	if isRoot {
		return "system"
	}
	switch parentKind {
	case "bots":
		return "bot"
	case "adapters":
		return "adapter"
	case "dependencies":
		return "dependency"
	case "plugins":
		return "plugin"
	case "infrastructure":
		if k, ok := knownKinds[name]; ok {
			return k
		}
	}
	if k, ok := knownKinds[name]; ok {
		return k
	}
	return name
}

var knownKinds = map[string]string{
	"lifecycle": "lifecycle",
	"engine":    "engine",
	"runtime":   "runtime",
	"scheduler": "scheduler",
	"memory":    "cache",
	"eventbus":  "eventbus",
}

func shouldOutputStatus(kind string) bool {
	switch kind {
	case "bots", "dependencies", "infrastructure", "adapters", "plugins":
		return false
	default:
		return true
	}
}

func shouldOutputDuration(kind string) bool {
	switch kind {
	case "lifecycle", "engine", "runtime", "scheduler", "bot", "system",
		"bots", "dependencies", "infrastructure", "adapters", "plugins":
		return false
	default:
		return true
	}
}

// RemoveChecker 按检查器名称移除已注册的检查器。
func (h *Check) RemoveChecker(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.root == nil {
		return
	}
	h.removeNode(h.root, name)
}

// CheckerCount 返回已注册的检查器总数。
func (h *Check) CheckerCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.root == nil {
		return 0
	}
	count := 0
	h.countCheckers(h.root, &count)
	return count
}

// HasChecker 返回指定名称的检查器是否已注册。
func (h *Check) HasChecker(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.root == nil {
		return false
	}
	return h.findChecker(h.root, name)
}

func (h *Check) countCheckers(n *internalNode, count *int) {
	if n.checker != nil {
		*count++
	}
	for _, child := range n.children {
		h.countCheckers(child, count)
	}
}

func (h *Check) findChecker(n *internalNode, name string) bool {
	if n.checker != nil && n.name == name {
		return true
	}
	for _, child := range n.children {
		if h.findChecker(child, name) {
			return true
		}
	}
	return false
}

func (h *Check) removeNode(n *internalNode, name string) bool {
	for cn, child := range n.children {
		if child.name == name && child.checker != nil {
			delete(n.children, cn)
			return true
		}
		if h.removeNode(child, name) {
			return true
		}
	}
	return false
}

// Check 执行所有已注册的检查器，返回完整树。
func (h *Check) Check(ctx context.Context) CheckResponse {
	h.cacheMu.RLock()
	ttl := h.cacheTTL
	cached := h.cachedResult
	cacheTime := h.cacheTime
	h.cacheMu.RUnlock()

	if ttl > 0 && cached != nil && time.Since(cacheTime) < ttl {
		return *cached
	}

	results := h.runAllCheckers(ctx)

	h.mu.Lock()
	rootCopy := h.root
	h.mu.Unlock()

	if rootCopy == nil {
		resp := CheckResponse{
			Status: Healthy,
			Root:   &Node{Name: "system", Kind: "system", Status: Healthy},
			Time:   time.Now(),
		}
		return resp
	}

	node := h.buildNode(rootCopy, "/"+rootCopy.name, results)

	resp := CheckResponse{
		Status:    node.Status,
		Root:      node,
		Version:   h.Version,
		Commit:    h.Commit,
		BuildTime: h.BuildTime,
		Time:      time.Now(),
	}

	if ttl > 0 {
		h.cacheMu.Lock()
		h.cachedResult = &resp
		h.cacheTime = time.Now()
		h.cacheMu.Unlock()
	}

	return resp
}

func (h *Check) runAllCheckers(ctx context.Context) map[string]CheckResult {
	h.mu.Lock()
	root := h.root
	h.mu.Unlock()

	entries := make(map[string]Checker)
	if root != nil {
		h.collectCheckers(root, "", entries)
	}

	results := make(map[string]CheckResult)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for fullPath, checker := range entries {
		wg.Add(1)
		go func(fp string, ck Checker) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, time.Duration(h.timeout.Load()))
			defer cancel()
			start := time.Now()
			r := ck.Check(checkCtx)
			r.Duration = time.Since(start)
			mu.Lock()
			results[fp] = r
			mu.Unlock()
		}(fullPath, checker)
	}
	wg.Wait()
	return results
}

func (h *Check) collectCheckers(n *internalNode, prefix string, out map[string]Checker) {
	fp := prefix + "/" + n.name
	if n.checker != nil {
		out[fp] = n.checker
	}
	for _, child := range n.children {
		h.collectCheckers(child, fp, out)
	}
}

func (h *Check) buildNode(n *internalNode, fullPath string, results map[string]CheckResult) *Node {
	node := &Node{Name: n.name, Kind: n.kind}

	for _, child := range n.children {
		childPath := fullPath + "/" + child.name
		node.Children = append(node.Children, h.buildNode(child, childPath, results))
	}
	sort.Slice(node.Children, func(i, j int) bool {
		return node.Children[i].Name < node.Children[j].Name
	})

	if n.checker != nil {
		if result, ok := results[fullPath]; ok {
			node.Status = result.Status
			if result.Error != "" {
				node.Message = result.Error
			}
			if result.MaxSeverity != "" {
				if l := StatusToLevel(node.Status); l > StatusToLevel(result.MaxSeverity) {
					node.Status = result.MaxSeverity
				}
			}
			node.Metadata = result.Metadata
			if shouldOutputDuration(n.kind) {
				node.DurationMs = result.Duration.Milliseconds()
			}
		}
	}

	if len(node.Children) > 0 {
		agg := h.aggregateNode(node)
		if n.checker == nil {
			node.Status = agg
		}
	}

	if !shouldOutputStatus(n.kind) {
		node.Status = ""
	}

	return node
}

func (h *Check) aggregateNode(node *Node) Status {
	if len(node.Children) == 0 {
		return node.Status
	}
	if node.Kind == "adapters" {
		return h.aggregateAdapters(node.Children)
	}
	worst := HealthyLevel
	for _, child := range node.Children {
		s := child.Status
		if s == "" {
			s = h.aggregateNode(child)
		}
		if l := StatusToLevel(s); l > worst {
			worst = l
		}
	}
	return LevelToStatus(worst)
}

func (h *Check) aggregateAdapters(children []*Node) Status {
	if len(children) <= 1 {
		if len(children) == 0 {
			return Healthy
		}
		return children[0].Status
	}
	hasUnhealthy := false
	allHealthy := true
	for _, child := range children {
		s := child.Status
		l := StatusToLevel(s)
		if l >= UnhealthyLevel {
			hasUnhealthy = true
		}
		if l != HealthyLevel {
			allHealthy = false
		}
	}
	if allHealthy {
		return Healthy
	}
	if hasUnhealthy {
		return Degraded
	}
	return Degraded
}

// --- 视图输出 ---

// BuildSummary 从完整树生成摘要视图。
//
// 遍历规则：
//   - 按 kind 分三类：bots / dependencies / infrastructure
//   - 统计各类别的状态分布
//   - issues 收集所有非 healthy 的叶子节点（不含 group）
func BuildSummary(root *Node) *SummaryReport {
	if root == nil {
		return &SummaryReport{}
	}

	r := &SummaryReport{Status: root.Status}

	for _, child := range root.Children {
		switch child.Name {
		case "bots":
			r.Summary.Bots = countByStatus(child, "bot")
		case "dependencies":
			r.Summary.Dependencies = countByStatus(child, "dependency")
		case "infrastructure":
			r.Summary.Infrastructure = aggregateGroupStatus(child)
		}
	}

	r.Issues = collectIssues(root)
	return r
}

func countByStatus(node *Node, targetKind string) CategorySummary {
	var cs CategorySummary
	for _, child := range node.Children {
		if child.Kind != targetKind {
			continue
		}
		cs.Total++
		switch child.Status {
		case Healthy:
			cs.Healthy++
		case Degraded:
			cs.Degraded++
		case Unhealthy, Critical:
			cs.Unhealthy++
		}
	}
	return cs
}

func aggregateGroupStatus(node *Node) Status {
	if len(node.Children) == 0 {
		return Healthy
	}
	worst := HealthyLevel
	for _, child := range node.Children {
		l := StatusToLevel(child.Status)
		if l > worst {
			worst = l
		}
	}
	return LevelToStatus(worst)
}

func collectIssues(root *Node) []IssueItem {
	var issues []IssueItem
	walkIssues(root, &issues)
	return issues
}

func walkIssues(node *Node, out *[]IssueItem) {
	if len(node.Children) > 0 {
		for _, child := range node.Children {
			walkIssues(child, out)
		}
		return
	}
	if node.Status != Healthy && node.Status != "" {
		*out = append(*out, IssueItem{
			Kind:    node.Kind,
			Name:    node.Name,
			Status:  node.Status,
			Message: node.Message,
		})
	}
}

// HTTPHandler 处理 HTTP 健康检查请求。
//
// 路由规则：
//   - GET /health           → 摘要视图（SummaryReport），人类可读
//   - GET /health?view=full → 完整树视图（CheckResponse），监控/调试用
//
// 返回的 HTTP 状态码：
//   - 200: Healthy 或 Degraded
//   - 503: Unhealthy 或 Critical
func (h *Check) HTTPHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")

	switch response.Status {
	case Healthy, Degraded:
		w.WriteHeader(http.StatusOK)
	case Unhealthy:
		w.WriteHeader(http.StatusServiceUnavailable)
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}

	if r.URL.Query().Get("view") == "full" {
		_ = json.NewEncoder(w).Encode(response)
		return
	}

	summary := BuildSummary(response.Root)
	summary.Status = response.Status
	summary.Version = response.Version
	summary.Commit = response.Commit
	summary.BuildTime = response.BuildTime
	summary.Time = response.Time
	_ = json.NewEncoder(w).Encode(summary)
}

// ReadinessHandler 用于 K8s readiness probe。
// Unhealthy / Critical 返回 503，其余返回 200。
func (h *Check) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")
	switch response.Status {
	case Healthy, Degraded:
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(response)
}

// LivenessHandler 用于 K8s liveness probe。
// Unhealthy 返回 503，其余返回 200。
func (h *Check) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")
	if response.Status == Unhealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(response)
}
