// duration.go — 提醒时长的解析与格式化。
//
// 动作侧（set_reminder / list_reminders）与子命令侧（/ai remind）共用同一套
// 文本约定，因此放在动作目录层，两边都只依赖这里。
package catalog

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseRemindDuration 解析提醒时长字符串。
//
// 支持：
//   - 英文缩写：30s、5m、1h、2d（及 30sec/5min/1hour/2days）
//   - 中文：30秒、5分钟、5分、1小时、1时、2天、2日
//   - 纯数字：视为分钟（"5" → 5 分钟）
func ParseRemindDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// 纯数字 → 分钟
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("duration must be positive: %q", s)
		}
		return time.Duration(n) * time.Minute, nil
	}

	type unit struct {
		names  []string
		amount time.Duration
	}
	units := []unit{
		{[]string{"天", "日", "d", "days", "day"}, 24 * time.Hour},
		{[]string{"小时", "时", "h", "hours", "hour"}, time.Hour},
		{[]string{"分钟", "分", "m", "mins", "min", "minutes", "minute"}, time.Minute},
		{[]string{"秒", "s", "sec", "secs", "seconds", "second"}, time.Second},
	}
	for _, u := range units {
		for _, name := range u.names {
			if before, ok := strings.CutSuffix(s, name); ok {
				numStr := before
				n, err := strconv.Atoi(numStr)
				if err != nil || n <= 0 {
					return 0, fmt.Errorf("invalid duration: %q", s)
				}
				return time.Duration(n) * u.amount, nil
			}
		}
	}
	return 0, fmt.Errorf("unsupported duration format: %q", s)
}

// FormatRemindDuration 格式化剩余时长（"5分钟"、"1小时30分"、"2天"）。
func FormatRemindDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)
	d -= time.Duration(mins) * time.Minute
	secs := int(d / time.Second)

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d天", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d小时", hours))
	}
	if mins > 0 {
		parts = append(parts, fmt.Sprintf("%d分钟", mins))
	}
	if secs > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d秒", secs))
	}
	return strings.Join(parts, "")
}
