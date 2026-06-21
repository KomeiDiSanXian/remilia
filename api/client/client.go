// Package client 提供 Remilia 管理 API 的 Go 客户端 SDK。
//
// 使用方式：
//
//	cli := client.New("http://localhost:9002", "my-api-key")
//
//	// 查询 Bot 状态
//	bots, err := cli.ListBots(ctx)
//
//	// 启用插件
//	err := cli.EnablePlugin(ctx, "my-plugin")
//
//	// 重启 Bot
//	err := cli.RestartBot(ctx, "remilia")
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// --- 核心客户端 ---

// Client 是管理 API 的 HTTP 客户端。
// 所有方法均接受 context 参数以支持超时和取消。
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// New 创建管理 API 客户端。
//
// 参数：
//   - baseURL：API 服务器地址，如 "http://localhost:9002"
//   - apiKey：API 访问密钥（Bearer Token），为空时跳过认证
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// --- 内部方法 ---

// apiResponse 匹配服务器端的统一响应结构，用于内部解析。
type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// do 发送 HTTP 请求并解析统一响应。
// result 为可选参数，非 nil 时将 data 字段反序列化到 result。
func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("client: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("client: create request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("client: do request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("client: read response: %w", err)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return fmt.Errorf("client: parse response: %w (body: %s)", err, string(raw))
	}

	if apiResp.Code != 0 {
		return fmt.Errorf("client: %s %s: %s (code=%d)", method, path, apiResp.Message, apiResp.Code)
	}

	if result != nil && len(apiResp.Data) > 0 {
		if err := json.Unmarshal(apiResp.Data, result); err != nil {
			return fmt.Errorf("client: unmarshal data: %w", err)
		}
	}
	return nil
}

// --- 响应类型 ---

// BotInfo 是 Bot 实例的公开信息，响应结构同服务端 api.BotInfo。
type BotInfo struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Uptime      string   `json:"uptime"`
	Version     string   `json:"version"`
	Platforms   []string `json:"platforms,omitempty"`
	PluginCount int      `json:"plugin_count"`
}

// PluginInfo 是插件的公开摘要信息。
type PluginInfo struct {
	Name         string   `json:"name"`
	State        string   `json:"state"`
	Version      string   `json:"version"`
	Uptime       string   `json:"uptime"`
	Dependencies []string `json:"dependencies,omitempty"`
	MatcherCount int      `json:"matcher_count"`
	LastError    string   `json:"last_error,omitempty"`
	LoadTime     string   `json:"load_time,omitempty"`
}

// VersionInfo 是版本信息响应。
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
	GoVersion string `json:"go_version"`
}

// --- Bot API ---

// ListBots 获取所有 Bot 实例的摘要信息列表。
func (c *Client) ListBots(ctx context.Context) ([]BotInfo, error) {
	var bots []BotInfo
	if err := c.do(ctx, http.MethodGet, "/api/v1/bots", nil, &bots); err != nil {
		return nil, err
	}
	return bots, nil
}

// GetBot 获取指定名称的 Bot 详情。
func (c *Client) GetBot(ctx context.Context, name string) (*BotInfo, error) {
	var bot BotInfo
	if err := c.do(ctx, http.MethodGet, "/api/v1/bots/"+name, nil, &bot); err != nil {
		return nil, err
	}
	return &bot, nil
}

// StartBot 启动指定 Bot。
func (c *Client) StartBot(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/bots/"+name+"/start", nil, nil)
}

// StopBot 停止指定 Bot。
func (c *Client) StopBot(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/bots/"+name+"/stop", nil, nil)
}

// RestartBot 重启指定 Bot（先停止再启动）。
func (c *Client) RestartBot(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/bots/"+name+"/restart", nil, nil)
}

// --- Plugin API ---

// ListPlugins 获取所有已注册插件的摘要信息列表。
func (c *Client) ListPlugins(ctx context.Context) ([]PluginInfo, error) {
	var plugins []PluginInfo
	if err := c.do(ctx, http.MethodGet, "/api/v1/plugins", nil, &plugins); err != nil {
		return nil, err
	}
	return plugins, nil
}

// GetPlugin 获取指定插件的详细状态信息。
// 返回 map 结构，具体字段参见服务端 plugin.Status。
func (c *Client) GetPlugin(ctx context.Context, name string) (map[string]any, error) {
	var status map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/plugins/"+name, nil, &status); err != nil {
		return nil, err
	}
	return status, nil
}

// EnablePlugin 启用指定插件。
func (c *Client) EnablePlugin(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/plugins/"+name+"/enable", nil, nil)
}

// DisablePlugin 禁用指定插件。
func (c *Client) DisablePlugin(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/plugins/"+name+"/disable", nil, nil)
}

// ReloadPlugin 热重载指定插件。
func (c *Client) ReloadPlugin(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/plugins/"+name+"/reload", nil, nil)
}

// --- Config API ---

// GetConfig 获取当前配置快照。
func (c *Client) GetConfig(ctx context.Context) (map[string]any, error) {
	var cfg map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/config", nil, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ReloadConfig 从磁盘重新加载配置文件并触发热更新。
func (c *Client) ReloadConfig(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/v1/config/reload", nil, nil)
}

// PutConfig 更新配置（JSON 格式的部分更新，深合并后写入并触发热重载）。
func (c *Client) PutConfig(ctx context.Context, updates map[string]any) error {
	return c.do(ctx, http.MethodPut, "/api/v1/config", updates, nil)
}

// --- System API ---

// GetHealth 获取 Bot 健康检查结果。
func (c *Client) GetHealth(ctx context.Context) (map[string]any, error) {
	var health map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/health", nil, &health); err != nil {
		return nil, err
	}
	return health, nil
}

// GetVersion 获取框架版本信息。
func (c *Client) GetVersion(ctx context.Context) (*VersionInfo, error) {
	var ver VersionInfo
	if err := c.do(ctx, http.MethodGet, "/api/v1/version", nil, &ver); err != nil {
		return nil, err
	}
	return &ver, nil
}

// GetStats 获取插件管理器运行时统计。
func (c *Client) GetStats(ctx context.Context) (map[string]any, error) {
	var stats map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/stats", nil, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// --- Platform API ---

// ListPlatforms 列出所有已注册的平台适配器。
func (c *Client) ListPlatforms(ctx context.Context) ([]map[string]any, error) {
	var platforms []map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/platforms", nil, &platforms); err != nil {
		return nil, err
	}
	return platforms, nil
}

// GetPlatform 获取指定平台详情。
func (c *Client) GetPlatform(ctx context.Context, name string) (map[string]any, error) {
	var platform map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/platforms/"+name, nil, &platform); err != nil {
		return nil, err
	}
	return platform, nil
}

// --- Engine API ---

// ListCommands 列出所有已注册的命令。
func (c *Client) ListCommands(ctx context.Context) ([]map[string]any, error) {
	var cmds []map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/engine/commands", nil, &cmds); err != nil {
		return nil, err
	}
	return cmds, nil
}

// GetMatcherStats 获取匹配器统计信息。
func (c *Client) GetMatcherStats(ctx context.Context) (map[string]any, error) {
	var stats map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/engine/matchers", nil, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// DisableMatcherGroup 禁用指定匹配器组。
func (c *Client) DisableMatcherGroup(ctx context.Context, group string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/engine/matchers/group/"+group+"/disable", nil, nil)
}

// EnableMatcherGroup 启用指定匹配器组。
func (c *Client) EnableMatcherGroup(ctx context.Context, group string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/engine/matchers/group/"+group+"/enable", nil, nil)
}

// --- Audit Log API ---

// GetAuditLog 获取最近的审计日志。
func (c *Client) GetAuditLog(ctx context.Context, n int) (map[string]any, error) {
	path := "/api/v1/auditlog"
	if n > 0 {
		path += "?n=" + fmt.Sprintf("%d", n)
	}
	var result map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetAuditLogByUser 获取指定用户的审计日志。
func (c *Client) GetAuditLogByUser(ctx context.Context, userID string, n int) (map[string]any, error) {
	path := "/api/v1/auditlog/user/" + userID
	if n > 0 {
		path += "?n=" + fmt.Sprintf("%d", n)
	}
	var result map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetAuditLogCount 获取审计日志总数。
func (c *Client) GetAuditLogCount(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/auditlog/count", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// --- Permission API ---

// AssignRole 为用户分配角色。
func (c *Client) AssignRole(ctx context.Context, userID, role string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/permission/users/"+userID+"/roles", map[string]string{"role": role}, nil)
}

// RevokeRole 撤销用户的角色。
func (c *Client) RevokeRole(ctx context.Context, userID, role string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/permission/users/"+userID+"/roles/"+role, nil, nil)
}

// GetUserPermissions 获取用户的角色和权限。
func (c *Client) GetUserPermissions(ctx context.Context, userID string) (map[string]any, error) {
	var result map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/permission/users/"+userID+"/permissions", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// CheckPermission 检查用户是否有指定权限。
func (c *Client) CheckPermission(ctx context.Context, userID, resource, action string) (map[string]any, error) {
	var result map[string]any
	if err := c.do(ctx, http.MethodPost, "/api/v1/permission/check", map[string]string{
		"user_id": userID, "resource": resource, "action": action,
	}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// --- FSM API ---

// ListFSMs 列出所有已注册的 FSM。
func (c *Client) ListFSMs(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/fsm", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// EndFSMSession 终止一个 FSM 会话。
func (c *Client) EndFSMSession(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/fsm/sessions/"+sessionID, nil, nil)
}

// --- Scheduler API ---

// GetSchedulerJobs 获取计划任务数量。
func (c *Client) GetSchedulerJobs(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/scheduler/jobs", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetSchedulerHistory 获取计划任务执行历史。
func (c *Client) GetSchedulerHistory(ctx context.Context, n int) (map[string]any, error) {
	path := "/api/v1/scheduler/history"
	if n > 0 {
		path += "?n=" + fmt.Sprintf("%d", n)
	}
	var result map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}
