// Package bundle 提供内置插件的批量注册入口。
//
// 参考：一次性注册所有核心插件，无需逐一 import 和调用 Register。
//
// # 使用示例
//
//	// 注册全部核心插件（storage + cache + permission + acl + help）
//	pm.RegisterMultipleAtomic(bundle.Core())
//
//	// 注册所有内置插件
//	pm.RegisterMultipleAtomic(bundle.All())
//
//	// 自定义组合（Core + 可选插件）
//	pm.RegisterMultipleAtomic(append(bundle.Core(),
//	    cooldown.New(),
//	    antispam.New(antispam.Config{MaxCount: 5}),
//	))
package bundle

import (
	"github.com/KomeiDiSanXian/remilia/builtin/acl"
	"github.com/KomeiDiSanXian/remilia/builtin/cooldown"
	"github.com/KomeiDiSanXian/remilia/builtin/core/admin"
	"github.com/KomeiDiSanXian/remilia/builtin/core/cache"
	"github.com/KomeiDiSanXian/remilia/builtin/core/help"
	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/core/storage"
	"github.com/KomeiDiSanXian/remilia/builtin/dev/debug"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Core 返回核心插件集合（建议所有 Bot 使用）。
//
// 包含：
//   - storage  — 键值存储（内存后端，可替换）
//   - cache    — 基于 storage 的缓存层
//   - permission — 角色/权限管理
//   - acl      — 访问控制列表
//   - help     — /help 命令
//
// 插件已按依赖顺序排列，可直接传给 RegisterMultipleAtomic。
func Core() []*plugin.Descriptor {
	return []*plugin.Descriptor{
		storage.New(),
		cache.New(),
		permission.New(),
		acl.New(),
		help.New(),
	}
}

// All 返回所有可通过零配置使用的内置插件。
//
// 包含 Core() 的全部插件，以及：
//   - cooldown — 命令冷却时间控制
//
// 需要额外配置的插件（antispam、auditlog、broadcast 等）不包含在此集合中，
// 请手动 import 并调用对应的 New(cfg) 构造函数。
func All() []*plugin.Descriptor {
	return append(Core(),
		cooldown.New(),
	)
}

// Dev 返回开发/管理插件集合（适合调试环境使用）。
//
// 包含：
//   - admin — /plugin、/perm、/acl、/status 等管理命令（需要 permission 插件）
//   - debug — /debug 调试命令集（需要 permission 插件）
//
// 两个插件都依赖 permission，通常与 Core() 一起使用：
//
//	pm.RegisterMultipleAtomic(bundle.Core())
//	pm.RegisterMultipleAtomic(bundle.Dev())
//
// 或直接合并：
//
//	pm.RegisterMultipleAtomic(append(bundle.Core(), bundle.Dev()...))
func Dev() []*plugin.Descriptor {
	return []*plugin.Descriptor{
		admin.New(),
		debug.New(),
	}
}
