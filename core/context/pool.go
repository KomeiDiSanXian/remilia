package context

import (
	stdctx "context"
	"sync"
)

// pool.go — Context 对象池

// contextPool is a sync.Pool for Context objects to reduce GC pressure
var contextPool = sync.Pool{
	New: func() any {
		return &Context{}
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

	if ctx.extensions != nil {
		ctx.extensions.Clear()
		ctx.extensions = nil
	}

	// Clear content cache
	ctx.contentOnce = sync.Once{}
	ctx.content = ""

	ctx.ctxMu.Lock()
	ctx.ctx = stdctx.Background()
	ctx.ctxMu.Unlock()

	contextPool.Put(ctx)
}
