package engine

import (
	"regexp"
	"sync"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// CompiledMatcher 预编译的匹配器
// 提供更快的匹配性能和规则缓存
type CompiledMatcher struct { // TODO: 集成到engine中替代matcher
	EventType     dto.EventType
	FastPathCheck func(*context.Context) bool // 快速路径检查
	Rules         []CompiledRule
	Handler       context.Handler
	Source        string
	Priority      uint

	// 原始 matcher 引用
	original *Matcher
}

// CompiledRule 预编译的规则
type CompiledRule struct {
	Predicate func(*context.Context) bool
	Cost      int    // 执行成本估算（毫秒级，用于排序）
	Type      string // 规则类型，用于调试
}

// MatcherCompiler 匹配器编译器
type MatcherCompiler struct {
	cache   sync.Map // map[*Matcher]*CompiledMatcher
	regexps sync.Map // map[string]*regexp.Regexp - 正则表达式缓存
}

// NewMatcherCompiler 创建新的匹配器编译器
func NewMatcherCompiler() *MatcherCompiler {
	return &MatcherCompiler{}
}

// Compile 编译 Matcher 为 CompiledMatcher
func (mc *MatcherCompiler) Compile(m *Matcher) *CompiledMatcher {
	if m == nil {
		return nil
	}

	// 检查缓存
	if cached, ok := mc.cache.Load(m); ok {
		return cached.(*CompiledMatcher)
	}

	// 编译规则
	compiledRules := make([]CompiledRule, 0, len(m.Rules))

	for _, rule := range m.Rules {
		compiledRule := mc.compileRule(rule)
		compiledRules = append(compiledRules, compiledRule)
	}

	// 按成本排序（低成本优先）
	mc.sortRulesByCost(compiledRules)

	// 创建快速路径
	fastPath := mc.createFastPath(m)

	compiled := &CompiledMatcher{
		EventType:     m.EventType,
		FastPathCheck: fastPath,
		Rules:         compiledRules,
		Handler:       m.Handler,
		Source:        m.Source,
		Priority:      m.priority,
		original:      m,
	}

	// 缓存结果
	mc.cache.Store(m, compiled)

	return compiled
}

// compileRule 编译单个规则
func (mc *MatcherCompiler) compileRule(rule context.Rule) CompiledRule {
	// 尝试识别规则类型并估算成本
	// 这里使用启发式方法

	return CompiledRule{
		Predicate: rule,
		Cost:      mc.estimateCost(rule),
		Type:      "generic",
	}
}

// estimateCost 估算规则执行成本
func (mc *MatcherCompiler) estimateCost(rule context.Rule) int {
	// 简单的启发式成本估算
	// 实际应用中可以通过 benchmark 获得更准确的值
	// TODO: 根据规则类型进行更精细的估算

	// 这里返回默认成本
	// 未来可以通过运行时统计来动态调整
	return 10 // 默认 10ms
}

// sortRulesByCost 按成本排序规则
func (mc *MatcherCompiler) sortRulesByCost(rules []CompiledRule) {
	// 使用简单的插入排序
	for i := 1; i < len(rules); i++ {
		key := rules[i]
		j := i - 1
		for j >= 0 && rules[j].Cost > key.Cost {
			rules[j+1] = rules[j]
			j--
		}
		rules[j+1] = key
	}
}

// createFastPath 创建快速路径检查
func (mc *MatcherCompiler) createFastPath(m *Matcher) func(*context.Context) bool {
	// 对于常见的简单匹配模式，创建优化的快速路径

	// 1. 如果只有一个简单的字符串匹配规则
	if len(m.Rules) == 1 {
		// 尝试提取简单模式
		// 这里是一个示例，实际需要更复杂的分析
		return nil
	}

	// 2. 对于命令匹配器
	if m.definition != nil && m.definition.Name != "" {
		cmdPrefix := "/" + m.definition.Name
		return func(ctx *context.Context) bool {
			// 快速检查：这里需要根据实际的 context API 调整
			// 暂时返回 nil，等待实际 API 确定
			_ = cmdPrefix
			return true
		}
	}

	// 没有快速路径
	return nil
}

// GetCompiledRegexp 获取或编译正则表达式（带缓存）
func (mc *MatcherCompiler) GetCompiledRegexp(pattern string) (*regexp.Regexp, error) {
	// 检查缓存
	if cached, ok := mc.regexps.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}

	// 编译正则表达式
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	// 缓存结果
	mc.regexps.Store(pattern, re)

	return re, nil
}

// Invalidate 使缓存失效
func (mc *MatcherCompiler) Invalidate(m *Matcher) {
	mc.cache.Delete(m)
}

// InvalidateAll 清空所有缓存
func (mc *MatcherCompiler) InvalidateAll() {
	mc.cache.Range(func(key, value any) bool {
		mc.cache.Delete(key)
		return true
	})
}

// Match 使用编译后的匹配器进行匹配
func (cm *CompiledMatcher) Match(ctx *context.Context) bool {
	// 1. 尝试快速路径
	if cm.FastPathCheck != nil {
		if !cm.FastPathCheck(ctx) {
			return false
		}
	}

	// 2. 执行所有规则（已按成本排序）
	for _, rule := range cm.Rules {
		if !rule.Predicate(ctx) {
			return false
		}
	}

	return true
}

// MatcherCache 匹配器缓存管理
type MatcherCache struct {
	compiler *MatcherCompiler
	cache    sync.Map // map[dto.EventType][]*CompiledMatcher
	mu       sync.RWMutex
}

// NewMatcherCache 创建新的匹配器缓存
func NewMatcherCache() *MatcherCache {
	return &MatcherCache{
		compiler: NewMatcherCompiler(),
	}
}

// GetOrCompile 获取或编译匹配器
func (mc *MatcherCache) GetOrCompile(m *Matcher) *CompiledMatcher {
	return mc.compiler.Compile(m)
}

// GetCompiledMatchers 获取某个事件类型的所有编译后的匹配器
func (mc *MatcherCache) GetCompiledMatchers(eventType dto.EventType, matchers []*Matcher) []*CompiledMatcher {
	// 检查缓存
	if cached, ok := mc.cache.Load(eventType); ok {
		return cached.([]*CompiledMatcher)
	}

	// 编译所有匹配器
	compiled := make([]*CompiledMatcher, 0, len(matchers))
	for _, m := range matchers {
		cm := mc.compiler.Compile(m)
		if cm != nil {
			compiled = append(compiled, cm)
		}
	}

	// 缓存结果
	mc.cache.Store(eventType, compiled)

	return compiled
}

// InvalidateEventType 使某个事件类型的缓存失效
func (mc *MatcherCache) InvalidateEventType(eventType dto.EventType) {
	mc.cache.Delete(eventType)
}

// InvalidateAll 清空所有缓存
func (mc *MatcherCache) InvalidateAll() {
	mc.cache.Range(func(key, value any) bool {
		mc.cache.Delete(key)
		return true
	})
	mc.compiler.InvalidateAll()
}

// Stats 获取缓存统计信息
func (mc *MatcherCache) Stats() MatcherCacheStats {
	stats := MatcherCacheStats{}

	mc.cache.Range(func(key, value any) bool {
		stats.EventTypeCount++
		matchers := value.([]*CompiledMatcher)
		stats.TotalCompiledMatchers += len(matchers)
		return true
	})

	// 统计正则表达式缓存
	mc.compiler.regexps.Range(func(key, value any) bool {
		stats.RegexpCacheSize++
		return true
	})

	return stats
}

// MatcherCacheStats 匹配器缓存统计信息
type MatcherCacheStats struct {
	EventTypeCount        int
	TotalCompiledMatchers int
	RegexpCacheSize       int
}
