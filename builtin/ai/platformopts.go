package ai

import (
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// platformopts.go — 插件构造期的依赖注入点。
//
// 目前只有平台媒体来源一项：附件下载需要知道"平台自己托管的媒体服务在哪"，
// 而这只有适配器配置清楚（见 platform.MediaOriginProvider）。未注入时退化为
// "仅运维名单 + 严格公网"，功能不受影响，只是自建实现的内网媒体需要运维在
// allow_private_media_hosts 里显式声明。
//
// 注入的是 [platform.MediaOriginSource]（只有 MediaOrigins 一个只读方法），
// 不是 *platform.Registry：插件不需要、也不应该拿到能注册/注销适配器、
// 改事件分发的框架级句柄。
//
// 注意：本文件的 Option 是插件构造选项，与 options.go 里"配置 → 子包选项"
// 的映射函数无关。

// Option 构造插件时可注入的依赖。
type Option func(*Plugin)

// defaultDataDir 插件自有持久化数据的默认根目录。
//
// 与仓库里其他插件一致：默认值只在应用没有显式注入时使用（见 WithDataDir）。
const defaultDataDir = "data"

// WithMediaOriginSource 注入平台媒体来源：Setup 时据此读取各适配器自声明的
// 媒体来源，与 allow_private_media_hosts 合并为附件下载的受信来源。
func WithMediaOriginSource(src platform.MediaOriginSource) Option {
	return func(p *Plugin) { p.mediaSource = src }
}

// WithDataDir 指定插件自有持久化数据的根目录（默认 "data"）。
//
// 插件会在其下创建两个独立目录：ai（群策略）与 ai_memory（长期记忆）。
// 分开放是因为 LevelDB 同一进程只能打开一个句柄，共目录会导致锁冲突。
//
// 注入方式与仓库里其他带持久化的插件一致（acl/welcome/antispam 的
// WithStore(path)），由应用决定数据放哪；此前这里是硬编码的 "data"，
// 部署侧无法把插件数据挪到挂载卷或临时目录。
func WithDataDir(dir string) Option {
	return func(p *Plugin) { p.dataDir = dir }
}

// WithHistory 注入消息历史来源（默认 messagelog.Default()）。
//
// 群聊窗口、相关历史检索、回复上下文与"值不值得接话"的判定都读它。
// 显式注入的价值是：一个进程里可以有多个 messagelog 实例（测试、多租户），
// 也能让插件在自己不产生日志的部署里明确拿到"没有历史"而不是全局单例。
func WithHistory(h *messagelog.Logger) Option {
	return func(p *Plugin) { p.history = h }
}
