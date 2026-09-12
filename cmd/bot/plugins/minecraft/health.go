package minecraft

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/infra/health"
)

// pluginHealthChecker 报告插件自身的运行时状态（缓存规模与关键配置）。
//
// 它不做任何外部请求：缓存规模异常（例如长期不增长说明查询都在命中负缓存）
// 是排查"查询变慢/结果陈旧"类问题最直接的信号，注册到
// /health?view=full 的 system/dependencies/minecraft 节点下。
type pluginHealthChecker struct {
	p *mcPlugin
}

// Name 返回探针名称（health 树中的叶子名，需与其他插件不重名）。
func (c pluginHealthChecker) Name() string { return "minecraft" }

// Check 汇总缓存与配置快照。实现 health.Checker。
func (c pluginHealthChecker) Check(context.Context) health.CheckResult {
	meta := map[string]any{
		"srv_cache_entries":    srvCache.size(),
		"avatar_cache_entries": avatarCache.size(),
	}
	if c.p != nil {
		meta["result_cache_entries"] = c.p.cache.size()
		meta["error_cache_entries"] = c.p.errCache.size()
		meta["direct_query"] = c.p.cfg.DirectQuery
		meta["gs4_mode"] = c.p.cfg.GS4Mode
		meta["avatars"] = c.p.cfg.Avatars
		meta["cooldown"] = c.p.cfg.Cooldown.String()
		meta["favorites_enabled"] = c.p.fav != nil && c.p.fav.Available()
	}
	return health.CheckResult{Status: health.Healthy, Metadata: meta}
}
