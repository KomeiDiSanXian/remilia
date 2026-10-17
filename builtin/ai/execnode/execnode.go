// Package execnode 远程执行节点：协议、服务端与客户端。
//
// 定位：把"在哪个节点执行"从能力实现中解耦。执行节点是一个独立小进程
// （cmd/executor），持有自己的可信工作区与命令白名单；机器人侧通过 HTTP +
// Bearer Token 把同样的一组能力端口（文件读取/列目录/写入/精确替换、受限
// 进程执行）转发给它。端口契约与本地实现完全一致，因此任务、计划、策略、
// 预算与产物登记逻辑不需要感知节点差异。
//
// 安全边界：
//   - 节点必须配置 Token，所有端点要求 Bearer 认证；
//   - 路径与命令白名单由节点侧强制（复用 workspace 与 procexec 包），
//     机器人侧配置被入侵也无法越出节点工作区；
//   - 执行超时与取消随请求上下文传播，节点侧终止整棵进程树；
//   - 请求体有大小上限，响应输出有字节上限。
package execnode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
)

// API 路径（服务端与客户端共用，避免两侧漂移）。
const (
	// PathReady 节点自述与认证检查。
	PathReady = "/v1/ready"
	// PathExec 进程执行。
	PathExec = "/v1/exec"
	// PathFileRead 文件读取。
	PathFileRead = "/v1/file/read"
	// PathFileList 目录列举。
	PathFileList = "/v1/file/list"
	// PathFileWrite 文件写入。
	PathFileWrite = "/v1/file/write"
	// PathFileEdit 精确替换。
	PathFileEdit = "/v1/file/edit"
)

// maxRequestBody 请求体上限：写入内容上限（1 MiB）加 JSON 与编码余量。
const maxRequestBody = 4 << 20

// ReadyResponse 节点自述。
type ReadyResponse struct {
	// Version 节点版本（可选）。
	Version string `json:"version,omitempty"`
	// Roots 工作区根目录数量。
	Roots int `json:"roots"`
	// Allowed 允许执行的命令白名单。
	Allowed []string `json:"allowed,omitempty"`
	// TimeoutMS 单次执行超时（毫秒）。
	TimeoutMS int64 `json:"timeout_ms"`
}

// ExecRequest 进程执行请求。
type ExecRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Dir     string   `json:"dir,omitempty"`
}

// ExecResponse 进程执行结果。
type ExecResponse struct {
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	Dir        string   `json:"dir,omitempty"`
	ExitCode   int      `json:"exit_code"`
	Stdout     string   `json:"stdout,omitempty"`
	Stderr     string   `json:"stderr,omitempty"`
	TimedOut   bool     `json:"timed_out,omitempty"`
	Truncated  bool     `json:"truncated,omitempty"`
	DurationMS int64    `json:"duration_ms"`
}

// FileReadRequest 文件读取请求。
type FileReadRequest struct {
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

// FileReadResponse 文件读取结果。
type FileReadResponse struct {
	Path      string `json:"path"`
	Text      string `json:"text"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated,omitempty"`
}

// FileListRequest 目录列举请求。
type FileListRequest struct {
	Path       string `json:"path,omitempty"`
	MaxEntries int    `json:"max_entries,omitempty"`
}

// FileListEntry 目录条目。
type FileListEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir,omitempty"`
	Size  int64  `json:"size,omitempty"`
}

// FileListResponse 目录列举结果。
type FileListResponse struct {
	Entries   []FileListEntry `json:"entries"`
	Total     int             `json:"total"`
	Truncated bool            `json:"truncated,omitempty"`
}

// FileWriteRequest 文件写入请求。
type FileWriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FileWriteResponse 文件写入结果。
type FileWriteResponse struct {
	Path          string `json:"path"`
	Bytes         int    `json:"bytes"`
	PreviousBytes int    `json:"previous_bytes,omitempty"`
	Created       bool   `json:"created,omitempty"`
}

// FileEditRequest 精确替换请求。
type FileEditRequest struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// FileEditResponse 精确替换结果。
type FileEditResponse struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
	Diff  string `json:"diff,omitempty"`
}

// ErrorResponse 错误响应体。
type ErrorResponse struct {
	Error string `json:"error"`
}

// --- 客户端 ---

// ClientConfig 客户端配置。
type ClientConfig struct {
	// BaseURL 节点地址（如 http://10.0.0.19:9020）。
	BaseURL string
	// Token 节点访问令牌。
	Token string
	// Timeout 单次 HTTP 请求超时（<=0 时默认 120s）。进程执行另受调用方
	/// 上下文与节点侧超时约束。
	Timeout time.Duration
}

// DefaultClientTimeout 默认单次请求超时。
const DefaultClientTimeout = 120 * time.Second

// Client 远程执行节点客户端：实现 [catalog.FileAccess]、
// [catalog.FileWriteAccess] 与 [catalog.ProcessRunner]，因此可以像本地
// 实现一样注入能力端口。
type Client struct {
	base  string
	token string
	http  *http.Client
}

// 端口实现断言。
var (
	_ catalog.FileAccess      = (*Client)(nil)
	_ catalog.FileWriteAccess = (*Client)(nil)
	_ catalog.ProcessRunner   = (*Client)(nil)
)

// NewClient 构造客户端；地址或令牌为空时返回错误。
func NewClient(cfg ClientConfig) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("execnode: base url is required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("execnode: token is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultClientTimeout
	}
	return &Client{
		base:  base,
		token: cfg.Token,
		http:  &http.Client{Timeout: timeout},
	}, nil
}

// Ready 检查节点可达性与认证，返回节点自述。
func (c *Client) Ready(ctx context.Context) (ReadyResponse, error) {
	var out ReadyResponse
	if err := c.do(ctx, http.MethodGet, PathReady, nil, &out); err != nil {
		return ReadyResponse{}, err
	}
	return out, nil
}

// Read 实现 [catalog.FileAccess]。
func (c *Client) Read(rel string, maxBytes int) (catalog.FileContent, error) {
	var out FileReadResponse
	if err := c.do(context.Background(), http.MethodPost, PathFileRead,
		FileReadRequest{Path: rel, MaxBytes: maxBytes}, &out); err != nil {
		return catalog.FileContent{}, err
	}
	return catalog.FileContent{Path: out.Path, Text: out.Text, Size: out.Size, Truncated: out.Truncated}, nil
}

// List 实现 [catalog.FileAccess]。
func (c *Client) List(rel string, maxEntries int) (catalog.FileListing, error) {
	var out FileListResponse
	if err := c.do(context.Background(), http.MethodPost, PathFileList,
		FileListRequest{Path: rel, MaxEntries: maxEntries}, &out); err != nil {
		return catalog.FileListing{}, err
	}
	entries := make([]catalog.FileEntry, 0, len(out.Entries))
	for _, e := range out.Entries {
		entries = append(entries, catalog.FileEntry{Name: e.Name, IsDir: e.IsDir, Size: e.Size})
	}
	return catalog.FileListing{Entries: entries, Total: out.Total, Truncated: out.Truncated}, nil
}

// Write 实现 [catalog.FileWriteAccess]。
func (c *Client) Write(rel, content string) (catalog.FileWriteResult, error) {
	var out FileWriteResponse
	if err := c.do(context.Background(), http.MethodPost, PathFileWrite,
		FileWriteRequest{Path: rel, Content: content}, &out); err != nil {
		return catalog.FileWriteResult{}, err
	}
	return catalog.FileWriteResult{Path: out.Path, Bytes: out.Bytes, PreviousBytes: out.PreviousBytes, Created: out.Created}, nil
}

// Edit 实现 [catalog.FileWriteAccess]。
func (c *Client) Edit(rel, oldString, newString string) (catalog.FileEditResult, error) {
	var out FileEditResponse
	if err := c.do(context.Background(), http.MethodPost, PathFileEdit,
		FileEditRequest{Path: rel, OldString: oldString, NewString: newString}, &out); err != nil {
		return catalog.FileEditResult{}, err
	}
	return catalog.FileEditResult{Path: out.Path, Bytes: out.Bytes, Diff: out.Diff}, nil
}

// Run 实现 [catalog.ProcessRunner]；上下文取消会中断请求，节点侧终止进程树。
func (c *Client) Run(ctx context.Context, req catalog.ExecRequest) (catalog.ExecResult, error) {
	var out ExecResponse
	if err := c.do(ctx, http.MethodPost, PathExec,
		ExecRequest{Command: req.Command, Args: req.Args, Dir: req.Dir}, &out); err != nil {
		return catalog.ExecResult{}, err
	}
	return catalog.ExecResult{
		Command:   out.Command,
		Args:      out.Args,
		Dir:       out.Dir,
		ExitCode:  out.ExitCode,
		Stdout:    out.Stdout,
		Stderr:    out.Stderr,
		TimedOut:  out.TimedOut,
		Truncated: out.Truncated,
		Duration:  time.Duration(out.DurationMS) * time.Millisecond,
	}, nil
}

// do 发起一次 JSON 请求并解码响应；非 2xx 返回携带节点错误信息的错误。
func (c *Client) do(ctx context.Context, method, path string, reqBody any, out any) error {
	var body io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("execnode: marshal request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return fmt.Errorf("execnode: build request: %w", err)
	}
	if reqBody != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("execnode: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr ErrorResponse
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		_ = json.Unmarshal(data, &apiErr)
		message := strings.TrimSpace(apiErr.Error)
		if message == "" {
			message = strings.TrimSpace(string(data))
		}
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("execnode: %s: %s", path, message)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRequestBody)).Decode(out); err != nil {
		return fmt.Errorf("execnode: decode response from %s: %w", path, err)
	}
	return nil
}
