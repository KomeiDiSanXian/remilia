package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeConfig 是最小 ConfigReader 实现（values 为 nil 时返回默认值）。
type fakeConfig struct{ values map[string]any }

func (f *fakeConfig) Get(key string) any { return f.values[key] }

func (f *fakeConfig) GetString(key string, def string) string {
	if v, ok := f.values[key].(string); ok {
		return v
	}
	return def
}

func (f *fakeConfig) GetInt(key string, def int) int {
	if v, ok := f.values[key].(int); ok {
		return v
	}
	return def
}

func (f *fakeConfig) GetBool(key string, def bool) bool {
	if v, ok := f.values[key].(bool); ok {
		return v
	}
	return def
}

func (f *fakeConfig) GetDuration(key string, def time.Duration) time.Duration { return def }
func (f *fakeConfig) GetFloat64(key string, def float64) float64              { return def }
func (f *fakeConfig) GetStringSlice(key string, def []string) []string        { return def }
func (f *fakeConfig) GetStringMap(key string, def map[string]any) map[string]any {
	return def
}

func (f *fakeConfig) GetAll() map[string]any { return f.values }

// newTestPlugin 构造带 mock 数据目录的插件实例。
func newTestPlugin(t *testing.T) *Plugin {
	t.Helper()
	p := &Plugin{
		cfg:     &fakeConfig{},
		dataDir: t.TempDir(),
		client:  newGitHubClient("KomeiDiSanXian", "remilia", "", 5*time.Second),
		state:   newStateStore(t.TempDir()),
	}
	p.autoCheck.Store(true)
	return p
}

// archivePayload 是测试用的"新版本"二进制内容。
func archivePayload() []byte { return []byte("NEW-BINARY-V9.9.9") }

// exeSuffix 返回当前平台的可执行文件后缀。
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// makeArchiveForPlatform 构造当前平台的发布归档（tar.gz / zip）。
func makeArchiveForPlatform(t *testing.T, assetName string) []byte {
	t.Helper()
	dir := t.TempDir()
	var path string
	if strings.HasSuffix(assetName, ".zip") {
		path = makeZip(t, dir, map[string][]byte{"remilia.exe": archivePayload()})
	} else {
		path = makeTarGz(t, dir, map[string][]byte{"remilia": archivePayload()})
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// mockReleaseServer 提供完整的发布服务器：API + 资产 + checksums。
func mockReleaseServer(t *testing.T, assetName string, archive []byte) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(archive)
	sums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Write([]byte(fmt.Sprintf(`{"tag_name":"v9.9.9","assets":[
				{"name":"%s","browser_download_url":"%s/download/asset"},
				{"name":"checksums.txt","browser_download_url":"%s/download/checksums"}
			]}`, assetName, srv.URL, srv.URL)))
		case strings.HasSuffix(r.URL.Path, "/download/checksums"):
			w.Write([]byte(sums))
		case strings.HasSuffix(r.URL.Path, "/download/asset"):
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv
}

// TestApplyUpdateSuccess 全链路：检查→下载→校验→解压→替换→标记→拉起新进程。
func TestApplyUpdateSuccess(t *testing.T) {
	assetName := expectedAssetName(runtime.GOOS, runtime.GOARCH, goarm())
	archive := makeArchiveForPlatform(t, assetName)
	srv := mockReleaseServer(t, assetName, archive)
	defer srv.Close()

	oldAPI, oldHTTPS := apiBase, requireHTTPS
	apiBase = srv.URL
	requireHTTPS = false
	defer func() { apiBase, requireHTTPS = oldAPI, oldHTTPS }()

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "remilia"+exeSuffix())
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := newTestPlugin(t)

	var spawned []string
	oldSpawn := spawnNewProcess
	spawnNewProcess = func(path, marker string) error {
		spawned = []string{path, marker}
		return nil
	}
	defer func() { spawnNewProcess = oldSpawn }()

	var progress []string
	err := p.applyUpdateTo(context.Background(), exePath, false, func(m string) { progress = append(progress, m) })
	if err != nil {
		t.Fatalf("applyUpdateTo: %v", err)
	}

	// 1. 二进制已被替换
	got, _ := os.ReadFile(exePath)
	if string(got) != string(archivePayload()) {
		t.Errorf("exe 内容未替换: got %d bytes", len(got))
	}
	// 2. 备份存在
	backup := backupPathFor(exePath, CurrentVersion())
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("备份缺失: %v", err)
	}
	// 3. 标记已写入
	markerPath := filepath.Join(p.dataDir, pendingFileName)
	pending, err := readPending(markerPath)
	if err != nil || pending == nil {
		t.Fatalf("标记未写入: %v", err)
	}
	if pending.ToVersion != "9.9.9" || pending.ExePath != exePath {
		t.Errorf("标记内容错误: %+v", pending)
	}
	// 4. 新进程已按正确参数拉起
	if len(spawned) != 2 || spawned[0] != exePath || spawned[1] != markerPath {
		t.Errorf("spawn 参数错误: %v", spawned)
	}
	// 5. 状态已记录
	st := p.state.load()
	if st.Applied != "9.9.9" || st.LastVersion != "9.9.9" {
		t.Errorf("状态未更新: %+v", st)
	}
	// 6. 进度汇报完整
	if len(progress) < 4 {
		t.Errorf("进度汇报不足: %v", progress)
	}
}

// TestApplyUpdateSpawnFailure 拉起新进程失败时必须回滚二进制并删除标记。
func TestApplyUpdateSpawnFailure(t *testing.T) {
	assetName := expectedAssetName(runtime.GOOS, runtime.GOARCH, goarm())
	archive := makeArchiveForPlatform(t, assetName)
	srv := mockReleaseServer(t, assetName, archive)
	defer srv.Close()

	oldAPI, oldHTTPS := apiBase, requireHTTPS
	apiBase = srv.URL
	requireHTTPS = false
	defer func() { apiBase, requireHTTPS = oldAPI, oldHTTPS }()

	exePath := filepath.Join(t.TempDir(), "remilia"+exeSuffix())
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := newTestPlugin(t)
	oldSpawn := spawnNewProcess
	spawnNewProcess = func(path, marker string) error { return fmt.Errorf("spawn boom") }
	defer func() { spawnNewProcess = oldSpawn }()

	err := p.applyUpdateTo(context.Background(), exePath, false, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "spawn boom") {
		t.Fatalf("err = %v, want spawn error", err)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "old-binary" {
		t.Error("替换失败后应回滚原二进制")
	}
	if _, err := os.Stat(filepath.Join(p.dataDir, pendingFileName)); err == nil {
		t.Error("失败后标记应被删除")
	}
}

// TestApplyUpdateAlreadyLatest 已是最新版本时拒绝更新。
func TestApplyUpdateAlreadyLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.0.1","assets":[]}`))
	}))
	defer srv.Close()

	oldAPI := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldAPI }()

	p := newTestPlugin(t)
	err := p.applyUpdateTo(context.Background(), "dummy", false, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "已是最新版本") {
		t.Errorf("err = %v, want already-latest", err)
	}
}

// TestApplyUpdateNoChecksums 缺少 checksums.txt 时中止（安全策略）。
func TestApplyUpdateNoChecksums(t *testing.T) {
	assetName := expectedAssetName(runtime.GOOS, runtime.GOARCH, goarm())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf(`{"tag_name":"v9.9.9","assets":[{"name":"%s","browser_download_url":"https://e/x"}]}`, assetName)))
	}))
	defer srv.Close()

	oldAPI := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldAPI }()

	p := newTestPlugin(t)
	err := p.applyUpdateTo(context.Background(), "dummy", false, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "checksums.txt") {
		t.Errorf("err = %v, want missing-checksums error", err)
	}
}
