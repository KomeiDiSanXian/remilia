package ai

import (
	"slices"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
)

func TestLoadConfigNil(t *testing.T) {
	ctx := plugintest.NewSetupContext("ai", nil)
	defer plugintest.StopSetupContext(ctx)

	cfg := loadConfig(ctx)
	if cfg == nil {
		t.Fatal("loadConfig returned nil")
	}
	if cfg.Provider != DefaultConfig.Provider {
		t.Errorf("expected default provider %q, got %q", DefaultConfig.Provider, cfg.Provider)
	}
}

func TestLoadConfigCustomValues(t *testing.T) {
	cfg := &mockConfig{
		values: map[string]any{
			"provider":                  "anthropic",
			"model":                     "claude-sonnet-4-20250514",
			"base_url":                  "https://api.anthropic.com",
			"api_key":                   "sk-test",
			"max_tokens":                4096,
			"max_depth":                 10,
			"max_history":               50,
			"temperature":               float64(0.5),
			"top_p":                     float64(0.9),
			"api_timeout":               "120s",
			"max_retries":               3,
			"tool_timeout":              "60s",
			"session_ttl":               "12h",
			"system_prompt":             "You are a test bot",
			"trigger_cmd":               "/ask",
			"at_bot":                    true,
			"private_chat":              false,
			"markdown":                  false,
			"fallback":                  true,
			"skill_timeout":             "120s",
			"skill_max_depth":           5,
			"max_attachment_size":       10485760,
			"vision_enabled":            false,
			"audio_enabled":             true,
			"max_user_skills":           20,
			"max_user_skill_prompt_len": 4000,
		},
	}
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	result := loadConfig(ctx)
	if result.Provider != "anthropic" {
		t.Errorf("expected provider %q, got %q", "anthropic", result.Provider)
	}
	if result.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model %q, got %q", "claude-sonnet-4-20250514", result.Model)
	}
	if result.MaxTokens != 4096 {
		t.Errorf("expected MaxTokens 4096, got %d", result.MaxTokens)
	}
	if result.MaxDepth != 10 {
		t.Errorf("expected MaxDepth 10, got %d", result.MaxDepth)
	}
	if result.Temperature != 0.5 {
		t.Errorf("expected Temperature 0.5, got %f", result.Temperature)
	}
	if result.APITimeout != 120*time.Second {
		t.Errorf("expected APITimeout 120s, got %v", result.APITimeout)
	}
	if result.MaxRetries != 3 {
		t.Errorf("expected MaxRetries 3, got %d", result.MaxRetries)
	}
	if !result.AtBot {
		t.Error("expected AtBot true")
	}
	if result.PrivateChat {
		t.Error("expected PrivateChat false")
	}
	if result.Markdown {
		t.Error("expected Markdown false")
	}
	if !result.Fallback {
		t.Error("expected Fallback true")
	}
	if result.VisionEnabled {
		t.Error("expected VisionEnabled false")
	}
	if !result.AudioEnabled {
		t.Error("expected AudioEnabled true")
	}
	if result.MaxUserSkills != 20 {
		t.Errorf("expected MaxUserSkills 20, got %d", result.MaxUserSkills)
	}
}

func TestLoadConfigToolAllowlist(t *testing.T) {
	cfg := &mockConfig{
		values: map[string]any{
			"tool_allowlist": []any{"ping", "help", "status"},
		},
	}
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	result := loadConfig(ctx)
	if len(result.ToolAllowlist) != 3 {
		t.Errorf("expected 3 tools in allowlist, got %d: %v", len(result.ToolAllowlist), result.ToolAllowlist)
	}
}

func TestLoadConfigContextPrivacyOptions(t *testing.T) {
	cfg := &mockConfig{
		values: map[string]any{
			"include_runtime_context": false,
			"include_mention_info":    false,
			"context_fields":          []any{"time", "platform", "user_name"},
		},
	}
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	result := loadConfig(ctx)
	if result.IncludeRuntimeContext {
		t.Error("expected include_runtime_context false")
	}
	if result.IncludeMentionInfo {
		t.Error("expected include_mention_info false")
	}
	if len(result.ContextFields) != 3 {
		t.Fatalf("expected 3 context fields, got %d: %v", len(result.ContextFields), result.ContextFields)
	}
	for _, f := range []string{"time", "platform", "user_name"} {
		found := slices.Contains(result.ContextFields, f)
		if !found {
			t.Errorf("expected context field %q in %v", f, result.ContextFields)
		}
	}
}

func TestLoadConfigContextPrivacyDefaults(t *testing.T) {
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: &mockConfig{values: map[string]any{}}})
	defer plugintest.StopSetupContext(ctx)

	result := loadConfig(ctx)
	if !result.IncludeRuntimeContext {
		t.Error("expected include_runtime_context default true")
	}
	if !result.IncludeMentionInfo {
		t.Error("expected include_mention_info default true")
	}
	if len(result.ContextFields) != 0 {
		t.Errorf("expected context_fields default empty (all fields), got %v", result.ContextFields)
	}
}

func TestLoadConfigReplyAndGroupContext(t *testing.T) {
	cfg := &mockConfig{
		values: map[string]any{
			"include_reply_context":      false,
			"context_group_messages":     15,
			"context_group_include_bot": true,
		},
	}
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	result := loadConfig(ctx)
	if result.IncludeReplyContext {
		t.Error("expected include_reply_context false")
	}
	if result.ContextGroupMessages != 15 {
		t.Errorf("expected context_group_messages 15, got %d", result.ContextGroupMessages)
	}
	if !result.ContextGroupIncludeBot {
		t.Error("expected context_group_include_bot true")
	}
}

func TestLoadConfigReplyAndGroupContextDefaults(t *testing.T) {
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: &mockConfig{values: map[string]any{}}})
	defer plugintest.StopSetupContext(ctx)

	result := loadConfig(ctx)
	if !result.IncludeReplyContext {
		t.Error("expected include_reply_context default true")
	}
	if result.ContextGroupMessages != 10 {
		t.Errorf("expected context_group_messages default 10, got %d", result.ContextGroupMessages)
	}
	if result.ContextGroupIncludeBot {
		t.Error("expected context_group_include_bot default false")
	}
}

func TestLoadConfigInvalidTemperature(t *testing.T) {
	cfg := &mockConfig{
		values: map[string]any{
			"temperature": float64(3.0),
		},
	}
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	result := loadConfig(ctx)
	if result.Temperature == 3.0 {
		t.Error("expected invalid temperature to be ignored")
	}
}

func TestLoadConfigInvalidTopP(t *testing.T) {
	cfg := &mockConfig{
		values: map[string]any{
			"top_p": float64(-1),
		},
	}
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	result := loadConfig(ctx)
	if result.TopP == -1 {
		t.Error("expected invalid top_p to be ignored")
	}
}

func TestLoadConfigNoTrigger(t *testing.T) {
	cfg := &mockConfig{
		values: map[string]any{},
	}
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: cfg})
	defer plugintest.StopSetupContext(ctx)

	result := loadConfig(ctx)
	if result.TriggerCmd == "" {
		t.Error("expected trigger_cmd default to '/ai'")
	}
	if result.AtBot != DefaultConfig.AtBot {
		t.Error("expected AtBot to use default when not overridden")
	}
}
