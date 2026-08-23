package main

import (
	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// discoverAll 在 FreezeContainer 后执行所有插件的自动发现与注册。
//
// 当前包含两个阶段（可并发）：
//  1. AI Tool/Skill 发现 — 扫描容器中的 ToolProvider/SkillProvider
//  2. 健康检查自动注册 — 扫描容器中的 CheckProvider
//
// 扩展: 后续如需新增自动发现阶段，在此函数中添加即可。
func discoverAll(bot *remilia.Bot, pm *plugin.Manager) {
	if aiRaw, ok := pm.GetContainer().Get("ai"); ok {
		aiPlugin := aiRaw.(*ai.Plugin)
		// 先注册显式工具/技能，再自动发现命令工具：
		// 已实现 ToolProvider/SkillProvider 的插件视为已向 AI 暴露结构化
		// 工具，其命令不再自动发现（避免粗糙命令工具挤占选择名额）。
		aiPlugin.DiscoverToolProviders(pm)
		aiPlugin.DiscoverSkillProviders(pm)
		aiPlugin.DiscoverCommands(excludedToolPlugins(pm)...)
	}

	if hc := bot.HealthCheck(); hc != nil {
		for _, name := range pm.List() {
			svc, ok := pm.GetContainer().Get(name)
			if !ok || svc == nil {
				continue
			}
			if hp, ok := svc.(health.CheckProvider); ok {
				for _, checker := range hp.HealthCheckers() {
					hc.Register(checker, "system", "dependencies", checker.Name())
				}
			}
		}
	}

	logger.Info("[remilia] Plugin discovery complete")
}

// excludedToolPlugins 返回已实现 ai.ToolProvider 或 ai.SkillProvider 的插件名，
// 用于跳过这些插件命令的自动发现（去重）。
func excludedToolPlugins(pm *plugin.Manager) []string {
	var out []string
	for _, name := range pm.List() {
		svc, ok := pm.GetContainer().Get(name)
		if !ok || svc == nil {
			continue
		}
		if _, ok := svc.(ai.ToolProvider); ok {
			out = append(out, name)
			continue
		}
		if _, ok := svc.(ai.SkillProvider); ok {
			out = append(out, name)
		}
	}
	return out
}
