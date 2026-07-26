package terminal

import (
	stdctx "context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// Sender implements platform.Sender for the terminal platform.
//
// It writes messages to stdout and records all sent messages for inspection.
type Sender struct {
	// mu 保护 messages。
	//
	// msgCount 是原子的，但它编号的这个切片此前完全没有保护：
	// 两个 goroutine 并发 Send 会在 append 上产生数据竞争（-race 必报），
	// 并可能丢失记录。Adapter 上等价的 messages 切片一直由 msgMu 保护，
	// 这里属于遗漏。
	mu       sync.Mutex
	messages []*SentMessage
	msgCount atomic.Uint64
}

// NewSender creates a new terminal sender.
func NewSender() *Sender {
	return &Sender{}
}

// Send writes the message to stdout and records it.
func (s *Sender) Send(_ stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if err := req.Validate(); err != nil {
		return platform.SendResult{}, err
	}

	content := extractMessageContent(req.Message)
	// atomic.Uint64.Add 返回的已是自增后的值，此前多加的 1 让首条消息
	// 编号为 "term-msg-2"，与 Adapter.Send 的编号规则对不上。
	msgID := fmt.Sprintf("term-msg-%d", s.msgCount.Add(1))

	sent := &SentMessage{
		ID:      msgID,
		Content: content,
		Target:  req.Target,
	}
	s.mu.Lock()
	s.messages = append(s.messages, sent)
	s.mu.Unlock()

	// 输出到 stdout（清洗控制序列，避免远端内容操纵操作员终端）
	fmt.Printf("[Bot Reply] %s\n", sanitizeForTerminal(content))

	return platform.SendResult{
		MessageID: msgID,
		Platform:  PlatformID,
	}, nil
}

// Messages returns a copy of all sent messages.
func (s *Sender) Messages() []*SentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*SentMessage, len(s.messages))
	copy(result, s.messages)
	return result
}

// LastMessage returns the most recently sent message, or nil if none.
func (s *Sender) LastMessage() *SentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return nil
	}
	return s.messages[len(s.messages)-1]
}

// Clear removes all recorded messages.
func (s *Sender) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = s.messages[:0]
}
