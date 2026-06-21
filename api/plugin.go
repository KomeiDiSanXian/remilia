package api

import (
	"context"
	"net/http"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// handleListPlugins 处理 GET /api/v1/plugins
// 返回所有已注册插件的摘要信息列表。
func (s *Server) handleListPlugins(w http.ResponseWriter, _ *http.Request) {
	if s.pluginMgr == nil {
		writeOK(w, []PluginInfo{})
		return
	}
	plugins := make([]PluginInfo, 0, len(s.pluginMgr.List()))
	for _, name := range s.pluginMgr.List() {
		inst, ok := s.pluginMgr.Get(name)
		if !ok {
			continue
		}
		plugins = append(plugins, instanceToPluginInfo(inst))
	}
	writeOK(w, plugins)
}

// handleGetPlugin 处理 GET /api/v1/plugins/{name}
// 返回指定插件的详细状态信息。
func (s *Server) handleGetPlugin(w http.ResponseWriter, r *http.Request) {
	if s.pluginMgr == nil {
		writeErr(w, 404, "plugin manager not available", http.StatusNotFound)
		return
	}
	name := r.PathValue("name")
	status, err := s.pluginMgr.GetStatus(name)
	if err != nil {
		writeErr(w, 404, "plugin not found", http.StatusNotFound)
		return
	}
	writeOK(w, status)
}

// handleEnablePlugin 处理 POST /api/v1/plugins/{name}/enable
// 启用已禁用的插件（恢复事件响应）。
func (s *Server) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.pluginMgr == nil {
		writeErr(w, 404, "plugin manager not available", http.StatusNotFound)
		return
	}
	if err := s.pluginMgr.Enable(name); err != nil {
		logger.WithError(err).Errorf("[API] Failed to enable plugin %s", name)
		writeErr(w, 500, err.Error(), http.StatusInternalServerError)
		return
	}
	writeOK(w, map[string]string{"message": "plugin enabled"})
}

// handleDisablePlugin 处理 POST /api/v1/plugins/{name}/disable
// 禁用指定插件（暂停事件响应，保持注册状态）。
func (s *Server) handleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.pluginMgr == nil {
		writeErr(w, 404, "plugin manager not available", http.StatusNotFound)
		return
	}
	if err := s.pluginMgr.Disable(name); err != nil {
		logger.WithError(err).Errorf("[API] Failed to disable plugin %s", name)
		writeErr(w, 500, err.Error(), http.StatusInternalServerError)
		return
	}
	writeOK(w, map[string]string{"message": "plugin disabled"})
}

// handleReloadPlugin 处理 POST /api/v1/plugins/{name}/reload
// 热重载指定插件（30 秒超时）。
func (s *Server) handleReloadPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.pluginMgr == nil {
		writeErr(w, 404, "plugin manager not available", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.pluginMgr.Reload(ctx, name); err != nil {
		logger.WithError(err).Errorf("[API] Failed to reload plugin %s", name)
		writeErr(w, 500, err.Error(), http.StatusInternalServerError)
		return
	}
	writeOK(w, map[string]string{"message": "plugin reloaded"})
}

// instanceToPluginInfo 将 plugin.Instance 转换为 PluginInfo 响应结构。
func instanceToPluginInfo(inst *plugin.Instance) PluginInfo {
	m := inst.Metadata()
	info := PluginInfo{
		Name:         m.Name,
		State:        inst.GetState().String(),
		Version:      m.Version,
		Uptime:       inst.GetUptime().String(),
		Dependencies: m.Dependencies,
		MatcherCount: len(inst.GetMatchers()),
	}
	if !inst.GetLoadTime().IsZero() {
		info.LoadTime = inst.GetLoadTime().Format(time.RFC3339)
	}
	if lastErr := inst.GetLastError(); lastErr != nil {
		info.LastError = lastErr.Error()
	}
	return info
}
