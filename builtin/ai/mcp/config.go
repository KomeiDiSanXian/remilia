package mcp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// config.go — 本子系统自持的配置。
//
// 外部工具服务器的配置（传输、命令、环境、地址、超时、重连、工具白名单等）
// 全部留在本包，不进入 AI 插件的 config：AI 只负责把本子系统装配为工具来源。
// 配置从插件配置节读取（见 [Load]）。

// Duration 配置里的时长。
//
// 同时接受两种写法：字符串（如 "30s"，推荐）与数字（按**秒**解释，便于书写）。
type Duration time.Duration

// UnmarshalJSON 解析时长。
func (d *Duration) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		v, err := time.ParseDuration(str)
		if err != nil {
			return fmt.Errorf("mcp: invalid duration %q: %w", str, err)
		}
		*d = Duration(v)
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*d = Duration(time.Duration(n * float64(time.Second)))
	return nil
}

// MarshalJSON 以 Go 时长字符串序列化。
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// Std 返回标准库时长。
func (d Duration) Std() time.Duration { return time.Duration(d) }

// ServerConfig 一个外部工具服务器的配置。
type ServerConfig struct {
	// Headers 附加请求头（值支持 ${ENV} 展开，用于凭据）。
	Headers map[string]string `json:"headers"`
	// —— 安全策略（作为登记进 AI 动作策略的输入）——
	// RequireApproval 该服务器工具是否需审批（默认 true）。显式 false 才关闭。
	RequireApproval *bool `json:"require_approval"`
	// Name 服务器名（用于来源标识与模型函数名限定）。
	Name string `json:"name"`
	// Transport 传输类型："stdio" 或 "http"。
	Transport string `json:"transport"`
	// —— stdio ——
	// Command 启动命令（必须在 allowed_commands 白名单内）。
	Command string `json:"command"`
	// Cwd 子进程工作目录（可选）。
	Cwd string `json:"cwd"`
	// —— http ——
	// URL 服务地址（http 传输；默认要求 https，回环地址可显式放开）。
	URL string `json:"url"`
	// CACertFile 自定义 CA 证书文件（可选；不提供时使用系统根证书，始终校验）。
	CACertFile string `json:"ca_cert_file"`
	// ToolPrefix 模型函数名前缀；为空时使用 "mcp_<server>_"。
	ToolPrefix string `json:"tool_prefix"`
	// Args 命令参数。
	Args []string `json:"args"`
	// Env 追加的环境变量（"KEY=VALUE"）。仅这些变量与白名单基础变量会传给子进程。
	Env []string `json:"env"`
	// EnvAllowlist 额外允许从宿主环境透传的变量名。
	EnvAllowlist []string `json:"env_allowlist"`
	// —— 工具范围 ——
	// AllowTools 非空时只暴露列出的工具。
	AllowTools []string `json:"allow_tools"`
	// DenyTools 屏蔽列出的工具（在 AllowTools 之后生效）。
	DenyTools []string `json:"deny_tools"`
	// Permissions 执行所需 RBAC 权限。
	Permissions []string `json:"permissions"`
	// —— 通用 ——
	// Timeout 单次请求超时（默认 30s）。
	Timeout Duration `json:"timeout"`
	// InitTimeout 启动/握手超时（默认 15s）。
	InitTimeout Duration `json:"init_timeout"`
	// MaxResponseBytes 单条响应大小上限（默认 4MB），超出即断开。
	MaxResponseBytes int64 `json:"max_response_bytes"`
	// ReconnectInterval 初始重连间隔（默认 5s，随后指数退避，上限 60s）。
	ReconnectInterval Duration `json:"reconnect_interval"`
	// Disabled 为 true 时不启动/不连接该服务器。
	Disabled bool `json:"disabled"`
	// AllowInsecureHTTP 允许对回环地址使用明文 http。
	AllowInsecureHTTP bool `json:"allow_insecure_http"`
	// AllowPrivateNetworks 允许连接私网地址（默认拒绝，仅 https 公网）。
	AllowPrivateNetworks bool `json:"allow_private_networks"`
	// Reconnect 连接断开后是否自动重连；nil 表示默认开启（true）。
	Reconnect *bool `json:"reconnect"`
	// AlwaysRequireApproval 任意审批模式下都强制审批。
	AlwaysRequireApproval bool `json:"always_require_approval"`
}

// Config 本子系统的配置。
type Config struct {
	// Servers 服务器列表。
	Servers []ServerConfig `json:"servers"`
	// AllowedCommands stdio 命令白名单（空的列表表示不允许启动任何命令）。
	AllowedCommands []string `json:"allowed_commands"`
	// ClientName 握手时上报的客户端名（默认 remilia）。
	ClientName string `json:"client_name"`
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{ClientName: "remilia"}
}

// Load 从插件配置视图（plugins.<name> 节的 map）解析配置。
func Load(values map[string]any) (*Config, error) {
	cfg := DefaultConfig()
	if len(values) == 0 {
		return &cfg, nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("mcp: encode config: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("mcp: decode config: %w", err)
	}
	return &cfg, nil
}

// 默认值。
const (
	defaultTimeout           = 30 * time.Second
	defaultInitTimeout       = 15 * time.Second
	defaultReconnectInterval = 5 * time.Second
	maxReconnectInterval     = 60 * time.Second
	defaultMaxResponseBytes  = int64(4 << 20)
)

// timeout 返回生效的请求超时。
func (s ServerConfig) timeout() time.Duration {
	if s.Timeout.Std() > 0 {
		return s.Timeout.Std()
	}
	return defaultTimeout
}

// initTimeout 返回生效的握手超时。
func (s ServerConfig) initTimeout() time.Duration {
	if s.InitTimeout.Std() > 0 {
		return s.InitTimeout.Std()
	}
	return defaultInitTimeout
}

// reconnectInterval 返回初始重连间隔。
func (s ServerConfig) reconnectInterval() time.Duration {
	if s.ReconnectInterval.Std() > 0 {
		return s.ReconnectInterval.Std()
	}
	return defaultReconnectInterval
}

// reconnect 返回生效的重连开关（默认开启）。
func (s ServerConfig) reconnect() bool {
	if s.Reconnect == nil {
		return true
	}
	return *s.Reconnect
}

// maxResponseBytes 返回响应大小上限。
func (s ServerConfig) maxResponseBytes() int64 {
	if s.MaxResponseBytes > 0 {
		return s.MaxResponseBytes
	}
	return defaultMaxResponseBytes
}

// requireApproval 返回是否需审批（默认 true）。
func (s ServerConfig) requireApproval() bool {
	if s.RequireApproval == nil {
		return true
	}
	return *s.RequireApproval
}

// toolAllowed 判断工具是否在服务器的 allow/deny 范围内。
func (s ServerConfig) toolAllowed(name string) bool {
	if len(s.AllowTools) > 0 {
		found := slices.Contains(s.AllowTools, name)
		if !found {
			return false
		}
	}
	return !slices.Contains(s.DenyTools, name)
}
