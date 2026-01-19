package command

import (
	"reflect"
	"strings"
	"testing"
)

func TestEnhancedSystem_StringSlice_Positional(t *testing.T) {
	t.Parallel()

	p := NewParser("/")
	p.Register(&Definition{
		Name: "x",
		Arguments: []*Argument{
			{Name: "first", Type: ArgTypeString, Required: true},
			{Name: "rest", Type: ArgTypeStringSlice, Required: false},
		},
	})

	parsed, err := p.Parse("/x a b c")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if parsed.GetString("first") != "a" {
		t.Fatalf("expected first=a, got %q", parsed.GetString("first"))
	}

	got := parsed.Arguments["rest"]
	want := []string{"b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected rest=%v, got %#v", want, got)
	}
}

func TestEnhancedSystem_StringSlice_Positional_RequiredMissing(t *testing.T) {
	t.Parallel()

	p := NewParser("/")
	p.Register(&Definition{
		Name: "x",
		Arguments: []*Argument{
			{Name: "rest", Type: ArgTypeStringSlice, Required: true},
		},
	})

	_, err := p.Parse("/x")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required argument rest is missing") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestEnhancedSystem_StringSlice_Positional_MustBeLast(t *testing.T) {
	t.Parallel()

	p := NewParser("/")
	p.Register(&Definition{
		Name: "x",
		Arguments: []*Argument{
			{Name: "rest", Type: ArgTypeStringSlice, Required: false},
			{Name: "after", Type: ArgTypeString, Required: false},
		},
	})

	_, err := p.Parse("/x a")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "stringSlice must be the last") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestEnhancedSystem_StringSlice_Flag(t *testing.T) {
	t.Parallel()

	p := NewParser("/")
	p.Register(&Definition{
		Name: "x",
		Flags: []*Flag{
			{Name: "tags", ShortName: "t", Type: ArgTypeStringSlice, Required: true},
		},
	})

	parsed, err := p.Parse(`/x --tags "a  b   c"`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	got := parsed.Flags["tags"]
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected tags=%v, got %#v", want, got)
	}

	// short name should also work
	parsed, err = p.Parse(`/x -t "one two"`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got = parsed.Flags["tags"]
	want = []string{"one", "two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected tags=%v, got %#v", want, got)
	}
}

func TestEnhancedSystem_StringSlice_Flag_RequiredButEmpty(t *testing.T) {
	t.Parallel()

	p := NewParser("/")
	p.Register(&Definition{
		Name: "x",
		Flags: []*Flag{
			{Name: "tags", Type: ArgTypeStringSlice, Required: true},
		},
	})

	_, err := p.Parse(`/x`)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required flag --tags is missing") {
		t.Fatalf("unexpected err: %v", err)
	}
}
