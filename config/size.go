package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSize 解析人类可读的字节大小字符串，如 "500MB"、"1GB"、"20MB"、"1024"。
// 支持后缀 B / KB / MB / GB / TB（不区分大小写）；无后缀时按字节解析。
// 返回 -1 表示未配置（空字符串），解析失败返回错误。
func ParseSize(s string) (int64, error) {
	if s == "" {
		return -1, nil
	}
	orig := s
	s = strings.TrimSpace(s)
	upper := strings.ToUpper(s)
	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	}
	for _, m := range multipliers {
		if strings.HasSuffix(upper, m.suffix) {
			numStr := strings.TrimSpace(s[:len(s)-len(m.suffix)])
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil || n < 0 {
				return 0, fmt.Errorf("invalid size %q", orig)
			}
			return n * m.mult, nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid size %q", orig)
	}
	return n, nil
}
