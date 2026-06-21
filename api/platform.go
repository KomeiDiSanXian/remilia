package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"gopkg.in/yaml.v3"
)

// handleListPlatforms 处理 GET /api/v1/platforms
func (s *Server) handleListPlatforms(w http.ResponseWriter, _ *http.Request) {
	if s.registry == nil {
		writeOK(w, []map[string]any{})
		return
	}
	platforms := make([]map[string]any, 0)
	for _, a := range s.registry.All() {
		platforms = append(platforms, s.platformToMap(a))
	}
	writeOK(w, platforms)
}

// handleGetPlatform 处理 GET /api/v1/platforms/{name}
func (s *Server) handleGetPlatform(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		writeErr(w, 404, "platform registry not available", http.StatusNotFound)
		return
	}
	name := r.PathValue("name")
	a, ok := s.registry.Get(name)
	if !ok {
		writeErr(w, 404, "platform not found", http.StatusNotFound)
		return
	}
	writeOK(w, s.platformDetail(a))
}

// handleAddPlatform 处理 POST /api/v1/platforms
// 将平台配置写入 config.yaml，重启后生效。
// Body: {"type":"qq","config":{"app_id":123,"bot_id":456,"token":"...","secret":"..."}}
func (s *Server) handleAddPlatform(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type   string         `json:"type"`
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Type == "" {
		writeErr(w, 400, "type is required", http.StatusBadRequest)
		return
	}

	updates := map[string]any{
		"bot": map[string]any{
			body.Type: body.Config,
		},
	}

	if err := s.writeConfigUpdate(updates); err != nil {
		logger.WithError(err).Errorf("[API] Failed to add platform %s", body.Type)
		writeErr(w, 500, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("[API] Platform %s config added", body.Type)
	writeOK(w, map[string]string{"message": "platform config added, restart required to take effect"})
}

// handleDeletePlatform 处理 DELETE /api/v1/platforms/{name}
// 停止并移除适配器，同时从 config.yaml 中删除对应配置。
func (s *Server) handleDeletePlatform(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.registry == nil {
		writeErr(w, 404, "platform registry not available", http.StatusNotFound)
		return
	}

	// 运行时移除
	if a, ok := s.registry.Get(name); ok {
		if a.IsRunning() {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			if err := a.Stop(ctx); err != nil {
				logger.WithError(err).Warnf("[API] Platform %s stop error", name)
			}
			cancel()
		}
		s.registry.Remove(name)
	}

	// 从 config.yaml 删除
	path := s.configPath
	if path == "" {
		path = "config.yaml"
	}
	currentData, err := os.ReadFile(path)
	if err == nil {
		var current map[string]any
		if yaml.Unmarshal(currentData, &current) == nil {
			deleteConfigKey(current, "bot", name)
			if out, err := yaml.Marshal(current); err == nil {
				_ = os.WriteFile(path, out, 0644)
				_, _ = config.Load(path)
			}
		}
	}

	logger.Infof("[API] Platform %s removed", name)
	writeOK(w, map[string]string{"message": "platform removed"})
}

func (s *Server) platformToMap(a platform.Adapter) map[string]any {
	p := map[string]any{
		"name":         a.Platform(),
		"running":      a.IsRunning(),
		"capabilities": a.Capabilities(),
	}
	if bi, ok := a.(interface{ BotID() string }); ok {
		p["bot_id"] = bi.BotID()
	}
	return p
}

func (s *Server) platformDetail(a platform.Adapter) map[string]any {
	p := s.platformToMap(a)
	if bn, ok := a.(interface{ BotName() string }); ok {
		p["bot_name"] = bn.BotName()
	}
	return p
}
