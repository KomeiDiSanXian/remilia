package toolkit

import "testing"

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
