package minecraft

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

// TestAIToolResolvesFavorite 验证 AI 工具可通过会话上下文解析收藏名/序号。
func TestAIToolResolvesFavorite(t *testing.T) {
	statusJSON := `{"version":{"name":"1.21.1","protocol":767},"players":{"max":10,"online":1},"description":"Local Server"}`
	port, stop := startFakeSLPServer(t, statusJSON)
	defer stop()

	fav := newTestFavManager(t)
	scope := "qq:888"
	if _, err := fav.Add(scope, "local", fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	p := &mcPlugin{
		fav:    fav,
		cfg:    Config{Timeout: 5 * time.Second, CacheTTL: time.Minute, Avatars: false, EnableQuery: false, DirectQuery: true},
		client: &http.Client{Timeout: 5 * time.Second},
		cache:  newTTLCache[*MCServerStatus](time.Minute, 8),
	}
	tool := p.ListTools()[0]
	toolCtx := ai.WithToolSource(context.Background(), ai.ToolSource{Platform: "qq", ChatID: "888"})

	// 收藏名解析
	out, err := tool.Execute(toolCtx, map[string]any{"server_address": "local"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "1.21.1") || !strings.Contains(out, "Local Server") {
		t.Errorf("收藏名未解析到仿真服务器: %s", out)
	}

	// 序号解析
	out, err = tool.Execute(toolCtx, map[string]any{"server_address": "1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "1.21.1") {
		t.Errorf("序号未解析: %s", out)
	}

	// 无会话上下文：按字面地址处理，"local" 无法连接
	out, _ = tool.Execute(context.Background(), map[string]any{"server_address": "local"})
	if !strings.Contains(out, "无法连接") {
		t.Errorf("无上下文时应按地址查询并报错: %s", out)
	}

	// 其他会话：收藏不可见
	otherCtx := ai.WithToolSource(context.Background(), ai.ToolSource{Platform: "qq", ChatID: "999"})
	out, _ = tool.Execute(otherCtx, map[string]any{"server_address": "local"})
	if !strings.Contains(out, "无法连接") {
		t.Errorf("其他会话不应看到收藏: %s", out)
	}
}
