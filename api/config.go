package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"gopkg.in/yaml.v3"
)

// handleGetConfig 处理 GET /api/v1/config
func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, ok := config.Get()
	if !ok {
		writeErr(w, 500, "config not loaded", http.StatusInternalServerError)
		return
	}
	writeOK(w, maskSensitive(cfg))
}

// handlePutConfig 处理 PUT /api/v1/config
//
// 接收 JSON 格式的部分配置更新，深合并到当前配置后：
//  1. 序列化为 YAML → 写入 config.yaml
//  2. 写入后 config.Load() 触发监听器
//  3. 若 Validate() 不通过，文件写入失败，原配置不变
// writeConfigUpdate 将 updates 深合并到当前配置文件并持久化。
// 内部完成验证、写盘、重载全流程，可由平台增删等 handler 复用。
func (s *Server) writeConfigUpdate(updates map[string]any) error {
	path := s.configPath
	if path == "" {
		path = "config.yaml"
	}
	currentData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	current := make(map[string]any)
	if err := yaml.Unmarshal(currentData, &current); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	merged := deepMerge(current, updates)
	mergedYAML, err := yaml.Marshal(merged)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var testCfg config.Config
	if err := yaml.Unmarshal(mergedYAML, &testCfg); err != nil {
		return fmt.Errorf("validation parse: %w", err)
	}
	if err := testCfg.Validate(); err != nil {
		return fmt.Errorf("validation: %w", err)
	}
	if err := os.WriteFile(path, mergedYAML, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if _, err := config.Load(path); err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	return nil
}

// deleteConfigKey 从深层嵌套的 map 中删除由 keys 指定的路径。
// 例如 deleteConfigKey(m, "bot", "qq") 会删除 m["bot"]["qq"]。
func deleteConfigKey(m map[string]any, keys ...string) {
	if len(keys) == 0 {
		return
	}
	if len(keys) == 1 {
		delete(m, keys[0])
		return
	}
	if child, ok := m[keys[0]].(map[string]any); ok {
		deleteConfigKey(child, keys[1:]...)
	}
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeErr(w, 400, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.writeConfigUpdate(updates); err != nil {
		logger.WithError(err).Error("[API] Failed to update config")
		writeErr(w, 500, err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Info("[API] Config updated successfully")
	writeOK(w, map[string]string{"message": "config updated"})
}

// deepMerge 递归合并两个 map。map[string]any 类型的值递归合并，标量值直接覆盖。
func deepMerge(dst, src map[string]any) map[string]any {
	result := make(map[string]any, len(dst))
	for k, v := range dst {
		result[k] = v
	}
	for k, srcVal := range src {
		dstVal, dstOK := result[k]
		srcMap, srcIsMap := srcVal.(map[string]any)
		dstMap, dstIsMap := dstVal.(map[string]any)
		if dstOK && dstIsMap && srcIsMap {
			result[k] = deepMerge(dstMap, srcMap)
		} else {
			result[k] = srcVal
		}
	}
	return result
}

// maskSensitive 将 config.Get() 返回的配置脱敏后返回（JSON 序列化 → map 操作）。
func maskSensitive(raw any) any {
	data, err := json.Marshal(raw)
	if err != nil {
		return raw
	}
	var tree map[string]any
	if err := json.Unmarshal(data, &tree); err != nil {
		return raw
	}
	maskTree(tree)
	return tree
}

func maskTree(tree map[string]any) {
	mask := func(s string) string {
		if len(s) == 0 {
			return ""
		}
		if len(s) <= 4 {
			return "****"
		}
		return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
	}
	for _, path := range []string{
		"bot.qq.token", "bot.qq.secret",
		"bot.onebot.token", "bot.onebot.secret",
		"bot.discord.token", "bot.satori.token",
		"bot.milky.access_token",
	} {
		parts := strings.Split(path, ".")
		if v := walkGet(tree, parts); v != nil {
			if s, ok := v.(string); ok && s != "" {
				walkSet(tree, parts, mask(s))
			}
		}
	}
	if plugins, _ := tree["plugins"].(map[string]any); plugins != nil {
		for _, pcfg := range plugins {
			pm, _ := pcfg.(map[string]any)
			if pm == nil {
				continue
			}
			for _, sk := range []string{"api_key", "token", "secret", "password", "access_token"} {
				if val, ok := pm[sk]; ok {
					if s, ok := val.(string); ok && s != "" {
						pm[sk] = mask(s)
					}
				}
			}
		}
	}
}

func walkGet(m map[string]any, path []string) any {
	current := m
	for i, p := range path {
		if i == len(path)-1 {
			return current[p]
		}
		next, ok := current[p].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return nil
}

func walkSet(m map[string]any, path []string, value any) {
	current := m
	for i, p := range path {
		if i == len(path)-1 {
			current[p] = value
			return
		}
		next, ok := current[p].(map[string]any)
		if !ok {
			return
		}
		current = next
	}
}

// handleReloadConfig 处理 POST /api/v1/config/reload
func (s *Server) handleReloadConfig(w http.ResponseWriter, _ *http.Request) {
	path := s.configPath
	if path == "" {
		path = "config.yaml"
	}
	if _, err := config.Load(path); err != nil {
		logger.WithError(err).Error("[API] Failed to reload config")
		writeErr(w, 500, "failed to reload config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Info("[API] Config reloaded successfully")
	writeOK(w, map[string]string{"message": "config reloaded"})
}
