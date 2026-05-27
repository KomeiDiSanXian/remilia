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
	Unloading              // 卸载中
	Error                  // 错误状态
	Reloading              // 重载中
	Disabled               // 已禁用（Matcher 已暂停，Container 条目保留）
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
	case Unloading:
		return "Unloading"
	case Error:
		return "Error"
	case Reloading:
		return "Reloading"
	case Disabled:
		return "Disabled"
	default:
		return "Unknown"
	}
}

// Status 插件状态信息
type Status struct {
	Name                  string        // 插件名称
	State                 State         // 当前状态
	LoadTime              time.Time     // 加载时间
	LastError             error         // 最后的错误
	MatcherCount          int           // Matcher 数量
	Metadata              *Metadata     // 元数据
	Uptime                time.Duration // 运行时长
	HasSaveState          bool          // 是否实现了状态保存/恢复（SaveState != nil）
	EventBusSubscriptions int           // EventBus 中的订阅数（当前总订阅数快照）
	GoroutineCount        int           // 当前活跃的生命周期绑定 goroutine 数量（ctx.Spawn/SpawnNamed 启动的）
}
