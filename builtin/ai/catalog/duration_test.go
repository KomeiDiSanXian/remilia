package catalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemindDuration(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"30s", 30 * time.Second, true},
		{"30秒", 30 * time.Second, true},
		{"5m", 5 * time.Minute, true},
		{"5分钟", 5 * time.Minute, true},
		{"5分", 5 * time.Minute, true},
		{"1h", time.Hour, true},
		{"1小时", time.Hour, true},
		{"1时", time.Hour, true},
		{"2d", 48 * time.Hour, true},
		{"2天", 48 * time.Hour, true},
		{"2日", 48 * time.Hour, true},
		{"90sec", 90 * time.Second, true},
		{"10min", 10 * time.Minute, true},
		{"2hour", 2 * time.Hour, true},
		{"3days", 72 * time.Hour, true},
		{"5", 5 * time.Minute, true}, // 纯数字 = 分钟
		{"", 0, false},
		{"abc", 0, false},
		{"-5m", 0, false},
		{"0", 0, false},
		{"5x", 0, false},
		{"分钟", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseRemindDuration(tc.in)
			if !tc.ok {
				assert.Error(t, err, "expected error for %q", tc.in)
				return
			}
			require.NoError(t, err, "unexpected error for %q", tc.in)
			assert.Equal(t, tc.want, got, "duration for %q", tc.in)
		})
	}
}

func TestFormatRemindDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{5 * time.Minute, "5分钟"},
		{90 * time.Minute, "1小时30分钟"},
		{2 * 24 * time.Hour, "2天"},
		{45 * time.Second, "45秒"},
		{1*time.Hour + 5*time.Minute, "1小时5分钟"},
		{0, "0秒"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, FormatRemindDuration(tc.in), "duration %v", tc.in)
	}
}
