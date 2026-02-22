package dto

import "sync"

// payloadPool 是 Payload 对象池，减少高频 webhook 场景的堆分配和 GC 压力。
//
// 改进 3.3：Payload 是 webhook 热路径上每个请求都创建一次的短生命周期对象。
// 在 unlimited 场景下 GC runs 达 3249 次/10s，约 1128ms pause。
// 引入 pool 后预期 GC runs 降低 50-70%。
var payloadPool = sync.Pool{
	New: func() any {
		return &Payload{}
	},
}

// AcquirePayload 从池中取出一个已清零的 Payload。
//
// 调用方使用完毕后必须调用 ReleasePayload 归还，否则起不到复用效果。
// 典型生命周期：
//
//  1. webhook.Handle 调用 AcquirePayload
//  2. json.Unmarshal 填充字段
//  3. 写入 eventChan，消费者读出后处理
//  4. bot.handleEvent / 处理函数结束后调用 ReleasePayload
func AcquirePayload() *Payload {
	p := payloadPool.Get().(*Payload)
	// 清零所有字段，防止上一次使用的数据泄漏
	p.ID = ""
	p.Operation = 0
	p.Detail = nil
	p.Sequence = 0
	p.Type = ""
	p.Raw = nil
	return p
}

// ReleasePayload 将 Payload 归还池中。
//
// 注意事项：
//   - 归还后不得再访问该 Payload（包括其 Detail/Raw 切片）
//   - Clone() 返回的副本不应归还（Clone 语义是独立生命周期）
//   - 如果 Payload 被传递给异步 goroutine，应先 Clone 再归还原对象
func ReleasePayload(p *Payload) {
	if p == nil {
		return
	}
	// 保留底层 slice 容量以便下次复用，只清空内容
	p.ID = ""
	p.Operation = 0
	if p.Detail != nil {
		p.Detail = p.Detail[:0]
	}
	p.Sequence = 0
	p.Type = ""
	if p.Raw != nil {
		p.Raw = p.Raw[:0]
	}
	payloadPool.Put(p)
}
