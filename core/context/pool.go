package context

import (
	stdctx "context"
	"sync"
)

// pool.go — Context 对象池

// contextPool is a sync.Pool for Context objects to reduce GC pressure
var contextPool = sync.Pool{
	New: func() any {
		return &Context{ctx: stdctx.Background()}
	},
}

// ReleaseContext returns a Context to the pool after clearing sensitive data
//
// IMPORTANT: The context must not be used after calling ReleaseContext.
// This is enforced by clearing all fields.
func ReleaseContext(ctx *Context) {
	if ctx == nil {
		return
	}

	ctx.matcher = nil

	// 平台无关字段清理
	ctx.platformEvent = nil
	ctx.platformSender = nil
	ctx.botID = ""

	if ctx.extensions != nil {
		ctx.extensions.Clear()
		ctx.extensions = nil
	}
	// 重置 extInitialized，否则池化复用时快路径会返回 nil *Extensions
	ctx.extInitialized.Store(false)

	// 调用 Clone() 中存储的 cancel 函数，释放 WithDeadline 创建的 runtime timer
	if ctx.cancel != nil {
		ctx.cancel()
		ctx.cancel = nil
	}

	// Clear content cache
	ctx.contentOnce = sync.Once{}
	ctx.content = ""

	ctx.ctxMu.Lock()
	ctx.ctx = stdctx.Background()
	ctx.ctxMu.Unlock()

	contextPool.Put(ctx)
}
