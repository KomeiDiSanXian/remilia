package api

import (
	"net/http"
	"sort"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// engineRef 返回 Engine 实例，优先从 Deps.Engine 取，其次从 bot.Engine()。
func (s *Server) engineRef() *engine.Engine {
	if s.engine != nil {
		return s.engine
	}
	if s.bot != nil {
		return s.bot.Engine()
	}
	return nil
}

// cmdResponse 是命令列表的 JSON 安全响应类型，排除不可 JSON 序列化的 Definition 字段。
type cmdResponse struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Usage       string   `json:"usage"`
	Aliases     []string `json:"aliases,omitempty"`
	Category    string   `json:"category"`
	Examples    []string `json:"examples,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Plugin      string   `json:"plugin"`
}

// handleGetEngineCommands 处理 GET /api/v1/engine/commands
// 返回所有已注册的命令列表，按分类排序。
func (s *Server) handleGetEngineCommands(w http.ResponseWriter, _ *http.Request) {
	eng := s.engineRef()
	if eng == nil {
		writeErr(w, 404, "engine not available", http.StatusNotFound)
		return
	}
	cmds := eng.GetAllCommands()
	sort.Slice(cmds, func(i, j int) bool {
		if cmds[i].Category != cmds[j].Category {
			return cmds[i].Category < cmds[j].Category
		}
		return cmds[i].Command < cmds[j].Command
	})
	resp := make([]cmdResponse, len(cmds))
	for i, c := range cmds {
		resp[i] = cmdResponse{
			Command:     c.Command,
			Description: c.Description,
			Usage:       c.Usage,
			Aliases:     c.Aliases,
			Category:    c.Category,
			Examples:    c.Examples,
			Permissions: c.Permissions,
			Plugin:      c.Plugin,
		}
	}
	writeOK(w, resp)
}

// handleGetEngineMatchers 处理 GET /api/v1/engine/matchers
// 返回匹配器统计信息（总数、临时数、按插件分布等）。
func (s *Server) handleGetEngineMatchers(w http.ResponseWriter, _ *http.Request) {
	eng := s.engineRef()
	if eng == nil {
		writeErr(w, 404, "engine not available", http.StatusNotFound)
		return
	}
	stats := eng.GetMatcherStats()
	resp := map[string]any{
		"total":          stats.Total,
		"global":         stats.Global,
		"by_plugin":      stats.ByPlugin,
		"global_enabled": stats.GlobalEnabled,
		"temp":           eng.GetTempMatcherCount(),
	}
	writeOK(w, resp)
}

// handleListMatcherGroups 处理 GET /api/v1/engine/matchers/groups
// 返回所有匹配器分组的只读快照（名称、匹配器数、启用状态）。
func (s *Server) handleListMatcherGroups(w http.ResponseWriter, _ *http.Request) {
	eng := s.engineRef()
	if eng == nil {
		writeErr(w, 404, "engine not available", http.StatusNotFound)
		return
	}
	groups := eng.ListGroups()
	writeOK(w, map[string]any{
		"groups": groups,
		"count":  len(groups),
	})
}

// handleDisableMatcherGroup 处理 POST /api/v1/engine/matchers/group/{name}/disable
func (s *Server) handleDisableMatcherGroup(w http.ResponseWriter, r *http.Request) {
	eng := s.engineRef()
	if eng == nil {
		writeErr(w, 404, "engine not available", http.StatusNotFound)
		return
	}
	name := r.PathValue("name")
	eng.DisableGroup(name)
	logger.Infof("[API] Matcher group %s disabled", name)
	writeOK(w, map[string]string{"message": "matcher group disabled"})
}

// handleEnableMatcherGroup 处理 POST /api/v1/engine/matchers/group/{name}/enable
func (s *Server) handleEnableMatcherGroup(w http.ResponseWriter, r *http.Request) {
	eng := s.engineRef()
	if eng == nil {
		writeErr(w, 404, "engine not available", http.StatusNotFound)
		return
	}
	name := r.PathValue("name")
	eng.EnableGroup(name)
	logger.Infof("[API] Matcher group %s enabled", name)
	writeOK(w, map[string]string{"message": "matcher group enabled"})
}
