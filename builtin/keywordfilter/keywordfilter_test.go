package keywordfilter_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/keywordfilter"
)

func TestKeywordFilter_Check(t *testing.T) {
	p := keywordfilter.NewPlugin(keywordfilter.Config{
		Keywords: []string{"bad", "evil"},
	})

	if got := p.Check("this is a bad message"); got != "bad" {
		t.Fatalf("expected 'bad', got %q", got)
	}
	if got := p.Check("clean message"); got != "" {
		t.Fatalf("expected no match, got %q", got)
	}
}

func TestKeywordFilter_CaseInsensitive(t *testing.T) {
	p := keywordfilter.NewPlugin(keywordfilter.Config{
		Keywords:      []string{"spam"},
		CaseSensitive: false,
	})

	if got := p.Check("This is SPAM!"); got == "" {
		t.Fatal("case-insensitive match should work")
	}
}

func TestKeywordFilter_PatternMatch(t *testing.T) {
	p := keywordfilter.NewPlugin(keywordfilter.Config{
		Patterns: []string{`\d{4}-\d{4}-\d{4}-\d{4}`}, // credit card-like
	})

	if got := p.Check("card: 1234-5678-9012-3456"); got == "" {
		t.Fatal("pattern match should work")
	}
}

func TestKeywordFilter_AddRemoveKeyword(t *testing.T) {
	p := keywordfilter.NewPlugin(keywordfilter.Config{})

	p.AddKeyword("newkw")
	if p.KeywordCount() != 1 {
		t.Fatal("should have 1 keyword after add")
	}

	p.RemoveKeyword("newkw")
	if p.KeywordCount() != 0 {
		t.Fatal("should have 0 keywords after remove")
	}
}

func TestKeywordFilter_AddPattern(t *testing.T) {
	p := keywordfilter.NewPlugin(keywordfilter.Config{})

	if err := p.AddPattern(`\d+`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PatternCount() != 1 {
		t.Fatal("should have 1 pattern")
	}

	if err := p.AddPattern(`[invalid`); err == nil {
		t.Fatal("invalid regex should return error")
	}
}

func TestKeywordFilter_Descriptor(t *testing.T) {
	desc := keywordfilter.New(keywordfilter.Config{Keywords: []string{"test"}})
	if desc == nil || desc.Name != "keywordfilter" {
		t.Fatal("invalid descriptor")
	}
}
