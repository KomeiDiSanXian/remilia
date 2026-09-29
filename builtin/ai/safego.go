// Package ai safego.go — 后台协程的 panic 兜底。
//
// 插件里的后台任务（记忆抽取、对话总结、定时提醒推送）跑在独立协程上，
// Go 中任意协程的未捕获 panic 都会终止整个进程。goSafe 统一兜底：把 panic
// 连同堆栈记入日志后吞掉，不让单个失败的后台任务拖垮整个 bot。
//
// 工具执行路径的同类兜底见 builtin/ai/runtime（单个工具调用的 panic 会被
// 转成该调用的失败结果，而不是中止整轮对话）。
package ai

import (
	"runtime/debug"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// goSafe 启动一个后台任务，并在其 panic 时兜底（记录堆栈）。label 用于日志
// 定位 panic 来源。
func goSafe(label string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[AI] panic in %s: %v\n%s", label, r, debug.Stack())
			}
		}()
		fn()
	}()
}
