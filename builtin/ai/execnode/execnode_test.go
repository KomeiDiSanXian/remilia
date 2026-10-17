package execnode

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/procexec"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/workspace"
)

// catalogExecRequest 构造进程执行请求。
func catalogExecRequest(command string, args ...string) catalog.ExecRequest {
	return catalog.ExecRequest{Command: command, Args: args}
}

// newTestNode 建立临时工作区、可选执行器、服务端与客户端。
func newTestNode(t *testing.T, allow []string) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := workspace.New([]string{dir})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	var runner *procexec.Manager
	if len(allow) > 0 {
		runner, err = procexec.New(procexec.Config{
			Workspace: ws,
			Allowlist: allow,
			Timeout:   10 * time.Second,
		})
		if err != nil {
			t.Fatalf("procexec.New: %v", err)
		}
	}
	server, err := NewServer(ServerConfig{
		Workspace: ws,
		Runner:    runner,
		Token:     "test-token",
		Version:   "test",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	client, err := NewClient(ClientConfig{BaseURL: httpServer.URL, Token: "test-token", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, dir
}

func TestNewClientAndServerValidation(t *testing.T) {
	if _, err := NewClient(ClientConfig{}); err == nil {
		t.Fatal("client without url/token must fail")
	}
	if _, err := NewClient(ClientConfig{BaseURL: "http://x"}); err == nil {
		t.Fatal("client without token must fail")
	}
	if _, err := NewServer(ServerConfig{Token: "t"}); err == nil {
		t.Fatal("server without workspace must fail")
	}
	if _, err := NewServer(ServerConfig{Workspace: mustWorkspace(t)}); err == nil {
		t.Fatal("server without token must fail")
	}
}

// mustWorkspace 构造一个临时工作区（仅用于校验用例）。
func mustWorkspace(t *testing.T) *workspace.Manager {
	t.Helper()
	ws, err := workspace.New([]string{t.TempDir()})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	return ws
}

func TestReadyAndAuth(t *testing.T) {
	client, _ := newTestNode(t, []string{"go"})

	ready, err := client.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if ready.Roots != 1 || ready.Version != "test" {
		t.Fatalf("unexpected ready: %+v", ready)
	}
	if len(ready.Allowed) != 1 || ready.Allowed[0] != "go" {
		t.Fatalf("allowed = %v", ready.Allowed)
	}

	bad, err := NewClient(ClientConfig{BaseURL: client.base, Token: "wrong"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := bad.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("wrong token should be rejected, got %v", err)
	}
}

func TestFileRoundTrip(t *testing.T) {
	client, dir := newTestNode(t, nil)

	write, err := client.Write(filepath.Join("docs", "note.md"), "hello world")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !write.Created || write.Bytes != 11 {
		t.Fatalf("unexpected write: %+v", write)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "note.md")); err != nil {
		t.Fatalf("file should exist on the node: %v", err)
	}

	read, err := client.Read(filepath.Join("docs", "note.md"), 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.Text != "hello world" || read.Size != 11 {
		t.Fatalf("unexpected read: %+v", read)
	}

	edit, err := client.Edit(filepath.Join("docs", "note.md"), "world", "node")
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if !strings.Contains(edit.Diff, "-world") || !strings.Contains(edit.Diff, "+node") {
		t.Fatalf("unexpected diff: %q", edit.Diff)
	}

	listing, err := client.List("docs", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Total != 1 || listing.Entries[0].Name != "note.md" {
		t.Fatalf("unexpected listing: %+v", listing)
	}

	// 越界路径由节点侧拒绝，机器人侧无法绕过。
	if _, err := client.Write(filepath.Join("..", "escape.txt"), "x"); err == nil {
		t.Fatal("traversal write must be rejected by the node")
	}
	if _, err := client.Read("missing.txt", 0); err == nil {
		t.Fatal("missing file must be reported as an error")
	}
}

func TestExecRoundTrip(t *testing.T) {
	client, _ := newTestNode(t, []string{"go"})

	result, err := client.Run(context.Background(), catalogExecRequest("go", "env", "GOROOT"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) == "" {
		t.Fatalf("unexpected exec result: %+v", result)
	}
	if result.Duration <= 0 {
		t.Fatal("duration should be measured on the node")
	}

	// 未列入白名单的命令在节点侧被拒绝。
	if _, err := client.Run(context.Background(), catalogExecRequest("git", "status")); err == nil {
		t.Fatal("unlisted command must be rejected")
	}
}

func TestExecDisabledOnFileOnlyNode(t *testing.T) {
	client, _ := newTestNode(t, nil)
	if _, err := client.Run(context.Background(), catalogExecRequest("go", "version")); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("file-only node should refuse execution, got %v", err)
	}
}

func TestServerRejectsBadBody(t *testing.T) {
	client, _ := newTestNode(t, nil)
	// 非法 JSON（数组而非对象）由节点返回 400 并带可读信息。
	if err := client.do(context.Background(), "POST", PathFileRead, []int{1}, nil); err == nil ||
		!strings.Contains(err.Error(), "invalid request body") {
		t.Fatalf("bad body error = %v", err)
	}
}

func TestStatusForError(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{workspace.ErrOutside, 403},
		{procexec.ErrCommandNotAllowed, 403},
		{workspace.ErrInvalidPath, 400},
		{os.ErrNotExist, 404},
		{workspace.ErrPatternNotUnique, 409},
		{errors.New("other"), 500},
	}
	for _, c := range cases {
		if got := statusForError(c.err); got != c.want {
			t.Fatalf("statusForError(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}
