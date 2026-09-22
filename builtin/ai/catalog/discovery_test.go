package catalog

import (
	"context"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/engine"
)

func TestIsCommandSafeForAI(t *testing.T) {
	tests := []struct {
		name string
		cmd  engine.CommandInfo
		want bool
	}{
		{
			name: "safe command",
			cmd:  engine.CommandInfo{Command: "/ping"},
			want: true,
		},
		{
			name: "ai command",
			cmd:  engine.CommandInfo{Command: "/ai"},
			want: false,
		},
		{
			name: "empty name after trim",
			cmd:  engine.CommandInfo{Command: "/"},
			want: false,
		},
		{
			name: "with permissions",
			cmd:  engine.CommandInfo{Command: "/admin", Permissions: []string{"admin"}},
			want: false,
		},
		{
			name: "with definition permissions",
			cmd: engine.CommandInfo{
				Command:    "/secret",
				Definition: &command.Definition{Permissions: []string{"secret"}},
			},
			want: false,
		},
		{
			name: "hidden command",
			cmd: engine.CommandInfo{
				Command:    "/hidden",
				Definition: &command.Definition{Hidden: true},
			},
			want: true, // IsCommandSafeForAI doesn't check Hidden
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCommandSafeForAI(tt.cmd)
			if got != tt.want {
				t.Errorf("IsCommandSafeForAI(%+v) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestBuildToolFromCommand(t *testing.T) {
	cmd := engine.CommandInfo{Command: "/test_cmd", Description: "A test command"}
	tool := ToolFromCommand(cmd)
	if tool == nil {
		t.Fatal("ToolFromCommand returned nil")
	}
	if tool.Name != "test_cmd" {
		t.Errorf("expected name %q, got %q", "test_cmd", tool.Name)
	}
	if tool.Description != "A test command" {
		t.Errorf("expected description %q, got %q", "A test command", tool.Description)
	}
	if len(tool.Categories) != 1 || tool.Categories[0] != toolkit.CategoryGeneral {
		t.Errorf("expected [general] categories, got %v", tool.Categories)
	}

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(result, "/test_cmd") {
		t.Errorf("expected result to contain command name, got %q", result)
	}
}

func TestBuildToolFromCommandEmpty(t *testing.T) {
	cmd := engine.CommandInfo{Command: "/"}
	tool := ToolFromCommand(cmd)
	if tool != nil {
		t.Error("expected nil for empty command")
	}
}

func TestBuildToolFromCommandNoDescription(t *testing.T) {
	cmd := engine.CommandInfo{Command: "/no_desc"}
	tool := ToolFromCommand(cmd)
	if tool == nil {
		t.Fatal("ToolFromCommand returned nil")
	}
	if !strings.Contains(tool.Description, "/no_desc") {
		t.Errorf("expected description to contain command, got %q", tool.Description)
	}
}
