package context

import (
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// OnEventType 匹配特定事件类型
func OnEventType(eventType dto.EventType) Rule {
	return func(ctx *Context) bool {
		return ctx.GetEventType() == eventType
	}
}

// OnC2CMessage 匹配私聊消息
//
// 旧用法(已废弃):
//
//	engine.On(OnC2CMessage(), OnCommand("/ping")).Handle(handler)
//
// 新推荐用法:
//
//	engine.OnC2C(OnCommand("/ping")).Handle(handler)
func OnC2CMessage() Rule {
	return OnEventType(dto.C2CMessageCreate)
}

// OnGroupAtMessage 匹配被 @ 的群消息
//
// 旧用法(已废弃):
//
//	engine.On(OnGroupAtMessage(), OnCommand("/ping")).Handle(handler)
//
// 新推荐用法:
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

// OnCommand 匹配命令(以指定前缀开头的消息)
// 忽略前导空白后再判断前缀
func OnCommand(prefix string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
		return strings.HasPrefix(trimmed, prefix)
	}
}

// OnKeyword 匹配包含关键词的消息(不忽略前导空白，避免误匹配)
func OnKeyword(keyword string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		return strings.Contains(content, keyword)
	}
}

// OnFullMatch 匹配完全相同的消息(忽略前导空白再比较)
func OnFullMatch(text string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
		return trimmed == text
	}
}

// OnPrefix 匹配以特定前缀开头的消息(忽略前导空白)
func OnPrefix(prefix string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
		return strings.HasPrefix(trimmed, prefix)
	}
}

// OnSuffix 匹配以特定后缀结尾的消息(不忽略前导空白)
func OnSuffix(suffix string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		return strings.HasSuffix(content, suffix)
	}
}

// regexCacheStore 全局正则表达式缓存，避免重复编译相同模式
// 使用 LRU 策略限制缓存大小，防止内存泄漏和 DoS 攻击
type regexCacheStore struct {
	cache   *lru.Cache[string, *regexp.Regexp]
	maxSize int
}

var (
	regexCache     *regexCacheStore
	regexCacheOnce sync.Once
)

// initRegexCache 初始化正则表达式缓存
func initRegexCache() {
	regexCacheOnce.Do(func() {
		// 创建一个新的 LRU 缓存，容量为 1000
		cache, err := lru.New[string, *regexp.Regexp](1000)
		if err != nil {
			// 理论上只有当 size <= 0 时才会返回错误，这里 panic 是安全的
			panic(err)
		}
		regexCache = &regexCacheStore{
			cache:   cache,
			maxSize: 1000,
		}
	})
}

// get 从缓存获取正则表达式
func (rc *regexCacheStore) get(pattern string) (*regexp.Regexp, bool) {
	return rc.cache.Get(pattern)
}

// put 将正则表达式放入缓存
func (rc *regexCacheStore) put(pattern string, re *regexp.Regexp) {
	rc.cache.Add(pattern, re)
}

// OnRegex 匹配正则表达式(预编译并缓存)
//
// pattern: 正则表达式模式
//
// 注意:
//   - 如果正则表达式无效会 panic，生产环境建议使用 OnRegexSafe
//   - 缓存最多保存 1000 个正则表达式，超过后会使用 LRU 策略淘汰
//
// 性能优化: 相同模式只编译一次，后续调用直接从缓存获取
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

// OnRegexSafe 安全的正则表达式匹配(返回错误，带缓存)
//
// 用于处理用户输入的正则表达式或不确定的模式。
// 与 OnRegex 不同，此方法在正则表达式无效时返回错误而不是 panic。
//
// 注意: 缓存最多保存 1000 个正则表达式，超过后会使用 LRU 策略淘汰
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

// And 逻辑与: 所有规则都必须满足
//
// 使用短路优化: 如果某个规则返回 false，后续规则不会执行。
//
// 警告: 传入的规则函数应该是纯函数(无副作用)。
// 如果规则有副作用(如修改状态、调用 API)，短路会导致不一致的行为。
// 副作用应该放在 Handler 或 Middleware 中，不应该在 Rule 中。
//
// 示例:
//
//	// 正确: 纯函数
//	And(OnCommand("/ping"), OnlyAdmin())
//
//	// 错误: 有副作用
//	And(func(ctx *Context) bool {
//	    counter++ // 副作用! 短路时可能不执行
//	    return true
//	})
func And(rules ...Rule) Rule {
	return func(ctx *Context) bool {
		for _, rule := range rules {
			if !rule(ctx) {
				return false // 短路: 后续规则不执行
			}
		}
		return true
	}
}

// Or 逻辑或: 任一规则满足即可
//
// 使用短路优化: 如果某个规则返回 true，后续规则不会执行。
//
// 警告: 传入的规则函数应该是纯函数(无副作用)。
// 如果规则有副作用，短路会导致不一致的行为。
// 详见 And 函数的说明和 docs/RULE_BEST_PRACTICES.md
func Or(rules ...Rule) Rule {
	return func(ctx *Context) bool {
		for _, rule := range rules {
			if rule(ctx) {
				return true // 短路: 后续规则不执行
			}
		}
		return false
	}
}

// Not 逻辑非: 规则不满足
func Not(rule Rule) Rule {
	return func(ctx *Context) bool {
		return !rule(ctx)
	}
}

// WithTimeout 为规则添加超时控制(可选)
//
// 此函数为可能执行时间长的规则提供超时保护。
// 注意: 大多数规则应该快速返回(< 1ms)，只有在确实需要时才使用此包装器。
//
// 使用场景:
// - 规则中包含外部调用(不推荐，但如果必须)
// - 规则中有复杂计算
// - 需要防止慢规则阻塞事件处理
//
// 注意事项:
// - 超时后规则 goroutine 仍在运行(直到完成)
// - 每次调用都会创建新的 goroutine(有性能开销)
// - 最佳实践是让规则快速返回，而不是依赖超时
//
// 使用示例:
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
					logger.WithField("panic", r).Error("[Rule] Panic in rule with timeout")
					resultChan <- false
				}
			}()
			resultChan <- rule(ctx)
		}()

		select {
		case result := <-resultChan:
			return result
		case <-time.After(timeout):
			logger.WithFields(logger.Fields{
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
// 使用示例:
//
//	rule := MonitorRule("checkUser", func(ctx *Context) bool {
//	    return ctx.GetString("user") == "admin"
//	})
//	engine.OnC2C(rule).Handle(handler)
//
// 建议阈值:
// - 10ms: 生产环境警告阈值
// - 1ms: 开发环境优化目标
func MonitorRule(name string, rule Rule, threshold time.Duration) Rule {
	if threshold == 0 {
		threshold = 10 * time.Millisecond // 默认阈值
	}

	return func(ctx *Context) bool {
		start := time.Now()
		result := rule(ctx)
		duration := time.Since(start)

		if duration > threshold {
			logger.WithFields(logger.Fields{
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
// 注意: 清空缓存会导致下次使用时重新编译正则表达式。
func ClearRegexCache() {
	initRegexCache()
	regexCache.cache.Purge()
}

// GetRegexCacheSize 获取正则表达式缓存当前大小(用于监控)
func GetRegexCacheSize() int {
	initRegexCache()
	return regexCache.cache.Len()
}

// GetRegexCacheMaxSize 获取正则表达式缓存最大容量
func GetRegexCacheMaxSize() int {
	initRegexCache()
	return regexCache.maxSize
}

// SetRegexCacheMaxSize 设置正则表达式缓存最大容量
func SetRegexCacheMaxSize(size int) {
	if size <= 0 {
		size = 1000
	}
	initRegexCache()
	regexCache.cache.Resize(size)
	regexCache.maxSize = size
}

// ---- Cooldown Rule --------------------------------------------------------

// cooldownStore 全局用户冷却时间存储（LRU，最多 50000 条）
var (
	cooldownStore     *lru.Cache[string, time.Time]
	cooldownStoreOnce sync.Once
)

func initCooldownStore() {
	cooldownStoreOnce.Do(func() {
		c, err := lru.New[string, time.Time](50000)
		if err != nil {
			panic(err)
		}
		cooldownStore = c
	})
}

// OnCooldown 创建用户级冷却时间规则。
// 同一用户在 d 时间内只允许触发一次；冷却期间该规则返回 false，事件不匹配。
//
// keyFn 用于从 Context 中提取冷却 key（通常是 UserOpenID）。
// 若 keyFn 为 nil，则默认使用 ctx.GetAuthor().UserOpenID。
//
// 使用示例:
//
//	engine.OnC2C(OnCommand("/sign"), OnCooldown(24*time.Hour, nil)).Handle(signHandler)
func OnCooldown(d time.Duration, keyFn func(*Context) string) Rule {
	initCooldownStore()
	if keyFn == nil {
		keyFn = func(ctx *Context) string {
			if a := ctx.GetAuthor(); a != nil {
				return a.UserOpenID
			}
			return ""
		}
	}
	return func(ctx *Context) bool {
		key := keyFn(ctx)
		if key == "" {
			return true // 无法确定 key，放行
		}
		now := time.Now()
		if last, ok := cooldownStore.Get(key); ok && now.Sub(last) < d {
			logger.WithFields(logger.Fields{
				"key":       key,
				"remaining": (d - now.Sub(last)).Round(time.Second).String(),
			}).Debug("[OnCooldown] User is in cooldown")
			return false
		}
		cooldownStore.Add(key, now)
		return true
	}
}

// OnAtBot 匹配消息中包含 @Bot 标记的事件。
// botOpenID 为机器人自身的 OpenID，若为空则只要 content 中包含 qqbot-at 标签即视为 @Bot。
func OnAtBot(botOpenID string) Rule {
	return func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		if botOpenID != "" {
			return strings.Contains(content, `id="`+botOpenID+`"`)
		}
		return strings.Contains(content, "<qqbot-at-user")
	}
}

// ---- Group / Permission / Ban Rules ---------------------------------------

// BannedChecker 封禁检查接口（由 antispam 插件等实现）
// 此接口允许规则与具体封禁实现解耦。
type BannedChecker interface {
	// IsBanned 检查 userID 是否在封禁名单中
	IsBanned(userID string) bool
}

// PermissionChecker 权限检查接口（由 permission 插件实现）
type PermissionChecker interface {
	// HasPermissionEx 检查 userID 是否拥有 resource:action 权限
	HasPermissionEx(userID, resource, action string) bool
}

// InGroup 仅在指定群列表中触发。
// 若 groupIDs 为空，则始终返回 false（保护性设计，避免意外放行所有群）。
//
// 注意：此规则仅适用于群消息事件（GroupAtMessageCreate）。
//
// 使用示例:
//
//	engine.OnGroupAt(InGroup("group-id-1", "group-id-2"), OnCommand("/admin")).Handle(...)
func InGroup(groupIDs ...string) Rule {
	if len(groupIDs) == 0 {
		return func(*Context) bool { return false }
	}
	set := make(map[string]bool, len(groupIDs))
	for _, id := range groupIDs {
		set[id] = true
	}
	return func(ctx *Context) bool {
		var event dto.GroupAtMessageCreateEvent
		if err := ctx.DecodeEvent(&event); err != nil {
			return false
		}
		return set[event.GroupOpenID]
	}
}

// HasPermission 权限检查规则。
// 要求消息发送者拥有指定的 resource:action 权限。
//
// 使用示例:
//
//	engine.OnGroupAt(
//	    OnCommand("/ban"),
//	    HasPermission(permPlugin, "user", "ban"),
//	).Handle(banHandler)
func HasPermission(checker PermissionChecker, resource, action string) Rule {
	return func(ctx *Context) bool {
		userID := ctx.GetUserID()
		if userID == "" {
			return false
		}
		return checker.HasPermissionEx(userID, resource, action)
	}
}

// NotBanned 封禁名单检查规则。
// 若消息发送者在封禁名单中，则规则返回 false（事件不匹配，Handler 不执行）。
//
// 使用示例:
//
//	engine.OnGroupAt(OnCommand("/sign"), NotBanned(antispamPlugin)).Handle(signHandler)
func NotBanned(checker BannedChecker) Rule {
	return func(ctx *Context) bool {
		userID := ctx.GetUserID()
		if userID == "" {
			return true // 无法确定用户身份，放行
		}
		return !checker.IsBanned(userID)
	}
}
