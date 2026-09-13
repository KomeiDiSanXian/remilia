package api

import (
	"net/http"
	"runtime"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/infra/buildinfo"
)

// handleHealth 处理 GET /api/v1/health
// 返回 Bot 健康检查结果，包含各子系统的健康状态树。
// 与现有 /health 端点不同，此端点返回结构化 JSON（统一响应格式）。
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if s.bot == nil {
		writeOK(w, map[string]any{"status": "no bot"})
		return
	}
	commit, date := buildinfo.Get()
	resp := s.bot.Health()
	resp.Version = remilia.Version
	resp.Commit = commit
	resp.BuildTime = date
	writeOK(w, resp)
}

// handleVersion 处理 GET /api/v1/version
// 返回框架版本、Git commit、构建时间和 Go 运行时版本。
// 此为公开端点，无需认证。
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	commit, date := buildinfo.Get()
	writeOK(w, VersionInfo{
		Version:   remilia.Version,
		Commit:    commit,
		BuildDate: date,
		GoVersion: runtime.Version(),
	})
}

// handleStats 处理 GET /api/v1/stats
// 返回插件管理器的运行时统计快照，包括插件状态分布、goroutine 统计等。
func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	if s.pluginMgr == nil {
		writeErr(w, 404, "plugin manager not available", http.StatusNotFound)
		return
	}
	writeOK(w, s.pluginMgr.Stats())
}

// SetBuildInfo 由 main 包在初始化时注入构建信息。
// commit 和 date 是 -ldflags 传入的编译时变量。
//
// 信息实际存储在 infra/buildinfo，供本包与 builtin 等组件共享读取。
func SetBuildInfo(commit, date string) {
	buildinfo.Set(commit, date)
}

// GetBuildInfo 返回注入的构建信息（Git commit 与构建时间）。
// 未经 SetBuildInfo 注入时两个返回值均为空字符串。
func GetBuildInfo() (commit, date string) {
	return buildinfo.Get()
}
