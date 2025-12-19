package remilia

import (
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
)

// OnEventType 匹配特定事件类型
func OnEventType(eventType dto.EventType) Rule {
	return func(ctx *Context) bool {
		return ctx.GetEventType() == eventType
	}
}

// OnC2CMessage 匹配私聊消息
//
// 旧用法（已废弃）：
//
//	engine.On(OnC2CMessage(), OnCommand("/ping")).Handle(handler)
//
// 新推荐用法：
//
//	engine.OnC2C(OnCommand("/ping")).Handle(handler)
func OnC2CMessage() Rule {
	return OnEventType(dto.C2CMessageCreate)
}

// OnGroupAtMessage 匹配被 @ 的群消息
//
// 旧用法（已废弃）：
//
//	engine.On(OnGroupAtMessage(), OnCommand("/ping")).Handle(handler)
//
// 新推荐用法：
//
//	engine.OnGroupAt(OnCommand("/ping")).Handle(handler)
func OnGroupAtMessage() Rule {
	return OnEventType(dto.GroupAtMessageCreate)
}

// OnGroupAddRobot 匹配机器人加入群聊事件
func OnGroupAddRobot() Rule {
	return OnEventType(dto.GroupAddRobot)
}

// OnGroupDelRobot 匹配机器人退出群聊事件
func OnGroupDelRobot() Rule {
	return OnEventType(dto.GroupDelRobot)
}

// OnCommand 匹配命令（以指定前缀开头的消息）
// 忽略前导空白后再判断前缀
func OnCommand(prefix string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
		return strings.HasPrefix(trimmed, prefix)
	}
}

// OnKeyword 匹配包含关键词的消息（不忽略前导空白，避免误匹配）
func OnKeyword(keyword string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		return strings.Contains(content, keyword)
	}
}

// OnFullMatch 匹配完全相同的消息（忽略前导空白再比较）
func OnFullMatch(text string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
		return trimmed == text
	}
}

// OnPrefix 匹配以特定前缀开头的消息（忽略前导空白）
func OnPrefix(prefix string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
		return strings.HasPrefix(trimmed, prefix)
	}
}

// OnSuffix 匹配以特定后缀结尾的消息（不忽略前导空白）
func OnSuffix(suffix string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		return strings.HasSuffix(content, suffix)
	}
}

// regexCacheStore 全局正则表达式缓存，避免重复编译相同模式
// 使用 LRU 策略限制缓存大小，防止内存泄漏和 DoS 攻击
type regexCacheStore struct {
	mu      sync.RWMutex
	cache   map[string]*regexCacheEntry
	lruList []string // 简单的 LRU 列表
	maxSize int
}

type regexCacheEntry struct {
	re         *regexp.Regexp
	lastAccess time.Time
}

var (
	regexCache     *regexCacheStore
	regexCacheOnce sync.Once
)

// initRegexCache 初始化正则表达式缓存
func initRegexCache() {
	regexCacheOnce.Do(func() {
		regexCache = &regexCacheStore{
			cache:   make(map[string]*regexCacheEntry),
			lruList: make([]string, 0, 1000),
			maxSize: 1000, // 最多缓存 1000 个正则表达式
		}
	})
}

// get 从缓存获取正则表达式
func (rc *regexCacheStore) get(pattern string) (*regexp.Regexp, bool) {
	rc.mu.RLock()
	entry, ok := rc.cache[pattern]
	rc.mu.RUnlock()

	if ok {
		// 在锁保护下更新访问时间，避免与淘汰并发产生竞态
		rc.mu.Lock()
		if e, stillPresent := rc.cache[pattern]; stillPresent {
			e.lastAccess = time.Now()
		}
		rc.mu.Unlock()
		return entry.re, true
	}
	return nil, false
}

// put 将正则表达式放入缓存
func (rc *regexCacheStore) put(pattern string, re *regexp.Regexp) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// 如果已存在，更新并返回
	if entry, exists := rc.cache[pattern]; exists {
		entry.re = re
		entry.lastAccess = time.Now()
		return
	}

	// 检查缓存大小，如果达到上限则删除最旧的
	if len(rc.cache) >= rc.maxSize {
		rc.evictOldest()
	}

	// 添加新条目
	rc.cache[pattern] = &regexCacheEntry{
		re:         re,
		lastAccess: time.Now(),
	}
	rc.lruList = append(rc.lruList, pattern)
}

// evictOldest 删除最旧的缓存条目（必须在持有锁的情况下调用）
func (rc *regexCacheStore) evictOldest() {
	if len(rc.cache) == 0 {
		return
	}

	// 找到最旧的条目
	var oldestPattern string
	var oldestTime time.Time
	first := true

	for pattern, entry := range rc.cache {
		if first || entry.lastAccess.Before(oldestTime) {
			oldestPattern = pattern
			oldestTime = entry.lastAccess
			first = false
		}
	}

	// 删除最旧的条目
	delete(rc.cache, oldestPattern)

	// 从 LRU 列表中移除
	for i, p := range rc.lruList {
		if p == oldestPattern {
			rc.lruList = append(rc.lruList[:i], rc.lruList[i+1:]...)
			break
		}
	}
}

// OnRegex 匹配正则表达式（预编译并缓存）
//
// pattern: 正则表达式模式
//
// 注意：
//   - 如果正则表达式无效会 panic，生产环境建议使用 OnRegexSafe
//   - 缓存最多保存 1000 个正则表达式，超过后会使用 LRU 策略淘汰
//
// 性能优化：相同模式只编译一次，后续调用直接从缓存获取
func OnRegex(pattern string) Rule {
	initRegexCache()

	// 尝试从缓存获取
	if re, ok := regexCache.get(pattern); ok {
		return func(ctx *Context) bool {
			content := ctx.GetMessageContent()
			return re.MatchString(content)
		}
	}

	// 缓存未命中，编译并缓存
	re := regexp.MustCompile(pattern)
	regexCache.put(pattern, re)

	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		return re.MatchString(content)
	}
}

// OnRegexSafe 安全的正则表达式匹配（返回错误，带缓存）
//
// 用于处理用户输入的正则表达式或不确定的模式。
// 与 OnRegex 不同，此方法在正则表达式无效时返回错误而不是 panic。
//
// 注意：缓存最多保存 1000 个正则表达式，超过后会使用 LRU 策略淘汰
func OnRegexSafe(pattern string) (Rule, error) {
	initRegexCache()

	// 尝试从缓存获取
	if re, ok := regexCache.get(pattern); ok {
		return func(ctx *Context) bool {
			content := ctx.GetMessageContent()
			return re.MatchString(content)
		}, nil
	}

	// 缓存未命中，编译并缓存
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCache.put(pattern, re)

	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		return re.MatchString(content)
	}, nil
}

// OnRegexCompiled 使用已编译的正则表达式
// 适用于需要复用正则表达式对象的场景
func OnRegexCompiled(re *regexp.Regexp) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		return re.MatchString(content)
	}
}

// And 逻辑与：所有规则都必须满足
//
// 使用短路优化：如果某个规则返回 false，后续规则不会执行。
//
// 警告：传入的规则函数应该是纯函数（无副作用）。
// 如果规则有副作用（如修改状态、调用 API），短路会导致不一致的行为。
// 副作用应该放在 Handler 或 Middleware 中，不应该在 Rule 中。
//
// 示例：
//
//	// ✅ 正确：纯函数
//	And(OnCommand("/ping"), OnlyAdmin())
//
//	// ❌ 错误：有副作用
//	And(func(ctx *Context) bool {
//	    counter++ // 副作用！短路时可能不执行
//	    return true
//	})
func And(rules ...Rule) Rule {
	return func(ctx *Context) bool {
		for _, rule := range rules {
			if !rule(ctx) {
				return false // 短路：后续规则不执行
			}
		}
		return true
	}
}

// Or 逻辑或：任一规则满足即可
//
// 使用短路优化：如果某个规则返回 true，后续规则不会执行。
//
// 警告：传入的规则函数应该是纯函数（无副作用）。
// 如果规则有副作用，短路会导致不一致的行为。
// 详见 And 函数的说明和 docs/RULE_BEST_PRACTICES.md
func Or(rules ...Rule) Rule {
	return func(ctx *Context) bool {
		for _, rule := range rules {
			if rule(ctx) {
				return true // 短路：后续规则不执行
			}
		}
		return false
	}
}

// Not 逻辑非：规则不满足
func Not(rule Rule) Rule {
	return func(ctx *Context) bool {
		return !rule(ctx)
	}
}

// WithTimeout 为规则添加超时控制（可选）
//
// 此函数为可能执行时间长的规则提供超时保护。
// 注意：大多数规则应该快速返回（< 1ms），只有在确实需要时才使用此包装器。
//
// 使用场景：
// - 规则中包含外部调用（不推荐，但如果必须）
// - 规则中有复杂计算
// - 需要防止慢规则阻塞事件处理
//
// 注意事项：
// - 超时后规则 goroutine 仍在运行（直到完成）
// - 每次调用都会创建新的 goroutine（有性能开销）
// - 最佳实践是让规则快速返回，而不是依赖超时
//
// 使用示例：
//
//	// 包装可能很慢的规则
//	slowRule := func(ctx *Context) bool {
//	    // 可能很慢的操作
//	    return checkSomething()
//	}
//	engine.OnC2C(WithTimeout(slowRule, 100*time.Millisecond)).Handle(handler)
func WithTimeout(rule Rule, timeout time.Duration) Rule {
	return func(ctx *Context) bool {
		resultChan := make(chan bool, 1)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					logrus.WithField("panic", r).Error("[Rule] Panic in rule with timeout")
					resultChan <- false
				}
			}()
			resultChan <- rule(ctx)
		}()

		select {
		case result := <-resultChan:
			return result
		case <-time.After(timeout):
			logrus.WithFields(logrus.Fields{
				"timeout": timeout,
			}).Warn("[Rule] Rule timeout exceeded")
			return false
		}
	}
}

// MonitorRule 包装规则以监控其执行时间
//
// 此函数用于检测慢规则，帮助优化性能。
// 如果规则执行时间超过阈值，会记录警告日志。
//
// 使用示例：
//
//	rule := MonitorRule("checkUser", func(ctx *Context) bool {
//	    return ctx.GetString("user") == "admin"
//	})
//	engine.OnC2C(rule).Handle(handler)
//
// 建议阈值：
// - 10ms：生产环境警告阈值
// - 1ms：开发环境优化目标
func MonitorRule(name string, rule Rule, threshold time.Duration) Rule {
	if threshold == 0 {
		threshold = 10 * time.Millisecond // 默认阈值
	}

	return func(ctx *Context) bool {
		start := time.Now()
		result := rule(ctx)
		duration := time.Since(start)

		if duration > threshold {
			logrus.WithFields(logrus.Fields{
				"rule":     name,
				"duration": duration,
			}).Warn("[Rule] Slow rule detected")
		}

		return result
	}
}

// ClearRegexCache 清空正则表达式缓存
//
// 主要用于测试或内存管理场景。
// 注意：清空缓存会导致下次使用时重新编译正则表达式。
func ClearRegexCache() {
	initRegexCache()
	regexCache.mu.Lock()
	defer regexCache.mu.Unlock()
	regexCache.cache = make(map[string]*regexCacheEntry)
	regexCache.lruList = make([]string, 0, regexCache.maxSize)
}

// GetRegexCacheSize 获取正则表达式缓存当前大小（用于监控）
//
// 返回当前缓存中的正则表达式数量。
// 注意：这是一个 O(1) 操作。
func GetRegexCacheSize() int {
	initRegexCache()
	regexCache.mu.RLock()
	defer regexCache.mu.RUnlock()
	return len(regexCache.cache)
}

// GetRegexCacheMaxSize 获取正则表达式缓存最大容量
func GetRegexCacheMaxSize() int {
	initRegexCache()
	return regexCache.maxSize
}

// SetRegexCacheMaxSize 设置正则表达式缓存最大容量
//
// 主要用于调整缓存大小以适应不同的使用场景。
// 如果新容量小于当前缓存大小，会立即淘汰多余的条目。
//
// 使用示例：
//
//	// 在应用启动时调整缓存大小
//	remilia.SetRegexCacheMaxSize(5000)
func SetRegexCacheMaxSize(size int) {
	if size <= 0 {
		size = 1000 // 默认值
	}

	initRegexCache()
	regexCache.mu.Lock()
	defer regexCache.mu.Unlock()

	regexCache.maxSize = size

	// 如果当前缓存大小超过新限制，淘汰多余的条目
	for len(regexCache.cache) > size {
		regexCache.evictOldest()
	}
}
