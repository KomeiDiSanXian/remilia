package terminal

import (
	stdctx "context"
	"fmt"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// Sender implements platform.Sender for the terminal platform.
//
// It writes messages to stdout and records all sent messages for inspection.
type Sender struct {
	messages []*SentMessage
	msgCount atomic.Uint64
}

// NewSender creates a new terminal sender.
func NewSender() *Sender {
	return &Sender{}
}

// Send writes the message to stdout and records it.
func (s *Sender) Send(_ stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	content := extractMessageContent(req.Message)
	msgID := fmt.Sprintf("term-msg-%d", s.msgCount.Add(1)+1)

	sent := &SentMessage{
		ID:      msgID,
		Content: content,
		Target:  req.Target,
	}
	s.messages = append(s.messages, sent)

	// Output to stdout
	fmt.Printf("[Bot Reply] %s\n", content)

	return platform.SendResult{
		MessageID: msgID,
		Platform:  PlatformID,
	}, nil
}

// Messages returns a copy of all sent messages.
func (s *Sender) Messages() []*SentMessage {
	result := make([]*SentMessage, len(s.messages))
	copy(result, s.messages)
	return result
}

// LastMessage returns the most recently sent message, or nil if none.
func (s *Sender) LastMessage() *SentMessage {
	if len(s.messages) == 0 {
		return nil
	}
	return s.messages[len(s.messages)-1]
}

// Clear removes all recorded messages.
func (s *Sender) Clear() {
	s.messages = s.messages[:0]
}
