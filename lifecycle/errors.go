package lifecycle

import "fmt"

// StartError 表示组件启动失败的错误
type StartError struct {
	Component string
	Phase     string
	Err       error
}

func (e *StartError) Error() string {
	return fmt.Sprintf("Lifecycle: component '%s' %s failed: %v", e.Component, e.Phase, e.Err)
}

func (e *StartError) Unwrap() error {
	return e.Err
}

// StopError 表示组件停止失败的错误
type StopError struct {
	Err error
}

func (e *StopError) Error() string {
	return fmt.Sprintf("Lifecycle: stop failed: %v", e.Err)
}

func (e *StopError) Unwrap() error {
	return e.Err
}

// ErrInvalidState 表示无效的状态转换
type ErrInvalidState struct {
	Current  State
	Expected State
}

func (e ErrInvalidState) Error() string {
	return fmt.Sprintf("Lifecycle: invalid state: current=%s, expected=%s", e.Current, e.Expected)
}
