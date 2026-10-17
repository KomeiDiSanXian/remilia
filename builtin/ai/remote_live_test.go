//go:build network

package ai

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execnode"
)

// TestLiveRemoteTools 通过真实工具契约验证远程执行节点：插件的能力端口指向
// 远程节点后，file_write / file_read / file_list / file_edit / process_run
// 五个动作在节点上完成，任务与策略层不需要任何改动。
//
//	REMILIA_EXEC_NODE_URL=http://10.0.0.19:9020 \
//	REMILIA_EXEC_NODE_TOKEN=<token> \
//	go test -tags network ./builtin/ai -run TestLiveRemoteTools -v
func TestLiveRemoteTools(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("REMILIA_EXEC_NODE_URL"))
	token := strings.TrimSpace(os.Getenv("REMILIA_EXEC_NODE_TOKEN"))
	if url == "" || token == "" {
		t.Skip("REMILIA_EXEC_NODE_URL / REMILIA_EXEC_NODE_TOKEN not set")
	}

	client, err := execnode.NewClient(execnode.ClientConfig{BaseURL: url, Token: token, Timeout: time.Minute})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ready, err := client.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}

	// 与生产装配一致：remote 模式下能力端口全部来自节点客户端。
	p := &Plugin{cfg: &config.Config{WorkspaceAllowWrite: true}, remote: client}
	caps := p.toolCapabilities()
	ctx := catalog.WithCapabilities(context.Background(), caps)

	fileTools := map[string]func(context.Context, map[string]any) (string, error){}
	for _, tool := range catalog.BuildFileTools(catalog.FileToolOptions{AllowWrite: true}) {
		fileTools[tool.Name] = tool.Execute
	}
	execTool := catalog.BuildExecTools(catalog.ExecToolOptions{RequireApproval: true})[0].Execute

	probe := fmt.Sprintf("tool-probe-%d.txt", time.Now().UnixNano())

	writeOut, err := fileTools[catalog.FileWriteToolName](ctx, map[string]any{
		"path": probe, "content": "tool contract on remote node",
	})
	if err != nil || !strings.Contains(writeOut, "已创建") {
		t.Fatalf("file_write on remote node = (%q, %v)", writeOut, err)
	}

	readOut, err := fileTools[catalog.FileReadToolName](ctx, map[string]any{"path": probe})
	if err != nil || !strings.Contains(readOut, "tool contract on remote node") {
		t.Fatalf("file_read on remote node = (%q, %v)", readOut, err)
	}

	editOut, err := fileTools[catalog.FileEditToolName](ctx, map[string]any{
		"path": probe, "old_string": "remote node", "new_string": "remote executor",
	})
	if err != nil || !strings.Contains(editOut, "+remote executor") {
		t.Fatalf("file_edit on remote node = (%q, %v)", editOut, err)
	}

	listOut, err := fileTools[catalog.FileListToolName](ctx, map[string]any{})
	if err != nil || !strings.Contains(listOut, probe) {
		t.Fatalf("file_list on remote node = (%q, %v)", listOut, err)
	}

	if len(ready.Allowed) == 0 {
		t.Log("node is file-only; skipping process_run")
		return
	}
	execOut, err := execTool(ctx, map[string]any{"command": "uname", "args": []any{"-a"}})
	if err != nil || !strings.Contains(execOut, "Linux") || !strings.Contains(execOut, "退出码 0") {
		t.Fatalf("process_run on remote node = (%q, %v)", execOut, err)
	}
	t.Logf("all tool calls ran on the remote node:\n%s", strings.TrimSpace(execOut))
}
