package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strconv"
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
//
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
	// GET /config 返回 Go 字段名（Bot / QQ / Token），配置文件里是 YAML 键名
	// （bot / qq / token）。不归一化就深合并会写出并存的大小写重复段落，
	// YAML 大小写不敏感，实际生效的仍是旧的小写段落——仪表盘改了配置却"没保存"。
	canonicalizeKeys(current, updates)
	// GET /config 中 QQ AppID/BotID 等 ID 字段以字符串输出（json 标签 `,string`），
	// 写回时还原为数字，保证校验与落盘逻辑拿到的是数字。
	normalizeNumericStrings(current, updates)
	// 脱敏占位符不能写回磁盘：原始 JSON 编辑器会把含占位符的整棵配置原样回传。
	dropMaskedSecrets(current, updates)
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
	maps.Copy(result, dst)
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

// canonicalizeKeys 将 updates 中与 current 归一化同名的键改写为 current 的实际键名。
//
// 例如文件里是 "log"，更新来自 GET /config 的 "Log"，改写后深合并命中同一节点，
// 而不是生成并存的大小写重复段落。
func canonicalizeKeys(current, updates map[string]any) {
	for k, v := range updates {
		actual, found := lookupKey(current, k)
		if !found {
			continue
		}
		if actual != k {
			if _, exists := updates[actual]; !exists {
				updates[actual] = v
				delete(updates, k)
			}
			k = actual
		}
		child, ok := v.(map[string]any)
		if !ok {
			continue
		}
		parent, ok := current[k].(map[string]any)
		if !ok {
			continue
		}
		canonicalizeKeys(parent, child)
	}
}

// normalizeNumericStrings 将 updates 中"当前值是数字、提交值是可解析的数字字符串"的项还原为数字。
//
// GET /api/v1/config 对 QQ AppID/BotID 等 ID 字段用字符串输出（json 标签 `,string`），
// 避免 JavaScript Number 的 2^53 精度上限在浏览器端损坏大整数。
// 原样写回时若保持字符串，yaml.Unmarshal 到 uint64 会失败、配置文件里也会多出引号，
// 因此只有在确认目标字段本身就是数字时才转换；真正的字符串字段不受影响。
func normalizeNumericStrings(current, updates map[string]any) {
	for k, v := range updates {
		if child, ok := v.(map[string]any); ok {
			var currentChild map[string]any
			if actual, found := lookupKey(current, k); found {
				currentChild, _ = current[actual].(map[string]any)
			}
			if currentChild == nil {
				continue
			}
			normalizeNumericStrings(currentChild, child)
			continue
		}
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		actual, found := lookupKey(current, k)
		if !found || !isNumericValue(current[actual]) {
			continue
		}
		if n, ok := parseNumber(s); ok {
			updates[k] = n
		}
	}
}

// isNumericValue 判断值是否为 YAML / JSON 解码产生的数字类型。
func isNumericValue(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

// parseNumber 依次尝试把字符串解析为 uint64 / int64 / float64。
func parseNumber(s string) (any, bool) {
	if u, err := strconv.ParseUint(s, 10, 64); err == nil {
		return u, true
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	return nil, false
}

// sensitiveKeySuffixes 敏感配置键名后缀（归一化后比较）。
//
// 用后缀而不是精确名单，是为了覆盖 embedding_api_key、verify_token 等变体；
// max_tokens 之类包含 "token" 但不以其结尾的键不会误伤。
var sensitiveKeySuffixes = []string{"token", "secret", "password", "apikey", "credential"}

// isSensitiveKeyName 判断配置键名是否为敏感键（大小写、下划线不敏感）。
func isSensitiveKeyName(key string) bool {
	n := normalizeKey(key)
	for _, suffix := range sensitiveKeySuffixes {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

// maskSecret 对密钥脱敏：保留首尾两位，中间以 * 代替；长度 ≤ 4 时全部隐藏。
func maskSecret(s string) string {
	if len(s) == 0 {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

// dropMaskedSecrets 从 updates 中删除"等于当前值脱敏结果"的敏感键。
//
// 原始 JSON 编辑器会把 GET /config 返回的占位符（如 "re******en"）原样回传，
// 与当前值精确比对可以零误伤地识别占位符（含 5~6 位短密钥只有一两个星号的情况）；
// 含连续三个星号的值按约定直接视为占位符，兜底处理当前值已变化或节点不存在的场景。
func dropMaskedSecrets(current, updates map[string]any) {
	for k, v := range updates {
		if child, ok := v.(map[string]any); ok {
			var currentChild map[string]any
			if actual, found := lookupKey(current, k); found {
				currentChild, _ = current[actual].(map[string]any)
			}
			if currentChild == nil {
				currentChild = map[string]any{}
			}
			dropMaskedSecrets(currentChild, child)
			continue
		}
		s, ok := v.(string)
		if !ok || !isSensitiveKeyName(k) {
			continue
		}
		if strings.Contains(s, "***") {
			delete(updates, k)
			continue
		}
		actual, found := lookupKey(current, k)
		if !found {
			continue
		}
		if currentVal, ok := current[actual].(string); ok && currentVal != "" && s == maskSecret(currentVal) {
			delete(updates, k)
		}
	}
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

// maskTree 递归脱敏：键名敏感的非空字符串值一律替换为占位符。
//
// 递归遍历而不是固定路径，避免平台/插件新增字段（如 bot.telegram.token、
// api.api_key、plugins.*.embedding_api_key）漏脱敏导致明文泄露。
func maskTree(tree map[string]any) {
	for k, v := range tree {
		switch val := v.(type) {
		case map[string]any:
			maskTree(val)
		case string:
			if val != "" && isSensitiveKeyName(k) {
				tree[k] = maskSecret(val)
			}
		}
	}
}

// normalizeKey 归一化配置键名，用于宽松匹配。
//
// config 包的结构体只带 yaml/mapstructure 标签、没有 json 标签，
// 因此 json.Marshal 输出的是 Go 字段名（Bot / QQ / AccessToken），
// 与脱敏路径里的 "bot.qq.access_token" 无法直接相等匹配——
// 这正是此前脱敏静默失效、密钥被明文返回的原因。
// 这里统一转小写并去掉下划线后再比较，保持响应结构不变的前提下让匹配生效。
func normalizeKey(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "_", "")
}

// lookupKey 在 map 中按归一化规则查找实际存在的键名。
func lookupKey(m map[string]any, key string) (string, bool) {
	if _, ok := m[key]; ok {
		return key, true
	}
	target := normalizeKey(key)
	for k := range m {
		if normalizeKey(k) == target {
			return k, true
		}
	}
	return "", false
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
