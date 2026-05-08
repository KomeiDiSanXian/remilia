package engine

import "github.com/KomeiDiSanXian/remilia/core/context"

// SplitCommandPattern 将命令模式拆分为前缀和命令名。
//
// 已迁移至 [context.SplitCommandPattern]，此处保留为转发函数以保证向后兼容。
//
//go:fix inline
func SplitCommandPattern(pattern string) (prefix, name string) {
	return context.SplitCommandPattern(pattern)
}
