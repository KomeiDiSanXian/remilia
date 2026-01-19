package command

import (
	"errors"
	"strings"
	"testing"
)

func TestEnhancedSystem_Validators_ArgumentFlagDefinition(t *testing.T) {
	t.Parallel()

	p := NewParser("/")
	p.Register(&Definition{
		Name: "x",
		Arguments: []*Argument{
			{
				Name:     "a",
				Type:     ArgTypeString,
				Required: true,
				Validator: func(s string) error {
					if s != "ok" {
						return errors.New("a must be ok")
					}
					return nil
				},
			},
		},
		Flags: []*Flag{
			{
				Name:     "f",
				Type:     ArgTypeInt,
				Required: false,
				Validator: func(s string) error {
					if s == "13" {
						return errors.New("unlucky")
					}
					return nil
				},
			},
		},
		Validator: func(pc *Parsed) error {
			if pc.GetString("a") == "ok" && pc.GetInt("f") == 7 {
				return errors.New("no seven")
			}
			return nil
		},
	})

	// argument validator fail
	_, err := p.Parse("/x bad")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "argument a validation failed") {
		t.Fatalf("unexpected err: %v", err)
	}

	// flag validator fail
	_, err = p.Parse("/x ok --f 13")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flag --f validation failed") {
		t.Fatalf("unexpected err: %v", err)
	}

	// definition validator fail (wrapped)
	_, err = p.Parse("/x ok --f 7")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestEnhancedSystem_RequiredAndDefault(t *testing.T) {
	t.Parallel()

	p := NewParser("/")
	p.Register(&Definition{
		Name: "x",
		Arguments: []*Argument{
			{Name: "a", Type: ArgTypeString, Required: false, Default: "d"},
		},
		Flags: []*Flag{
			{Name: "n", Type: ArgTypeInt, Required: false, Default: 9},
			{Name: "req", Type: ArgTypeBool, Required: true},
		},
	})

	// missing required flag
	_, err := p.Parse("/x")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required flag --req is missing") {
		t.Fatalf("unexpected err: %v", err)
	}

	parsed, err := p.Parse("/x --req true")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := parsed.GetString("a"); got != "d" {
		t.Fatalf("expected default a=d, got %q", got)
	}
	if got := parsed.GetInt("n"); got != 9 {
		t.Fatalf("expected default n=9, got %d", got)
	}
}
