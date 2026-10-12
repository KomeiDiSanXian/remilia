// safety.go — 命令通道的参数安全校验与权限串解析。

package execution

import (
	"strings"
	"unicode"
)

// IsSafeCommandArg 校验 LLM 生成的命令参数是否安全。
// 允许可打印字符（含中文等非 ASCII 可打印字符，以及空格和 Tab），只禁止
// 控制字符（防换行/转义逃逸出参数域）；长度上限防超长输入。
//
// 命令重放不经过 shell：参数以纯文本拼进合成事件内容，由框架命令解析器
// 分词，可打印字符不会引入额外注入面。此前一刀切拒绝非 ASCII 会导致中文
// bot 的命令参数（如 "/todo add 买咖啡"）全部静默丢失。
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
