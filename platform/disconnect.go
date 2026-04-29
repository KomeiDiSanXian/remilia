package platform

import "sync"

// DisconnectNotifier 是 RecoverableAdapter.OnDisconnect 的共享实现。
//
// 平台适配器嵌入此类型即可获得断连回调的注册与通知能力：
//
//	type MyAdapter struct {
//	    DisconnectNotifier
//	    // ...
//	}
//
// 嵌入后 adapter.OnDisconnect(fn) 和 adapter.NotifyDisconnect(err) 自动可用，
// 无需在每个适配器中重复实现。
type DisconnectNotifier struct {
	mu  sync.Mutex
	fns []func(error)
}

// OnDisconnect 注册断连回调，返回注销函数。参见 [RecoverableAdapter.OnDisconnect]。
func (n *DisconnectNotifier) OnDisconnect(fn func(error)) (unregister func()) {
	if fn == nil {
		return func() {}
	}
	n.mu.Lock()
	idx := len(n.fns)
	n.fns = append(n.fns, fn)
	n.mu.Unlock()
	return func() {
		n.mu.Lock()
		if idx < len(n.fns) {
			n.fns[idx] = nil
		}
		n.mu.Unlock()
	}
}

// NotifyDisconnect 通知所有已注册的断连回调。
//
// 适配器在意外断连时调用此方法；若无已注册回调则跳过（零分配）。
func (n *DisconnectNotifier) NotifyDisconnect(err error) {
	n.mu.Lock()
	if len(n.fns) == 0 {
		n.mu.Unlock()
		return
	}
	fns := make([]func(error), len(n.fns))
	copy(fns, n.fns)
	n.mu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn(err)
		}
	}
}
