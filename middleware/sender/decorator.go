// Package sender 提供 platform.Sender 的装饰器（中间件）实现。
//
// 装饰器采用函数式组合模式，通过 Chain 将多个装饰器串联为单个 Sender：
//
//	sender := sender.Chain(
//	    sender.Metrics("remilia"),
//	    sender.Logging(),
//	    sender.Retry(3, 200*time.Millisecond),
//	    sender.Timeout(30*time.Second),
//	)(qqSender)
//
// 装饰器执行顺序从外到内：Metrics → Logging → Retry → Timeout → PlatformSender。
// 即 Metrics 包裹所有，Timeout 紧贴平台调用。
package sender

import (
	"github.com/KomeiDiSanXian/remilia/platform"
)

// SenderDecorator 包装一个 platform.Sender 并返回增强版本。
// 多个装饰器可通过 Chain 组合。
type SenderDecorator func(platform.Sender) platform.Sender

// Chain 将多个装饰器组合为一个。装饰器按参数顺序从外到内包裹。
// 等价于 Chain(a, b, c)(s) == a(b(c(s)))。
func Chain(decorators ...SenderDecorator) SenderDecorator {
	return func(s platform.Sender) platform.Sender {
		for i := len(decorators) - 1; i >= 0; i-- {
			s = decorators[i](s)
		}
		return s
	}
}
