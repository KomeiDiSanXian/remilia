//go:build network

// Package agent 提供 Agent 能力的端到端联调用例（需要 network 构建标签）。
//
// 用例组装与 cmd/bot 一致的插件运行时（storage + permission + vevent + ai），
// 使用 Mock 平台发送真实用户消息，AI 调用真实 LLM 并在远程执行节点上完成
// 任务；审批请求由用例自动批准（等价于用户点"允许"）。
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
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
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

// scenarioResult 一次场景运行的结果。
type scenarioResult struct {
	// Task 结束后落库的任务记录。
	Task *task.Task
	// Messages 全部出站消息。
	Messages []platform.OutboundMessage
	// Approvals 自动批准的审批 ID。
	Approvals []string
}

// requireScenarioEnv 读取联调所需的环境变量；缺失时跳过用例。
func requireScenarioEnv(t *testing.T) (apiKey, nodeURL, nodeToken string) {
	t.Helper()
	apiKey = strings.TrimSpace(os.Getenv("REMILIA_AI_API_KEY"))
	nodeURL = strings.TrimSpace(os.Getenv("REMILIA_EXEC_NODE_URL"))
	nodeToken = strings.TrimSpace(os.Getenv("REMILIA_EXEC_NODE_TOKEN"))
	if apiKey == "" || nodeURL == "" || nodeToken == "" {
		t.Skip("REMILIA_AI_API_KEY / REMILIA_EXEC_NODE_URL / REMILIA_EXEC_NODE_TOKEN not set")
	}
	return apiKey, nodeURL, nodeToken
}

// runAgentScenario 组装真实运行时、发送提示词、自动批准审批，直到任务终结。
func runAgentScenario(t *testing.T, prompt string) scenarioResult {
	t.Helper()
	apiKey, nodeURL, nodeToken := requireScenarioEnv(t)

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

	aiInst, ok := mgr.Get("ai")
	if !ok {
		t.Fatal("ai plugin instance not found")
	}
	aiPlugin, ok := aiInst.GetAPI().(*ai.Plugin)
	if !ok || aiPlugin == nil {
		t.Fatalf("unexpected ai plugin API %T", aiInst.GetAPI())
	}

	sender := &testbot.MockSender{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		eng.ProcessPlatformEvent(testbot.MakePlatformC2CEvent("scenario-user", prompt), sender)
		eng.WaitForAsyncHandlers()
	}()

	result := scenarioResult{}
	approved := map[string]bool{}
	seen := 0
	deadline := time.Now().Add(8 * time.Minute)
	var lastStatus string

	for time.Now().Before(deadline) {
		messages := sender.Sent()
		for ; seen < len(messages); seen++ {
			text := messageText(messages[seen])
			if text == "" {
				continue
			}
			t.Logf("[bot] %s", truncate(text, 500))
			if m := approvalIDPattern.FindStringSubmatch(text); len(m) == 2 && !approved[m[1]] {
				approved[m[1]] = true
				result.Approvals = append(result.Approvals, m[1])
				t.Logf("[user] /ai approve %s", m[1])
				eng.ProcessPlatformEvent(
					testbot.MakePlatformC2CEvent("scenario-user", "/ai approve "+m[1]), sender)
			}
		}

		tasks, err := aiPlugin.RecentTasks(5)
		if err == nil && len(tasks) > 0 {
			status := string(tasks[0].Status)
			if status != lastStatus {
				t.Logf("[task] %s -> %s (steps=%d artifacts=%d)",
					tasks[0].Goal, status, len(tasks[0].Steps), len(tasks[0].Artifacts))
				lastStatus = status
			}
			if status == "succeeded" || status == "failed" || status == "cancelled" {
				result.Task = tasks[0]
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	wg.Wait()

	result.Messages = sender.Sent()
	if result.Task == nil {
		t.Fatalf("scenario did not finish: approvals=%v", result.Approvals)
	}
	t.Logf("task %s: status=%s goal=%q", result.Task.ID, result.Task.Status, result.Task.Goal)
	for _, step := range result.Task.Steps {
		t.Logf("  step %s [%s] %s", step.ID, step.Status, step.Description)
	}
	for _, artifact := range result.Task.Artifacts {
		t.Logf("  artifact %s %s (%d bytes)", artifact.Kind, artifact.Name, artifact.Size)
	}
	return result
}

// TestRemoteCodingScenario 编码场景：真实 LLM 在远程节点完成多步骤文件任务
// （写脚本 → 运行 → 检查环境 → 汇报），全程自动批准工具调用。
func TestRemoteCodingScenario(t *testing.T) {
	prompt := strings.Join([]string{
		"请完成一个远程工作区任务：",
		"1. 在工作区创建 Python 脚本 fib.py，打印斐波那契数列前 10 项；",
		"2. 用 python3 运行它并确认输出；",
		"3. 运行 uname -a 确认执行节点环境；",
		"4. 汇报创建的文件与运行结果。",
	}, "\n")
	result := runAgentScenario(t, prompt)

	if len(result.Approvals) == 0 {
		t.Error("expected at least one tool approval request")
	}
	if result.Task.Status != task.StatusSucceeded {
		t.Errorf("task status = %s, want succeeded", result.Task.Status)
	}
	foundFile := false
	for _, artifact := range result.Task.Artifacts {
		if strings.Contains(artifact.Name, "fib.py") {
			foundFile = true
		}
	}
	if !foundFile {
		t.Errorf("expected fib.py to be registered as an artifact, got %+v", result.Task.Artifacts)
	}
	if all := strings.ToLower(joinMessages(result.Messages)); !strings.Contains(all, "fib") {
		t.Error("final replies should mention fib")
	}
}

// TestRemoteOpsInspectionScenario 运维巡检场景（只读）：同一套任务与能力契约
// 完成系统巡检并输出报告，验证通用架构不绑定编码场景。
func TestRemoteOpsInspectionScenario(t *testing.T) {
	prompt := strings.Join([]string{
		"请对执行节点做一次只读巡检并输出中文报告：",
		"1. 查看系统版本与内核（uname -a）；",
		"2. 查看磁盘使用情况（df -h）；",
		"3. 查看内存使用情况（free -h）；",
		"4. 查看运行时长与负载（uptime）；",
		"5. 汇总成一份巡检结论（正常/异常与关注点）。",
	}, "\n")
	result := runAgentScenario(t, prompt)

	if result.Task.Status != task.StatusSucceeded {
		t.Errorf("task status = %s, want succeeded", result.Task.Status)
	}
	all := joinMessages(result.Messages)
	for _, keyword := range []string{"磁盘", "内存"} {
		if !strings.Contains(all, keyword) {
			t.Errorf("inspection report should mention %q; replies:\n%s", keyword, all)
		}
	}
	if len(result.Task.Artifacts) != 0 {
		t.Logf("inspection task produced %d artifact(s) (read-only scenario)", len(result.Task.Artifacts))
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
