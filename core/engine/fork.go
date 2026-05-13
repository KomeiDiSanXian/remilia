package engine

import (
	"sync/atomic"
	"time"
)

// forkState 记录 fork 来源信息，用于懒同步。
type forkState struct {
	template    *Engine
	templateVer int64
	channelKey  ChannelKey
	lastUsed    atomic.Int64 // unix timestamp
}

// Version 返回当前引擎的 matcher 版本号。
// 每次 matcher 增删改后 bumpVersion 递增此值。
func (e *Engine) Version() int64 {
	return e.templateVer.Load()
}

// bumpVersion 递增 matcher 版本号。每次写操作（增删改 matcher）后调用，
// 通知 fork 出的子引擎在下次事件时懒同步。
func (e *Engine) bumpVersion() {
	e.templateVer.Add(1)
}

// ForkFrom 将引擎标记为指定模板引擎的子引擎。
// 子引擎初始为空，首次事件时通过 syncTemplates 从模板同步 matcher。
// 同时共享模板的 ExecPool，避免每个 channel 引擎独立创建线程池导致资源膨胀。
func (e *Engine) ForkFrom(template *Engine, channelKey ChannelKey) {
	e.fork = &forkState{
		template:    template,
		templateVer: template.Version(),
		channelKey:  channelKey,
	}
	// 共享模板的 ExecPool，fork 子引擎不拥有独立的线程池
	if template.internals.execPool != nil {
		e.internals.execPool = template.internals.execPool
	}
	// 同步模板的中间件链，使去重、限流等中间件对 fork 子引擎生效
	if tmplMW := template.middleware.Load(); tmplMW != nil {
		e.middleware.Store(copyMiddlewareState(tmplMW))
	}
	e.syncTemplates()
}

// syncTemplates 从模板引擎同步所有 matcher 到当前引擎。
// 只同步通用 matcher（事件类型为 ""）和与 channelKey 相关的 matcher。
func (e *Engine) syncTemplates() {
	if e.fork == nil {
		return
	}

	tmpl := e.fork.template
	tmplState := tmpl.state.Load()

	e.writeMu.Lock()
	childState := e.state.Load()
	currentLen := len(childState.matchers)

	tmplMatchers := tmplState.matchers

	newMatchers := make([]*Matcher, 0, len(tmplMatchers))
	for _, m := range tmplMatchers {
		if !m.IsDeleted() {
			newMatchers = append(newMatchers, m)
		}
	}

	if len(newMatchers) == currentLen {
		same := true
		for i, m := range childState.matchers {
			if i >= len(newMatchers) || m != newMatchers[i] {
				same = false
				break
			}
		}
		if same {
			e.writeMu.Unlock()
			e.fork.templateVer = tmpl.Version()
			return
		}
	}

	newState := &state{
		matchers:    newMatchers,
		block:       childState.block,
		maxMatchers: childState.maxMatchers,
	}
	newState.rebuildIndex()
	e.state.Store(newState)
	e.writeMu.Unlock()

	e.fork.templateVer = tmpl.Version()
}

// touch 更新 fork 子引擎的最后使用时间。
func (e *Engine) touch() {
	if e.fork != nil {
		e.fork.lastUsed.Store(time.Now().Unix())
	}
}

// LastUsed 返回 fork 子引擎的最后使用时间。非 fork 引擎返回零值。
func (e *Engine) LastUsed() int64 {
	if e.fork != nil {
		return e.fork.lastUsed.Load()
	}
	return 0
}

// IsFork 返回引擎是否为某个模板的子引擎。
func (e *Engine) IsFork() bool {
	return e.fork != nil
}
