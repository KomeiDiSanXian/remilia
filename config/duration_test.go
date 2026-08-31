package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"empty", "", 0, false},
		{"standard", "1h30m", 90 * time.Minute, false},
		{"fractional", "1.5h", 90 * time.Minute, false},
		{"milliseconds", "500ms", 500 * time.Millisecond, false},
		{"day", "7d", 7 * 24 * time.Hour, false},
		{"day fractional", "1.5d", 36 * time.Hour, false},
		{"week", "2w", 14 * 24 * time.Hour, false},
		{"compound days", "7d12h", 180 * time.Hour, false},
		{"compound weeks and days", "2w3d", 17 * 24 * time.Hour, false},
		{"negative standard", "-1h", -time.Hour, false},
		{"garbage", "abc", 0, true},
		{"trailing garbage", "1d abc", 0, true},
		{"missing unit", "30", 0, true},
		{"unknown uppercase day", "1D", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDuration(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestParseDurationMatchesTimeParseDuration 验证标准语法与 time.ParseDuration 一致，
// 防止扩展解析在标准输入上行为漂移。
func TestParseDurationMatchesTimeParseDuration(t *testing.T) {
	for _, in := range []string{"1h30m", "2h", "500ms", "1.5h", "90s", "30m", "1us", "1µs", "1ns"} {
		want, wantErr := time.ParseDuration(in)
		got, err := ParseDuration(in)
		if wantErr != nil {
			require.Error(t, err, in)
			continue
		}
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}
