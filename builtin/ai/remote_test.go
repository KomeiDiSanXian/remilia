package ai

import (
	"net/http/httptest"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execnode"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/workspace"
)

// newRemoteClientForTest 启动一个内存执行节点并返回其客户端。
func newRemoteClientForTest(t *testing.T) *execnode.Client {
	t.Helper()
	ws, err := workspace.New([]string{t.TempDir()})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	server, err := execnode.NewServer(execnode.ServerConfig{Workspace: ws, Token: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	client, err := execnode.NewClient(execnode.ClientConfig{BaseURL: httpServer.URL, Token: "test"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// TestToolCapabilitiesUseRemoteNode remote 模式下文件与进程端口都指向远程
// 节点；写入端口同样受 workspace_allow_write 控制。
func TestToolCapabilitiesUseRemoteNode(t *testing.T) {
	p := &Plugin{cfg: &config.Config{}, remote: newRemoteClientForTest(t)}
	caps := p.toolCapabilities()
	if caps.Files == nil {
		t.Fatal("remote mode should provide the files port")
	}
	if caps.Process == nil {
		t.Fatal("remote mode should provide the process port")
	}
	if caps.FileWrite != nil {
		t.Fatal("write port must stay nil unless workspace_allow_write is set")
	}

	p.cfg.WorkspaceAllowWrite = true
	if p.toolCapabilities().FileWrite == nil {
		t.Fatal("write port should be present when writes are enabled")
	}
	// 本地工作区未装配，remote 端口不依赖本地 roots。
	if p.files != nil {
		t.Fatal("local workspace should not be required in remote mode")
	}
}
