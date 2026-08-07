package updater

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// version 语义化版本号（SemVer 2.0.0 子集）。
//
// 仅支持 major.minor.patch[-prerelease] 形式，符合本仓库的发布规范
// （GitHub tag 形如 v1.30.0，可带 v 前缀）。比较规则遵循 SemVer：
//   - 数字段按数值比较
//   - 正式版 > 预发布版（1.0.0 > 1.0.0-rc.1）
//   - 预发布标识符按点分段比较：数字 < 字母数字；数字按数值、字母数字按字典序
type version struct {
	major, minor, patch int
	pre                 []string // 预发布标识符，空 = 正式版
	raw                 string
}

// parseVersion 解析语义化版本号，自动剥离 v/V 前缀。
func parseVersion(s string) (version, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if s == "" {
		return version{}, fmt.Errorf("empty version")
	}

	core := s
	var pre []string
	if before, after, ok := strings.Cut(s, "-"); ok {
		core = before
		pre = strings.Split(after, ".")
		if slices.Contains(pre, "") {
			return version{}, fmt.Errorf("invalid version %q: empty prerelease identifier", s)
		}
	}

	nums := strings.Split(core, ".")
	if len(nums) < 1 || len(nums) > 3 {
		return version{}, fmt.Errorf("invalid version %q: want major[.minor[.patch]]", s)
	}
	ints := make([]int, 3)
	for i, n := range nums {
		v, err := strconv.Atoi(n)
		if err != nil || v < 0 {
			return version{}, fmt.Errorf("invalid version %q: bad numeric segment %q", s, n)
		}
		ints[i] = v
	}

	return version{
		major: ints[0], minor: ints[1], patch: ints[2],
		pre: pre, raw: s,
	}, nil
}

// IsEmpty 报告版本号是否为解析失败产生的零值。
func (v version) IsEmpty() bool { return v.raw == "" }

// String 返回原始字符串（不含 v 前缀）。
func (v version) String() string { return v.raw }

// Compare 与 other 比较：v < other 返回负数，v == other 返回 0，v > other 返回正数。
func (v version) Compare(other version) int {
	if c := cmpInt(v.major, other.major); c != 0 {
		return c
	}
	if c := cmpInt(v.minor, other.minor); c != 0 {
		return c
	}
	if c := cmpInt(v.patch, other.patch); c != 0 {
		return c
	}
	return comparePre(v.pre, other.pre)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// comparePre 按 SemVer 规则比较预发布标识符列表（空列表 = 正式版，最大）。
func comparePre(a, b []string) int {
	// 正式版 > 预发布版
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1
	}
	if len(b) == 0 {
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := comparePreID(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a), len(b))
}

// comparePreID 比较单个预发布标识符。
func comparePreID(a, b string) int {
	aNum, aErr := strconv.Atoi(a)
	bNum, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return cmpInt(aNum, bNum)
	case aErr == nil:
		return -1 // 数字 < 字母数字
	case bErr == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}
