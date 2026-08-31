package config

// duration.go — 人类可读时长解析
//
// time.ParseDuration 不支持天（d）/周（w）单位，而 messagelog 的保留策略、
// 附件 GC 宽限期等字段以天为自然单位（如 grace_period: "7d"）。本文件提供
// ParseDuration 作为超集：先尝试标准解析，失败后再按 token 累加解析。

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// durationTokenRE 匹配时长 token：数字（可带小数）+ 单位。
// 单位覆盖 time.ParseDuration 全集（ns/us/µs/ms/s/m/h），并额外支持 d/w。
var durationTokenRE = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)(ns|us|µs|ms|s|m|h|d|w)`)

var durationUnits = map[string]time.Duration{
	"ns": time.Nanosecond,
	"us": time.Microsecond,
	"µs": time.Microsecond,
	"ms": time.Millisecond,
	"s":  time.Second,
	"m":  time.Minute,
	"h":  time.Hour,
	"d":  24 * time.Hour,
	"w":  7 * 24 * time.Hour,
}

// ParseDuration 解析人类可读的时长字符串。
//
// 在 time.ParseDuration 的基础上额外支持天（d）与周（w）单位，例如
// "7d"、"2w3d"、"7d12h"；其余语法与 time.ParseDuration 一致（复合单位
// 如 "1h30m"、小数如 "1.5h"）。空字符串返回 0 与 nil（未配置）。
func ParseDuration(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	rest := s
	var total time.Duration
	for {
		loc := durationTokenRE.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		n, err := strconv.ParseFloat(rest[loc[2]:loc[3]], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		total += time.Duration(n * float64(durationUnits[rest[loc[4]:loc[5]]]))
		rest = rest[loc[1]:]
	}
	if strings.TrimSpace(rest) != "" {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return total, nil
}
