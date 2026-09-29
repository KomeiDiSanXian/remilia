package toolkit

import (
	"context"
	"testing"
)

func TestEffectiveSourceDefaultsToBuiltin(t *testing.T) {
	if got := (Tool{Name: "t"}).EffectiveSource(); got != SourceBuiltin {
		t.Fatalf("empty source must default to builtin, got %q", got)
	}
	if got := (Tool{Name: "t", Source: SourceCommand}).EffectiveSource(); got != SourceCommand {
		t.Fatalf("explicit source must be kept, got %q", got)
	}
}

func TestMCPSource(t *testing.T) {
	if got := MCPSource("github"); got != ActionSource("mcp:github") {
		t.Fatalf("MCPSource(github) = %q", got)
	}
	if got := MCPSource("  "); got != SourceMCP {
		t.Fatalf("blank server must collapse to %q, got %q", SourceMCP, got)
	}
}

func TestActionIDString(t *testing.T) {
	if got := (ActionID{Source: SourceMCP, Name: "search"}).String(); got != "mcp/search" {
		t.Fatalf("ActionID.String() = %q", got)
	}
	if got := (ActionID{Name: "search"}).String(); got != "builtin/search" {
		t.Fatalf("blank source String() = %q", got)
	}
}

func TestActionIDOf(t *testing.T) {
	id := ActionIDOf(Tool{Name: "echo", Source: MCPSource("srv")})
	if id.Source != ActionSource("mcp:srv") || id.Name != "echo" {
		t.Fatalf("ActionIDOf = %+v", id)
	}
	id = ActionIDOf(Tool{Name: "echo"})
	if id.Source != SourceBuiltin {
		t.Fatalf("default ActionID source = %q", id.Source)
	}
}

// Source 与 ExecuteRich 必须在注册表往返中保真，否则身份与富结果会在
// Get/List 后丢失。
func TestRegistryPreservesSourceAndExecuteRich(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(Tool{
		Name:   "search",
		Source: MCPSource("github"),
		ExecuteRich: func(ctx context.Context, args map[string]any) (ToolResult, error) {
			return TextResult("ok"), nil
		},
	})
	got, ok := reg.Get("search")
	if !ok {
		t.Fatal("tool not found")
	}
	if got.Source != MCPSource("github") {
		t.Fatalf("source not preserved: %q", got.Source)
	}
	if got.ExecuteRich == nil {
		t.Fatal("ExecuteRich not preserved")
	}
	if list := reg.List(); len(list) != 1 || list[0].Source != MCPSource("github") {
		t.Fatalf("List lost source: %+v", list)
	}
}
