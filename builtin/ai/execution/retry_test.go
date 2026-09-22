package execution

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

func TestBuildReflectionMessage(t *testing.T) {
	msg := BuildReflectionMessage("get_weather", 2, "错误: 工具执行失败")
	if msg.Role != protocol.RoleUser {
		t.Errorf("reflection should be a user message, got %v", msg.Role)
	}
	if !strings.Contains(msg.Content, "get_weather") || !strings.Contains(msg.Content, "反思") {
		t.Errorf("reflection content should mention tool and ask for reflection: %q", msg.Content)
	}
}

func TestBuildRetryAbortMessage(t *testing.T) {
	msg := BuildRetryAbortMessage("get_weather", 3, "错误: 超时")
	if !strings.Contains(msg, "get_weather") || !strings.Contains(msg, "已停止尝试") {
		t.Errorf("abort message mismatch: %q", msg)
	}
}
