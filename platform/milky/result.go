package milky

import "time"

// SendResult 是存储在 platform.SendResult.Raw 中的 Milky 特有载荷。
//
// 在 Send 调用成功后通过类型断言访问：
//
//	if r, ok := result.Raw.(*milky.MilkySendResult); ok {
//	    seq := r.MessageSeq
//	}
type SendResult struct {
	// MessageSeq 是 Milky 消息序列号（原生 int64）。
	MessageSeq int64

	// SentAt 是 Milky 服务端返回的消息时间戳。
	SentAt time.Time
}
