//go:build network

package execnode

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
)

// TestLiveRemoteNode 针对真实执行节点的联调用例（需要 network 构建标签）：
//
//	REMILIA_EXEC_NODE_URL=http://10.0.0.19:9020 \
//	REMILIA_EXEC_NODE_TOKEN=<token> \
//	go test -tags network ./builtin/ai/execnode -run TestLiveRemoteNode -v
//
// 用例覆盖：认证与自述、文件写入/读取/替换/列举、白名单进程执行。
// 探针文件使用随机名，避免污染节点工作区。
func TestLiveRemoteNode(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("REMILIA_EXEC_NODE_URL"))
	token := strings.TrimSpace(os.Getenv("REMILIA_EXEC_NODE_TOKEN"))
	if url == "" || token == "" {
		t.Skip("REMILIA_EXEC_NODE_URL / REMILIA_EXEC_NODE_TOKEN not set")
	}

	client, err := NewClient(ClientConfig{BaseURL: url, Token: token, Timeout: 2 * time.Minute})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ready, err := client.Ready(ctx)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if ready.Roots < 1 {
		t.Fatalf("node reports no workspace roots: %+v", ready)
	}
	t.Logf("node ready: version=%q roots=%d allowed=%v timeout=%dms",
		ready.Version, ready.Roots, ready.Allowed, ready.TimeoutMS)

	probe := fmt.Sprintf("probe-%d.txt", time.Now().UnixNano())
	write, err := client.Write(probe, "hello remote node")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !write.Created || write.Bytes != 17 {
		t.Fatalf("unexpected write result: %+v", write)
	}

	read, err := client.Read(probe, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.Text != "hello remote node" {
		t.Fatalf("read back %q", read.Text)
	}

	edit, err := client.Edit(probe, "remote node", "remote executor")
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if !strings.Contains(edit.Diff, "-remote node") || !strings.Contains(edit.Diff, "+remote executor") {
		t.Fatalf("unexpected diff: %q", edit.Diff)
	}

	listing, err := client.List("", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, entry := range listing.Entries {
		if entry.Name == probe {
			found = true
		}
	}
	if !found {
		t.Fatalf("probe file %q not listed (%d entries)", probe, listing.Total)
	}

	// 越界路径必须由节点拒绝。
	if _, err := client.Read("../etc/passwd", 0); err == nil {
		t.Fatal("traversal read must be rejected by the node")
	}

	if len(ready.Allowed) == 0 {
		t.Log("node is file-only; skipping exec check")
		return
	}
	result, err := client.Run(ctx, catalog.ExecRequest{Command: allowedOr(ready.Allowed, "uname"), Args: []string{"-a"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 || !strings.Contains(result.Stdout, "Linux") {
		t.Fatalf("unexpected exec result: %+v", result)
	}
	t.Logf("exec ok: %q exit=%d in %s", strings.TrimSpace(result.Stdout), result.ExitCode, result.Duration)

	// 取消传播：客户端超时应中断请求，节点侧终止进程树（配合 allowlist 中的 sleep）。
	if containsCommand(ready.Allowed, "sleep") {
		runCtx, cancelRun := context.WithTimeout(context.Background(), time.Second)
		_, runErr := client.Run(runCtx, catalog.ExecRequest{Command: "sleep", Args: []string{"30"}})
		cancelRun()
		if runErr == nil {
			t.Fatal("cancelled execution must return an error")
		}
		t.Logf("cancellation propagated: %v", runErr)
	}
}

// containsCommand 报告白名单是否包含指定命令。
func containsCommand(allowed []string, name string) bool {
	for _, candidate := range allowed {
		if candidate == name {
			return true
		}
	}
	return false
}

// allowedOr 优先返回白名单中的 preferred，否则返回第一个可用命令。
func allowedOr(allowed []string, preferred string) string {
	for _, name := range allowed {
		if name == preferred {
			return name
		}
	}
	return allowed[0]
}
