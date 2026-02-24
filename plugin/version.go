package plugin

import (
	"fmt"
	"strconv"
	"strings"
)

// depSpec 解析后的依赖规格
type depSpec struct {
	name       string // 插件名
	constraint string // 版本约束（如 ">=1.2.0", "^2.0.0", "~1.5.0"，空串=不限制）
}

// parseDepSpec 解析依赖规格字符串
//
// 格式：
//   - "storage"          → {name:"storage", constraint:""}
//   - "permission@>=3.0" → {name:"permission", constraint:">=3.0"}
//   - "cache@^2.1.0"     → {name:"cache", constraint:"^2.1.0"}
//   - "acl@~1.5"         → {name:"acl", constraint:"~1.5"}
func parseDepSpec(dep string) depSpec {
	parts := strings.SplitN(dep, "@", 2)
	if len(parts) == 1 {
		return depSpec{name: parts[0]}
	}
	return depSpec{name: parts[0], constraint: parts[1]}
}

// semver 简单的语义版本号，支持 major.minor.patch（patch 可选）
type semver struct {
	major, minor, patch int
	raw                 string
}

// parseSemver 解析版本字符串（忽略 v 前缀，忽略预发布标签）
func parseSemver(v string) (semver, error) {
	v = strings.TrimPrefix(v, "v")
	// 去掉预发布部分（如 "1.2.3-beta" → "1.2.3"）
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return semver{}, fmt.Errorf("invalid semver: %q", v)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semver major in %q: %w", v, err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semver minor in %q: %w", v, err)
	}
	patch := 0
	if len(parts) == 3 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil {
			return semver{}, fmt.Errorf("invalid semver patch in %q: %w", v, err)
		}
	}
	return semver{major: major, minor: minor, patch: patch, raw: v}, nil
}

// compare 比较两个版本：返回 -1 (a<b), 0 (a==b), 1 (a>b)
func (a semver) compare(b semver) int {
	if a.major != b.major {
		if a.major < b.major {
			return -1
		}
		return 1
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1
		}
		return 1
	}
	if a.patch != b.patch {
		if a.patch < b.patch {
			return -1
		}
		return 1
	}
	return 0
}

// checkVersionConstraint 检查 have 版本是否满足 constraint 约束
//
// 支持的约束格式：
//   - ">=1.2.0"  — 大于等于
//   - ">1.2.0"   — 大于
//   - "<=1.2.0"  — 小于等于
//   - "<1.2.0"   — 小于
//   - "=1.2.0"   — 等于（也可写 "==1.2.0"）
//   - "^2.1.0"   — 兼容：主版本相同，>=2.1.0
//   - "~1.5.0"   — 补丁兼容：主次版本相同，>=1.5.0
//   - ""         — 不限制，始终通过
//
// 若 have 或 constraint 版本格式不合法，返回 false 和错误信息。
func checkVersionConstraint(have, constraint string) (bool, error) {
	if constraint == "" {
		return true, nil
	}
	if have == "" {
		// 插件未提供版本号，无法检查
		return false, fmt.Errorf("plugin has no version; cannot verify constraint %q", constraint)
	}

	haveVer, err := parseSemver(have)
	if err != nil {
		return false, fmt.Errorf("installed plugin has invalid version %q: %w", have, err)
	}

	var op, rawVer string
	switch {
	case strings.HasPrefix(constraint, ">="):
		op, rawVer = ">=", constraint[2:]
	case strings.HasPrefix(constraint, ">"):
		op, rawVer = ">", constraint[1:]
	case strings.HasPrefix(constraint, "<="):
		op, rawVer = "<=", constraint[2:]
	case strings.HasPrefix(constraint, "<"):
		op, rawVer = "<", constraint[1:]
	case strings.HasPrefix(constraint, "=="):
		op, rawVer = "=", constraint[2:]
	case strings.HasPrefix(constraint, "="):
		op, rawVer = "=", constraint[1:]
	case strings.HasPrefix(constraint, "^"):
		op, rawVer = "^", constraint[1:]
	case strings.HasPrefix(constraint, "~"):
		op, rawVer = "~", constraint[1:]
	default:
		// 裸版本号视为 "="
		op, rawVer = "=", constraint
	}

	reqVer, err := parseSemver(rawVer)
	if err != nil {
		return false, fmt.Errorf("invalid constraint version %q in constraint %q: %w", rawVer, constraint, err)
	}

	cmp := haveVer.compare(reqVer)
	switch op {
	case ">=":
		return cmp >= 0, nil
	case ">":
		return cmp > 0, nil
	case "<=":
		return cmp <= 0, nil
	case "<":
		return cmp < 0, nil
	case "=":
		return cmp == 0, nil
	case "^":
		// 主版本相同，且 have >= req
		return haveVer.major == reqVer.major && cmp >= 0, nil
	case "~":
		// 主次版本相同，且 have >= req
		return haveVer.major == reqVer.major && haveVer.minor == reqVer.minor && cmp >= 0, nil
	default:
		return false, fmt.Errorf("unknown constraint operator: %q", op)
	}
}
