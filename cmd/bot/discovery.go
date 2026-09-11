package main

import (
	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// wirePermissionManager 把 permission 插件的 RBAC 管理器注入 Bot，使每个事件
// Context 都带上权限管理器（ctx.GetPermissionManager）。
//
// 必须在插件注册（permission 插件 Setup）之后、Bot.Start() 之前调用。
// 不注入时所有基于 ctx 的 RBAC 检查都会退化为“权限系统未初始化”：
// core/context 的 OnHasRole / OnHasPermission 规则恒不命中、middleware/auth
// 的 RequireRole 等中间件 fail-closed 拒绝、插件内的 isAdmin / isSuperAdmin
// 恒为 false（超管同样被判为无权）。
func wirePermissionManager(bot *remilia.Bot, pm *plugin.Manager) {
	raw, ok := pm.GetContainer().Get("permission")
	if !ok || raw == nil {
		logger.Warn("[remilia] permission plugin not found; RBAC checks based on " +
			"ctx.GetPermissionManager() will deny by default")
		return
	}
	pp, ok := raw.(*permission.Plugin)
	if !ok {
		logger.Warn("[remilia] unexpected permission plugin type; RBAC manager not wired")
		return
	}
	bot.UsePermissionManager(pp.GetManager())
	logger.Info("[remilia] RBAC permission manager wired into event contexts")
}

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
