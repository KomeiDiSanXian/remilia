package config

import (
	"testing"
	"time"
)

func TestWebhookConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  WebhookConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with dedup enabled",
			config: WebhookConfig{
				EventBuffer:      1024,
				DedupEnable:      true,
				Shards:           1024,
				LifeWindow:       "5m",
				CleanWindow:      "1m",
				MaxEntrySize:     4096,
				HardMaxCacheSize: 100,
			},
			wantErr: false,
		},
		{
			name: "valid config with dedup disabled",
			config: WebhookConfig{
				EventBuffer: 1024,
				DedupEnable: false,
			},
			wantErr: false,
		},
		{
			name: "negative event buffer",
			config: WebhookConfig{
				EventBuffer: -1,
				DedupEnable: false,
			},
			wantErr: true,
			errMsg:  "event_buffer must be >= 0",
		},
		{
			name: "negative shards",
			config: WebhookConfig{
				EventBuffer: 1024,
				DedupEnable: true,
				Shards:      -1,
			},
			wantErr: true,
			errMsg:  "dedup_shards must be >= 0",
		},
		{
			name: "invalid life window duration",
			config: WebhookConfig{
				EventBuffer: 1024,
				DedupEnable: true,
				Shards:      1024,
				LifeWindow:  "invalid",
			},
			wantErr: true,
			errMsg:  "dedup_life_window is not a valid duration",
		},
		{
			name: "invalid clean window duration",
			config: WebhookConfig{
				EventBuffer: 1024,
				DedupEnable: true,
				Shards:      1024,
				LifeWindow:  "5m",
				CleanWindow: "not-a-duration",
			},
			wantErr: true,
			errMsg:  "dedup_clean_window is not a valid duration",
		},
		{
			name: "negative max entry size",
			config: WebhookConfig{
				EventBuffer:  1024,
				DedupEnable:  true,
				Shards:       1024,
				LifeWindow:   "5m",
				CleanWindow:  "1m",
				MaxEntrySize: -1,
			},
			wantErr: true,
			errMsg:  "dedup_max_entry_size must be >= 0",
		},
		{
			name: "negative hard max cache size",
			config: WebhookConfig{
				EventBuffer:      1024,
				DedupEnable:      true,
				Shards:           1024,
				LifeWindow:       "5m",
				CleanWindow:      "1m",
				MaxEntrySize:     4096,
				HardMaxCacheSize: -1,
			},
			wantErr: true,
			errMsg:  "dedup_hard_max_size must be >= 0",
		},
		{
			name: "valid config with all duration formats",
			config: WebhookConfig{
				EventBuffer:      1024,
				DedupEnable:      true,
				Shards:           2048,
				LifeWindow:       "10m",
				CleanWindow:      "30s",
				MaxEntrySize:     8192,
				HardMaxCacheSize: 200,
			},
			wantErr: false,
		},
		{
			name: "zero values allowed when dedup disabled",
			config: WebhookConfig{
				EventBuffer: 0,
				DedupEnable: false,
				Shards:      0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("WebhookConfig.Validate() expected error but got none")
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("WebhookConfig.Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("WebhookConfig.Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestWebhookConfig_ParseDurations(t *testing.T) {
	tests := []struct {
		name        string
		lifeWindow  string
		cleanWindow string
		wantLife    time.Duration
		wantClean   time.Duration
	}{
		{
			name:        "standard durations",
			lifeWindow:  "5m",
			cleanWindow: "1m",
			wantLife:    5 * time.Minute,
			wantClean:   1 * time.Minute,
		},
		{
			name:        "seconds format",
			lifeWindow:  "300s",
			cleanWindow: "60s",
			wantLife:    300 * time.Second,
			wantClean:   60 * time.Second,
		},
		{
			name:        "hours format",
			lifeWindow:  "1h",
			cleanWindow: "5m",
			wantLife:    1 * time.Hour,
			wantClean:   5 * time.Minute,
		},
		{
			name:        "mixed format",
			lifeWindow:  "1h30m",
			cleanWindow: "2m30s",
			wantLife:    90 * time.Minute,
			wantClean:   150 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := WebhookConfig{
				DedupEnable: true,
				LifeWindow:  tt.lifeWindow,
				CleanWindow: tt.cleanWindow,
			}

			// Validate should pass
			if err := config.Validate(); err != nil {
				t.Fatalf("WebhookConfig.Validate() unexpected error = %v", err)
			}

			// Parse and verify durations
			life, err := time.ParseDuration(config.LifeWindow)
			if err != nil {
				t.Errorf("ParseDuration(LifeWindow) error = %v", err)
			}
			if life != tt.wantLife {
				t.Errorf("LifeWindow = %v, want %v", life, tt.wantLife)
			}

			clean, err := time.ParseDuration(config.CleanWindow)
			if err != nil {
				t.Errorf("ParseDuration(CleanWindow) error = %v", err)
			}
			if clean != tt.wantClean {
				t.Errorf("CleanWindow = %v, want %v", clean, tt.wantClean)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
