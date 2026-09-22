// safety.go — 命令通道的参数安全校验与权限串解析。

package execution

import (
	"strings"
	"unicode"
)

// IsSafeCommandArg 校验 LLM 生成的命令参数是否安全。
// 只允许可打印的 ASCII 字符（含空格和 Tab），禁止控制字符和常见的 shell 注入字符。
func IsSafeCommandArg(s string) bool {
	if len(s) > 4096 {
		return false
	}
	for _, r := range s {
		if r == '\t' {
			continue
		}
		if !unicode.IsPrint(r) {
			return false
		}
		if r > 0x7E {
			return false
		}
	}
	return true
}

// ParseToolPermission 解析工具权限字符串（与框架 parsePermission 同语义）。
// 支持 "resource.action" / "resource:action" / "resource"（action 通配）以及 "*"。
func ParseToolPermission(perm string) (resource, action string) {
	if idx := strings.Index(perm, ":"); idx > 0 {
		return perm[:idx], perm[idx+1:]
	}
	if idx := strings.LastIndex(perm, "."); idx > 0 {
		return perm[:idx], perm[idx+1:]
	}
	if perm == "*" {
		return "*", "*"
	}
	return perm, "*"
}
