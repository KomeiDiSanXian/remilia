package plugin

import (
	"time"
)

// State 插件状态
type State int

const (
	Unloaded  State = iota // 未加载
	Loading                // 加载中
	Loaded                 // 已加载
	Error                  // 错误状态
	Reloading              // 重载中
)

// String 返回状态字符串
func (s State) String() string {
	switch s {
	case Unloaded:
		return "Unloaded"
	case Loading:
		return "Loading"
	case Loaded:
		return "Loaded"
	case Error:
		return "Error"
	case Reloading:
		return "Reloading"
	default:
		return "Unknown"
	}
}

// Status 插件状态信息
type Status struct {
	Name         string        // 插件名称
	State        State         // 当前状态
	LoadTime     time.Time     // 加载时间
	LastError    error         // 最后的错误
	MatcherCount int           // Matcher 数量
	Metadata     *Metadata     // 元数据
	Uptime       time.Duration // 运行时长
}
