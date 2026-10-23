package toolkit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExecuteTextProjectsRichResult(t *testing.T) {
	tool := Tool{Execute: func(context.Context, *Invocation, map[string]any) (ToolResult, error) {
		return ToolResult{Parts: []ResultPart{
			{Kind: ResultText, Text: "回答"},
			{Kind: ResultImage, URI: "https://x/y.png"},
		}}, nil
	}}
	got, err := tool.ExecuteText(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("ExecuteText: %v", err)
	}
	if !strings.Contains(got, "回答") || !strings.Contains(got, "https://x/y.png") {
		t.Fatalf("ExecuteText = %q", got)
	}
}

func TestExecuteTextPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	tool := Tool{Execute: func(context.Context, *Invocation, map[string]any) (ToolResult, error) {
		return ToolResult{}, boom
	}}
	if _, err := tool.ExecuteText(context.Background(), nil, nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestTextResultFlatten(t *testing.T) {
	if got := TextResult("你好").Flatten(); got != "你好" {
		t.Fatalf("Flatten = %q", got)
	}
	if TextResult("").Empty() != true {
		t.Fatal("empty text result must be Empty")
	}
	if TextResult("x").Empty() != false {
		t.Fatal("non-empty text result must not be Empty")
	}
}

func TestFlattenMixedParts(t *testing.T) {
	r := ToolResult{Parts: []ResultPart{
		{Kind: ResultText, Text: "第一段"},
		{Kind: ResultImage, MimeType: "image/png", Name: "chart"},
		{Kind: ResultStructured, Text: `{"a":1}`},
		{Kind: ResultResource, URI: "https://example.com/x", MimeType: "text/plain"},
	}}
	got := r.Flatten()
	want := "第一段\n[图片: chart (image/png)]\n{\"a\":1}\n[资源 (text/plain) https://example.com/x]"
	if got != want {
		t.Fatalf("Flatten =\n%q\nwant\n%q", got, want)
	}
}

func TestFlattenSkipsEmptyParts(t *testing.T) {
	r := ToolResult{Parts: []ResultPart{
		{Kind: ResultText},
		{Kind: ResultImage},
		{Kind: ResultAudio},
	}}
	if !r.Empty() {
		t.Fatal("all-empty parts must be Empty")
	}
	if got := r.Flatten(); got != "" {
		t.Fatalf("Flatten of empty parts = %q", got)
	}
}
