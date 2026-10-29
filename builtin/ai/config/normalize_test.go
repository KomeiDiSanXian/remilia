package config

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
)

// loadWith 用给定 key/value 构造一次配置读取。
func loadWith(t *testing.T, values map[string]any) *Config {
	t.Helper()
	ctx := plugintest.NewSetupContext("ai", &plugintest.SetupOptions{Config: &mockConfig{values: values}})
	defer plugintest.StopSetupContext(ctx)
	return Load(ctx)
}

// TestProviderIsCaseInsensitive 守住提供商名规范化。
//
// newProvider 只认小写名（openai/anthropic），配置层此前不做规范化，
// 于是 provider: "OpenAI" 会在装配期报 unknown provider——一个纯拼写问题
// 被推迟成了启动失败。
func TestProviderIsCaseInsensitive(t *testing.T) {
	for _, in := range []string{"OpenAI", "openai", "  OPENAI  ", "Anthropic", "ANTHROPIC"} {
		cfg := loadWith(t, map[string]any{"provider": in})
		if !ValidProvider(cfg.Provider) {
			t.Errorf("provider %q normalized to %q, which is not a valid provider", in, cfg.Provider)
		}
	}
}

// TestProviderUnknownKeepsDefault 守住非法提供商不静默生效。
func TestProviderUnknownKeepsDefault(t *testing.T) {
	cfg := loadWith(t, map[string]any{"provider": "gemini"})
	if cfg.Provider != DefaultConfig.Provider {
		t.Errorf("unknown provider must keep default %q, got %q", DefaultConfig.Provider, cfg.Provider)
	}
}

// TestExplicitZeroValues 守住"0 值有语义"的配置键。
//
// 这些键写 0 是意图表达（关闭某项能力）；历史上它们被当成"未配置"而静默忽略，
// 用户配了却没有任何效果。load.go 里用 present() 统一区分两种语义。
func TestExplicitZeroValues(t *testing.T) {
	cfg := loadWith(t, map[string]any{
		"trigger_cmd":            "",
		"image_merge_window":     "0s",
		"image_context_window":   "0s",
		"context_group_messages": 0,
		"session_retention":      "0s",
	})

	if cfg.TriggerCmd != "" {
		t.Errorf("trigger_cmd: \"\" must disable command triggering, got %q", cfg.TriggerCmd)
	}
	if cfg.ImageMergeWindow != 0 {
		t.Errorf("image_merge_window: 0s must disable merging, got %v", cfg.ImageMergeWindow)
	}
	if cfg.ImageContextWindow != 0 {
		t.Errorf("image_context_window: 0s must disable the window, got %v", cfg.ImageContextWindow)
	}
	if cfg.ContextGroupMessages != 0 {
		t.Errorf("context_group_messages: 0 must disable the group window, got %d", cfg.ContextGroupMessages)
	}
	if cfg.SessionRetention != 0 {
		t.Errorf("session_retention: 0s must mean unlimited, got %v", cfg.SessionRetention)
	}
}

// TestOmittedZeroValuedKeysKeepDefaults 守住"未配置时回落默认"。
//
// 与上一个用例成对：同一批键在没出现时必须保持默认，不能因为改成存在性判断
// 就把默认值一起丢掉。
func TestOmittedZeroValuedKeysKeepDefaults(t *testing.T) {
	cfg := loadWith(t, map[string]any{})
	if cfg.TriggerCmd != DefaultConfig.TriggerCmd {
		t.Errorf("TriggerCmd = %q, want default %q", cfg.TriggerCmd, DefaultConfig.TriggerCmd)
	}
	if cfg.ImageMergeWindow != DefaultConfig.ImageMergeWindow {
		t.Errorf("ImageMergeWindow = %v, want default %v", cfg.ImageMergeWindow, DefaultConfig.ImageMergeWindow)
	}
	if cfg.ContextGroupMessages != DefaultConfig.ContextGroupMessages {
		t.Errorf("ContextGroupMessages = %d, want default %d", cfg.ContextGroupMessages, DefaultConfig.ContextGroupMessages)
	}
	if cfg.SessionRetention != DefaultConfig.SessionRetention {
		t.Errorf("SessionRetention = %v, want default %v", cfg.SessionRetention, DefaultConfig.SessionRetention)
	}
}

// TestStringListParsing 守住列表配置的解析口径。
func TestStringListParsing(t *testing.T) {
	cfg := loadWith(t, map[string]any{
		"tool_allowlist":  []any{"ping", "", "  ", "echo"},
		"workspace_roots": []any{"  /srv/a  ", "/srv/b", ""},
		"exec_allowlist":  []any{"git", " go "},
	})

	wantAllow := []string{"ping", "echo"}
	if !equalStrings(cfg.ToolAllowlist, wantAllow) {
		t.Errorf("ToolAllowlist = %v, want %v", cfg.ToolAllowlist, wantAllow)
	}
	wantRoots := []string{"/srv/a", "/srv/b"}
	if !equalStrings(cfg.WorkspaceRoots, wantRoots) {
		t.Errorf("WorkspaceRoots = %v, want %v", cfg.WorkspaceRoots, wantRoots)
	}
	wantExec := []string{"git", "go"}
	if !equalStrings(cfg.ExecAllowlist, wantExec) {
		t.Errorf("ExecAllowlist = %v, want %v", cfg.ExecAllowlist, wantExec)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
