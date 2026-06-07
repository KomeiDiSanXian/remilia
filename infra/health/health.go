package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/syncx"
)

// Status 表示整体健康状态。
type Status string

const (
	// Healthy 完全健康 - 所有功能正常
	Healthy Status = "healthy"
	// Degraded 降级但可用 - 部分功能受影响，但核心功能正常
	Degraded Status = "degraded"
	// Unhealthy 不健康 - 核心功能受影响，但服务仍在运行
	Unhealthy Status = "unhealthy"
	// Critical 严重故障 - 服务即将停止或无法正常工作
	Critical Status = "critical"
)

// Level 健康级别（用于数值比较）
type Level int

const (
	HealthyLevel Level = iota
	DegradedLevel
	UnhealthyLevel
	CriticalLevel
)

// StatusToLevel 将状态转换为级别
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

// LevelToStatus 将级别转换为状态
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
type Checker interface {
	Name() string
	Check(ctx context.Context) CheckResult
}

// CheckResult 是单个检查器的结果。
type CheckResult struct {
	Status      Status         `json:"status"`
	Error       string         `json:"error,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Duration    time.Duration  `json:"-"`
	MaxSeverity Status         `json:"-"` // 贡献给整体的最大严重级别，空表示不限制
}

// CheckItem 是响应中单个检查项。
type CheckItem struct {
	Name     string         `json:"name"`
	Status   Status         `json:"status"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Duration float64        `json:"duration_ms"`
}

// CheckGroup 是响应中的一组相关检查。
type CheckGroup struct {
	Name   string      `json:"name"`
	Type   string      `json:"type"`
	Status Status      `json:"status"`
	Checks []CheckItem `json:"checks"`
}

// CheckResponse 是健康检查的完整响应。
type CheckResponse struct {
	Status Status       `json:"status"`
	Groups []CheckGroup `json:"groups"`
	Time   time.Time    `json:"time"`
}

type groupEntry struct {
	groupName string
	groupType string
	checker   Checker
}

// Check 管理多个检查器并提供 HTTP 处理器。
type Check struct {
	entries syncx.Map[string, groupEntry]
	timeout atomic.Int64

	// 结果缓存，避免高频调用时对所有 checker 并发执行
	cacheMu      sync.RWMutex
	cachedResult *CheckResponse
	cacheTime    time.Time
	cacheTTL     time.Duration
}

// DefaultCacheTTL 默认健康检查结果缓存时间
const DefaultCacheTTL = time.Second

func NewCheck() *Check {
	c := &Check{cacheTTL: DefaultCacheTTL}
	c.timeout.Store(int64(5 * time.Second))
	return c
}

func (h *Check) SetTimeout(timeout time.Duration) *Check {
	h.timeout.Store(int64(timeout))
	return h
}

// SetCacheTTL 设置健康检查结果缓存时间。
// 设为 0 禁用缓存（每次调用都执行所有 checker）。
// 变更 TTL 时会清空过期缓存，防止重新启用后返回旧数据。
func (h *Check) SetCacheTTL(ttl time.Duration) *Check {
	h.cacheMu.Lock()
	h.cacheTTL = ttl
	h.cachedResult = nil
	h.cacheTime = time.Time{}
	h.cacheMu.Unlock()
	return h
}

// AddChecker 注册一个无分组的检查器（用于测试或简单场景）。
func (h *Check) AddChecker(checker Checker) {
	h.entries.Store(checker.Name(), groupEntry{checker: checker})
}

// AddGroupedChecker 注册一个带分组的检查器。
// groupName 为组名称（如 bot 名），groupType 为组类型（bot / adapters / apis）。
func (h *Check) AddGroupedChecker(checker Checker, groupName, groupType string) {
	h.entries.Store(checker.Name(), groupEntry{
		groupName: groupName,
		groupType: groupType,
		checker:   checker,
	})
}

func (h *Check) RemoveChecker(name string) {
	h.entries.Delete(name)
}

// CheckerCount 返回已注册的检查器数量（含分组的和未分组的）。
func (h *Check) CheckerCount() int { return h.entries.Len() }

// HasChecker 返回指定名称的检查器是否已注册。
func (h *Check) HasChecker(name string) bool {
	_, ok := h.entries.Load(name)
	return ok
}

// Check 执行所有检查器并按组聚合，返回分组后的 CheckResponse。
func (h *Check) Check(ctx context.Context) CheckResponse {
	// 尝试返回缓存结果
	h.cacheMu.RLock()
	ttl := h.cacheTTL
	cached := h.cachedResult
	cacheTime := h.cacheTime
	h.cacheMu.RUnlock()

	if ttl > 0 && cached != nil && time.Since(cacheTime) < ttl {
		return *cached
	}

	entries := make(map[string]groupEntry, h.entries.Len())
	h.entries.Range(func(name string, e groupEntry) bool {
		entries[name] = e
		return true
	})

	type namedResult struct {
		name   string
		group  string
		gtype  string
		result CheckResult
	}
	resultCh := make(chan namedResult, len(entries))

	var wg sync.WaitGroup
	for name, e := range entries {
		wg.Add(1)
		go func(name string, e groupEntry) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, time.Duration(h.timeout.Load()))
			defer cancel()
			start := time.Now()
			r := e.checker.Check(checkCtx)
			r.Duration = time.Since(start)
			resultCh <- namedResult{name: name, group: e.groupName, gtype: e.groupType, result: r}
		}(name, e)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// groupName -> groupType -> []namedResult
	type groupKey struct{ name, gtype string }
	groups := make(map[groupKey][]namedResult)
	for nr := range resultCh {
		key := groupKey{name: nr.group, gtype: nr.gtype}
		groups[key] = append(groups[key], nr)
	}

	// 按组聚合
	type aggGroup struct {
		name   string
		gtype  string
		status Status
		items  []CheckItem
	}
	var aggGroups []aggGroup
	overallLevel := HealthyLevel

	for key, nrs := range groups {
		items := make([]CheckItem, 0, len(nrs))
		groupLevel := HealthyLevel

		for _, nr := range nrs {
			resultLevel := StatusToLevel(nr.result.Status)
			if nr.result.MaxSeverity != "" {
				if capLevel := StatusToLevel(nr.result.MaxSeverity); resultLevel > capLevel {
					resultLevel = capLevel
				}
			}
			if resultLevel > groupLevel {
				groupLevel = resultLevel
			}
			items = append(items, CheckItem{
				Name:     nr.name,
				Status:   nr.result.Status,
				Error:    nr.result.Error,
				Metadata: nr.result.Metadata,
				Duration: float64(nr.result.Duration) / float64(time.Millisecond),
			})
		}

		groupStatus := LevelToStatus(groupLevel)

		// adapters 组特殊聚合：多 adapter 时不直接取最差
		if key.gtype == "adapters" && len(nrs) > 1 {
			groupStatus = aggregateMultiAdapters(items)
		}

		if l := StatusToLevel(groupStatus); l > overallLevel {
			overallLevel = l
		}

		aggGroups = append(aggGroups, aggGroup{
			name:   key.name,
			gtype:  key.gtype,
			status: groupStatus,
			items:  items,
		})
	}

	resultGroups := make([]CheckGroup, 0, len(aggGroups))
	for _, g := range aggGroups {
		resultGroups = append(resultGroups, CheckGroup{
			Name:   g.name,
			Type:   g.gtype,
			Status: g.status,
			Checks: g.items,
		})
	}
	if resultGroups == nil {
		resultGroups = []CheckGroup{}
	}

	resp := CheckResponse{
		Status: LevelToStatus(overallLevel),
		Groups: resultGroups,
		Time:   time.Now(),
	}

	if ttl > 0 {
		h.cacheMu.Lock()
		h.cachedResult = &resp
		h.cacheTime = time.Now()
		h.cacheMu.Unlock()
	}

	return resp
}

// aggregateMultiAdapters 处理多 adapter 场景：
// 部分 unhealthy → Degraded（而非 Unhealthy），全部 healthy → Healthy。
func aggregateMultiAdapters(items []CheckItem) Status {
	hasUnhealthy := false
	allHealthy := true
	for _, item := range items {
		l := StatusToLevel(item.Status)
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

	_ = json.NewEncoder(w).Encode(response)
}

func (h *Check) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response := h.Check(ctx)
	w.Header().Set("Content-Type", "application/json")

	// Degraded 表示"部分功能受影响但核心功能正常"，对 K8s 等编排系统仍应返回 200
	// 避免编排系统将仍可服务的实例错误地从流量中剔除
	switch response.Status {
	case Healthy, Degraded:
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	_ = json.NewEncoder(w).Encode(response)
}

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
