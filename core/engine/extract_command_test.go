package engine

// extract_command_test.go — 命令词提取的边界处理
//
// extractCommand 取首个空白分隔的单词，并去掉首尾空白
// （"  /ping  " 也必须是 "/ping"）；只有空白或空串时返回 ""。

import "testing"

func TestExtractCommand_TrimsSurroundingWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal command", "/ping", "/ping"},
		{"with leading space", "  /ping", "/ping"},
		{"with trailing space", "/ping  ", "/ping"},
		{"with both spaces", "  /ping  ", "/ping"},
		{"command with args", "/ping arg1", "/ping"},
		{"with spaces and args", "  /ping arg1  ", "/ping"},
		{"only spaces", "   ", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCommand(tt.input)
			if result != tt.expected {
				t.Errorf("extractCommand(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
