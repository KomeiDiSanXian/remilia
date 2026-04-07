package pluginctrl

// options 插件配置
type options struct {
	// superUsers 超级管理员 ID 列表（平台无关的用户 ID）
	superUsers []string
	// defaultEnabled 插件未设置时的默认开关状态（默认 true）
	defaultEnabled bool

	// 指令文本（均可自定义）
	enableCmd        string
	disableCmd       string
	listCmd          string
	globalEnableCmd  string
	globalDisableCmd string
}

func defaultOptions() *options {
	return &options{
		defaultEnabled:   true,
		enableCmd:        "开启",
		disableCmd:       "关闭",
		listCmd:          "服务列表",
		globalEnableCmd:  "全局开启",
		globalDisableCmd: "全局关闭",
	}
}

// Option 函数式选项。
type Option func(*options)

// WithSuperUsers 设置超级管理员用户 ID 列表。
// 超级管理员可以使用"全局开启/关闭"指令，且不受群权限限制。
func WithSuperUsers(userIDs ...string) Option {
	return func(o *options) { o.superUsers = userIDs }
}

// WithDefaultEnabled 设置插件未配置时的默认状态。
// 默认为 true（开启）。设为 false 时，新群需要显式开启插件才能使用。
func WithDefaultEnabled(enabled bool) Option {
	return func(o *options) { o.defaultEnabled = enabled }
}

// WithCommands 自定义管理指令文本。
//
//	pluginctrl.WithCommands("enable", "disable", "plugins", "global-enable", "global-disable")
func WithCommands(enable, disable, list, globalEnable, globalDisable string) Option {
	return func(o *options) {
		if enable != "" {
			o.enableCmd = enable
		}
		if disable != "" {
			o.disableCmd = disable
		}
		if list != "" {
			o.listCmd = list
		}
		if globalEnable != "" {
			o.globalEnableCmd = globalEnable
		}
		if globalDisable != "" {
			o.globalDisableCmd = globalDisable
		}
	}
}
