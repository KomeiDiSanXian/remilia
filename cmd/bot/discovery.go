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
		aiPlugin.DiscoverCommands()
		aiPlugin.DiscoverToolProviders(pm)
		aiPlugin.DiscoverSkillProviders(pm)
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
