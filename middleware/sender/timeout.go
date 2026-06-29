package sender

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// Timeout 包装 Sender，为每次发送设置独立的超时。
//
// 与 context 超时的区别：Timeout 装饰器在每次 Send 调用前创建新的带超时的 context，
// 不影响外部 context。这使得 Retry(Timeout(Sender)) 组合中每次重试都有独立的超时。
//
// 装饰器顺序要求：Timeout 应作为最内层装饰器（紧贴 PlatformSender）：
//
//	sender.Chain(sender.Retry(3, 200*ms), sender.Timeout(30*s))(qqSender)
func Timeout(d time.Duration) SenderDecorator {
	return func(next platform.Sender) platform.Sender {
		return timeoutSender{next: next, timeout: d}
	}
}

type timeoutSender struct {
	next    platform.Sender
	timeout time.Duration
}

func (s timeoutSender) Send(ctx context.Context, req platform.SendRequest) (platform.SendResult, error) {
	sendCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.next.Send(sendCtx, req)
}
