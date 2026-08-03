package context

// candidate.go — 候选 Meta 注入（候选携带匹配结果）
//
// 慢带索引（如 regexIndex）在候选生成阶段已产生匹配结果
// （如正则捕获组），经 CandidatePlan.Meta() 随候选流入执行循环，
// 引擎在 invokeHandler 前注入 Context。Handler 通过类型化 getter
// 直接读取，无需重新执行匹配逻辑。

// RegexMatch 是正则索引预匹配的结果，随候选 Meta 注入 Context，
// Handler 通过 [Context.RegexResult] 读取捕获组。
// 由 regexIndex（引擎侧）构建，Handler 只读。
type RegexMatch struct {
	// Pattern 是触发匹配的正则模式（matcher.Regex 注册值）。
	Pattern string
	// Groups 是 FindStringSubmatch 的结果：下标 0 为完整匹配，
	// 其后依次为各捕获组（无捕获组时长度为 1）。
	Groups []string
}

// candidateMeta 以 typed extension 形式存放当前候选携带的 Meta。
// 由引擎在 invokeHandler 前注入；非索引预匹配路径为 nil。
type candidateMeta struct {
	Meta any
}

// SetCandidateMeta 设置当前候选携带的匹配结果（框架内部，由引擎注入）。
// meta 为 nil 时为空操作。
func (ctx *Context) SetCandidateMeta(meta any) {
	if ctx == nil || meta == nil {
		return
	}
	ExtSet(ctx.Ext(), candidateMeta{Meta: meta})
}

// CandidateMeta 返回当前候选携带的 Meta；未注入时为 nil。
func (ctx *Context) CandidateMeta() any {
	if ctx == nil {
		return nil
	}
	if cm, ok := ExtGet[candidateMeta](ctx.Ext()); ok {
		return cm.Meta
	}
	return nil
}

// RegexResult 返回正则索引预匹配的捕获组结果（类型化 getter）。
//
// 仅对 regexIndex 路由的 matcher（matcher.Regex 注册）可用：
//
//	m := e.On(et).Regex(`hello (\w+)`)
//	m.Handle(func(ctx *context.Context) error {
//	    if res, ok := ctx.RegexResult(); ok {
//	        name := res.Groups[1] // 捕获组，无需重新执行正则
//	    }
//	    return nil
//	})
func (ctx *Context) RegexResult() (RegexMatch, bool) {
	m, ok := ctx.CandidateMeta().(RegexMatch)
	return m, ok
}
