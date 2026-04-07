package fs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	remfs "github.com/KomeiDiSanXian/remilia/infra/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 测试辅助 ──────────────────────────────────────────────────────────────────

// newTestServer 启动一个测试 HTTP 服务器，对任意路径返回 body 内容（状态 200）。
func newTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newFailServer 启动一个总是返回 500 的测试服务器。
func newFailServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// tempDir 创建一个临时目录，测试结束时自动清理。
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "remilia-fs-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// ─── Ensure：本地文件已存在 ───────────────────────────────────────────────────

func TestLazyResource_Ensure_LocalExists_SkipsDownload(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "data.txt")
	require.NoError(t, os.WriteFile(localPath, []byte("cached"), 0o644))

	// 即使服务器返回其他内容，也不应发起请求（因为本地文件已存在）
	srv := newTestServer(t, "new content")

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  srv.URL + "/data.txt",
		HTTPClient: srv.Client(),
	}

	err := r.Ensure(context.Background())
	require.NoError(t, err)

	// 文件内容应保持原来的值
	data, _ := os.ReadFile(localPath)
	assert.Equal(t, "cached", string(data))
}

// ─── Ensure：下载并写入本地 ───────────────────────────────────────────────────

func TestLazyResource_Ensure_DownloadsWhenMissing(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "data.txt")

	const content = "hello from server"
	srv := newTestServer(t, content)

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  srv.URL + "/data.txt",
		HTTPClient: srv.Client(),
	}

	require.NoError(t, r.Ensure(context.Background()))
	assert.True(t, r.LocalExists())

	data, _ := os.ReadFile(localPath)
	assert.Equal(t, content, string(data))
}

// ─── Read ─────────────────────────────────────────────────────────────────────

func TestLazyResource_Read_DownloadsAndReturnsContent(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "res.txt")

	const content = "resource content"
	srv := newTestServer(t, content)

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  srv.URL + "/res.txt",
		HTTPClient: srv.Client(),
	}

	data, err := r.Read(context.Background())
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestLazyResource_Read_AlreadyExists(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "res.txt")
	require.NoError(t, os.WriteFile(localPath, []byte("existing"), 0o644))

	r := &remfs.LazyResource{
		LocalPath: localPath,
		// 无 RemoteURL — 若触发下载会失败，用于验证不会尝试下载
	}

	data, err := r.Read(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "existing", string(data))
}

// ─── Ensure：幂等（多次调用）─────────────────────────────────────────────────

func TestLazyResource_Ensure_Idempotent(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "data.txt")

	downloadCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloadCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer srv.Close()

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  srv.URL,
		HTTPClient: srv.Client(),
	}

	for range 5 {
		require.NoError(t, r.Ensure(context.Background()))
	}

	assert.Equal(t, 1, downloadCount, "should only download once")
}

// ─── 镜像 URL 降级 ────────────────────────────────────────────────────────────

func TestLazyResource_MirrorFallback(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "data.txt")

	failSrv := newFailServer(t)
	okSrv := newTestServer(t, "from mirror")

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  failSrv.URL + "/data.txt",
		MirrorURLs: []string{okSrv.URL + "/data.txt"},
		HTTPClient: okSrv.Client(),
	}

	require.NoError(t, r.Ensure(context.Background()))

	data, _ := os.ReadFile(localPath)
	assert.Equal(t, "from mirror", string(data))
}

// ─── 所有 URL 失败 ────────────────────────────────────────────────────────────

func TestLazyResource_AllSourcesFail(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "data.txt")

	failSrv := newFailServer(t)

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  failSrv.URL + "/data.txt",
		MirrorURLs: []string{failSrv.URL + "/mirror.txt"},
		HTTPClient: failSrv.Client(),
	}

	err := r.Ensure(context.Background())
	assert.ErrorIs(t, err, remfs.ErrDownloadFailed)
}

// ─── ErrNoSource ─────────────────────────────────────────────────────────────

func TestLazyResource_ErrNoSource_BothEmpty(t *testing.T) {
	r := &remfs.LazyResource{}
	err := r.Ensure(context.Background())
	assert.ErrorIs(t, err, remfs.ErrNoSource)
}

func TestLazyResource_ErrNoSource_NoRemoteURL_LocalMissing(t *testing.T) {
	r := &remfs.LazyResource{
		LocalPath: "/nonexistent/path/to/file.txt",
	}
	err := r.Ensure(context.Background())
	assert.ErrorIs(t, err, remfs.ErrNoSource)
}

// ─── Reload + ForceDownload ───────────────────────────────────────────────────

func TestLazyResource_Reload_AllowsReDownload(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "data.txt")

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("updated"))
	}))
	defer srv.Close()

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  srv.URL,
		HTTPClient: srv.Client(),
	}

	// 首次下载
	require.NoError(t, r.Ensure(context.Background()))
	assert.Equal(t, 1, callCount)

	// Reload 后再次下载
	r.Reload()
	require.NoError(t, r.Ensure(context.Background()))
	assert.Equal(t, 2, callCount)
}

func TestLazyResource_ForceDownload_OverridesLocalCache(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "data.txt")
	require.NoError(t, os.WriteFile(localPath, []byte("old"), 0o644))

	srv := newTestServer(t, "fresh")

	r := &remfs.LazyResource{
		LocalPath:     localPath,
		RemoteURL:     srv.URL,
		HTTPClient:    srv.Client(),
		ForceDownload: true,
	}

	require.NoError(t, r.Ensure(context.Background()))
	data, _ := os.ReadFile(localPath)
	assert.Equal(t, "fresh", string(data))
	// ForceDownload 应在 Ensure 成功后自动清除
	assert.False(t, r.ForceDownload)
}

// ─── LocalExists ─────────────────────────────────────────────────────────────

func TestLazyResource_LocalExists(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "file.txt")

	r := &remfs.LazyResource{LocalPath: localPath}
	assert.False(t, r.LocalExists())

	require.NoError(t, os.WriteFile(localPath, []byte("x"), 0o644))
	assert.True(t, r.LocalExists())
}

func TestLazyResource_LocalExists_EmptyPath(t *testing.T) {
	r := &remfs.LazyResource{}
	assert.False(t, r.LocalExists())
}

// ─── 上下文取消 ───────────────────────────────────────────────────────────────

func TestLazyResource_ContextCancellation(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "slow.txt")

	// 构造一个慢服务器（下载时阻塞）
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
			_, _ = w.Write([]byte("done"))
		}
	}))
	defer slow.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  slow.URL,
		HTTPClient: slow.Client(),
		Timeout:    5 * time.Second,
	}

	err := r.Ensure(ctx)
	assert.Error(t, err, "should fail due to context cancellation")
}

// ─── 并发安全：多 goroutine 同时调用 Ensure ───────────────────────────────────

func TestLazyResource_ConcurrentEnsure(t *testing.T) {
	dir := tempDir(t)
	localPath := filepath.Join(dir, "data.txt")

	downloadCount := 0
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		downloadCount++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond) // 模拟网络延迟
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer srv.Close()

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  srv.URL,
		HTTPClient: srv.Client(),
	}

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)
	errs := make([]error, concurrency)

	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = r.Ensure(context.Background())
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		assert.NoError(t, err)
	}
	// 并发调用只应下载一次
	assert.Equal(t, 1, downloadCount, "concurrent Ensure should download only once")
}

// ─── 目录自动创建 ─────────────────────────────────────────────────────────────

func TestLazyResource_AutoCreateDir(t *testing.T) {
	dir := tempDir(t)
	// 深层目录（下载时应自动创建）
	localPath := filepath.Join(dir, "a", "b", "c", "data.txt")

	srv := newTestServer(t, "content")

	r := &remfs.LazyResource{
		LocalPath:  localPath,
		RemoteURL:  srv.URL,
		HTTPClient: srv.Client(),
	}

	require.NoError(t, r.Ensure(context.Background()))
	assert.True(t, r.LocalExists())
}

// ─── LastError ────────────────────────────────────────────────────────────────

func TestLazyResource_LastError(t *testing.T) {
	r := &remfs.LazyResource{} // 两个字段均为空

	assert.NoError(t, r.LastError()) // 未调用前为 nil

	_ = r.Ensure(context.Background())
	assert.ErrorIs(t, r.LastError(), remfs.ErrNoSource)
}
