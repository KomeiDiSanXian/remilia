// Package buildinfo 保存构建期注入的版本信息（Git commit 与构建时间）。
//
// 这组信息由构建脚本通过 -ldflags -X 写入 main 包的变量（见 Makefile 与
// .goreleaser.yaml），再由宿主程序在启动时调用 Set 注入到本包。
//
// 本包位于 infra 层且不依赖仓库内任何其他包，因此 builtin、api 等上层组件
// 都可以直接引用它，而不需要为了读取构建信息而互相依赖。
package buildinfo

import "sync/atomic"

// Info 构建期注入的版本信息。
type Info struct {
	// Commit 是 Git 提交短哈希。
	Commit string
	// Date 是构建时间。
	Date string
}

// current 保存当前构建信息。
//
// 用 atomic.Value 而非裸变量：注入发生在启动期，读取可能来自任意
// goroutine（HTTP handler、命令处理），避免 -race 下出现数据竞争。
var current atomic.Value

// Set 保存构建期注入的信息。
func Set(commit, date string) {
	current.Store(Info{Commit: commit, Date: date})
}

// Get 返回构建期注入的信息。
// 未经 Set 调用时两个返回值均为空字符串。
func Get() (commit, date string) {
	if v, ok := current.Load().(Info); ok {
		return v.Commit, v.Date
	}
	return "", ""
}
