package ai

import (
	"testing"
	"time"
)

func TestDefaultConfigValues(t *testing.T) {
	cfg := DefaultConfig
	if cfg.Provider != "openai" {
		t.Errorf("Provider: expected %q, got %q", "openai", cfg.Provider)
	}
	if cfg.Model != "gpt-4o-mini" {
		t.Errorf("Model: expected %q, got %q", "gpt-4o-mini", cfg.Model)
	}
	if cfg.MaxTokens != 2048 {
		t.Errorf("MaxTokens: expected %d, got %d", 2048, cfg.MaxTokens)
	}
	if cfg.MaxDepth != 5 {
		t.Errorf("MaxDepth: expected %d, got %d", 5, cfg.MaxDepth)
	}
	if cfg.MaxHistory != 20 {
		t.Errorf("MaxHistory: expected %d, got %d", 20, cfg.MaxHistory)
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("Temperature: expected %f, got %f", 0.7, cfg.Temperature)
	}
	if cfg.TopP != 1.0 {
		t.Errorf("TopP: expected %f, got %f", 1.0, cfg.TopP)
	}
	if cfg.APITimeout != 60*time.Second {
		t.Errorf("APITimeout: expected %v, got %v", 60*time.Second, cfg.APITimeout)
	}
	if cfg.MaxRetries != 0 {
		t.Errorf("MaxRetries: expected %d, got %d", 0, cfg.MaxRetries)
	}
	if cfg.ToolTimeout != 30*time.Second {
		t.Errorf("ToolTimeout: expected %v, got %v", 30*time.Second, cfg.ToolTimeout)
	}
	if cfg.SessionTTL != 24*time.Hour {
		t.Errorf("SessionTTL: expected %v, got %v", 24*time.Hour, cfg.SessionTTL)
	}
	if cfg.SystemPrompt != "你是 Remilia Bot 的 AI 助手。" {
		t.Errorf("SystemPrompt: unexpected value: %q", cfg.SystemPrompt)
	}
	if cfg.TriggerCmd != "/ai" {
		t.Errorf("TriggerCmd: expected %q, got %q", "/ai", cfg.TriggerCmd)
	}
	if !cfg.AtBot {
		t.Error("AtBot should be true by default")
	}
	if !cfg.PrivateChat {
		t.Error("PrivateChat should be true by default")
	}
	if !cfg.Markdown {
		t.Error("Markdown should be true by default")
	}
	if cfg.Fallback {
		t.Error("Fallback should be false by default")
	}
	if cfg.SkillTimeout != 60*time.Second {
		t.Errorf("SkillTimeout: expected %v, got %v", 60*time.Second, cfg.SkillTimeout)
	}
	if cfg.SkillMaxDepth != 3 {
		t.Errorf("SkillMaxDepth: expected %d, got %d", 3, cfg.SkillMaxDepth)
	}
	if !cfg.VisionEnabled {
		t.Error("VisionEnabled should be true by default")
	}
	if cfg.AudioEnabled {
		t.Error("AudioEnabled should be false by default")
	}
	if cfg.MaxAttachmentSize != 20*1024*1024 {
		t.Errorf("MaxAttachmentSize: expected %d, got %d", 20*1024*1024, cfg.MaxAttachmentSize)
	}
	if cfg.MaxUserSkills != 10 {
		t.Errorf("MaxUserSkills: expected %d, got %d", 10, cfg.MaxUserSkills)
	}
	if cfg.MaxUserSkillPromptLen != 2000 {
		t.Errorf("MaxUserSkillPromptLen: expected %d, got %d", 2000, cfg.MaxUserSkillPromptLen)
	}
}
