package context

import (
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// rules.go — 事件匹配规则函数库
//
// # 路由层次说明
//
// 本文件中的规则函数均为平台无关规则（适用所有平台）：
//
//   - [OnEventKind]：按 platform.EventKind 匹配，如 EventKindPrivateMessage / EventKindGroupMessage
//   - [OnCommand]、[OnKeyword]、[OnRegex] 等内容规则：全平台有效
//   - [OnUserWhitelist]、[OnUserBlacklist]：基于 GetSenderInfo().ID，全平台有效

// OnEventType 匹配特定事件类型字符串（低级 API，通常不直接使用）
//
// 注意：GetEventType() 返回 EventKind 字符串（如 "PRIVATE_MESSAGE"）。
// 推荐使用 [OnEventKind] 代替。
func OnEventType(eventType string) Rule {
	return func(ctx *Context) bool {
		return ctx.GetEventType() == eventType
	}
}

// OnPlatform 匹配来自指定平台的事件（多平台架构下推荐使用）。
//
// 在同时运行多个平台适配器时，可通过此规则将某个命令限定到特定平台：
//
//	// 只在 QQ 平台响应 /ban 命令
//	engine.OnEventKind(platform.EventKindGroupMessage,
//	    OnPlatform("qq"),
//	    OnCommand("/ban"),
//	).Handle(banHandler)
//
//	// 只在 Discord 响应 /embed 命令（Discord 原生支持 Embeds）
//	engine.OnEventKind(platform.EventKindGroupMessage,
//	    OnPlatform("discord"),
//	    OnCommand("/embed"),
//	).Handle(embedHandler)
func OnPlatform(platformID string) Rule {
	return func(ctx *Context) bool {
		return ctx.GetEventPlatform() == platformID
	}
}

// OnEventKind 匹配平台无关的事件类别（多平台推荐方式）。
//
// 对所有平台（QQ、Discord、Telegram 等）透明地生效。
// 与 OnC2CMessage / OnGroupAtMessage 等 QQ 专属规则相比，此函数是首选。
//
// 示例：
//
//	// 私聊消息（所有平台）
//	engine.On(OnEventKind(platform.EventKindPrivateMessage), OnCommand("/ping")).Handle(h)
//	// 群组消息（所有平台）
//	engine.On(OnEventKind(platform.EventKindGroupMessage), OnCommand("/help")).Handle(h)
func OnEventKind(kind platform.EventKind) Rule {
	return func(ctx *Context) bool {
		return ctx.GetEventKind() == kind
	}
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
	maxSize atomic.Int64
}

// DefaultRegexCacheSize 是正则表达式 LRU 缓存的默认容量。
// 对于大多数 Bot（< 500 个不同正则规则）此值足够，不会频繁淘汰。
const DefaultRegexCacheSize = 1000

var (
	regexCache     *regexCacheStore
	regexCacheOnce sync.Once
	// regexCacheDesiredSize 允许在 initRegexCache 首次被调用前通过 SetRegexCacheSize 指定大小
	// 使用 init() 而非声明时初始化，避免 "var regexCacheDesiredSize atomic.Int64 = ..." 的语法限制
	regexCacheDesiredSize atomic.Int64
)

func init() {
	regexCacheDesiredSize.Store(DefaultRegexCacheSize)
}

// SetRegexCacheSize 在框架初始化前设置正则表达式 LRU 缓存的最大容量。
//
// 必须在**首次调用 OnRegex / OnRegexSafe 之前**调用，否则无效（缓存已初始化）。
// 若未调用，默认使用 DefaultRegexCacheSize（1000）。
//
// 使用场景：
//   - 小型 Bot（< 50 个正则规则）：传入较小值（如 64）节省内存
//   - 大型 Bot（> 1000 个不同正则规则）：传入较大值避免频繁淘汰
//
// 注意：size <= 0 时静默忽略，保留当前值。
func SetRegexCacheSize(size int) {
	if size > 0 {
		regexCacheDesiredSize.Store(int64(size))
	}
}

// initRegexCache 初始化正则表达式缓存（使用 regexCacheDesiredSize）
func initRegexCache() {
	regexCacheOnce.Do(func() {
		desired := int(regexCacheDesiredSize.Load())
		if desired <= 0 {
			desired = DefaultRegexCacheSize
		}
		cache, err := lru.New[string, *regexp.Regexp](desired)
		if err != nil {
			panic(err)
		}
		regexCache = &regexCacheStore{
			cache:   cache,
			maxSize: atomic.Int64{},
		}
		regexCache.maxSize.Store(int64(desired))
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
//   - 如果正则表达式无效会 panic（内部使用 regexp.MustCompile）
//   - 仅在模式为编译期已知的常量字符串时使用此函数
//   - 处理用户输入或运行时拼接的模式时，请改用 [OnRegexSafe]（返回 error，不 panic）
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

// OnRegexSafe 安全的正则表达式匹配（返回 error，带缓存）
//
// 用于处理用户输入的正则表达式或运行时拼接的不确定模式。
// 与 [OnRegex] 不同，此函数在正则表达式无效时返回 (nil, error) 而不是 panic，
// 适合生产环境中需要对外暴露正则配置的场景。
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

// MustGetCachedRegexp 返回 pattern 对应的编译后正则表达式，与 [OnRegex] 共享同一 LRU 缓存。
// pattern 无效时 panic，与 regexp.MustCompile 行为一致。
//
// 供框架层（如 plugin.RegisterRegex）在 setup 阶段使用：
// 既保留 MustCompile 语义，又避免相同 pattern 被多处重复编译。
func MustGetCachedRegexp(pattern string) *regexp.Regexp {
	initRegexCache()
	if re, ok := regexCache.get(pattern); ok {
		return re
	}
	re := regexp.MustCompile(pattern)
	regexCache.put(pattern, re)
	return re
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
	return int(regexCache.maxSize.Load())
}

// SetRegexCacheMaxSize 设置正则表达式缓存最大容量
func SetRegexCacheMaxSize(size int) {
	if size <= 0 {
		size = 1000
	}
	initRegexCache()
	regexCache.cache.Resize(size)
	regexCache.maxSize.Store(int64(size))
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
// keyFn 用于从 Context 中提取冷却 key（通常是用户 ID）。
// 若 keyFn 为 nil，则默认使用 ctx.GetSenderInfo().ID（平台无关）。
//
// 使用示例:
//
//	engine.On(OnEventKind(platform.EventKindPrivateMessage), OnCommand("/sign"), OnCooldown(24*time.Hour, nil)).Handle(signHandler)
func OnCooldown(d time.Duration, keyFn func(*Context) string) Rule {
	initCooldownStore()
	if keyFn == nil {
		keyFn = func(ctx *Context) string {
			return ctx.GetSenderInfo().ID
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
		// 从 platform.Event 提取 chat ID
		if e := ctx.GetPlatformEvent(); e != nil {
			return set[e.Chat().ID]
		}
		return false
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

// OnTimeRange 时间段规则。
//
// 仅当服务器当前时间的 Hour（0-23）处于 [startHour, endHour] 区间内时，规则才匹配。
// 支持跨午夜区间，例如 OnTimeRange(21, 3) 表示 21:00-03:59（晚间区间）。
//
// 典型用途：限制"晚安"命令仅在21:00-次日03:00之间响应，
// 避免白天用户随意发"晚安"触发睡眠记录。
//
// 修复框架问题 #23：为 RegisterRegex / RegisterCommand 提供时间段过滤能力，
// 插件无需在 Handler 内手动判断时间，直接在注册时附加此规则即可。
//
// 使用示例:
//
//	// 仅在晚上21点到凌晨3点响应"晚安"（跨午夜区间）
//	ctx.Reg.RegisterRegex("", `^晚安$`, OnTimeRange(21, 3)).Handle(handleGoodNight)
//
//	// 仅在上午6点到中午12点响应"早安"
//	ctx.Reg.RegisterRegex("", `^早安$`, OnTimeRange(6, 12)).Handle(handleGoodMorning)
func OnTimeRange(startHour, endHour int) Rule {
	return func(_ *Context) bool {
		hour := time.Now().Hour()
		if startHour <= endHour {
			// 正常区间，如 6-12
			return hour >= startHour && hour <= endHour
		}
		// 跨午夜区间，如 21-3
		return hour >= startHour || hour <= endHour
	}
}

// OnGroupAdminOrOwner 群管理员/群主权限规则。
//
// 仅当消息发送者是当前群的管理员（GroupRoleAdmin）或群主（GroupRoleOwner）时，规则才匹配。
// 在私聊场景中，此规则始终返回 false（管理操作仅限群内）。
//
// 修复框架问题 #24：为管理员命令提供内置的权限规则，
// 插件无需在 Handler 内手动比较 platform.UserInfo.GroupRole。
//
// 注意：部分平台（如 OneBot 的普通群消息）可能无法在事件 payload 中携带 GroupRole，
// 此时 GroupRole == GroupRoleUnknown == 0，规则返回 false。
// 如需支持此类场景，可通过额外调用 API 或存储历史身份来补充判断。
//
// 使用示例:
//
//	// 仅群管理员可以添加签池
//	ctx.Reg.RegisterCommand("", "/添加签池", OnGroupAdminOrOwner()).Handle(handleAddSign)
func OnGroupAdminOrOwner() Rule {
	return func(ctx *Context) bool {
		chat := ctx.GetChatInfo()
		if !chat.IsGroup {
			return false // 私聊不适用管理员规则
		}
		sender := ctx.GetSenderInfo()
		return sender.GroupRole == platform.GroupRoleAdmin ||
			sender.GroupRole == platform.GroupRoleOwner
	}
}

// OnMentionedBot 要求消息中 @ 了机器人自身。
//
// 基于 MentionsEvent 接口和 UserInfo.IsSelf 字段工作，
// 适用于 `GROUP_MESSAGE_CREATE`（全量消息）下需要 @ 才能触发的命令。
//
// 若平台不支持 MentionsEvent（如旧版 QQ GROUP_AT_MESSAGE_CREATE），
// 此规则始终返回 true（放行），由 EventType 路由自行过滤。
//
// 使用示例（仅 @ 机器人时响应 /ping）：
//
//	engine.OnEventKind(platform.EventKindGroupMessage,
//	    OnMentionedBot(),
//	    OnCommand("/ping"),
//	).Handle(pingHandler)
func OnMentionedBot() Rule {
	return func(ctx *Context) bool {
		event := ctx.GetPlatformEvent()
		if event == nil {
			return false
		}
		for _, m := range platform.GetMentions(event) {
			if m.IsSelf {
				return true
			}
		}
		return false
	}
}

// OnMentionedBotOrNoMentions 仅在以下情况放行：
//  1. 消息中没有 @ 任何人
//  2. 消息中 @ 了机器人自身
//
// 适用于 `GROUP_MESSAGE_CREATE` 场景：
// 群内普通聊天机器人可参与，@ 他人的对话不响应。
//
// 统计、日志类插件不应附加此规则，以便接收全量消息。
//
// 使用示例：
//
//	engine.OnEventKind(platform.EventKindGroupMessage,
//	    OnMentionedBotOrNoMentions(),
//	    OnCommand("/ping"),
//	).Handle(pingHandler)
func OnMentionedBotOrNoMentions() Rule {
	return func(ctx *Context) bool {
		event := ctx.GetPlatformEvent()
		if event == nil {
			return false
		}
		mentions := platform.GetMentions(event)
		if len(mentions) == 0 {
			return true
		}
		for _, m := range mentions {
			if m.IsSelf {
				return true
			}
		}
		return false
	}
}

// OnFromUser 匹配来自特定用户的消息（发送者 ID 等于 userID）。
//
// 适用于将某个命令永久限制为特定用户的场景（静态过滤）。
// 若需根据运行时状态动态决定期望用户，请使用 [OnFromUserFunc]。
//
// 示例：
//
//	ctx.Reg.RegisterCommand("", "/管理", OnFromUser(adminID)).Handle(h)
func OnFromUser(userID string) Rule {
	return func(ctx *Context) bool {
		return ctx.GetUserID() == userID
	}
}

// OnFromUserFunc 匹配来自动态期望用户的消息。
//
// fn 在每次 dispatch 时被调用，返回当前期望的用户 ID：
//   - 返回非空字符串：仅当发送者 ID 与其相等时规则匹配
//   - 返回空字符串 ""：视为"无限制"，规则直接放行（任意用户均匹配）
//
// 空字符串放行语义使得"当前没有活跃状态"时 handler 仍能运行，从而给出友好提示。
//
// 适用于轮流制游戏等"当前期望用户随状态变化"的动态场景。
//
// 示例（五子棋轮流落子）：
//
//	ctx.Reg.RegisterCommand("", "/下",
//	    context.OnFromUserFunc(func(c *context.Context) string {
//	        state, ok := gameStore.Get(c.GetChatInfo().ID)
//	        if !ok || !state.Active {
//	            return "" // 无活跃棋局 → 放行，让 handler 给出提示
//	        }
//	        return state.CurrentPlayerID
//	    }),
//	).Handle(handleMove)
func OnFromUserFunc(fn func(*Context) string) Rule {
	return func(ctx *Context) bool {
		expected := fn(ctx)
		if expected == "" {
			return true // 空字符串 → 无限制，放行
		}
		return ctx.GetUserID() == expected
	}
}
