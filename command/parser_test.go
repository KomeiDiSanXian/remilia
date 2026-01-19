package command

import "testing"

func TestParseCommandLine_Basic(t *testing.T) {
	args, err := ParseCommandLine("/x a --k v")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if args.Command != "/x" {
		t.Fatalf("expected /x, got %q", args.Command)
	}
	if len(args.Positional) != 1 || args.Positional[0] != "a" {
		t.Fatalf("unexpected positional: %#v", args.Positional)
	}
	if args.Flags["k"] != "v" {
		t.Fatalf("expected flag k=v, got %#v", args.Flags)
	}
}

func TestEnhancedParser_Basic(t *testing.T) {
	p := NewParser("/")
	p.Register(&Definition{Name: "ping"})

	parsed, err := p.Parse("/ping")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if parsed.Definition == nil || parsed.Definition.Name != "ping" {
		t.Fatalf("unexpected definition: %#v", parsed.Definition)
	}
	if len(parsed.CommandPath) != 1 || parsed.CommandPath[0] != "ping" {
		t.Fatalf("unexpected path: %#v", parsed.CommandPath)
	}
}

func TestEnhancedParser_SubCommand(t *testing.T) {
	p := NewParser("/")
	p.Register(&Definition{
		Name: "admin",
		SubCommands: []*Definition{{
			Name:        "ban",
			Arguments:   []*Argument{{Name: "user", Type: ArgTypeString, Required: true}},
			SubCommands: nil,
		}},
	})

	parsed, err := p.Parse("/admin ban alice")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if want := []string{"admin", "ban"}; len(parsed.CommandPath) != len(want) || parsed.CommandPath[0] != want[0] || parsed.CommandPath[1] != want[1] {
		t.Fatalf("unexpected path: %#v", parsed.CommandPath)
	}
	if parsed.GetString("user") != "alice" {
		t.Fatalf("expected user=alice, got %q", parsed.GetString("user"))
	}
}
