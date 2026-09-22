// naming.go — 工具名规范：LLM API 对工具名的字符约束只有一处定义。
//
// 注册表在注册时校验并修正名称，命令发现（把真实命令包装成工具）复用同一套
// 规则，避免"命令包装器认为合法、注册表认为非法"这类不一致。
package toolkit

import (
	"regexp"
	"strings"
)

var (
	validToolNameRegex   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	invalidToolNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	multiUnderscore      = regexp.MustCompile(`_+`)
)

// ValidToolName 报告名称是否符合 AI API 的要求（仅含字母、数字、下划线、连字符）。
func ValidToolName(name string) bool {
	return validToolNameRegex.MatchString(name)
}

// SanitizeToolName 确保工具名称只包含 [a-zA-Z0-9_-]。
// 不符合的字符会被替换为下划线，连续多个下划线合并为一个。
func SanitizeToolName(name string) string {
	// 先替换所有非法字符为下划线
	result := invalidToolNameChars.ReplaceAllString(name, "_")
	// 合并连续下划线
	result = multiUnderscore.ReplaceAllString(result, "_")
	// 去掉首尾下划线
	result = strings.Trim(result, "_")
	if result == "" {
		result = "unknown_tool"
	}
	return result
}
