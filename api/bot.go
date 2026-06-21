package api

import (
	"net/http"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// handleListBots 处理 GET /api/v1/bots
// 返回所有 Bot 实例的摘要信息列表（单 Bot 模式返回单元素数组）。
func (s *Server) handleListBots(w http.ResponseWriter, _ *http.Request) {
	if s.bot == nil {
		writeOK(w, []BotInfo{})
		return
	}
	writeOK(w, []BotInfo{s.botToInfo()})
}

// handleGetBot 处理 GET /api/v1/bots/{name}
// 返回指定名称的 Bot 详情。
func (s *Server) handleGetBot(w http.ResponseWriter, r *http.Request) {
	bot := s.resolveBot(w, r)
	if bot == nil {
		return
	}
	writeOK(w, s.botToInfo())
}

// handleStartBot 处理 POST /api/v1/bots/{name}/start
// 启动指定 Bot。若未配置 Bot 或名称不匹配返回 404。
func (s *Server) handleStartBot(w http.ResponseWriter, r *http.Request) {
	bot := s.resolveBot(w, r)
	if bot == nil {
		return
	}
	if err := bot.Start(); err != nil {
		logger.WithError(err).Error("[API] Failed to start bot")
		writeErr(w, 500, "failed to start bot: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeOK(w, map[string]string{"message": "bot started"})
}

// handleStopBot 处理 POST /api/v1/bots/{name}/stop
// 停止指定 Bot。
func (s *Server) handleStopBot(w http.ResponseWriter, r *http.Request) {
	bot := s.resolveBot(w, r)
	if bot == nil {
		return
	}
	if err := bot.Shutdown(); err != nil {
		logger.WithError(err).Error("[API] Failed to stop bot")
		writeErr(w, 500, "failed to stop bot: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeOK(w, map[string]string{"message": "bot stopped"})
}

// handleRestartBot 处理 POST /api/v1/bots/{name}/restart
// 先停止再启动指定 Bot。
func (s *Server) handleRestartBot(w http.ResponseWriter, r *http.Request) {
	bot := s.resolveBot(w, r)
	if bot == nil {
		return
	}
	if err := bot.Restart(); err != nil {
		logger.WithError(err).Error("[API] Failed to restart bot")
		writeErr(w, 500, "failed to restart bot: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeOK(w, map[string]string{"message": "bot restarted"})
}

// resolveBot 从请求路径中提取 bot name，验证是否存在并返回 Bot 实例。
// 若 Bot 未配置或名称不匹配，自动写入错误响应并返回 nil。
func (s *Server) resolveBot(w http.ResponseWriter, r *http.Request) *remilia.Bot {
	name := r.PathValue("name")
	if s.bot == nil {
		writeErr(w, 404, "no bot configured", http.StatusNotFound)
		return nil
	}
	cfg := s.bot.Config()
	if cfg.Name != name {
		writeErr(w, 404, "bot not found", http.StatusNotFound)
		return nil
	}
	return s.bot
}

// botToInfo 将当前 Bot 实例转换为 BotInfo 响应结构。
func (s *Server) botToInfo() BotInfo {
	cfg := s.bot.Config()
	platforms := make([]string, 0)
	if s.registry != nil {
		for _, a := range s.registry.All() {
			platforms = append(platforms, string(a.Platform()))
		}
	}
	status := "stopped"
	if s.bot.IsRunning() {
		status = "running"
	}
	pluginCount := 0
	if pm := s.bot.Plugins(); pm != nil {
		pluginCount = len(pm.List())
	}
	return BotInfo{
		Name:        cfg.Name,
		Status:      status,
		Uptime:      s.bot.Uptime().String(),
		Version:     cfg.Version,
		Platforms:   platforms,
		PluginCount: pluginCount,
	}
}
