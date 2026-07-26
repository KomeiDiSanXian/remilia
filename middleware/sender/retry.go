package sender

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// Retry 包装 Sender，在失败时按指数退避策略重试。
//
// maxAttempts 包括首次尝试，所以 maxAttempts=3 表示首次 + 2 次重试。
// baseBackoff 是重试间隔的基准时间，每次重试翻倍（指数退避）。
//
// 装饰器顺序要求：Retry 应放在 Timeout 外层，确保每次重试都有独立的超时：
//
//	sender.Retry(3, 200*time.Millisecond)(sender.Timeout(30*time.Second)(qqSender))
func Retry(maxAttempts int, baseBackoff time.Duration) SenderDecorator {
	// maxAttempts<=0 时退回默认 3 次，避免循环一次都不执行导致"零发送却报成功(nil)"。
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return func(next platform.Sender) platform.Sender {
		return retrySender{
			next:        next,
			maxAttempts: maxAttempts,
			baseBackoff: baseBackoff,
		}
	}
}

type retrySender struct {
	next        platform.Sender
	maxAttempts int
	baseBackoff time.Duration
}

func (s retrySender) Send(ctx context.Context, req platform.SendRequest) (platform.SendResult, error) {
	var lastErr error
	for attempt := 0; attempt < s.maxAttempts; attempt++ {
		if attempt > 0 {
			// 指数退避等待（限制移位上界，避免 baseBackoff<<shift 溢出为负值）
			shift := min(attempt-1, 30)
			backoff := s.baseBackoff * (1 << shift)
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return platform.SendResult{}, ctx.Err()
			}
		}

		res, err := s.next.Send(ctx, req)
		if err == nil {
			return res, nil
		}
		lastErr = err
	}
	return platform.SendResult{}, lastErr
}
