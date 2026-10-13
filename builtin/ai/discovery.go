// Package ai discovery.go — 工具与技能（Skill）的自动发现与注册。
//
// 本文件实现两种自动发现机制：
//  1. 工具发现（discoverTools）：在 Setup 阶段扫描所有已注册的**无权限**命令，
//     自动将其包装为 Tool 供 LLM 调用。跳过 AI 自身命令、隐藏命令、需权限命令。
//  2. 技能发现（DiscoverSkillProviders）：在 FreezeContainer 后扫描所有插件服务，
//     实现 SkillProvider 接口的插件自动注册其 Skill。
//
// 同样适用于 ToolProvider 接口的显式注册模式。
package ai

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// DiscoverCommands 扫描当前所有已注册的无权限命令。
// excludePlugins 为已提供显式 AI 工具（ToolProvider/SkillProvider）的插件名，
// 这些插件的命令不再自动发现为工具（避免与结构化工具重复）。
// 应在插件容器冻结后、开始处理平台事件前调用。
func (p *Plugin) DiscoverCommands(excludePlugins ...string) {
	if p.coord == nil {
		return
	}
	excludeSet := make(map[string]struct{}, len(excludePlugins))
	for _, name := range excludePlugins {
		if name != "" {
			excludeSet[name] = struct{}{}
		}
	}
	catalog.Discover(p.coord, catalog.Options{
		Allowlist:      p.cfg.ToolAllowlist,
		TriggerCmd:     p.cfg.TriggerCmd,
		ExcludePlugins: excludeSet,
	}, pluginCatalogSink{p: p})
}

// pluginCatalogSink 把自动发现的结果写回插件目录：动作进注册表，
// 命令模式进执行期映射（发现与执行可能并行，故加锁）。
type pluginCatalogSink struct{ p *Plugin }

// Has 报告动作名是否已存在（显式注册优先于自动发现）。
func (s pluginCatalogSink) Has(name string) bool {
	return s.p.executionToolSnapshot(name).found
}

// Add 登记自动发现的命令动作，并记下它对应的命令模式。
func (s pluginCatalogSink) Add(tool toolkit.Tool, commandPattern string) {
	s.p.registerCommandCatalogTool(tool, commandPattern)
}

// toolProviderBinding 跟踪一个来源当前提供的工具定义。
//
// 同名工具由最后注册的 Provider 获胜；每个 Provider 保留自己的定义，移除时
// 可回退到仍提供该名称的前一个来源。绑定随来源注册一次性创建，并被
// [toolkit.ToolChangeNotifier] 的回调闭包捕获，不进入目录的公共结构。
type toolProviderBinding struct {
	// mu 串行化同一来源的同步：来源可能从多个监督协程并发通知变化。
	mu    sync.Mutex
	tools map[string]toolkit.Tool
}

// catalogWinner 是模型可见名称当前解析到的来源。provider 为 nil 时来自基础目录。
type catalogWinner struct {
	tool           toolkit.Tool
	provider       *toolProviderBinding
	commandPattern string
}

// RegisterToolProvider 注册一个实现了 ToolProvider 接口的插件所提供的工具集。
//
// 其他插件可在自己的 Setup 中通过 ctx.TryService 获取 AI 插件的服务实例
// 后调用此方法注册自定义工具，尤其是需要权限校验的敏感命令。
//
// 显式注册优先于自动发现：若工具名与自动发现的命令工具重名，
// 会覆盖自动发现的版本并清除其命令映射，保证 LLM 调用的是插件
// 自己实现的 Execute，而非通过合成事件触发真实命令。
//
// 来源若实现 [toolkit.ToolChangeNotifier]（工具集合会运行时变化），
// 其后续变化会被自动同步进目录。较新的同名 Provider 获胜，移除后回退到
// 仍提供该名称的较早来源；工具定义未变的刷新不推进目录代数。
//
// 使用示例：
//
//	if aiSvc, ok := ctx.TryService[*ai.Plugin]("ai"); ok {
//	    aiSvc.RegisterToolProvider(myToolProvider)
//	}
func (c *catalogState) RegisterToolProvider(tp toolkit.ToolProvider) {
	if tp == nil {
		return
	}
	binding := &toolProviderBinding{tools: make(map[string]toolkit.Tool)}
	c.catalogGen.mu.Lock()
	c.catalogGen.providers = append(c.catalogGen.providers, binding)
	c.catalogGen.mu.Unlock()
	c.syncToolProvider(tp, binding)
	if notifier, ok := tp.(toolkit.ToolChangeNotifier); ok {
		notifier.OnToolsChanged(func() { c.syncToolProvider(tp, binding) })
	}
}

// syncToolProvider 用来源当前的工具集刷新目录。
//
// Provider 自身的工具集先完整替换，再按注册顺序解析每个受影响名称的当前来源。
// 这样一个来源只会移除自己的定义；同名定义变化、来源切换也会推进目录代数。
func (c *catalogState) syncToolProvider(tp toolkit.ToolProvider, b *toolProviderBinding) {
	b.mu.Lock()
	defer b.mu.Unlock()

	tools := tp.ListTools()
	next := make(map[string]toolkit.Tool, len(tools))
	for _, t := range tools {
		if t.Name == "" {
			continue
		}
		t = normalizeCatalogTool(t)
		next[t.Name] = t
	}

	c.catalogGen.mu.Lock()
	affectedSet := make(map[string]struct{}, len(b.tools)+len(next))
	for name := range b.tools {
		affectedSet[name] = struct{}{}
	}
	for name := range next {
		affectedSet[name] = struct{}{}
	}
	b.tools = next
	affected := make([]string, 0, len(affectedSet))
	for name := range affectedSet {
		affected = append(affected, name)
	}
	slices.Sort(affected)
	for _, name := range affected {
		c.applyCatalogWinnerLocked(name)
	}
	c.catalogGen.mu.Unlock()
}

// normalizeCatalogTool 净化模型可见名称并复制可变字段，避免 Provider 后续修改
// 返回对象时改变目录定义。
func normalizeCatalogTool(t toolkit.Tool) toolkit.Tool {
	if !toolkit.ValidToolName(t.Name) {
		original := t.Name
		t.Name = toolkit.SanitizeToolName(t.Name)
		logger.Warnf("[AI] Tool name %q is invalid (must match ^[a-zA-Z0-9_-]+$), sanitized to %q", original, t.Name)
	}
	return toolkit.CloneTool(t)
}

// DiscoverToolProviders 扫描插件管理器中所有已注册的插件服务，
// 自动发现实现了 [ToolProvider] 接口的插件并注册其工具。
// 应在 [plugin.Manager.FreezeContainer] 之后调用。
func (c *catalogState) DiscoverToolProviders(mgr *plugin.Manager) {
	for _, name := range mgr.List() {
		svc, ok := mgr.GetContainer().Get(name)
		if !ok || svc == nil {
			continue
		}
		tp, ok := svc.(toolkit.ToolProvider)
		if !ok {
			continue
		}
		c.RegisterToolProvider(tp)
	}
}

// ensureCatalogMapsLocked 惰性初始化目录映射。测试与轻量调用方可直接构造 Plugin。
func (c *catalogState) ensureCatalogMapsLocked() {
	if c.catalogGen.baseTools == nil {
		c.catalogGen.baseTools = make(map[string]toolkit.Tool)
	}
	if c.catalogGen.baseCommandPatterns == nil {
		c.catalogGen.baseCommandPatterns = make(map[string]string)
	}
	if c.catalogGen.winners == nil {
		c.catalogGen.winners = make(map[string]catalogWinner)
	}
}

func (c *catalogState) registerCommandCatalogTool(t toolkit.Tool, commandPattern string) {
	c.registerBaseCatalogTool(t, commandPattern)
}

func (c *catalogState) registerBaseCatalogTool(t toolkit.Tool, commandPattern string) {
	if t.Name == "" {
		return
	}
	t = normalizeCatalogTool(t)
	c.catalogGen.mu.Lock()
	c.ensureCatalogMapsLocked()
	if _, exists := c.catalogGen.baseTools[t.Name]; exists {
		c.catalogGen.mu.Unlock()
		logger.Warnf("[AI] Tool %q already registered, skipping duplicate", t.Name)
		return
	}
	c.catalogGen.baseTools[t.Name] = t
	if commandPattern == "" {
		delete(c.catalogGen.baseCommandPatterns, t.Name)
	} else {
		c.catalogGen.baseCommandPatterns[t.Name] = commandPattern
	}
	c.applyCatalogWinnerLocked(t.Name)
	c.catalogGen.mu.Unlock()
}

func (c *catalogState) resolveCatalogWinnerLocked(name string) (catalogWinner, bool) {
	for _, provider := range slices.Backward(c.catalogGen.providers) {

		if tool, ok := provider.tools[name]; ok {
			return catalogWinner{tool: toolkit.CloneTool(tool), provider: provider}, true
		}
	}
	tool, ok := c.catalogGen.baseTools[name]
	if !ok {
		return catalogWinner{}, false
	}
	return catalogWinner{
		tool:           toolkit.CloneTool(tool),
		commandPattern: c.catalogGen.baseCommandPatterns[name],
	}, true
}

func sameCatalogWinner(a, b catalogWinner) bool {
	return a.provider == b.provider &&
		a.commandPattern == b.commandPattern &&
		a.tool.EffectiveSource() == b.tool.EffectiveSource() &&
		reflect.DeepEqual(toolkit.ActionOf(a.tool), toolkit.ActionOf(b.tool))
}

// applyCatalogWinnerLocked 原子更新注册表、命令映射和目录代数。调用方须持有 mu。
func (c *catalogState) applyCatalogWinnerLocked(name string) {
	c.ensureCatalogMapsLocked()
	previous, hadPrevious := c.catalogGen.winners[name]
	next, hasNext := c.resolveCatalogWinnerLocked(name)

	reason := ""
	switch {
	case !hadPrevious && hasNext:
		reason = "add"
	case hadPrevious && !hasNext:
		reason = "remove"
	case hadPrevious && hasNext && !sameCatalogWinner(previous, next):
		reason = "update"
	}

	if hasNext {
		c.reg.Upsert(next.tool)
		c.catalogGen.winners[name] = next
	} else {
		c.reg.Remove(name)
		delete(c.catalogGen.winners, name)
	}

	c.cmdMu.Lock()
	if c.cmdPatterns == nil {
		c.cmdPatterns = make(map[string]string)
	}
	if hasNext && next.commandPattern != "" {
		c.cmdPatterns[name] = next.commandPattern
	} else {
		delete(c.cmdPatterns, name)
	}
	c.cmdMu.Unlock()

	if reason != "" {
		c.bumpCatalogGeneration(reason)
	}
}

// registerCatalogTool 登记一个基础目录工具；同名基础工具仅首次生效。
func (c *catalogState) registerCatalogTool(t toolkit.Tool) {
	c.registerBaseCatalogTool(t, "")
}

// bumpCatalogGeneration 推进目录代数并打点。reason 为 add/remove/update。
func (c *catalogState) bumpCatalogGeneration(reason string) {
	recordCatalogGeneration(reason, c.catalogGen.Add(1))
}

// catalogGeneration 返回当前目录内容版本。
func (c *catalogState) catalogGeneration() uint64 {
	c.catalogGen.mu.RLock()
	defer c.catalogGen.mu.RUnlock()
	return c.catalogGen.Load()
}

// catalogSnapshot 返回当前**全局目录**（全部已登记动作）的不可变快照。
//
// 这是聚合边界：来源（注册表、外部工具服务器）的可变状态在此固定为一份带代数
// 的只读视图。群策略与 RBAC 的收敛是每轮、每调用者的事，在 actionCandidates
// 中于快照之上完成，不进快照。
func (c *catalogState) catalogSnapshot() catalog.CatalogSnapshot {
	c.catalogGen.mu.RLock()
	defer c.catalogGen.mu.RUnlock()
	return catalog.NewSnapshot(c.catalogGen.Load(), c.reg.Actions())
}

// executionToolSnapshot 同时读取工具定义和对应的命令模式。动态 Provider 更新
// 会在 catalogGen.mu 写锁内替换两者，因此执行入口持读锁取得的组合始终来自同一目录版本。
func (c *catalogState) executionToolSnapshot(name string) toolExecutionSnapshot {
	c.catalogGen.mu.RLock()
	defer c.catalogGen.mu.RUnlock()
	if c.reg == nil {
		return toolExecutionSnapshot{}
	}
	tool, ok := c.reg.Get(name)
	if !ok {
		return toolExecutionSnapshot{}
	}
	c.cmdMu.RLock()
	pattern, isCommand := c.cmdPatterns[name]
	c.cmdMu.RUnlock()
	return toolExecutionSnapshot{
		tool:           tool,
		commandPattern: pattern,
		hasCommand:     isCommand,
		found:          true,
	}
}

// RegisterSkill 注册一个系统级 Skill。
// OwnerID 为空时自动设为 OwnerSystem。
// Skill 会自动包装为 Tool 供 LLM 发现和调用。
// 如果 Parameters 为空，自动使用 {"query": string} 作为默认参数。
func (p *Plugin) RegisterSkill(s toolkit.Skill) {
	s = catalog.SystemSkill(s)
	p.skillReg.Register(s)
	p.registerSkillAsTool(s)
}

// RegisterUserSkill 注册一个用户自定义 Skill。
// name 会自动添加 u_ 前缀，OwnerID 设为 ownerID。
// 注册到 skillReg 但不注册到全局 ToolRegistry（由 processWithTools 按会话注入）。
func (p *Plugin) RegisterUserSkill(s toolkit.Skill, ownerID string) error {
	s, err := catalog.UserSkill(s, ownerID)
	if err != nil {
		return err
	}

	// Prompt 长度按字符（rune）计：配置语义是"最大字符数"，中文一字一字符；
	// 按字节数会让中文用户可用的长度只有配置值的三分之一。
	if promptLen := utf8.RuneCountInString(s.Prompt); promptLen > p.cfg.MaxUserSkillPromptLen {
		return fmt.Errorf("技能 Prompt 过长（%d > %d），请缩短", promptLen, p.cfg.MaxUserSkillPromptLen)
	}

	// 数量上限与注册在同一把写锁内完成（AddCapped）：先前的"先计数再 Add"
	// 存在 TOCTOU，并发注册可一起通过检查而突破上限。
	if err := p.skillReg.AddCapped(s, p.cfg.MaxUserSkills); err != nil {
		if errors.Is(err, toolkit.ErrOwnerSkillLimit) {
			return fmt.Errorf("达到技能数量上限 (%d)，请先删除一个再添加", p.cfg.MaxUserSkills)
		}
		return err
	}
	return nil
}

func (p *Plugin) registerSkillAsTool(s toolkit.Skill) {
	skill := s
	p.registerCatalogTool(catalog.ToolFromSkill(skill, func(ctx context.Context, args map[string]any) (string, error) {
		return p.executeSkill(ctx, skill, args)
	}))
}

// RegisterSkillProvider 注册一个实现了 SkillProvider 接口的插件所提供的技能集。
func (p *Plugin) RegisterSkillProvider(sp toolkit.SkillProvider) {
	for _, s := range sp.ListSkills() {
		p.RegisterSkill(s)
	}
}

// DiscoverSkillProviders 扫描插件管理器中所有已注册的插件服务，
// 自动发现实现了 [SkillProvider] 接口的插件并注册其技能。
// 应在 [plugin.Manager.FreezeContainer] 之后调用。
func (p *Plugin) DiscoverSkillProviders(mgr *plugin.Manager) {
	for _, name := range mgr.List() {
		svc, ok := mgr.GetContainer().Get(name)
		if !ok || svc == nil {
			continue
		}
		if sp, ok := svc.(toolkit.SkillProvider); ok {
			p.RegisterSkillProvider(sp)
		}
	}
}
