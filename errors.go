package remilia

import "github.com/KomeiDiSanXian/remilia/errutil"

// 根包公开错误——通过别名指向 errutil，保持单一真相来源。
// 调用方可直接使用 remilia.ErrXxx 或 errutil.ErrXxx，两者指向同一实例，
// errors.Is 检查可跨包正常工作。
var (
	// ErrAdapterRequired indicates that an adapter is required to build a bot
	ErrAdapterRequired = errutil.ErrAdapterRequired

	// ErrEngineRequired indicates that an engine is required
	ErrEngineRequired = errutil.ErrEngineRequired

	// ErrBotInfoRequired indicates that bot info is required for certain operations
	ErrBotInfoRequired = errutil.ErrBotInfoRequired
)
