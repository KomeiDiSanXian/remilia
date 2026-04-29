package platform

import "github.com/KomeiDiSanXian/remilia/infra/logger"

// SafeDispatch 安全调用事件 handler，捕获并记录 panic 以防止崩溃。
//
// 所有平台适配器的 Start() 方法应使用此函数包装对 handler 的调用，
// 而非在每个适配器中重复实现相同的 defer-recover 逻辑。
//
// 使用示例：
//
//	platform.SafeDispatch(handler, event)
func SafeDispatch(handler func(Event), event Event) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[platform] panic in event handler: %v", r)
		}
	}()
	handler(event)
}
