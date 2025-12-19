package remilia

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

type (
	// Rule 规则函数，用于判断事件是否匹配特定条件
	//
	// 重要：Rule 函数应该是纯函数（pure function）：
	// - 只读取 Context，不修改 Context 状态
	// - 不修改外部变量或调用外部 API
	// - 相同输入总是返回相同输出（幂等性）
	//
	// 原因：
	// - And/Or 规则使用短路优化，副作用可能不执行
	// - 规则可能被多次调用或缓存
	// - 副作用应该在 Handler 或 Middleware 中执行
	//
	// 详见：docs/RULE_BEST_PRACTICES.md
	Rule     func(ctx *Context) bool
	Handler  func(ctx *Context)
	HandlerE func(ctx *Context) error // 新增：返回错误的处理函数
)

// Matcher 事件匹配器
type Matcher struct {
	IsTemp      bool          // 是否为临时Matcher（SetTemp(true) 时默认 maxUse=1，一次性匹配器）
	IsBlock     bool          // 是否为阻塞后续Matcher
	Priority    uint          // 优先级，数值越小优先级越高，0为最高优先级
	EventType   dto.EventType // 显式事件类型
	Rules       []Rule        // 其他匹配规则
	Handler     Handler       // 处理函数（无错误返回）
	HandlerErr  HandlerE      // 处理函数（带错误返回）
	Engine      *Engine       // 所属引擎
	Source      string        // 来源标签："global" 或 "plugin:<name>"
	pluginName  string        // 归属插件名（避免反复解析 Source）
	middlewares []HandlerMiddleware

	// 组合后的中间件链缓存及对应的代际号快照
	combinedChain atomic.Value // []HandlerMiddleware
	cachedGen     struct {
		global uint64
		plugin uint64
	}

	// 生命周期与临时 matcher 管理
	mu          sync.RWMutex // 保护 deleted / useCount / maxUseCount / createdAt / expiresAt 等
	deleted     bool         // 是否已从 Engine 中删除
	useCount    int32        // 已使用次数（仅临时 matcher 使用）
	maxUseCount int32        // 最大使用次数（>0 时启用自动删除）
	createdAt   time.Time    // 创建时间
	expiresAt   time.Time    // 过期时间（零值表示不过期）
}

func (m *Matcher) copy() *Matcher {
	return &Matcher{
		EventType:   m.EventType,
		Rules:       m.Rules,
		IsBlock:     m.IsBlock,
		Priority:    m.Priority,
		Handler:     m.Handler,
		HandlerErr:  m.HandlerErr,
		Engine:      m.Engine,
		IsTemp:      m.IsTemp,
		Source:      m.Source,
		pluginName:  m.pluginName,
		middlewares: m.middlewares,
	}
}

// getCombinedChain 获取组合的中间件链（无锁读取）
func (m *Matcher) getCombinedChain() []HandlerMiddleware {
	if v := m.combinedChain.Load(); v != nil {
		return v.([]HandlerMiddleware)
	}
	return nil
}

// setCombinedChain 设置组合的中间件链（写操作）
func (m *Matcher) setCombinedChain(chain []HandlerMiddleware, globalGen, pluginGen uint64) {
	m.cachedGen.global = globalGen
	m.cachedGen.plugin = pluginGen
	m.combinedChain.Store(chain)
}

// Delete 从所属引擎中删除该匹配器
//
// 生命周期说明：
// - 此操作是异步的，不会立即中断正在执行的 handler
// - 标记为 deleted 的 matcher 不会匹配新的事件
// - 正在执行的 handler 会继续完成执行
// - 删除操作是幂等的，重复调用无副作用
// - 删除后该 matcher 会从 Engine 的索引中移除，不再参与事件匹配
//
// 使用场景：
//   - 临时 matcher 达到最大使用次数时自动删除
//   - 插件卸载时删除所有关联的 matcher
//   - 动态规则更新时删除旧的 matcher
//
// 示例：
//
//	// 手动删除 matcher
//	matcher.Delete()
//
//	// 临时 matcher 自动删除（使用一次后）
//	engine.On(dto.C2CMessageCreate, OnCommand("test")).
//	    SetTemp(true).
//	    Handle(func(ctx *Context) {
//	        // handler 执行完后，matcher 会自动删除
//	    })
func (m *Matcher) Delete() {
	m.mu.Lock()
	if m.deleted {
		m.mu.Unlock()
		return
	}
	m.deleted = true
	engine := m.Engine
	m.mu.Unlock()

	if engine != nil {
		engine.DeleteMatcher(m)
	}
}

// IsDeleted 返回 matcher 是否已经被删除
func (m *Matcher) IsDeleted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.deleted
}

// Match 检查事件是否匹配此 Matcher
func (m *Matcher) Match(ctx *Context) bool {
	m.mu.RLock()
	if m.deleted {
		m.mu.RUnlock()
		return false
	}
	rules := m.Rules
	m.mu.RUnlock()

	// 事件类型过滤已经由 Engine.getMatchersForEvent 通过索引完成

	// 检查其他规则
	for _, rule := range rules {
		if !rule(ctx) {
			return false
		}
	}

	return true
}

// Handle 设置 Matcher 的处理函数（无错误返回）
func (m *Matcher) Handle(handler Handler) *Matcher {
	if m == noopMatcher {
		return m // noop matcher 不执行任何操作
	}
	m.mu.Lock()
	m.Handler = handler
	m.HandlerErr = nil
	m.mu.Unlock()
	// Lazy recomposition now happens on-demand; no eager rebuild needed
	if m.Engine != nil {
		m.Engine.rebuildMatcherChain(m)
	}
	return m
}

// HandleE 设置 Matcher 的处理函数（带错误返回）
func (m *Matcher) HandleE(handler HandlerE) *Matcher {
	if m == noopMatcher {
		return m // noop matcher 不执行任何操作
	}
	m.mu.Lock()
	m.HandlerErr = handler
	m.Handler = nil
	m.mu.Unlock()
	if m.Engine != nil {
		m.Engine.rebuildMatcherChain(m)
	}
	return m
}

// SetPriority 设置 Matcher 的优先级
//
// 注意：修改优先级会导致 Engine 的排序缓存失效并重建。
// 如果需要批量修改多个 matcher 的优先级，建议在添加到 Engine 之前设置。
func (m *Matcher) SetPriority(priority uint) *Matcher {
	if m == noopMatcher {
		return m
	}

	// 只有在优先级真正改变时才失效缓存
	if m.Priority != priority {
		m.Priority = priority

		// 通知 Engine 重建排序缓存
		if m.Engine != nil {
			m.Engine.invalidateSortedCache(m.EventType)
		}
	}

	return m
}

// SetBlock 设置 Matcher 是否阻塞后续匹配
func (m *Matcher) SetBlock(block bool) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.IsBlock = block
	return m
}

// SetTemp 设置 Matcher 是否为临时匹配器
// 约定：SetTemp(true) 默认视为一次性匹配器（maxUse=1），使用一次后自动删除；
// 若需自定义最大使用次数，请使用 SetTempWithMaxUse。
func (m *Matcher) SetTemp(temp bool) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.IsTemp = temp
	if temp {
		// 默认一次性 matcher
		m.maxUseCount = 1
		m.useCount = 0
	} else {
		m.maxUseCount = 0
		m.useCount = 0
	}
	return m
}

// SetTempWithMaxUse 将 matcher 标记为临时匹配器，并指定最大使用次数（<=0 时等价于一次性 matcher）。
func (m *Matcher) SetTempWithMaxUse(maxUse int) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.IsTemp = true
	if maxUse <= 0 {
		m.maxUseCount = 1
	} else {
		m.maxUseCount = int32(maxUse)
	}
	m.useCount = 0
	return m
}

// SetTempWithTimeout 将 matcher 标记为临时匹配器，并指定过期时间
//
// 过期后，matcher 会被自动删除（需要启用 Engine 的清理器）。
// 可以同时设置最大使用次数和超时时间，满足任一条件即删除。
//
// 使用示例：
//
//	// 5 分钟后自动过期
//	engine.OnC2C(OnCommand("temp")).
//	    SetTempWithTimeout(5 * time.Minute).
//	    HandleE(handler)
//
//	// 最多使用 3 次或 10 分钟后过期
//	engine.OnC2C(OnCommand("temp")).
//	    SetTempWithMaxUse(3).
//	    SetTempWithTimeout(10 * time.Minute).
//	    HandleE(handler)
func (m *Matcher) SetTempWithTimeout(timeout time.Duration) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.IsTemp = true
	m.createdAt = time.Now()
	m.expiresAt = m.createdAt.Add(timeout)
	return m
}

// Use 为当前 matcher 注册局部中间件（仅对此 matcher 生效）
func (m *Matcher) Use(mw ...HandlerMiddleware) *Matcher {
	if m == noopMatcher {
		return m // noop matcher 不执行任何操作
	}
	m.mu.Lock()
	m.middlewares = append(m.middlewares, mw...)
	m.invalidateCombinedChain()
	m.mu.Unlock()
	if m.Engine != nil {
		m.Engine.rebuildMatcherChain(m)
	}
	return m
}

// Command 添加命令匹配规则（链式调用）
// 匹配以指定前缀开头的消息，忽略前导空白
func (m *Matcher) Command(cmd string) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.Rules = append(m.Rules, OnCommand(cmd))
	return m
}

// Keyword 添加关键词匹配规则（链式调用）
// 匹配包含指定关键词的消息
func (m *Matcher) Keyword(keyword string) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.Rules = append(m.Rules, OnKeyword(keyword))
	return m
}

// Prefix 添加前缀匹配规则（链式调用）
// 匹配以指定前缀开头的消息，忽略前导空白
func (m *Matcher) Prefix(prefix string) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.Rules = append(m.Rules, OnPrefix(prefix))
	return m
}

// Suffix 添加后缀匹配规则（链式调用）
// 匹配以指定后缀结尾的消息
func (m *Matcher) Suffix(suffix string) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.Rules = append(m.Rules, OnSuffix(suffix))
	return m
}

// FullMatch 添加完全匹配规则（链式调用）
// 匹配完全相同的消息，忽略前导空白
func (m *Matcher) FullMatch(text string) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.Rules = append(m.Rules, OnFullMatch(text))
	return m
}

// Regex 添加正则表达式匹配规则（链式调用）
// 注意：如果正则表达式无效会 panic
func (m *Matcher) Regex(pattern string) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.Rules = append(m.Rules, OnRegex(pattern))
	return m
}

// Where 添加自定义规则（链式调用）
// 用于添加任意 Rule 函数
func (m *Matcher) Where(rule Rule) *Matcher {
	if m == noopMatcher {
		return m
	}
	m.Rules = append(m.Rules, rule)
	return m
}

func (m *Matcher) invalidateCombinedChain() {
	if m == nil {
		return
	}
	m.cachedGen.global = 0
	m.cachedGen.plugin = 0
	// protect against nil/zero atomic.Value on noop or zero matchers
	defer func() { _ = recover() }()
	m.combinedChain.Store(nil)
}
