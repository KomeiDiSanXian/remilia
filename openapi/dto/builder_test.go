package dto_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func TestMessageBuilder_Text(t *testing.T) {
	msg := dto.NewBuilder().Text("hello").Build()
	if msg.Content != "hello" {
		t.Errorf("expected 'hello', got %q", msg.Content)
	}
	if msg.Type != dto.TextMessage {
		t.Errorf("expected TextMessage, got %d", msg.Type)
	}
}
func TestMessageBuilder_At(t *testing.T) {
	msg := dto.NewBuilder().Text("Hi ").At("uid1").Text("!").Build()
	if msg.Type != dto.TextMessage {
		t.Errorf("expected TextMessage")
	}
	if msg.Content == "" {
		t.Error("content should not be empty")
	}
}
func TestMessageBuilder_AtAll(t *testing.T) {
	msg := dto.NewBuilder().Text("Hello ").AtAll().Build()
	if msg.Content == "" {
		t.Error("expected non-empty content")
	}
}
func TestMessageBuilder_Markdown(t *testing.T) {
	msg := dto.NewBuilder().Markdown("**bold**").Build()
	if msg.Type != dto.MarkdownMessage {
		t.Errorf("expected MarkdownMessage, got %d", msg.Type)
	}
	if msg.Markdown == nil || msg.Markdown.Content != "**bold**" {
		t.Errorf("unexpected markdown: %+v", msg.Markdown)
	}
}
func TestMessageBuilder_Ark(t *testing.T) {
	msg := dto.NewBuilder().Ark(23, nil).Build()
	if msg.Type != dto.ArkMessage {
		t.Errorf("expected ArkMessage")
	}
	if msg.Ark == nil || msg.Ark.TemplateID != 23 {
		t.Errorf("unexpected ark: %+v", msg.Ark)
	}
}
func TestMessageBuilder_Media(t *testing.T) {
	msg := dto.NewBuilder().Media("uuid1", "info1").Build()
	if msg.Type != dto.MediaMessage {
		t.Errorf("expected MediaMessage")
	}
}
func TestMessageBuilder_ReplyTo(t *testing.T) {
	msg := dto.NewBuilder().ReplyTo("orig-id").Text("reply").Build()
	if msg.MessageID != "orig-id" {
		t.Errorf("expected MessageID=orig-id, got %q", msg.MessageID)
	}
}
func TestMessageBuilder_WithEventID(t *testing.T) {
	msg := dto.NewBuilder().WithEventID("ev1").Text("x").Build()
	if msg.EventID != "ev1" {
		t.Errorf("expected EventID=ev1, got %q", msg.EventID)
	}
}
func TestMessageBuilder_WithSeq(t *testing.T) {
	msg := dto.NewBuilder().WithSeq(42).Text("x").Build()
	if msg.MessageSeq != 42 {
		t.Errorf("expected seq=42, got %d", msg.MessageSeq)
	}
}
func TestTextMsg(t *testing.T) {
	msg := dto.TextMsg("quick text")
	if msg.Content != "quick text" || msg.Type != dto.TextMessage {
		t.Errorf("TextMsg failed: %+v", msg)
	}
}
func TestMarkdownMsg(t *testing.T) {
	msg := dto.MarkdownMsg("# Title")
	if msg.Type != dto.MarkdownMessage || msg.Markdown == nil {
		t.Errorf("MarkdownMsg failed: %+v", msg)
	}
}
func TestArkCard(t *testing.T) {
	msg := dto.NewArkCard(23).KV("title", "Hello").KV("desc", "World").Build()
	if msg.Type != dto.ArkMessage {
		t.Errorf("expected ArkMessage")
	}
	if msg.Ark == nil || len(msg.Ark.KV) != 2 {
		t.Errorf("expected 2 KV pairs, got %+v", msg.Ark)
	}
}
