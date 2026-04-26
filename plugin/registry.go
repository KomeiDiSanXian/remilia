package plugin

import (
	"strings"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// RegistryWriter Matcher/Command 注册接口
//
// 通过 [SetupContext.Reg] 访问。插件应通过此接口注册命令和事件监听，
// 框架自动为每个 Matcher 设置 Group 和 Source，确保 Disable/Enable 功能正常。
//
// DryRun 模式下，框架注入 [noopRegistryWriter]，所有注册操作均为无副作用的空操作，
// 插件代码无需判断 ctx.DryRun。
//
// eventType 为平台无关的事件类型字符串（如 "C2C_MESSAGE_CREATE"）或 dto.EventType 常量，
// 传入空字符串 "" 表示通配所有事件类型。
type RegistryWriter interface {
	// RegisterCommand 注册命令 Matcher 并自动追踪
	RegisterCommand(eventType string, pattern string, extraRules ...context.Rule) *engine.Matcher

	// RegisterMatcher 注册自定义事件 Matcher 并自动追踪
	RegisterMatcher(eventType string, rules ...context.Rule) *engine.Matcher

	// RegisterPrefix 注册前缀触发 Matcher。
	//
	// 当消息内容以 prefixes 中任意一个开头时触发；去掉该前缀后的剩余部分通过
	// ctx.GetString("prefix_args") 读取（已 TrimSpace）。
	// 单个前缀传入长度为 1 的切片即可。
	//
	// prefixes 不能为空，否则 panic。
	//
	// 适用于自然语言触发（如 "选择 A还是B"）或带 / 的多别名命令前缀。
	//
	// 示例：
	//   ctx.Reg.RegisterPrefix("", []string{"选择"}).Handle(handler)
	//   // handler 内通过 ctx.GetString("prefix_args") 获取 "A还是B"
	//   ctx.Reg.RegisterPrefix("", []string{"/小红书", "/xhs"}).Handle(handler)
	//   // 两种前缀共享同一 handler，prefix_args 均正确注入
	RegisterPrefix(eventType string, prefixes []string, extraRules ...context.Rule) *engine.Matcher

	// RegisterRegex 注册正则表达式触发 Matcher。
	//
	// 当消息内容匹配正则 pattern 时触发（部分匹配，如需完整匹配请使用 ^...$ 锚点）；
	// 首个匹配结果及全部捕获组（[]string）通过 ctx.Get("regex_matched") 读取。
	//
	// pattern 编译失败时 panic，与 regexp.MustCompile 行为一致。
	// 相同 pattern 只编译一次（与 [context.OnRegex] 共享 LRU 缓存）。
	//
	// 示例：
	//   ctx.Reg.RegisterRegex("", `^[?？]{1,2}\s*([a-z0-9]+)$`).Handle(handler)
	//   // handler 内：
	//   //   if groups, ok := ctx.Get("regex_matched"); ok {
	//   //       matches := groups.([]string)
	//   //       keyword := matches[1]
	//   //   }
	RegisterRegex(eventType string, pattern string, extraRules ...context.Rule) *engine.Matcher

	// RegisterFullMatch 注册精确全文匹配 Matcher。
	//
	// 当消息内容（忽略前导空白）与 texts 中任意一个完全一致时触发，不支持参数。
	// 适用于不带参数的固定命令词，如"舔狗日记"、"摸鱼"等自然语言触发词。
	// 单个触发词传入长度为 1 的切片即可。
	//
	// texts 不能为空，否则 panic。
	//
	// 示例：
	//   ctx.Reg.RegisterFullMatch("", []string{"舔狗日记"}).Handle(handler)
	//   ctx.Reg.RegisterFullMatch("", []string{"摸鱼"}, context.OnTimeRange(9, 18)).Handle(handler)
	//   ctx.Reg.RegisterFullMatch("", []string{"讲个笑话", "来个笑话", "说个笑话"}).Handle(handler)
	RegisterFullMatch(eventType string, texts []string, extraRules ...context.Rule) *engine.Matcher

	// RegisterSuffix 注册后缀触发 Matcher。
	//
	// 当消息内容以 suffixes 中任意一个结尾时触发；去掉后缀前的剩余部分通过
	// ctx.GetString("suffix_args") 读取（已 TrimSpace）。
	// 单个后缀传入长度为 1 的切片即可。
	//
	// suffixes 不能为空，否则 panic。
	//
	// 适用于"以问号结尾的消息视为提问"等场景。
	//
	// 示例：
	//   ctx.Reg.RegisterSuffix("", []string{"？", "?"}).Handle(handler)
	//   // handler 内通过 ctx.GetString("suffix_args") 获取问题内容
	RegisterSuffix(eventType string, suffixes []string, extraRules ...context.Rule) *engine.Matcher

	// RegisterKeyword 注册关键词包含触发 Matcher。
	//
	// 当消息内容包含 keywords 中任意一个关键词时触发（子串匹配，大小写敏感）；
	// 首个命中的关键词通过 ctx.GetString("keyword_matched") 读取。
	// 单个关键词传入长度为 1 的切片即可。
	//
	// keywords 不能为空，否则 panic。
	//
	// 适用于关键词响应、内容过滤等场景；与 [context.OnKeyword] 语义一致，
	// 额外提供多关键词 OR 匹配与 keyword_matched 注入。
	//
	// 示例：
	//   ctx.Reg.RegisterKeyword("", []string{"你好", "hello", "hi"}).Handle(handler)
	//   // handler 内通过 ctx.GetString("keyword_matched") 获取触发的关键词
	RegisterKeyword(eventType string, keywords []string, extraRules ...context.Rule) *engine.Matcher
}

// --- 真实实现 ---

// registryBackend 是 liveRegistryWriter 对 engine 的完整依赖。
// 组合 MatcherWriter（注册） + Reader（FindCommand 用于别名冲突检测）。
type registryBackend interface {
	engine.MatcherWriter
	engine.Reader
}

// liveRegistryWriter 正常运行阶段的 RegistryWriter，绑定到具体 engine 和 Instance
type liveRegistryWriter struct {
	eng      registryBackend
	name     string
	instance *Instance
}

func newLiveRegistryWriter(eng registryBackend, name string, instance *Instance) RegistryWriter {
	return &liveRegistryWriter{eng: eng, name: name, instance: instance}
}

func (r *liveRegistryWriter) RegisterCommand(eventType string, pattern string, extraRules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	matcher := r.eng.OnCommand(eventType, pattern, extraRules...)
	if matcher != nil && r.name != "" {
		// SetMatcherGroup 同步更新 engine 内部的 groupIndex，
		// 确保 RemoveGroup/DisableGroup/EnableGroup 能正确找到此 Matcher。
		r.eng.SetMatcherGroup(matcher, r.name, "plugin:"+r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
		// 注入插件感知的别名自动注册回调。
		// 回调在 Matcher.Handle() 首次被调用时触发，为 definition.Aliases 中的每个别名
		// 自动注册独立的路由 Matcher（Hidden=true，不出现在命令列表）。
		// 别名 Matcher 与主命令共享相同的 Group/Source 和 instance 追踪，
		// 从而支持插件级别的 Disable/Enable 联动。
		// 传入 extraRules 确保别名 Matcher 具有与主命令相同的额外规则（如权限检查）。
		r.injectAliasRegistrar(matcher, eventType, extraRules)
	}
	return matcher
}

// injectAliasRegistrar 向 matcher 注入别名自动注册回调。
// 当 matcher.Handle() 被调用且 definition.Aliases 非空时触发一次。
func (r *liveRegistryWriter) injectAliasRegistrar(primary *engine.Matcher, eventType string, extraRules []context.Rule) {
	primary.SetAliasRegistrar(func(def *command.Definition, handler context.Handler) {
		if def == nil || len(def.Aliases) == 0 {
			return
		}
		primaryCmd := "/" + def.Name
		for _, alias := range def.Aliases {
			aliasPattern := "/" + alias
			// 冲突检测：若别名已被其他命令（非当前主命令）占用则跳过
			if existing := r.eng.FindCommand(alias); existing != nil && existing.Command != primaryCmd {
				continue
			}
			// 通过 eng.OnCommand 注册别名路由
			aliasMatcher := r.eng.OnCommand(eventType, aliasPattern, extraRules...)
			if aliasMatcher == nil {
				continue
			}
			// 别名 Matcher 与主命令同 Group/Source，以支持 Disable/Enable 联动
			// SetMatcherGroup 同步更新 groupIndex，确保 RemoveGroup 能找到别名 Matcher。
			r.eng.SetMatcherGroup(aliasMatcher, primary.GetGroup(), primary.GetSource())
			// Hidden=true：不出现在 GetAllCommands() / /help 命令列表中
			aliasMatcher.SetDefinition(&command.Definition{Name: alias, Hidden: true})
			// 与主命令共享同一 handler
			aliasMatcher.Handle(handler)
			// 注册到插件实例以便生命周期管理
			if r.instance != nil {
				r.instance.addMatcher(aliasMatcher)
			}
		}
	})
}

func (r *liveRegistryWriter) RegisterMatcher(eventType string, rules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	matcher := r.eng.On(eventType, rules...)
	if matcher != nil && r.name != "" {
		// SetMatcherGroup 同步更新 engine 内部的 groupIndex，
		// 确保 RemoveGroup/DisableGroup/EnableGroup 能正确找到此 Matcher。
		r.eng.SetMatcherGroup(matcher, r.name, "plugin:"+r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
	}
	return matcher
}

func (r *liveRegistryWriter) RegisterPrefix(eventType string, prefixes []string, extraRules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	if len(prefixes) == 0 {
		panic("RegisterPrefix: prefixes must not be empty")
	}
	prefixRule := func(c *context.Context) bool {
		// 与 context.OnPrefix 保持一致：先忽略前导空白再判断前缀
		trimmed := strings.TrimLeftFunc(c.GetMessageContent(), unicode.IsSpace)
		for _, prefix := range prefixes {
			if strings.HasPrefix(trimmed, prefix) {
				// prefix_args 同样从去除前导空白后的内容计算
				args := strings.TrimSpace(trimmed[len(prefix):])
				c.Set("prefix_args", args)
				return true
			}
		}
		return false
	}
	rules := make([]context.Rule, 0, 1+len(extraRules))
	rules = append(rules, prefixRule)
	rules = append(rules, extraRules...)

	matcher := r.eng.On(eventType, rules...)
	if matcher != nil && r.name != "" {
		r.eng.SetMatcherGroup(matcher, r.name, "plugin:"+r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
	}
	return matcher
}

func (r *liveRegistryWriter) RegisterRegex(eventType string, pattern string, extraRules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	// MustGetCachedRegexp 与 context.OnRegex 共享同一 LRU 缓存，
	// 避免相同 pattern 被多处重复编译；pattern 无效时 panic。
	re := context.MustGetCachedRegexp(pattern)
	regexRule := func(c *context.Context) bool {
		matches := re.FindStringSubmatch(c.GetMessageContent())
		if matches == nil {
			return false
		}
		// 注入捕获组（含完整匹配 matches[0]），供 handler 使用
		c.Set("regex_matched", matches)
		return true
	}
	rules := make([]context.Rule, 0, 1+len(extraRules))
	rules = append(rules, regexRule)
	rules = append(rules, extraRules...)

	matcher := r.eng.On(eventType, rules...)
	if matcher != nil && r.name != "" {
		r.eng.SetMatcherGroup(matcher, r.name, "plugin:"+r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
	}
	return matcher
}

func (r *liveRegistryWriter) RegisterFullMatch(eventType string, texts []string, extraRules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	if len(texts) == 0 {
		panic("RegisterFullMatch: texts must not be empty")
	}

	// 构建 set，快速查找
	textSet := make(map[string]struct{}, len(texts))
	for _, t := range texts {
		textSet[t] = struct{}{}
	}

	anyMatchRule := func(c *context.Context) bool {
		content := strings.TrimLeftFunc(c.GetMessageContent(), unicode.IsSpace)
		_, ok := textSet[content]
		return ok
	}

	rules := make([]context.Rule, 0, 1+len(extraRules))
	rules = append(rules, anyMatchRule)
	rules = append(rules, extraRules...)

	matcher := r.eng.On(eventType, rules...)
	if matcher != nil && r.name != "" {
		r.eng.SetMatcherGroup(matcher, r.name, "plugin:"+r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
	}
	return matcher
}

func (r *liveRegistryWriter) RegisterSuffix(eventType string, suffixes []string, extraRules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	if len(suffixes) == 0 {
		panic("RegisterSuffix: suffixes must not be empty")
	}
	suffixRule := func(c *context.Context) bool {
		content := c.GetMessageContent()
		for _, suffix := range suffixes {
			if strings.HasSuffix(content, suffix) {
				// 去掉后缀后剩余部分注入 context，供 handler 读取
				args := strings.TrimSpace(content[:len(content)-len(suffix)])
				c.Set("suffix_args", args)
				return true
			}
		}
		return false
	}

	rules := make([]context.Rule, 0, 1+len(extraRules))
	rules = append(rules, suffixRule)
	rules = append(rules, extraRules...)

	matcher := r.eng.On(eventType, rules...)
	if matcher != nil && r.name != "" {
		r.eng.SetMatcherGroup(matcher, r.name, "plugin:"+r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
	}
	return matcher
}

func (r *liveRegistryWriter) RegisterKeyword(eventType string, keywords []string, extraRules ...context.Rule) *engine.Matcher {
	if r.eng == nil {
		return nil
	}
	if len(keywords) == 0 {
		panic("RegisterKeyword: keywords must not be empty")
	}
	keywordRule := func(c *context.Context) bool {
		content := c.GetMessageContent()
		for _, kw := range keywords {
			if strings.Contains(content, kw) {
				// 注入首个命中的关键词，供 handler 区分触发来源
				c.Set("keyword_matched", kw)
				return true
			}
		}
		return false
	}
	rules := make([]context.Rule, 0, 1+len(extraRules))
	rules = append(rules, keywordRule)
	rules = append(rules, extraRules...)

	matcher := r.eng.On(eventType, rules...)
	if matcher != nil && r.name != "" {
		r.eng.SetMatcherGroup(matcher, r.name, "plugin:"+r.name)
		if r.instance != nil {
			r.instance.addMatcher(matcher)
		}
	}
	return matcher
}

// --- DryRun no-op 实现（P2-3）---
// 所有注册调用均立即返回 nil，无任何副作用。
// 框架内部在 RegisterMultipleSmart 依赖推断阶段注入此实现，
// 插件代码无需感知 DryRun，直接使用 ctx.Reg 即可。
type noopRegistryWriter struct{}

func (n *noopRegistryWriter) RegisterCommand(_ string, _ string, _ ...context.Rule) *engine.Matcher {
	return nil
}

func (n *noopRegistryWriter) RegisterMatcher(_ string, _ ...context.Rule) *engine.Matcher {
	return nil
}

func (n *noopRegistryWriter) RegisterPrefix(_ string, _ []string, _ ...context.Rule) *engine.Matcher {
	return nil
}

func (n *noopRegistryWriter) RegisterRegex(_ string, _ string, _ ...context.Rule) *engine.Matcher {
	return nil
}

func (n *noopRegistryWriter) RegisterFullMatch(_ string, _ []string, _ ...context.Rule) *engine.Matcher {
	return nil
}

func (n *noopRegistryWriter) RegisterSuffix(_ string, _ []string, _ ...context.Rule) *engine.Matcher {
	return nil
}

func (n *noopRegistryWriter) RegisterKeyword(_ string, _ []string, _ ...context.Rule) *engine.Matcher {
	return nil
}
