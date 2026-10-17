//go:build network

// Package agent 提供 Agent 能力的端到端联调用例（需要 network 构建标签）。
//
// 用例组装与 cmd/bot 一致的插件运行时（storage + vevent + ai），使用 Mock 平台
// 发送真实用户消息，AI 调用真实 LLM 并在远程执行节点上完成任务；审批请求由
// 用例自动批准（等价于用户点"允许"）。
//
// 环境变量：
//
//	REMILIA_AI_API_KEY       LLM API Key（必填）
//	REMILIA_AI_BASE_URL      默认 https://api.deepseek.com
//	REMILIA_AI_MODEL         默认 deepseek-flash
//	REMILIA_EXEC_NODE_URL    远程执行节点地址（必填）
//	REMILIA_EXEC_NODE_TOKEN  远程执行节点令牌（必填）
package agent

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	builtinstorage "github.com/KomeiDiSanXian/remilia/builtin/storage"
	"github.com/KomeiDiSanXian/remilia/builtin/vevent"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

// mapConfigProvider 提供 AI 插件配置（与 cmd/bot 的 YAML provider 同形状）。
type mapConfigProvider struct{ cfg map[string]any }

// Sub 返回插件专属配置视图。
func (p mapConfigProvider) Sub(name string) map[string]any {
	if name == "ai" {
		return p.cfg
	}
	return nil
}

// OnConfigChange 测试不需要热更新。
func (mapConfigProvider) OnConfigChange(func()) {}

// approvalIDPattern 从审批消息中提取审批 ID（"`/ai approve A1-2`"）。
var approvalIDPattern = regexp.MustCompile(`approve ([A-Za-z0-9-]+)`)

// TestRemoteCodingScenario 端到端场景：真实 LLM + 远程执行节点完成一个多步骤
// 文件任务（写脚本 → 运行 → 检查环境 → 汇报），全程自动批准工具调用。
func TestRemoteCodingScenario(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("REMILIA_AI_API_KEY"))
	nodeURL := strings.TrimSpace(os.Getenv("REMILIA_EXEC_NODE_URL"))
	nodeToken := strings.TrimSpace(os.Getenv("REMILIA_EXEC_NODE_TOKEN"))
	if apiKey == "" || nodeURL == "" || nodeToken == "" {
		t.Skip("REMILIA_AI_API_KEY / REMILIA_EXEC_NODE_URL / REMILIA_EXEC_NODE_TOKEN not set")
	}

	cfg := map[string]any{
		"provider":              "openai",
		"model":                 envOr("REMILIA_AI_MODEL", "deepseek-flash"),
		"base_url":              envOr("REMILIA_AI_BASE_URL", "https://api.deepseek.com"),
		"api_key":               apiKey,
		"max_tokens":            2048,
		"max_depth":             12,
		"trigger_cmd":           "/ai",
		"private_chat":          true,
		"at_bot":                true,
		"markdown":              true,
		"system_prompt":         "你是 Remilia 的远程执行助手，回答简洁，面向中文用户。",
		"exec_node":             "remote",
		"exec_remote_url":       nodeURL,
		"exec_remote_token":     nodeToken,
		"workspace_allow_write": true,
		"exec_require_approval": true,
		"task_max_tool_calls":   60,
		"task_max_duration":     "15m",
		"memory_enabled":        false,
		"tool_select_max":       20,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	eng := engine.NewEngine()
	mgr := plugin.NewManager(eng, plugin.WithConfigProvider(mapConfigProvider{cfg: cfg}))
	if err := mgr.RegisterBatch(ctx, []*plugin.Descriptor{
		builtinstorage.New(infrastorage.WithDSN("file:agent-scenario?mode=memory&cache=shared")),
		permission.New(),
		vevent.New(eng),
		ai.New(eng),
	}); err != nil {
		t.Fatalf("register plugins: %v", err)
	}
	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("start plugins: %v", err)
	}
	defer func() { _ = mgr.StopAll(context.Background()) }()

	// 文件与执行动作要求 RBAC 权限；生产部署同样需要为使用者授予这些权限。
	permInst, ok := mgr.Get("permission")
	if !ok {
		t.Fatal("permission plugin instance not found")
	}
	permPlugin, ok := permInst.GetAPI().(*permission.Plugin)
	if !ok || permPlugin == nil {
		t.Fatalf("unexpected permission plugin API %T", permInst.GetAPI())
	}
	for _, perm := range []string{"ai.file.read", "ai.file.write", "ai.exec.run"} {
		if err := permPlugin.Grant("scenario-user", perm); err != nil {
			t.Fatalf("grant %s: %v", perm, err)
		}
	}

	inst, ok := mgr.Get("ai")
	if !ok {
		t.Fatal("ai plugin instance not found")
	}
	aiPlugin, ok := inst.GetAPI().(*ai.Plugin)
	if !ok || aiPlugin == nil {
		t.Fatalf("unexpected ai plugin API %T", inst.GetAPI())
	}

	sender := &testbot.MockSender{}
	prompt := strings.Join([]string{
		"请完成一个远程工作区任务：",
		"1. 在工作区创建 Python 脚本 fib.py，打印斐波那契数列前 10 项；",
		"2. 用 python3 运行它并确认输出；",
		"3. 运行 uname -a 确认执行节点环境；",
		"4. 汇报创建的文件与运行结果。",
	}, "\n")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		eng.ProcessPlatformEvent(testbot.MakePlatformC2CEvent("scenario-user", prompt), sender)
		eng.WaitForAsyncHandlers()
	}()

	approved := map[string]bool{}
	seen := 0
	deadline := time.Now().Add(8 * time.Minute)
	var finalTaskStatus string

	for time.Now().Before(deadline) {
		messages := sender.Sent()
		for ; seen < len(messages); seen++ {
			msg := messages[seen]
			text := messageText(msg)
			if text == "" {
				continue
			}
			t.Logf("[bot] %s", truncate(text, 500))

			// 自动批准：解析审批消息中的 ID 并发送 /ai approve 命令。
			if m := approvalIDPattern.FindStringSubmatch(text); len(m) == 2 && !approved[m[1]] {
				approved[m[1]] = true
				t.Logf("[user] /ai approve %s", m[1])
				eng.ProcessPlatformEvent(
					testbot.MakePlatformC2CEvent("scenario-user", "/ai approve "+m[1]), sender)
			}
		}

		tasks, err := aiPlugin.RecentTasks(5)
		if err == nil && len(tasks) > 0 {
			status := string(tasks[0].Status)
			if status != finalTaskStatus {
				t.Logf("[task] %s -> %s (steps=%d artifacts=%d)",
					tasks[0].Goal, status, len(tasks[0].Steps), len(tasks[0].Artifacts))
				finalTaskStatus = status
			}
			switch status {
			case "succeeded", "failed", "cancelled":
				goto done
			}
		}
		time.Sleep(2 * time.Second)
	}
done:
	wg.Wait()

	tasks, err := aiPlugin.RecentTasks(5)
	if err != nil || len(tasks) == 0 {
		t.Fatalf("no task recorded: (%v, %v); approvals=%v", tasks, err, approved)
	}
	task := tasks[0]
	t.Logf("task %s: status=%s goal=%q", task.ID, task.Status, task.Goal)
	for _, step := range task.Steps {
		t.Logf("  step %s [%s] %s", step.ID, step.Status, step.Description)
	}
	for _, artifact := range task.Artifacts {
		t.Logf("  artifact %s %s (%d bytes)", artifact.Kind, artifact.Name, artifact.Size)
	}

	if len(approved) == 0 {
		t.Error("expected at least one tool approval request")
	}
	if task.Status != "succeeded" {
		t.Errorf("task status = %s, want succeeded", task.Status)
	}
	foundFile := false
	for _, artifact := range task.Artifacts {
		if strings.Contains(artifact.Name, "fib.py") {
			foundFile = true
		}
	}
	if !foundFile {
		t.Errorf("expected fib.py to be registered as an artifact, got %+v", task.Artifacts)
	}
	allText := strings.ToLower(joinMessages(sender.Sent()))
	if !strings.Contains(allText, "fib") {
		t.Error("final replies should mention fib")
	}
}

// envOr 返回环境变量值（空时回退默认值）。
func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// messageText 拼接出站消息的文本部分。
func messageText(msg platform.OutboundMessage) string {
	if msg.Text != "" {
		return msg.Text
	}
	if msg.Markdown != "" {
		return msg.Markdown
	}
	return ""
}

// joinMessages 把所有出站文本拼成一个字符串（用于关键词断言）。
func joinMessages(messages []platform.OutboundMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(messageText(msg))
		b.WriteString("\n")
	}
	return b.String()
}

// truncate 截断日志文本。
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + fmt.Sprintf("...(%d more bytes)", len(text)-limit)
}
