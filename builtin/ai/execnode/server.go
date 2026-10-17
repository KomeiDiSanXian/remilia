// server.go — 执行节点 HTTP 服务端：Bearer 认证 + 工作区/白名单强制的 JSON API。
//
// 服务端进程持有可信的工作区根目录与命令白名单（workspace / procexec 包），
// 机器人侧的配置被入侵也无法越出这里的边界；执行请求取消（机器人侧中断或
// 超时）会通过请求上下文终止节点侧进程树。
package execnode

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/procexec"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/workspace"
)

// ServerConfig 服务端配置。
type ServerConfig struct {
	// Workspace 工作区管理器（必填）。
	Workspace *workspace.Manager
	// Runner 受控进程执行器（必填）。
	Runner *procexec.Manager
	// Token 访问令牌（必填，所有端点要求 Bearer 认证）。
	Token string
	// Version 节点版本（可选，/v1/ready 返回）。
	Version string
}

// Server 执行节点服务端。
type Server struct {
	workspace *workspace.Manager
	runner    *procexec.Manager
	token     string
	version   string
}

// NewServer 构造服务端；工作区或令牌缺失时返回错误（拒绝以不安全配置启动）。
// Runner 可以为空：此时节点只提供文件能力，/v1/exec 返回 403。
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Workspace == nil {
		return nil, errors.New("execnode: workspace is required")
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, errors.New("execnode: token is required")
	}
	return &Server{
		workspace: cfg.Workspace,
		runner:    cfg.Runner,
		token:     token,
		version:   cfg.Version,
	}, nil
}

// Handler 返回带认证与路由的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathReady, s.handleReady)
	mux.HandleFunc("POST "+PathExec, s.handleExec)
	mux.HandleFunc("POST "+PathFileRead, s.handleFileRead)
	mux.HandleFunc("POST "+PathFileList, s.handleFileList)
	mux.HandleFunc("POST "+PathFileWrite, s.handleFileWrite)
	mux.HandleFunc("POST "+PathFileEdit, s.handleFileEdit)
	return s.auth(mux)
}

// auth 校验 Bearer 令牌（常量时间比较，避免时序侧信道）。
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, errors.New("invalid or missing token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleReady 返回节点自述（工作区数量、命令白名单、执行超时）。
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	resp := ReadyResponse{
		Version: s.version,
		Roots:   len(s.workspace.Roots()),
	}
	if s.runner != nil {
		resp.Allowed = s.runner.Allowed()
		resp.TimeoutMS = s.runner.Timeout().Milliseconds()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleExec 执行一次受控进程调用。
func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	if s.runner == nil {
		writeError(w, http.StatusForbidden, errors.New("process execution is not configured on this node"))
		return
	}
	var req ExecRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	result, err := s.runner.Run(r.Context(), procexec.Request{
		Command: req.Command,
		Args:    req.Args,
		Dir:     req.Dir,
	})
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ExecResponse{
		Command:    result.Command,
		Args:       result.Args,
		Dir:        result.Dir,
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		TimedOut:   result.TimedOut,
		Truncated:  result.Truncated,
		DurationMS: result.Duration.Milliseconds(),
	})
}

// handleFileRead 读取工作区内的文本文件。
func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	var req FileReadRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	content, err := s.workspace.Read(req.Path, req.MaxBytes)
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, FileReadResponse{
		Path:      content.Path,
		Text:      content.Text,
		Size:      content.Size,
		Truncated: content.Truncated,
	})
}

// handleFileList 列举工作区目录。
func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	var req FileListRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	listing, err := s.workspace.List(req.Path, req.MaxEntries)
	if err != nil {
		writeServerError(w, err)
		return
	}
	entries := make([]FileListEntry, 0, len(listing.Entries))
	for _, entry := range listing.Entries {
		entries = append(entries, FileListEntry{Name: entry.Name, IsDir: entry.IsDir, Size: entry.Size})
	}
	writeJSON(w, http.StatusOK, FileListResponse{
		Entries:   entries,
		Total:     listing.Total,
		Truncated: listing.Truncated,
	})
}

// handleFileWrite 覆盖写入工作区文件。
func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	var req FileWriteRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	result, err := s.workspace.Write(req.Path, req.Content)
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, FileWriteResponse{
		Path:          result.Path,
		Bytes:         result.Bytes,
		PreviousBytes: result.PreviousBytes,
		Created:       result.Created,
	})
}

// handleFileEdit 对工作区文件做一次唯一匹配的精确替换。
func (s *Server) handleFileEdit(w http.ResponseWriter, r *http.Request) {
	var req FileEditRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	result, err := s.workspace.Edit(req.Path, req.OldString, req.NewString)
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, FileEditResponse{
		Path:  result.Path,
		Bytes: result.Bytes,
		Diff:  result.Diff,
	})
}

// decodeRequest 读取并解码请求体（带大小上限）。
func decodeRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return false
	}
	return true
}

// writeServerError 把领域错误映射为 HTTP 状态码并返回可读信息。
func writeServerError(w http.ResponseWriter, err error) {
	writeError(w, statusForError(err), err)
}

// statusForError 领域错误到 HTTP 状态码的映射。
func statusForError(err error) int {
	switch {
	case errors.Is(err, workspace.ErrOutside),
		errors.Is(err, workspace.ErrNoRoots),
		errors.Is(err, procexec.ErrDisabled),
		errors.Is(err, procexec.ErrCommandNotAllowed):
		return http.StatusForbidden
	case errors.Is(err, workspace.ErrInvalidPath),
		errors.Is(err, procexec.ErrInvalidCommand):
		return http.StatusBadRequest
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, workspace.ErrBinary),
		errors.Is(err, workspace.ErrPatternNotFound),
		errors.Is(err, workspace.ErrPatternNotUnique):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError 写入错误响应。
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, ErrorResponse{Error: err.Error()})
}
