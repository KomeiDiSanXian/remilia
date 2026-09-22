// session_test.go — 会话消息进入请求前的窗口裁剪与附件收敛契约。

package ai

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
)

func TestPrepareRequestMessagesKeepsLatestUserParts(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleSystem, Content: "sys"},
		{Role: protocol.RoleUser, Content: "", ContentParts: []protocol.ContentPart{
			{Type: protocol.ContentPartImage, Data: []byte("img1"), MimeType: "image/png"},
		}},
		{Role: protocol.RoleAssistant, Content: "reply"},
		{Role: protocol.RoleUser, Content: "", ContentParts: []protocol.ContentPart{
			{Type: protocol.ContentPartText, Text: "new question"},
			{Type: protocol.ContentPartImage, Data: []byte("img2"), MimeType: "image/png"},
		}},
	}

	retention := runtime.Retention{MaxTurns: 5, Window: 10 * time.Minute, MaxPerRequest: 8}
	out, budgetDropped, staleDropped := runtime.PrepareRequestMessages(msgs, retention)

	// 最后一条用户消息保留完整 ContentParts（含二进制数据）
	last := out[len(out)-1]
	if len(last.ContentParts) != 2 {
		t.Fatalf("expected latest user message to keep 2 content parts, got %d", len(last.ContentParts))
	}
	if len(last.ContentParts[1].Data) != 4 {
		t.Errorf("expected binary data retained for latest user message")
	}

	// 历史用户消息在保留窗口内（最近 5 条 + 10 分钟内）：二进制保留，支持追问图片细节
	hist := out[1]
	if len(hist.ContentParts) != 1 || len(hist.ContentParts[0].Data) != 4 {
		t.Errorf("expected historical image retained within context window, got %+v", hist.ContentParts)
	}
	if budgetDropped != 0 || staleDropped != 0 {
		t.Errorf("expected no dropped images, got budget=%d stale=%d", budgetDropped, staleDropped)
	}

	// 原始切片不受影响
	if len(msgs[1].ContentParts) != 1 {
		t.Errorf("prepareRequestMessages must not mutate input")
	}
}

func TestPrepareRequestMessagesDropsBeyondContextTurns(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleUser, Content: "", ContentParts: []protocol.ContentPart{{Type: protocol.ContentPartImage, Data: []byte("img0")}}},
		{Role: protocol.RoleAssistant, Content: "r1"},
		{Role: protocol.RoleUser, Content: "q1"},
		{Role: protocol.RoleAssistant, Content: "r2"},
		{Role: protocol.RoleUser, Content: "q2"},
	}

	// maxTurns=2：只保留最近 2 条 user 消息（q1、q2）；img0 为第 3 旧 → 降级
	out, budgetDropped, staleDropped := runtime.PrepareRequestMessages(msgs, runtime.Retention{MaxTurns: 2, Window: 0, MaxPerRequest: 8})
	if staleDropped != 1 || budgetDropped != 0 {
		t.Fatalf("expected 1 stale-dropped image, got budget=%d stale=%d", budgetDropped, staleDropped)
	}
	if len(out[0].ContentParts) != 0 || out[0].Content == "" {
		t.Errorf("expected oldest user image stripped to placeholder, got %+v", out[0])
	}
	if len(out[2].ContentParts) != 0 || out[2].Content != "q1" {
		t.Errorf("expected q1 retained without parts, got %+v", out[2])
	}
}

func TestPrepareRequestMessagesDropsStaleImagesByWindow(t *testing.T) {
	now := time.Now()
	msgs := []protocol.Message{
		{Role: protocol.RoleUser, Content: "", Timestamp: now.Add(-30 * time.Minute),
			ContentParts: []protocol.ContentPart{{Type: protocol.ContentPartImage, Data: []byte("old")}}},
		{Role: protocol.RoleAssistant, Content: "r"},
		{Role: protocol.RoleUser, Content: "q", Timestamp: now},
	}

	// 时间窗 10 分钟：30 分钟前的图片即使条数在窗口内也降级
	out, budgetDropped, staleDropped := runtime.PrepareRequestMessages(msgs, runtime.Retention{MaxTurns: 5, Window: 10 * time.Minute, MaxPerRequest: 8})
	if staleDropped != 1 || budgetDropped != 0 {
		t.Fatalf("expected 1 stale-dropped image, got budget=%d stale=%d", budgetDropped, staleDropped)
	}
	if len(out[0].ContentParts) != 0 {
		t.Errorf("expected stale image stripped, got %+v", out[0].ContentParts)
	}
}

func TestPrepareRequestMessagesCapsImagesPerRequest(t *testing.T) {
	now := time.Now()
	msgs := []protocol.Message{
		{Role: protocol.RoleUser, Content: "", Timestamp: now.Add(-1 * time.Minute),
			ContentParts: []protocol.ContentPart{
				{Type: protocol.ContentPartImage, Data: []byte("a")},
				{Type: protocol.ContentPartImage, Data: []byte("b")},
			}},
		{Role: protocol.RoleAssistant, Content: "r"},
		{Role: protocol.RoleUser, Content: "", Timestamp: now,
			ContentParts: []protocol.ContentPart{
				{Type: protocol.ContentPartImage, Data: []byte("c")},
				{Type: protocol.ContentPartImage, Data: []byte("d")},
				{Type: protocol.ContentPartImage, Data: []byte("e")},
				{Type: protocol.ContentPartImage, Data: []byte("f")},
			}},
	}

	// maxPerRequest=5：当前轮 4 张 + 历史 2 张 → 预算只够 1 张，历史整条降级
	out, budgetDropped, staleDropped := runtime.PrepareRequestMessages(msgs, runtime.Retention{MaxTurns: 5, Window: 10 * time.Minute, MaxPerRequest: 5})
	if budgetDropped != 2 || staleDropped != 0 {
		t.Fatalf("expected 2 budget-dropped images, got budget=%d stale=%d", budgetDropped, staleDropped)
	}
	if len(out[0].ContentParts) != 0 {
		t.Errorf("expected over-budget historical images stripped, got %+v", out[0].ContentParts)
	}
	last := out[len(out)-1]
	if runtime.CountImageParts(last.ContentParts) != 4 {
		t.Errorf("expected current turn 4 images retained, got %d", runtime.CountImageParts(last.ContentParts))
	}
}

func TestPrepareRequestMessagesMaxTurnsZeroKeepsOnlyCurrent(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleUser, Content: "", ContentParts: []protocol.ContentPart{{Type: protocol.ContentPartImage, Data: []byte("old")}}},
		{Role: protocol.RoleAssistant, Content: "r"},
		{Role: protocol.RoleUser, Content: "q"},
	}

	// maxTurns=0：仅当前轮保留附件（旧行为）
	out, budgetDropped, staleDropped := runtime.PrepareRequestMessages(msgs, runtime.Retention{MaxTurns: 0, Window: 0, MaxPerRequest: 8})
	if staleDropped != 1 || budgetDropped != 0 {
		t.Fatalf("expected 1 stale-dropped image, got budget=%d stale=%d", budgetDropped, staleDropped)
	}
	if len(out[0].ContentParts) != 0 {
		t.Errorf("expected historical image stripped when maxTurns=0, got %+v", out[0].ContentParts)
	}
}

func TestTruncateToolResult(t *testing.T) {
	short := "short result"
	if got := runtime.TruncateToolResult(short); got != short {
		t.Errorf("expected short result unchanged, got %q", got)
	}

	long := strings.Repeat("字", 9000) // 9000 runes > 8000 limit
	got := runtime.TruncateToolResult(long)
	runes := []rune(got)
	if len(runes) != runtime.MaxToolResultLen+len([]rune("\n…(工具结果过长已截断)")) {
		t.Errorf("expected truncated length, got %d runes", len(runes))
	}
	// 截断后仍是合法 UTF-8（rune 边界）
	if !utf8.ValidString(got) {
		t.Error("truncated result should be valid UTF-8")
	}
	if !strings.HasSuffix(got, "…(工具结果过长已截断)") {
		t.Errorf("expected truncation marker suffix, got %q", got[len(got)-20:])
	}
}
