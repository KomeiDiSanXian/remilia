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
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
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
	_, exists := s.p.reg.Get(name)
	return exists
}

// Add 登记自动发现的命令动作，并记下它对应的命令模式。
func (s pluginCatalogSink) Add(tool toolkit.Tool, commandPattern string) {
	s.p.cmdMu.Lock()
	s.p.cmdPatterns[tool.Name] = commandPattern
	s.p.cmdMu.Unlock()
	s.p.registerCatalogTool(tool)
}

// toolProviderBinding 跟踪一个来源当前登记进目录的工具名。
//
// 动态来源（工具集合会运行时变化）在集合变化时只应移除"自己已不再提供"的工具，
// 因此需要记住自己上一轮登记了哪些名字。绑定随来源注册一次性创建，并被
// [toolkit.ToolChangeNotifier] 的回调闭包捕获，不进入目录的公共结构。
type toolProviderBinding struct {
	// mu 串行化同一来源的同步：来源可能从多个监督协程并发通知变化。
	mu    sync.Mutex
	names []string
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
// 其后续变化会被自动同步进目录：新增登记、消失移除、同名就地更新定义。
// 成员关系未变的刷新不推进目录代数，因此不会击穿会话缓存与前缀缓存。
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
	binding := &toolProviderBinding{}
	c.syncToolProvider(tp, binding)
	if notifier, ok := tp.(toolkit.ToolChangeNotifier); ok {
		notifier.OnToolsChanged(func() { c.syncToolProvider(tp, binding) })
	}
}

// syncToolProvider 用来源当前的工具集刷新目录。
//
// 三条路径彼此独立：来源新提供且目录中不存在的工具 → 登记（推进代数）；
// 来源曾提供但已消失的工具 → 移除（推进代数）；来源仍提供且目录中已存在的
// 工具 → 就地覆盖定义（成员关系不变，不推进代数，因此重连/同名改 schema
// 不会让缓存失效——定义每轮都会从注册表重新物化给模型）。
func (c *catalogState) syncToolProvider(tp toolkit.ToolProvider, b *toolProviderBinding) {
	b.mu.Lock()
	defer b.mu.Unlock()

	tools := tp.ListTools()
	next := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		if t.Name == "" {
			continue
		}
		next[t.Name] = struct{}{}
		c.upsertCatalogTool(t)
		// 显式注册覆盖同名的自动发现命令：清掉命令映射，避免执行期又走真实命令。
		c.clearCommandPattern(t.Name)
	}
	for _, name := range b.names {
		if _, ok := next[name]; ok {
			continue
		}
		c.removeCatalogTool(name)
	}
	names := make([]string, 0, len(next))
	for name := range next {
		names = append(names, name)
	}
	slices.Sort(names)
	b.names = names
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

// registerCatalogTool 登记一个目录工具（同名仅首次生效）；仅当成员关系变化
// （此前不存在）时推进目录代数。
func (c *catalogState) registerCatalogTool(t toolkit.Tool) {
	if t.Name == "" {
		return
	}
	if _, exists := c.reg.Get(t.Name); !exists {
		c.bumpCatalogGeneration("add")
	}
	c.reg.Register(t)
}

// upsertCatalogTool 登记或就地覆盖一个目录工具；仅新增成员时推进代数。
func (c *catalogState) upsertCatalogTool(t toolkit.Tool) {
	if t.Name == "" {
		return
	}
	if _, exists := c.reg.Get(t.Name); !exists {
		c.bumpCatalogGeneration("add")
	}
	c.reg.Upsert(t)
}

// removeCatalogTool 移除一个目录工具；实际删除时推进代数并清理命令映射。
func (c *catalogState) removeCatalogTool(name string) {
	if c.reg.Remove(name) {
		c.bumpCatalogGeneration("remove")
	}
	c.clearCommandPattern(name)
}

// bumpCatalogGeneration 推进目录代数并打点。reason 区分新增（add）与移除
// （remove）；每次推进都会让会话级选择缓存失效，指标用于观测抖动来源。
func (c *catalogState) bumpCatalogGeneration(reason string) {
	recordCatalogGeneration(reason, c.catalogGen.Add(1))
}

// clearCommandPattern 清除某动作名对应的真实命令映射。
func (c *catalogState) clearCommandPattern(name string) {
	c.cmdMu.Lock()
	delete(c.cmdPatterns, name)
	c.cmdMu.Unlock()
}

// catalogGeneration 返回当前目录代数（工具成员关系的版本号）。
func (c *catalogState) catalogGeneration() uint64 { return c.catalogGen.Load() }

// catalogSnapshot 返回当前**全局目录**（全部已登记动作）的不可变快照。
//
// 这是聚合边界：来源（注册表、外部工具服务器）的可变状态在此固定为一份带代数
// 的只读视图。群策略与 RBAC 的收敛是每轮、每调用者的事，在 actionCandidates
// 中于快照之上完成，不进快照。
func (c *catalogState) catalogSnapshot() catalog.CatalogSnapshot {
	return catalog.NewSnapshot(c.catalogGen.Load(), c.reg.Actions())
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
