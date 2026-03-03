package lifecycle

// State 表示生命周期状态
type State int

const (
	// StateCreated 组件已创建但未启动
	StateCreated State = iota
	// StateStarting 组件正在启动
	StateStarting
	// StateRunning 组件正在运行
	StateRunning
	// StateStopping 组件正在停止
	StateStopping
	// StateStopped 组件已停止
	StateStopped
)

// String 返回状态的字符串表示
func (s State) String() string {
	switch s {
	case StateCreated:
		return "created"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}
