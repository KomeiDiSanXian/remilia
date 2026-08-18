package api

import (
	"context"
	"net/http"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/core/permission"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infraserver "github.com/KomeiDiSanXian/remilia/infra/server"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Deps 是 Server 的依赖注入容器。
type Deps struct {
	Bot              *remilia.Bot
	PluginMgr        *plugin.Manager
	Registry         *platform.Registry
	Engine           *engine.Engine
	FSMMgr           *fsm.Manager
	PermissionMgr    *permission.Manager
	ConfigPath       string
	DashboardHandler http.Handler
}

// Server 封装了管理 API 的 HTTP 服务器。
type Server struct {
	srv              *infraserver.HTTPServer
	addr             string
	apiKey           string
	configPath       string
	dashboardHandler http.Handler
	bot              *remilia.Bot
	pluginMgr        *plugin.Manager
	registry         *platform.Registry
	engine           *engine.Engine
	fsmMgr           *fsm.Manager
	permMgr          *permission.Manager
}

// NewServer 创建并初始化管理 API 服务器。
func NewServer(addr, apiKey string, deps Deps) *Server {
	s := &Server{
		addr:             addr,
		apiKey:           apiKey,
		configPath:       deps.ConfigPath,
		dashboardHandler: deps.DashboardHandler,
		bot:              deps.Bot,
		pluginMgr:        deps.PluginMgr,
		registry:         deps.Registry,
		engine:           deps.Engine,
		fsmMgr:           deps.FSMMgr,
		permMgr:          deps.PermissionMgr,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.srv = infraserver.NewHTTPServer(addr, cors(mux))
	return s
}

// registerRoutes 注册所有路由。
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Bot
	mux.HandleFunc("GET /api/v1/bots", s.auth(s.handleListBots))
	mux.HandleFunc("GET /api/v1/bots/{name}", s.auth(s.handleGetBot))
	mux.HandleFunc("POST /api/v1/bots/{name}/start", s.auth(s.handleStartBot))
	mux.HandleFunc("POST /api/v1/bots/{name}/stop", s.auth(s.handleStopBot))
	mux.HandleFunc("POST /api/v1/bots/{name}/restart", s.auth(s.handleRestartBot))

	// Plugin
	mux.HandleFunc("GET /api/v1/plugins", s.auth(s.handleListPlugins))
	mux.HandleFunc("GET /api/v1/plugins/{name}", s.auth(s.handleGetPlugin))
	mux.HandleFunc("POST /api/v1/plugins/{name}/enable", s.auth(s.handleEnablePlugin))
	mux.HandleFunc("POST /api/v1/plugins/{name}/disable", s.auth(s.handleDisablePlugin))
	mux.HandleFunc("POST /api/v1/plugins/{name}/reload", s.auth(s.handleReloadPlugin))

	// Platform
	mux.HandleFunc("GET /api/v1/platforms", s.auth(s.handleListPlatforms))
	mux.HandleFunc("GET /api/v1/platforms/{name}", s.auth(s.handleGetPlatform))
	mux.HandleFunc("POST /api/v1/platforms", s.auth(s.handleAddPlatform))
	mux.HandleFunc("DELETE /api/v1/platforms/{name}", s.auth(s.handleDeletePlatform))

	// Engine
	mux.HandleFunc("GET /api/v1/engine/commands", s.auth(s.handleGetEngineCommands))
	mux.HandleFunc("GET /api/v1/engine/matchers", s.auth(s.handleGetEngineMatchers))
	mux.HandleFunc("GET /api/v1/engine/matchers/groups", s.auth(s.handleListMatcherGroups))
	mux.HandleFunc("POST /api/v1/engine/matchers/group/{name}/disable", s.auth(s.handleDisableMatcherGroup))
	mux.HandleFunc("POST /api/v1/engine/matchers/group/{name}/enable", s.auth(s.handleEnableMatcherGroup))

	// Audit Log
	mux.HandleFunc("GET /api/v1/auditlog", s.auth(s.handleGetAuditLog))
	mux.HandleFunc("GET /api/v1/auditlog/user/{id}", s.auth(s.handleGetAuditLogByUser))
	mux.HandleFunc("GET /api/v1/auditlog/action/{action}", s.auth(s.handleGetAuditLogByAction))
	mux.HandleFunc("GET /api/v1/auditlog/count", s.auth(s.handleGetAuditLogCount))

	// Permission
	mux.HandleFunc("GET /api/v1/permission/roles", s.auth(s.handleListRoles))
	mux.HandleFunc("POST /api/v1/permission/roles", s.auth(s.handleCreateRole))
	mux.HandleFunc("DELETE /api/v1/permission/roles/{role}", s.auth(s.handleDeleteRole))
	mux.HandleFunc("POST /api/v1/permission/roles/{role}/permissions", s.auth(s.handleAddRolePermission))
	mux.HandleFunc("DELETE /api/v1/permission/roles/{role}/permissions", s.auth(s.handleRemoveRolePermission))
	mux.HandleFunc("POST /api/v1/permission/users/{userID}/roles", s.auth(s.handleAssignRole))
	mux.HandleFunc("DELETE /api/v1/permission/users/{userID}/roles/{role}", s.auth(s.handleRevokeRole))
	mux.HandleFunc("GET /api/v1/permission/users/{userID}/permissions", s.auth(s.handleGetUserPermissions))
	mux.HandleFunc("POST /api/v1/permission/check", s.auth(s.handleCheckPermission))

	// FSM
	mux.HandleFunc("GET /api/v1/fsm", s.auth(s.handleListFSMs))
	mux.HandleFunc("GET /api/v1/fsm/{name}", s.auth(s.handleGetFSM))
	mux.HandleFunc("GET /api/v1/fsm/sessions", s.auth(s.handleListFSMSessions))
	mux.HandleFunc("DELETE /api/v1/fsm/sessions/{id}", s.auth(s.handleEndFSMSession))

	// Scheduler
	mux.HandleFunc("GET /api/v1/scheduler/jobs", s.auth(s.handleListSchedulerJobs))
	mux.HandleFunc("DELETE /api/v1/scheduler/jobs/{id}", s.auth(s.handleDeleteSchedulerJob))
	mux.HandleFunc("GET /api/v1/scheduler/history", s.auth(s.handleGetSchedulerHistory))

	// Logs
	mux.HandleFunc("GET /api/v1/logs", s.auth(s.handleGetLogs))
	mux.HandleFunc("GET /api/v1/logs/stream", s.auth(s.handleLogStream))

	// Config
	mux.HandleFunc("GET /api/v1/config", s.auth(s.handleGetConfig))
	mux.HandleFunc("PUT /api/v1/config", s.auth(s.handlePutConfig))
	mux.HandleFunc("POST /api/v1/config/reload", s.auth(s.handleReloadConfig))
	mux.HandleFunc("GET /api/v1/stats", s.auth(s.handleStats))

	// System (public)
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)

	if s.dashboardHandler != nil {
		mux.Handle("/", s.dashboardHandler)
	}
}

// Start 在后台 goroutine 中启动 HTTP 服务器。
func (s *Server) Start() {
	s.srv.Start()
	logger.Infof("[API] Management API at http://%s/api/v1/", s.addr)
}

// Stop 优雅关闭 HTTP 服务器。
func (s *Server) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
