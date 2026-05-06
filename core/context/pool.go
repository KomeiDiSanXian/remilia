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

// ReleaseContext 归还 Context 到对象池（降低 refCount）。
//
// 等同于调用 ctx.Release()，保留此函数仅用于兼容已有调用点。
// 新代码应优先使用 ctx.Release()。
//
// IMPORTANT: The context must not be used after calling ReleaseContext.
func ReleaseContext(ctx *Context) {
	if ctx == nil {
		return
	}
	ctx.Release()
}
