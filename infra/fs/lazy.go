// Package fs 提供框架级文件系统工具。
//
// 目前包含：
//   - [LazyResource]：懒加载文件资源，首次访问时从远程 URL 下载到本地，后续直接读本地缓存。
//
// # 设计目标
//
// 对标 FloatTech/floatfile 的懒加载能力，解决 Bot 插件常见的资源管理需求：
// 词库、字体、图片素材等文件首次运行时按需下载，无需预先打包进二进制。
//
// # 快速开始
//
//	r := &fs.LazyResource{
//	    LocalPath:  "data/wordlist.txt",
//	    RemoteURL:  "https://cdn.example.com/wordlist.txt",
//	    MirrorURLs: []string{"https://mirror.example.com/wordlist.txt"},
//	}
//
//	// 首次调用时下载，后续直接读本地文件
//	data, err := r.Read(ctx)
//
//	// 仅确保文件存在，不读取内容（适合字体等二进制文件）
//	if err := r.Ensure(ctx); err != nil { ... }
//
//	// 强制重新下载（更新资源版本）
//	r.Reload()
//	data, err = r.Read(ctx)
package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrNoSource 当 LocalPath 与 RemoteURL 均为空时返回此错误。
var ErrNoSource = errors.New("fs.LazyResource: LocalPath and RemoteURL are both empty")

// ErrDownloadFailed 所有下载源均失败时作为 wrapped 错误使用。
var ErrDownloadFailed = errors.New("fs.LazyResource: all download sources failed")

// defaultTimeout 单次 HTTP 下载的默认超时时间。
const defaultTimeout = 60 * time.Second

// LazyResource 按需下载并缓存本地文件资源。
//
// 调用 [LazyResource.Ensure] 或 [LazyResource.Read] 时的行为：
//  1. 若 LocalPath 文件已存在（且 ForceDownload=false），直接使用——不发起任何网络请求。
//  2. 若不存在，从 RemoteURL 下载到 LocalPath。
//     若主 URL 失败，按顺序尝试 MirrorURLs 中的镜像地址。
//  3. 下载成功后，后续调用同一 *LazyResource 实例将直接返回（不重复下载）。
//
// 并发安全：多个 goroutine 同时调用 Ensure / Read 时，实际下载至多发生一次；
// 其余 goroutine 在下载完成前阻塞，完成后直接返回结果。
//
// 使用方式（零值需设置 LocalPath 和/或 RemoteURL）：
//
//	r := &fs.LazyResource{
//	    LocalPath: "data/font.ttf",
//	    RemoteURL: "https://example.com/font.ttf",
//	}
type LazyResource struct {
	// LocalPath 本地缓存文件的路径（含文件名，如 "data/wordlist.txt"）。
	//
	// 若文件已存在，Ensure 跳过下载直接返回。
	// 若为空字符串且 RemoteURL 非空，下载完成后会将实际路径写回此字段。
	LocalPath string

	// RemoteURL 主下载 URL（LocalPath 不存在时使用）。
	// 若为空且 LocalPath 也不存在，Ensure 返回 [ErrNoSource]。
	RemoteURL string

	// MirrorURLs 备用镜像 URL 列表（主 URL 下载失败时按顺序尝试）。
	// 可为 nil，表示无镜像。
	MirrorURLs []string

	// Timeout 单次 HTTP 下载的超时时间（默认 60s，<=0 则使用默认值）。
	Timeout time.Duration

	// ForceDownload 设为 true 时，即使本地文件已存在也重新下载。
	// 适合在调用 [LazyResource.Reload] 后临时设置，更新资源版本。
	ForceDownload bool

	// HTTPClient 可选自定义 HTTP 客户端（nil 则使用 http.DefaultClient）。
	// 用于测试（httptest）或需要代理/TLS 配置的场景。
	HTTPClient *http.Client

	mu   sync.Mutex
	done bool  // true 表示 ensure 已成功完成
	err  error // ensure 最后一次执行的错误（done=true 时为 nil）
}

// Ensure 确保本地文件存在于 LocalPath。
//
// 若文件已存在（且 ForceDownload=false），立即返回 nil，不发起网络请求。
// 否则从 RemoteURL（及 MirrorURLs）下载，下载成功后写入 LocalPath。
//
// 首次成功下载后，后续调用无论并发与否均直接返回 nil（幂等）。
// 调用 [LazyResource.Reload] 可重置状态，强制下次重新下载。
func (r *LazyResource) Ensure(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.done && !r.ForceDownload {
		return nil
	}

	if err := r.doEnsure(ctx); err != nil {
		r.err = err
		return err
	}
	r.done = true
	r.err = nil
	r.ForceDownload = false // 清除一次性标志
	return nil
}

// Read 等价于 [LazyResource.Ensure] 后读取文件内容。
//
// 首次调用时会下载文件（若不存在），后续调用直接读本地文件。
// 每次调用都会读取文件内容（不缓存字节，适合需要观察文件更新的场景）。
func (r *LazyResource) Read(ctx context.Context) ([]byte, error) {
	if err := r.Ensure(ctx); err != nil {
		return nil, err
	}
	return os.ReadFile(r.LocalPath)
}

// Reload 重置下载状态，使下次调用 [LazyResource.Ensure] 或 [LazyResource.Read] 时
// 重新检查并按需下载。
//
// 适用场景：需要强制更新远端资源版本时，先调用 Reload，再调用 Read。
// 此方法是并发安全的。
func (r *LazyResource) Reload() {
	r.mu.Lock()
	r.done = false
	r.err = nil
	r.ForceDownload = true // 确保即使本地文件已存在也重新下载
	r.mu.Unlock()
}

// LocalExists 报告本地文件是否已存在（不触发下载）。
// 可用于在调用 Ensure 前先行检查，或在 UI 中显示"已缓存"状态。
func (r *LazyResource) LocalExists() bool {
	if r.LocalPath == "" {
		return false
	}
	_, err := os.Stat(r.LocalPath)
	return err == nil
}

// LastError 返回最近一次 Ensure 调用的错误（nil 表示成功或尚未调用）。
func (r *LazyResource) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// ─── 内部实现 ──────────────────────────────────────────────────────────────────

// doEnsure 实际的确保逻辑，在持有 r.mu 的情况下调用。
func (r *LazyResource) doEnsure(ctx context.Context) error {
	if r.LocalPath == "" && r.RemoteURL == "" {
		return ErrNoSource
	}

	// 本地文件已存在且不强制下载：直接返回
	if r.LocalPath != "" && !r.ForceDownload {
		if _, err := os.Stat(r.LocalPath); err == nil {
			return nil
		}
	}

	// 没有 RemoteURL：无法下载
	if r.RemoteURL == "" {
		return fmt.Errorf("%w: local file %q not found and no RemoteURL configured",
			ErrNoSource, r.LocalPath)
	}

	// 构建 URL 尝试列表（主 URL 在前，镜像在后）
	urls := make([]string, 0, 1+len(r.MirrorURLs))
	urls = append(urls, r.RemoteURL)
	urls = append(urls, r.MirrorURLs...)

	var lastErr error
	for _, u := range urls {
		localPath, err := r.download(ctx, u)
		if err != nil {
			lastErr = fmt.Errorf("source %q: %w", u, err)
			continue
		}
		// 若 LocalPath 之前为空（临时文件），写回
		if r.LocalPath == "" {
			r.LocalPath = localPath
		}
		return nil
	}

	return fmt.Errorf("%w: %w", ErrDownloadFailed, lastErr)
}

// download 从 rawURL 下载到 r.LocalPath（原子写入：先写 .tmp，再 rename）。
// 返回实际写入的本地路径。
func (r *LazyResource) download(ctx context.Context, rawURL string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	// 确保目标目录存在（如果 LocalPath 有指定的话）
	localPath := r.LocalPath
	if localPath != "" {
		dir := filepath.Dir(localPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create directory %q: %w", dir, err)
		}
	}

	dlCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("build HTTP request: %w", err)
	}
	req.Header.Set("User-Agent", "remilia-bot/1.0")

	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	// 写入临时文件，成功后原子 rename 到目标路径
	var tmpPath string
	if localPath != "" {
		tmpPath = localPath + ".tmp"
	}

	var tmpFile *os.File
	if tmpPath != "" {
		tmpFile, err = os.Create(tmpPath)
		if err != nil {
			return "", fmt.Errorf("create temp file: %w", err)
		}
	} else {
		// LocalPath 未指定：使用系统临时目录
		tmpFile, err = os.CreateTemp("", "remilia-lazy-*")
		if err != nil {
			return "", fmt.Errorf("create temp file: %w", err)
		}
		tmpPath = tmpFile.Name()
		localPath = tmpPath // 临时文件即最终路径，无需 rename
	}

	if _, err = io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("write to temp file: %w", err)
	}
	if err = tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	// 若 localPath 与 tmpPath 不同，执行原子 rename
	if r.LocalPath != "" && tmpPath != localPath {
		if err = os.Rename(tmpPath, localPath); err != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("rename %q -> %q: %w", tmpPath, localPath, err)
		}
	}

	return localPath, nil
}
