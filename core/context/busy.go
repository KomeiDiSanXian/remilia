package context

import "errors"

// ErrBusy 表示事件被准入/限流中间件拒绝（本地类已满，或 I/O 类队列已满）。
//
// 它与业务 handler 的错误语义不同：被拒绝的事件**没有执行任何业务代码**，
// 因此引擎不会把它记成 handler 错误，而是调用该 matcher 通过
// [engine.Matcher.OnReject] 注册的「忙时处理器」（如果有），
// 由插件决定文案（例如 AI 回"当前对话较多，请稍后再试"）。
//
// 中间件直接返回本值即可（可用 [IsBusy] 判断），无需感知具体实现包。
var ErrBusy = errors.New("event rejected: busy")

// IsBusy 报告错误是否表示事件被准入/限流拒绝。
func IsBusy(err error) bool { return errors.Is(err, ErrBusy) }
