package pluginctrl

// ServiceListRenderer 服务列表图像渲染器函数类型。
//
//   - groupID       查询的群 ID
//   - states        已明确设置状态的插件列表（仅含有过显式操作的条目）
//   - defaultEnabled 未出现在 states 中的插件的默认开关状态
//
// 返回：图像字节流、MIME 类型（如 "image/png"）和错误。
// 返回 error 时 pluginctrl 自动降级为纯文本输出。
type ServiceListRenderer func(groupID string, states []GroupPluginState, defaultEnabled bool) (imgData []byte, mimeType string, err error)

// options 插件配置
type options struct {
	// superUsers 超级管理员 ID 列表（平台无关的用户 ID）
	superUsers []string
	// defaultEnabled 插件未设置时的默认开关状态（默认 true）
	defaultEnabled bool

	// 群级/全局 指令文本（均可自定义）
	enableCmd        string
	disableCmd       string
	listCmd          string
	globalEnableCmd  string
	globalDisableCmd string

	// 用户级 指令文本
	userDisableCmd string // 超级管理员禁用指定用户对某插件的访问
	userEnableCmd  string // 超级管理员解除禁用

	// 群整体静默/恢复响应 指令文本（超级管理员）
	silenceCmd string
	resumeCmd  string

	// 全局用户封禁/解封 指令文本（超级管理员）
	banCmd   string
	unbanCmd string

	// 翻转插件默认启用状态 指令文本（超级管理员）
	flipDefaultCmd string

	// 服务列表图像渲染器（可选；nil 时降级为文本输出）
	serviceListRenderer ServiceListRenderer
}

func defaultOptions() *options {
	return &options{
		defaultEnabled:   true,
		enableCmd:        "开启",
		disableCmd:       "关闭",
		listCmd:          "服务列表",
		globalEnableCmd:  "全局开启",
		globalDisableCmd: "全局关闭",
		userDisableCmd:   "禁用用户",
		userEnableCmd:    "启用用户",
		silenceCmd:       "沉默",
		resumeCmd:        "响应",
		banCmd:           "封禁",
		unbanCmd:         "解封",
		flipDefaultCmd:   "反转默认",
	}
}

// Option 函数式选项。
type Option func(*options)

// WithSuperUsers 设置超级管理员用户 ID 列表。
// 超级管理员可以使用"全局开启/关闭"和"禁用/启用用户"指令，且不受群权限限制。
func WithSuperUsers(userIDs ...string) Option {
	return func(o *options) { o.superUsers = userIDs }
}

// WithDefaultEnabled 设置插件未配置时的默认状态。
// 默认为 true（开启）。设为 false 时，新群需要显式开启插件才能使用。
func WithDefaultEnabled(enabled bool) Option {
	return func(o *options) { o.defaultEnabled = enabled }
}

// WithCommands 自定义群级和全局管理指令文本。
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

// WithUserCommands 自定义用户级禁用/启用指令文本（默认 "禁用用户" / "启用用户"）。
//
// 这两个指令仅超级管理员可用，用于屏蔽特定用户对某插件的访问。
//
//	pluginctrl.WithUserCommands("ban-user", "unban-user")
func WithUserCommands(disable, enable string) Option {
	return func(o *options) {
		if disable != "" {
			o.userDisableCmd = disable
		}
		if enable != "" {
			o.userEnableCmd = enable
		}
	}
}

// WithSilenceCommands 自定义群整体静默/恢复响应指令文本（默认 "沉默" / "响应"）。
//
// 这两个指令仅超级管理员可用。
// 静默后该群的所有插件均不响应，直到超级管理员发送恢复指令。
//
//	pluginctrl.WithSilenceCommands("silence", "resume")
func WithSilenceCommands(silence, resume string) Option {
	return func(o *options) {
		if silence != "" {
			o.silenceCmd = silence
		}
		if resume != "" {
			o.resumeCmd = resume
		}
	}
}

// WithBanCommands 自定义全局用户封禁/解封指令文本（默认 "封禁" / "解封"）。
//
// 这两个指令仅超级管理员可用。
// 全局封禁的用户无法使用机器人的任何功能，无论单个插件的状态如何。
//
//	pluginctrl.WithBanCommands("ban", "unban")
func WithBanCommands(ban, unban string) Option {
	return func(o *options) {
		if ban != "" {
			o.banCmd = ban
		}
		if unban != "" {
			o.unbanCmd = unban
		}
	}
}

// WithFlipDefaultCommand 自定义翻转插件默认启用状态的指令文本（默认 "反转默认"）。
//
// 该指令仅超级管理员可用，用于在运行时翻转特定插件的默认开关状态。
//
//	pluginctrl.WithFlipDefaultCommand("flip-default")
func WithFlipDefaultCommand(cmd string) Option {
	return func(o *options) {
		if cmd != "" {
			o.flipDefaultCmd = cmd
		}
	}
}

// WithServiceListRenderer 注入自定义服务列表图像渲染器。
//
// 注入后，"服务列表"指令优先以图像形式发送结果；渲染出错时自动降级为文本输出。
//
// 示例（使用 textimage 包）：
//
//	renderer, _ := textimage.New()
//	pluginctrl.WithServiceListRenderer(func(groupID string, states []pluginctrl.GroupPluginState, defEnabled bool) ([]byte, string, error) {
//	    var sb strings.Builder
//	    for _, s := range states {
//	        fmt.Fprintf(&sb, "%s: %v\n", s.PluginName, s.Enabled)
//	    }
//	    img, err := renderer.Render(sb.String())
//	    if err != nil { return nil, "", err }
//	    return encodeToBytes(img), "image/png", nil
//	})
func WithServiceListRenderer(r ServiceListRenderer) Option {
	return func(o *options) { o.serviceListRenderer = r }
}
